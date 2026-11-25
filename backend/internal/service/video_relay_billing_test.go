//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoRelayBillingCacheFixture struct {
	BillingCache
	balances     map[int64]float64
	windows      map[int64]float64
	deductions   int
	windowWrites int
}

func TestVideoRelayBillingUsesLockedSources(t *testing.T) {
	accountRate := 2.0
	sources := &VideoRelayBillingSources{
		User:    User{ID: 7},
		Key:     APIKey{ID: 11, UserID: 7, Quota: 9, RateLimit5h: 8},
		Account: Account{ID: 13, Type: AccountTypeAPIKey, RateMultiplier: &accountRate},
	}
	svc := NewVideoRelayService(nil, &OpenAIGatewayService{}, &APIKeyService{})
	task := &VideoRelayTask{UserID: 7, APIKeyID: 11, AccountID: 13, HoldAmount: 5, TotalCost: 3}
	params := svc.videoRelayBillingParams(task, sources)
	require.Same(t, &sources.User, params.User)
	require.Same(t, &sources.Key, params.APIKey)
	require.Same(t, &sources.Account, params.Account)
	require.Equal(t, 5.0, params.Cost.ActualCost)
	require.Equal(t, 3.0, params.Cost.TotalCost)
	require.Equal(t, accountRate, params.AccountRateMultiplier)
	command := buildUsageBillingCommand(task.UsageRequestID(), nil, params)
	require.Equal(t, 5.0, command.APIKeyQuotaCost)
	require.Equal(t, 5.0, command.APIKeyRateLimitCost)
}

func (cache *videoRelayBillingCacheFixture) InvalidateUserBalance(_ context.Context, userID int64) error {
	delete(cache.balances, userID)
	return nil
}

func (cache *videoRelayBillingCacheFixture) InvalidateAPIKeyRateLimit(_ context.Context, keyID int64) error {
	delete(cache.windows, keyID)
	return nil
}

func (cache *videoRelayBillingCacheFixture) DeductUserBalance(_ context.Context, userID int64, amount float64) error {
	cache.deductions++
	cache.balances[userID] -= amount
	return nil
}

func (cache *videoRelayBillingCacheFixture) UpdateAPIKeyRateLimitUsage(_ context.Context, keyID int64, amount float64) error {
	cache.windowWrites++
	cache.windows[keyID] += amount
	return nil
}

func TestVideoRelayInvalidatesBalanceAndWindowWithoutApplyingAnotherDebit(t *testing.T) {
	const taskID = "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const userID, keyID = int64(5), int64(7)
	cache := &videoRelayBillingCacheFixture{
		balances: map[int64]float64{userID: 10}, windows: map[int64]float64{keyID: 4},
	}
	store := &videoRelaySubmissionStore{task: VideoRelayTask{
		TaskID: taskID, UserID: userID, APIKeyID: keyID, HoldAmount: 2,
		Status: VideoRelayStatusCompleted, BillingState: VideoRelayCharged, UpstreamTaskID: "fixture-task",
	}}
	svc := NewVideoRelayService(store, &OpenAIGatewayService{billingCacheService: &BillingCacheService{cache: cache}}, nil)
	svc.invalidateFunds(context.Background(), &store.task, false)
	require.NotContains(t, cache.balances, userID, "a hold or refund must clear the cached wallet value")
	require.Equal(t, float64(4), cache.windows[keyID], "a hold does not change the quota window")

	cache.balances[userID] = 8
	svc.invalidateFunds(context.Background(), &store.task, true)
	require.NotContains(t, cache.balances, userID, "charge must reload the available balance from the database")
	require.NotContains(t, cache.windows, keyID, "charge must reload the authoritative quota window")

	cache.balances[userID], cache.windows[keyID] = 8, 6
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+taskID, nil)
	for range 2 {
		task, err := svc.Lookup(c, &APIKey{ID: keyID, UserID: userID}, taskID)
		require.Nil(t, err)
		require.Equal(t, VideoRelayCharged, task.BillingState)
	}
	require.Equal(t, float64(8), cache.balances[userID])
	require.Equal(t, float64(6), cache.windows[keyID])
	require.Zero(t, cache.deductions)
	require.Zero(t, cache.windowWrites)
}

func TestVideoRelayZeroHoldKeepsCustomerCaches(t *testing.T) {
	const userID, keyID = int64(5), int64(7)
	cache := &videoRelayBillingCacheFixture{
		balances: map[int64]float64{userID: 10},
		windows:  map[int64]float64{keyID: 4},
	}
	userKeys := &videoRelayBillingKeysFixture{}
	keys := &APIKeyService{apiKeyRepo: userKeys}
	svc := NewVideoRelayService(nil, &OpenAIGatewayService{
		billingCacheService: &BillingCacheService{cache: cache},
	}, keys)
	task := &VideoRelayTask{UserID: userID, APIKeyID: keyID, HoldAmount: 0, TotalCost: 3}
	for _, charged := range []bool{false, true} {
		svc.invalidateFunds(context.Background(), task, charged)
		require.Equal(t, float64(10), cache.balances[userID], "zero amount must leave the actual cached wallet intact")
		require.Equal(t, float64(4), cache.windows[keyID], "zero amount must leave the actual cached key window intact")
	}
	require.Zero(t, userKeys.reads, "no cache work needs a key inventory read")
	task.HoldAmount = 1
	svc.invalidateFunds(context.Background(), task, true)
	require.NotContains(t, cache.balances, userID)
	require.NotContains(t, cache.windows, keyID)
	require.Equal(t, 1, userKeys.reads)
}

type videoRelayBillingKeysFixture struct {
	APIKeyRepository
	reads int
}

func (r *videoRelayBillingKeysFixture) ListKeysByUserID(context.Context, int64) ([]string, error) {
	r.reads++
	return nil, nil
}
