//go:build unit

package handler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func wsMeteredErrorEvent(payload string) string { return "data: " + payload + "\n\n" }

func wsMeteredErrorStream(firstID string, usageEvent string) string {
	return wsMeteredErrorEvent(fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"object":"response","status":"in_progress"}}`, firstID)) +
		wsMeteredErrorEvent(`{"type":"response.output_text.delta","delta":"ordinary partial answer"}`) +
		usageEvent
}

func wsMeteredErrorUsage(id string) string {
	return wsMeteredErrorEvent(fmt.Sprintf(`{"type":"response.in_progress","response":{"id":%q,"usage":{"input_tokens":12,"output_tokens":3}}}`, id))
}

// An ordinary authorized generation ends early after the upstream reported
// actual token usage. The client must still see failure, but the one bill must be recorded.
func TestOpenAIResponsesWebSocket_BridgeMeteredErrorBillsOnce(t *testing.T) {
	for _, tc := range []struct {
		name, firstID, usage, billID string
	}{
		{"confirmed_response", "resp_metered", wsMeteredErrorUsage("resp_metered") + wsMeteredErrorUsage("resp_metered"), "resp_metered"},
		{"event_then_confirmed", "", wsMeteredErrorEvent(`{"type":"response.in_progress","id":"evt_before_usage"}`) + wsMeteredErrorUsage("resp_later"), "resp_later"},
		{"top_level_response", "", wsMeteredErrorEvent(`{"type":"response.in_progress","object":"response","id":"resp_top","usage":{"input_tokens":12,"output_tokens":3}}`), "resp_top"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, bills, cache := runWSMeteredErrorBilling(t, wsMeteredErrorStream(tc.firstID, tc.usage), true, nil)
			require.Len(t, got.upstreamPayloads, 1)
			require.NotEqual(t, coderws.StatusNormalClosure, got.closeStatus)
			require.True(t, wsMeteredErrorHasEvent(got.clientEvents, "response.output_text.delta"))
			require.False(t, wsMeteredErrorHasEvent(got.clientEvents, "response.completed"))
			require.Len(t, got.logs, 1)
			require.Equal(t, tc.billID, got.logs[0].RequestID)
			require.Equal(t, 12, got.logs[0].InputTokens)
			require.Equal(t, 3, got.logs[0].OutputTokens)
			require.InDelta(t, 150e-6, got.logs[0].ActualCost, 1e-12)
			require.Len(t, bills, 1)
			require.Equal(t, tc.billID, bills[0].RequestID)
			require.Equal(t, int64(1801), bills[0].APIKeyID)
			require.Equal(t, int64(1701), bills[0].UserID)
			require.Equal(t, 12, bills[0].InputTokens)
			require.Equal(t, 3, bills[0].OutputTokens)
			require.InDelta(t, got.logs[0].ActualCost, bills[0].BalanceCost, 1e-10)
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
			cache.mu.Lock()
			balance := cache.balance
			cache.mu.Unlock()
			require.InDelta(t, 100.0-150e-6, balance, 1e-10)
		})
	}
}

func wsMeteredErrorHasEvent(events [][]byte, want string) bool {
	for _, event := range events {
		if gjson.GetBytes(event, "type").String() == want {
			return true
		}
	}
	return false
}

type wsMeteredError403Probe struct{ resets atomic.Int32 }

func (p *wsMeteredError403Probe) IncrementOpenAI403Count(context.Context, int64, int) (int64, error) {
	return 1, nil
}
func (p *wsMeteredError403Probe) ResetOpenAI403Count(context.Context, int64) error {
	p.resets.Add(1)
	return nil
}

func runWSMeteredErrorBilling(t *testing.T, stream string, withRepo bool, worker *service.UsageRecordWorkerPool) (openAIResponsesWSUsageLogResult, []*service.UsageBillingCommand, *wsLogicalTurnReservationProbe) {
	t.Helper()
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.apiKey.User.Balance = 100
	apiKeySvc := newWSGroupPricingAPIKeyService(repo)
	bills := &compatStreamUsageBillingRepo{}
	var billingRepo service.UsageBillingRepository
	if withRepo {
		billingRepo = bills
	}
	cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
	health := &wsMeteredError403Probe{}
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsGroupPricingFirst,
		ingressMode:  service.OpenAIWSIngressModeHTTPBridge,
		standardMode: true, standardInflight: true,
		apiKeyService: apiKeySvc, apiKeyCredential: wsGroupPricingKey,
		standardBillingCache: cache, standardBillingRepo: billingRepo, standardWorker: worker, standard403Counter: health,
		bridgeStream: stream, bridgeErrorProbe: true,
	})
	bills.mu.Lock()
	commands := append([]*service.UsageBillingCommand(nil), bills.commands...)
	bills.mu.Unlock()
	require.Zero(t, health.resets.Load(), "abnormal metered text is not a successful health observation")
	return got, commands, cache
}

func TestOpenAIResponsesWebSocket_BridgeMeteredErrorRequiresUsageAndResponseIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		withRepo     bool
	}{
		{"no_usage", wsMeteredErrorStream("resp_nouse", ""), true},
		{"zero_usage", wsMeteredErrorStream("resp_zero", wsMeteredErrorEvent(`{"type":"response.in_progress","response":{"id":"resp_zero","usage":{"input_tokens":0,"output_tokens":0}}}`)), true},
		{"no_response_id", wsMeteredErrorStream("", wsMeteredErrorUsage("")), true},
		{"event_id_only", wsMeteredErrorStream("", wsMeteredErrorEvent(`{"type":"response.in_progress","id":"evt_usage","usage":{"input_tokens":12,"output_tokens":3}}`)), true},
		{"conflicting_response_ids", wsMeteredErrorStream("resp_one", wsMeteredErrorUsage("resp_two")), true},
		{"missing_billing_repo", wsMeteredErrorStream("resp_unavailable", wsMeteredErrorUsage("resp_unavailable")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, bills, cache := runWSMeteredErrorBilling(t, tc.stream, tc.withRepo, nil)
			require.Len(t, got.upstreamPayloads, 1)
			require.True(t, wsMeteredErrorHasEvent(got.clientEvents, "response.output_text.delta"))
			require.False(t, wsMeteredErrorHasEvent(got.clientEvents, "response.completed"))
			require.Empty(t, bills)
			require.Empty(t, got.logs)
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
			cache.mu.Lock()
			balance := cache.balance
			cache.mu.Unlock()
			require.InDelta(t, 100.0, balance, 1e-12)
		})
	}
}

func TestOpenAIResponsesWebSocket_BridgeMeteredErrorMandatoryBilling(t *testing.T) {
	worker := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount: 1, QueueSize: 1, TaskTimeout: time.Second, OverflowPolicy: "drop",
	})
	t.Cleanup(worker.Stop)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	require.Equal(t, service.UsageRecordSubmitModeEnqueued, worker.Submit(func(context.Context) { close(started); <-release }))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	require.Equal(t, service.UsageRecordSubmitModeEnqueued, worker.Submit(func(context.Context) {}))
	got, bills, cache := runWSMeteredErrorBilling(t, wsMeteredErrorStream("resp_queue", wsMeteredErrorUsage("resp_queue")), true, worker)
	require.Len(t, got.logs, 1)
	require.Len(t, bills, 1)
	require.Equal(t, "resp_queue", bills[0].RequestID)
	require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
}

func TestOpenAIResponsesWebSocket_BridgeMeteredErrorPreservesExistingBills(t *testing.T) {
	for _, test := range []struct {
		name, stream string
		wantCyber    bool
		wantImage    int
	}{
		{"image", wsMeteredErrorStream("resp_image", wsMeteredErrorUsage("resp_image")) +
			wsMeteredErrorEvent(`{"type":"response.output_item.done","item":{"id":"img_ordinary","type":"image_generation_call","status":"completed","result":"dGVzdA=="}}`), false, 1},
		{"policy", wsMeteredErrorStream("resp_policy", wsMeteredErrorUsage("resp_policy")) +
			wsMeteredErrorEvent(`{"type":"error","error":{"code":"cyber_policy","message":"ordinary synthetic policy response"},"usage":{"input_tokens":12,"output_tokens":3}}`), true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.apiKey.User.Balance = 100
			svc := newWSGroupPricingAPIKeyService(repo)
			billing := &compatStreamUsageBillingRepo{}
			cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
			health := &wsMeteredError403Probe{}
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsGroupPricingFirst,
				ingressMode:  service.OpenAIWSIngressModeHTTPBridge,
				standardMode: true, standardInflight: true,
				apiKeyService: svc, apiKeyCredential: wsGroupPricingKey,
				standardBillingCache: cache, standardBillingRepo: billing, standard403Counter: health,
				bridgeStream: test.stream, bridgeErrorProbe: true, bridgeExpectedLogs: 1,
			})
			require.Len(t, got.logs, 1)
			require.Equal(t, test.wantImage, got.logs[0].ImageCount)
			require.Equal(t, test.wantCyber, got.logs[0].RequestType == service.RequestTypeCyberBlocked)
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
			billing.mu.Lock()
			commands := append([]*service.UsageBillingCommand(nil), billing.commands...)
			billing.mu.Unlock()
			require.Len(t, commands, 1, "preexisting partial image/policy path bills only once")
			require.Equal(t, 12, commands[0].InputTokens)
			require.Equal(t, 3, commands[0].OutputTokens)
			require.Positive(t, commands[0].BalanceCost)
			require.Equal(t, int32(1), health.resets.Load(), "pre-existing recorded-result health handling is unchanged")
		})
	}
}
