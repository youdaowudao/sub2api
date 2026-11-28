//go:build unit

package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The positive-path regression verifies accounting for two authorized
// logical turns; both use valid authentication and sufficient funds.
type wsLogicalTurnRPMProbe struct {
	service.UserRPMCache
	count      atomic.Int32
	groupCount atomic.Int32
}

func (r *wsLogicalTurnRPMProbe) IncrementUserRPM(context.Context, int64) (int, error) {
	return int(r.count.Add(1)), nil
}
func (r *wsLogicalTurnRPMProbe) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	return int(r.groupCount.Add(1)), nil
}
func (s *wsGroupPricingAPIKeyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}

type wsLogicalTurnReservationProbe struct {
	*preOutputRecoveryBillingCache
	reserves atomic.Int32
}

func (r *wsLogicalTurnReservationProbe) ReserveInflightBalance(ctx context.Context, userID int64, id string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	r.reserves.Add(1)
	return r.preOutputRecoveryBillingCache.ReserveInflightBalance(ctx, userID, id, amount, balance, ttl)
}

func TestOpenAIResponsesWebSocket_StandardLogicalTurnAccounting(t *testing.T) {
	t.Run("counts", func(t *testing.T) {
		for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModeHTTPBridge} {
			t.Run(mode, func(t *testing.T) {
				repo := newWSGroupPricingAPIKeyRepoStub(1)
				repo.apiKey.User.Balance = 100
				repo.apiKey.User.RPMLimit = 100
				repo.group.RPMLimit = 100
				apiKeySvc := newWSGroupPricingAPIKeyService(repo)
				rpm := &wsLogicalTurnRPMProbe{}
				bills := &compatStreamUsageBillingRepo{}
				cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
					standardMode: true, standardInflight: true, apiKeyService: apiKeySvc, apiKeyCredential: wsGroupPricingKey,
					standardBillingCache: cache, standardRPMCache: rpm, standardBillingRepo: bills,
				})
				require.Len(t, got.logs, 2)
				require.Len(t, got.upstreamPayloads, 2)
				bills.mu.Lock()
				commands := append([]*service.UsageBillingCommand(nil), bills.commands...)
				bills.mu.Unlock()
				require.Len(t, commands, 2)
				require.NotEqual(t, commands[0].RequestID, commands[1].RequestID)
				for _, cmd := range commands {
					require.Equal(t, repo.apiKey.ID, cmd.APIKeyID)
					require.Equal(t, repo.apiKey.UserID, cmd.UserID)
					require.Equal(t, 2, cmd.InputTokens)
					require.Equal(t, 1, cmd.OutputTokens)
					require.InDelta(t, 40e-6, cmd.BalanceCost, 1e-10)
				}
				require.Equal(t, int32(2), rpm.count.Load(), "one accounting admission for each authorized generation")
				require.Equal(t, int32(2), rpm.groupCount.Load(), "one group RPM increment per authorized generation")
				require.Equal(t, 3, repo.lookupCount(), "one authentication lookup per generation plus the fixture pre-read")
				require.Equal(t, int32(2), cache.reserves.Load(), "one reservation for each authorized generation")
				require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
			})
		}
	})
}

func TestOpenAIResponsesWebSocket_StandardLogicalTurnRetryAccounting(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.apiKey.User.Balance = 100
	repo.apiKey.User.RPMLimit = 100
	repo.group.RPMLimit = 100
	svc := newWSGroupPricingAPIKeyService(repo)
	rpm := &wsLogicalTurnRPMProbe{}
	bills := &compatStreamUsageBillingRepo{}
	cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:  wsGroupPricingFirst,
		midPayload:    `{"type":"response.create","model":"gpt-5.6-terra","input":[{"role":"user","content":"summarize a poem"}],"stream":false}`,
		secondPayload: wsGroupPricingSecond,
		ingressMode:   service.OpenAIWSIngressModeHTTPBridge,
		standardMode:  true, standardInflight: true, standardBridgeRetryAt: 2,
		apiKeyService: svc, apiKeyCredential: wsGroupPricingKey,
		standardBillingCache: cache, standardRPMCache: rpm, standardBillingRepo: bills,
		channelMapping:     map[string]string{"gpt-5.6-terra": "gpt-5.6-sol"},
		billingModelSource: service.BillingModelSourceRequested,
		afterFirstUpstreamRequest: func(*service.ChannelService) error {
			return repo.adminUpdate(svc, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 2 })
		},
	})
	require.Len(t, got.upstreamPayloads, 4)
	for _, body := range got.upstreamPayloads {
		require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(body, "model").String())
	}
	require.Len(t, got.logs, 3)
	require.Equal(t, got.logs[0].AccountID+1, got.logs[1].AccountID)
	require.Equal(t, got.logs[1].AccountID, got.logs[2].AccountID)
	require.Equal(t, "gpt-5.6-terra", got.logs[1].RequestedModel)
	require.NotNil(t, got.logs[1].ModelMappingChain)
	require.Equal(t, "gpt-5.6-terra→gpt-5.6-sol", *got.logs[1].ModelMappingChain)
	require.InDelta(t, 1.0, got.logs[0].RateMultiplier, 1e-12)
	require.InDelta(t, 2.0, got.logs[1].RateMultiplier, 1e-12)
	require.InDelta(t, 2.0, got.logs[2].RateMultiplier, 1e-12)
	require.InDelta(t, 16e-6, got.logs[1].TotalCost, 1e-12)
	bills.mu.Lock()
	commands := append([]*service.UsageBillingCommand(nil), bills.commands...)
	bills.mu.Unlock()
	require.Len(t, commands, 3)
	for i, cmd := range commands {
		require.Equal(t, repo.apiKey.ID, cmd.APIKeyID)
		require.Equal(t, repo.apiKey.UserID, cmd.UserID)
		require.Equal(t, got.logs[i].AccountID, cmd.AccountID)
		require.Equal(t, 2, cmd.InputTokens)
		require.Equal(t, 1, cmd.OutputTokens)
		require.InDelta(t, got.logs[i].ActualCost, cmd.BalanceCost, 1e-10)
	}
	require.Equal(t, int32(3), rpm.count.Load(), "four attempts are three logical generations")
	require.Equal(t, int32(3), rpm.groupCount.Load(), "one group RPM increment per authorized generation")
	require.Equal(t, 4, repo.lookupCount(), "one authentication lookup per generation plus the fixture pre-read")
	require.Equal(t, int32(3), cache.reserves.Load())
	require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
}

// Pause one legitimate settlement after its task has taken the reservation
// reference. A free following generation must not acquire that older reference.
type wsLogicalTurnBarrierBillingRepo struct {
	compatStreamUsageBillingRepo
	started, proceed             chan struct{}
	secondStarted, secondProceed chan struct{}
}

func (r *wsLogicalTurnBarrierBillingRepo) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	started, proceed := r.started, r.proceed
	if cmd.RequestID == "resp_usage_e2e_2" {
		started, proceed = r.secondStarted, r.secondProceed
	}
	close(started)
	select {
	case <-proceed:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.compatStreamUsageBillingRepo.Apply(ctx, cmd)
}

func TestOpenAIResponsesWebSocket_StandardLogicalTurnAsyncReservation(t *testing.T) {
	for _, free := range []bool{false, true} {
		name := "paid"
		if free {
			name = "free"
		}
		t.Run(name, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.apiKey.User.Balance = 100
			repo.apiKey.User.RPMLimit = 100
			repo.group.RPMLimit = 100
			svc := newWSGroupPricingAPIKeyService(repo)
			cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
			rpm := &wsLogicalTurnRPMProbe{}
			bills := &wsLogicalTurnBarrierBillingRepo{started: make(chan struct{}), proceed: make(chan struct{}), secondStarted: make(chan struct{}), secondProceed: make(chan struct{})}
			worker := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 2, QueueSize: 8, TaskTimeout: 8 * time.Second})
			t.Cleanup(worker.Stop)
			var resumeOnce, resumeSecondOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(bills.proceed) }) }
			resumeSecond := func() { resumeSecondOnce.Do(func() { close(bills.secondProceed) }) }
			defer resume()
			defer resumeSecond()
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: `{"type":"response.create","model":"gpt-5.6-sol","input":[{"role":"user","content":"hello"}],"stream":false}`, secondPayload: wsGroupPricingSecond,
				ingressMode:  service.OpenAIWSIngressModeCtxPool,
				standardMode: true, standardInflight: true, apiKeyService: svc, apiKeyCredential: wsGroupPricingKey,
				standardBillingCache: cache, standardRPMCache: rpm, standardBillingRepo: bills, standardWorker: worker,
				afterCompleted: func(turn int) {
					if turn == 1 {
						select {
						case <-bills.started:
						case <-time.After(2 * time.Second):
							t.Fatal("first settlement did not start")
						}
						require.Equal(t, 1, cache.count(), "pending first settlement retains its reservation")
						if free {
							require.NoError(t, repo.adminUpdate(svc, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 0 }))
						}
						return
					}
					select {
					case <-bills.secondStarted:
					case <-time.After(2 * time.Second):
						t.Fatal("second settlement did not start")
					}
					wantReservations := 2
					if free {
						wantReservations = 1
					}
					require.Equal(t, wantReservations, cache.count(), "every paid pending settlement owns its reservation")
					require.Equal(t, int32(wantReservations), cache.reserves.Load())
					resume()
					require.Eventually(t, func() bool { return worker.Stats().CompletedTasks >= 1 && cache.count() == wantReservations-1 }, 2*time.Second, 10*time.Millisecond, "first reservation is released independently while next settlement remains pending")
					resumeSecond()
				},
			})
			require.Len(t, got.logs, 2)
			logs := map[string]*service.UsageLog{}
			for _, log := range got.logs {
				logs[log.RequestID] = log
			}
			require.NotNil(t, logs["resp_usage_e2e_1"])
			require.NotNil(t, logs["resp_usage_e2e_2"])
			require.InDelta(t, 40e-6, logs["resp_usage_e2e_1"].ActualCost, 1e-12)
			if free {
				require.Zero(t, logs["resp_usage_e2e_2"].ActualCost)
			} else {
				require.InDelta(t, 40e-6, logs["resp_usage_e2e_2"].ActualCost, 1e-12)
			}
			require.Equal(t, int32(2), rpm.count.Load())
			require.Equal(t, int32(2), rpm.groupCount.Load(), "one group RPM increment per authorized generation")
			require.Equal(t, 3, repo.lookupCount(), "one authentication lookup per generation plus the fixture pre-read")
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)

		})
	}
}

func TestOpenAIResponsesWebSocket_StandardFirstAndSameAccountRetryAccounting(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   int
		same bool
	}{
		{"first generation replacement", 1, false},
		{"later generation same account", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.apiKey.User.Balance = 100
			repo.apiKey.User.RPMLimit = 100
			repo.group.RPMLimit = 100
			svc := newWSGroupPricingAPIKeyService(repo)
			cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
			rpm := &wsLogicalTurnRPMProbe{}
			bills := &compatStreamUsageBillingRepo{}
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:  `{"type":"response.create","model":"gpt-5.6-sol","input":[{"role":"user","content":"hello"}],"stream":false}`,
				secondPayload: `{"type":"response.create","model":"gpt-5.6-sol","input":[{"role":"user","content":"summarize a poem"}],"stream":false}`,
				ingressMode:   service.OpenAIWSIngressModeHTTPBridge,
				standardMode:  true, standardInflight: true, standardBridgeRetryAt: tc.at, standardBridgeSameAccount: tc.same,
				apiKeyService: svc, apiKeyCredential: wsGroupPricingKey,
				standardBillingCache: cache, standardRPMCache: rpm, standardBillingRepo: bills,
			})
			require.Len(t, got.upstreamPayloads, 3)
			require.Len(t, got.logs, 2)
			require.Equal(t, got.logs[0].AccountID, got.logs[1].AccountID)
			bills.mu.Lock()
			count := len(bills.commands)
			bills.mu.Unlock()
			require.Equal(t, 2, count)
			require.Equal(t, int32(2), rpm.count.Load())
			require.Equal(t, int32(2), rpm.groupCount.Load(), "one group RPM increment per authorized generation")
			require.Equal(t, 3, repo.lookupCount(), "one authentication lookup per generation plus the fixture pre-read")
			require.Equal(t, int32(2), cache.reserves.Load())
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
		})
	}
}

// The subscription fixtures contain only active, positive-limit subscriptions.
type wsLogicalTurnSubscriptionRepo struct {
	service.UserSubscriptionRepository
	id      atomic.Int64
	lookups atomic.Int32
}

func (r *wsLogicalTurnSubscriptionRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	r.lookups.Add(1)
	now := time.Now()
	return &service.UserSubscription{
		ID: r.id.Load(), UserID: userID, GroupID: groupID,
		Status: service.SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
		DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now,
	}, nil
}

type wsLogicalTurnSubscriptionCache struct {
	*wsLogicalTurnReservationProbe
	reads atomic.Int32
}

func (c *wsLogicalTurnSubscriptionCache) GetSubscriptionCache(context.Context, int64, int64) (*service.SubscriptionCacheData, error) {
	c.reads.Add(1)
	return &service.SubscriptionCacheData{Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (c *wsLogicalTurnSubscriptionCache) UpdateSubscriptionUsage(context.Context, int64, int64, float64) error {
	return nil
}

func TestOpenAIResponsesWebSocket_StandardLogicalTurnSubscriptionSnapshot(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.apiKey.User.Balance = 100
	repo.apiKey.User.RPMLimit = 100
	repo.group.RPMLimit = 100
	repo.group.SubscriptionType = service.SubscriptionTypeSubscription
	svc := newWSGroupPricingAPIKeyService(repo)
	subRepo := &wsLogicalTurnSubscriptionRepo{}
	subRepo.id.Store(501)
	subSvc := service.NewSubscriptionService(nil, subRepo, nil, nil, &config.Config{})
	t.Cleanup(subSvc.Stop)
	rpm := &wsLogicalTurnRPMProbe{}
	bills := &compatStreamUsageBillingRepo{}
	cache := &wsLogicalTurnSubscriptionCache{wsLogicalTurnReservationProbe: &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}}
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond,
		ingressMode: service.OpenAIWSIngressModeCtxPool, standardMode: true, standardInflight: true,
		apiKeyService: svc, apiKeyCredential: wsGroupPricingKey,
		standardSubscriptionSvc: subSvc, standardBillingCache: cache, standardRPMCache: rpm, standardBillingRepo: bills,
		afterCompleted: func(turn int) {
			if turn == 1 {
				subRepo.id.Store(502)
			}
		},
	})
	require.Len(t, got.logs, 2)
	bills.mu.Lock()
	commands := append([]*service.UsageBillingCommand(nil), bills.commands...)
	bills.mu.Unlock()
	require.Len(t, commands, 2)
	for i, log := range got.logs {
		id := int64(501 + i)
		require.NotNil(t, log.GroupID)
		require.Equal(t, wsGroupPricingGroupID, *log.GroupID)
		require.NotNil(t, log.SubscriptionID)
		require.Equal(t, id, *log.SubscriptionID)
		require.Equal(t, service.BillingTypeSubscription, log.BillingType)
		require.NotNil(t, commands[i].SubscriptionID)
		require.Equal(t, id, *commands[i].SubscriptionID)
		require.Equal(t, service.BillingTypeSubscription, commands[i].BillingType)
		require.Zero(t, commands[i].BalanceCost)
		require.InDelta(t, 40e-6, commands[i].SubscriptionCost, 1e-10)
	}
	require.Equal(t, int32(2), subRepo.lookups.Load())
	require.Equal(t, int32(2), cache.reads.Load())
	require.Equal(t, int32(2), rpm.count.Load())
	require.Equal(t, int32(2), rpm.groupCount.Load())
	require.Equal(t, 3, repo.lookupCount())
	require.Zero(t, cache.reserves.Load())
}

type wsLogicalQuotaEvent struct {
	userID   int64
	platform string
	cost     float64
}
type wsLogicalQuotaRepo struct {
	service.UserPlatformQuotaRepository
	events []wsLogicalQuotaEvent
	mu     sync.Mutex
}

func (r *wsLogicalQuotaRepo) IncrementUsageWithReset(_ context.Context, userID int64, platform string, cost float64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, wsLogicalQuotaEvent{userID, platform, cost})
	return nil
}

type wsLogicalQuotaCache struct {
	*wsLogicalTurnReservationProbe
	mu            sync.Mutex
	reads, events []wsLogicalQuotaEvent
}

func (c *wsLogicalQuotaCache) GetUserPlatformQuotaCache(_ context.Context, userID int64, platform string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads = append(c.reads, wsLogicalQuotaEvent{userID: userID, platform: platform})
	now := time.Now()
	limit := 100.0
	return &service.UserPlatformQuotaCacheEntry{SchemaVersion: service.UserPlatformQuotaCacheSchemaV1, DailyLimitUSD: &limit, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}, true, nil
}
func (c *wsLogicalQuotaCache) SetUserPlatformQuotaCache(context.Context, int64, string, *service.UserPlatformQuotaCacheEntry, time.Duration) error {
	return nil
}
func (c *wsLogicalQuotaCache) IncrUserPlatformQuotaUsageCache(_ context.Context, userID int64, platform string, cost float64, _ time.Duration, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, wsLogicalQuotaEvent{userID, platform, cost})
	return nil
}

func TestOpenAIResponsesWebSocket_StandardLogicalTurnQuotaPlatform(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.apiKey.User.Balance = 100
	repo.apiKey.User.RPMLimit = 100
	repo.group.RPMLimit = 100
	svc := newWSGroupPricingAPIKeyService(repo)
	quota := &wsLogicalQuotaRepo{}
	cache := &wsLogicalQuotaCache{wsLogicalTurnReservationProbe: &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}}
	rpm := &wsLogicalTurnRPMProbe{}
	bills := &compatStreamUsageBillingRepo{}
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond,
		ingressMode: service.OpenAIWSIngressModeHTTPBridge, standardMode: true, standardInflight: true,
		apiKeyService: svc, apiKeyCredential: wsGroupPricingKey,
		standardQuotaRepo: quota, standardBillingCache: cache, standardRPMCache: rpm, standardBillingRepo: bills,
	})
	require.Len(t, got.logs, 2)
	quota.mu.Lock()
	events := append([]wsLogicalQuotaEvent(nil), quota.events...)
	quota.mu.Unlock()
	cache.mu.Lock()
	reads := append([]wsLogicalQuotaEvent(nil), cache.reads...)
	cacheEvents := append([]wsLogicalQuotaEvent(nil), cache.events...)
	cache.mu.Unlock()
	require.Len(t, events, 2)
	require.Equal(t, events, cacheEvents)
	for _, event := range events {
		require.Equal(t, repo.apiKey.UserID, event.userID)
		require.Equal(t, service.PlatformOpenAI, event.platform)
		require.InDelta(t, 40e-6, event.cost, 1e-12)
	}
	// Each turn performs eligibility and the existing settlement quota lookup.
	require.Len(t, reads, 4)
	for _, read := range reads {
		require.Equal(t, repo.apiKey.UserID, read.userID)
		require.Equal(t, service.PlatformOpenAI, read.platform)
	}
	require.Equal(t, int32(2), rpm.count.Load())
	require.Equal(t, int32(2), rpm.groupCount.Load())
	require.Equal(t, 3, repo.lookupCount())
	require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
}
