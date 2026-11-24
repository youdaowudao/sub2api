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

type videoRelayBillingKeyFixture struct {
	APIKeyRepository
	key *APIKey
	err error
}

func (r videoRelayBillingKeyFixture) GetByID(context.Context, int64) (*APIKey, error) {
	return r.key, r.err
}

type videoRelayBillingConcurrencyFixture struct {
	ConcurrencyCache
	reads int
}

func (cache *videoRelayBillingConcurrencyFixture) GetAPIKeyConcurrencyBatch(context.Context, []int64) (map[int64]int, error) {
	cache.reads++
	return nil, nil
}

type videoRelayBillingUserFixture struct {
	UserRepository
	user  *User
	reads int
}

func (r *videoRelayBillingUserFixture) GetByID(context.Context, int64) (*User, error) {
	r.reads++
	return r.user, nil
}

func TestVideoRelayBillingReusesUserFromKeyRead(t *testing.T) {
	user := &User{ID: 7}
	users := &videoRelayBillingUserFixture{user: user}
	key := &APIKey{ID: 11, UserID: user.ID, User: user}
	concurrent := &videoRelayBillingConcurrencyFixture{}
	keys := &APIKeyService{apiKeyRepo: videoRelayBillingKeyFixture{key: key},
		concurrencyService: NewConcurrencyService(concurrent)}
	svc := NewVideoRelayService(nil, &OpenAIGatewayService{
		userRepo: users, accountRepo: stubOpenAIAccountRepo{accounts: []Account{{ID: 13, Type: AccountTypeAPIKey}}},
	}, keys)
	task := &VideoRelayTask{UserID: user.ID, APIKeyID: key.ID, AccountID: 13, HoldAmount: 5}

	params, err := svc.videoRelayBillingParams(context.Background(), task)
	require.NoError(t, err)
	require.Same(t, user, params.User)
	require.Zero(t, users.reads, "the key lookup already loaded this user")
	require.Zero(t, concurrent.reads, "settlement does not need current displayed key concurrency")
	require.Equal(t, 5.0, params.Cost.ActualCost)

	keys.apiKeyRepo = videoRelayBillingKeyFixture{err: ErrAPIKeyNotFound}
	params, err = svc.videoRelayBillingParams(context.Background(), task)
	require.NoError(t, err)
	require.Same(t, user, params.User)
	require.Equal(t, task.APIKeyID, params.APIKey.ID)
	require.Equal(t, 1, users.reads, "the original user is still needed when the key was removed")
	require.Zero(t, concurrent.reads)
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
