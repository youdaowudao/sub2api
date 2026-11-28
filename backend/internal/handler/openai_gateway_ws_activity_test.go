//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesWebSocket_HTTPBridgeActiveTimeoutReleasesSlots(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		later, guardian, withUsage, noID bool
	}{
		{"first_metered", false, false, true, false},
		{"later_metered", true, false, true, false},
		{"first_without_usage", false, false, false, false},
		{"first_without_identity", false, false, true, true},
		{"guardian_metered", false, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.apiKey.User.Balance = 100
			svc := newWSGroupPricingAPIKeyService(repo)
			bills := &compatStreamUsageBillingRepo{}
			cache := &wsLogicalTurnReservationProbe{preOutputRecoveryBillingCache: &preOutputRecoveryBillingCache{newHandlerInflightCache(100)}}
			var users, accounts, leases atomic.Int32
			slots := &concurrencyCacheMock{
				acquireUserSlotFn:     func(context.Context, int64, int, string) (bool, error) { users.Add(1); return true, nil },
				acquireAccountSlotFn:  func(context.Context, int64, int, string) (bool, error) { accounts.Add(1); return true, nil },
				acquireIngressLeaseFn: func(context.Context, int64, int, string) (bool, error) { leases.Add(1); return true, nil },
			}
			headers := http.Header{}
			firstPayload := wsGroupPricingFirst
			var channelMapping map[string]string
			if tc.guardian {
				firstPayload = `{"type":"response.create","model":"codex-auto-review","input":"review the spelling","stream":false}`
				channelMapping = map[string]string{"codex-auto-review": "gpt-5.6-luna"}
				headers.Set("x-openai-subagent", "guardian")
				headers.Set("x-codex-turn-metadata", `{"parent_thread_id":"fixture-parent"}`)
			}
			turns := 1
			followup := ""
			if tc.later {
				turns = 2
				followup = wsGroupPricingSecond
			}
			upstreamClosed := make(chan struct{}, 4)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: firstPayload, secondPayload: followup,
				channelMapping: channelMapping, billingModelSource: service.BillingModelSourceChannelMapped,
				ingressMode: service.OpenAIWSIngressModeHTTPBridge, standardMode: true, standardInflight: true,
				apiKeyService: svc, apiKeyCredential: wsGroupPricingKey, standardBillingCache: cache, standardBillingRepo: bills,
				bridgeErrorProbe: true, bridgeProbeFollowup: tc.later, bridgeIdleSeconds: 1, bridgeGuardHeader: headers, bridgeConcurrency: slots,
				bridgeServe: func(w http.ResponseWriter, r *http.Request, attempt int32) {
					if tc.later && attempt == 1 {
						_, _ = io.WriteString(w, wsMeteredErrorEvent(`{"type":"response.completed","response":{"id":"resp_activity_ok","usage":{"input_tokens":2,"output_tokens":1}}}`))
						return
					}
					defer func() { upstreamClosed <- struct{}{} }()
					id := fmt.Sprintf("resp_activity_%d", attempt)
					if tc.noID {
						id = ""
					}
					prefix := wsMeteredErrorStream(id, "")
					if tc.withUsage {
						prefix += wsMeteredErrorUsage(id)
					}
					_, _ = io.WriteString(w, prefix)
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					timer := time.NewTimer(2500 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-r.Context().Done():
						return
					case <-timer.C:
						_, _ = io.WriteString(w, wsMeteredErrorEvent(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"usage":{"input_tokens":12,"output_tokens":3}}}`, id)))
					}
				},
			})
			require.Equal(t, coderws.StatusInternalError, got.closeStatus)
			require.Len(t, got.upstreamPayloads, turns, "no transparent replay")
			require.True(t, wsMeteredErrorHasEvent(got.clientEvents, "error"))
			completions := 0
			for _, event := range got.clientEvents {
				switch gjson.GetBytes(event, "type").String() {
				case "response.completed":
					completions++
				case "error":
					require.Equal(t, int64(504), gjson.GetBytes(event, "status").Int())
				}
			}
			require.Equal(t, turns-1, completions)
			require.Equal(t, int32(turns), users.Load())
			require.Equal(t, int32(turns), accounts.Load())
			require.Equal(t, int32(turns), atomic.LoadInt32(&slots.releaseUserCalled))
			require.Equal(t, int32(turns), atomic.LoadInt32(&slots.releaseAccountCalled))
			require.Equal(t, int32(1), leases.Load())
			require.Equal(t, int32(1), atomic.LoadInt32(&slots.releaseIngressCalled))
			select {
			case <-upstreamClosed:
			case <-time.After(time.Second):
				t.Fatal("upstream did not finish")
			}
			wantBills := turns - 1
			if tc.withUsage && !tc.noID {
				wantBills++
			}
			require.Len(t, got.logs, wantBills)
			bills.mu.Lock()
			commands := append([]*service.UsageBillingCommand(nil), bills.commands...)
			bills.mu.Unlock()
			require.Len(t, commands, wantBills)
			if tc.withUsage && !tc.noID {
				cmd := commands[len(commands)-1]
				require.Equal(t, fmt.Sprintf("resp_activity_%d", turns), cmd.RequestID)
				require.Equal(t, 12, cmd.InputTokens)
				require.Equal(t, 3, cmd.OutputTokens)
				wantCost := 150e-6
				if tc.guardian {
					wantCost = 6e-6
					require.Equal(t, "gpt-5.6-luna", gjson.GetBytes(got.upstreamPayloads[0], "model").String())
				}
				require.InDelta(t, wantCost, cmd.BalanceCost, 1e-10)
			}
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, 10*time.Millisecond)
		})
	}
}
