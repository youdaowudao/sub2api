//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCompactionTriggerInputOrder_WSBodyBudget(t *testing.T) {
	const total = int64(1 << 20)
	availableBytes := func(t *testing.T, ctx context.Context) int64 {
		t.Helper()
		var available int64
		for step := total; step > 0; step /= 2 {
			owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, available+step)
			if err != nil {
				require.ErrorIs(t, err, pkghttputil.ErrRequestBodyMemoryBudget)
				continue
			}
			available += step
			owner.Drop()
		}
		return available
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{"reorder", `{"input":[{"type":"compaction_trigger"},{"type":"message","sequence":9007199254740993},{"type":"compaction_trigger"}]}`},
		{"duplicate_keys", `{"input":[{"type":"compaction_trigger"},{"type":"message","sequence":9007199254740993}],"metadata":{"x":1,"x":2}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, exhausted := range []bool{false, true} {
				name := "available"
				if exhausted {
					name = "exhausted"
				}
				t.Run(name, func(t *testing.T) {
					httpCtx, releaseHTTP := pkghttputil.WithRequestBodyMemoryBudget(context.Background(), pkghttputil.NewBodyMemoryBudget(total))
					defer releaseHTTP()
					ctx, releaseWS, err := pkghttputil.BeginWSRequestBodyMemory(httpCtx, 64)
					require.NoError(t, err)
					defer releaseWS()
					before := availableBytes(t, ctx)
					var blocker *pkghttputil.WSBodyMemoryOwner
					if exhausted {
						blocker, err = pkghttputil.ReserveWSBodyMemoryOwner(ctx, before)
						require.NoError(t, err)
						defer func() {
							if blocker != nil {
								blocker.Drop()
							}
						}()
					}
					materializer := newWSMaterializer(ctx)
					require.NotNil(t, materializer)
					defer materializer.close()
					body := []byte(tc.body)
					out, changed, err := NormalizeCompactionTriggerInputOrder(body, materializer)
					if exhausted {
						require.ErrorIs(t, err, pkghttputil.ErrRequestBodyMemoryBudget)
						require.False(t, changed)
						require.Equal(t, body, out)
					} else {
						require.NoError(t, err)
						require.True(t, changed)
						require.Equal(t, int64(2), gjson.GetBytes(out, "input.#").Int())
						require.Equal(t, "9007199254740993", gjson.GetBytes(out, "input.0.sequence").Raw)
						require.Equal(t, "compaction_trigger", gjson.GetBytes(out, "input.1.type").String())
						if tc.name == "duplicate_keys" {
							require.Equal(t, int64(2), gjson.GetBytes(out, "metadata.x").Int())
						}
						require.Less(t, availableBytes(t, ctx), before)
					}
					require.Equal(t, tc.body, string(body), "normalization must preserve the caller's input")
					materializer.close()
					if blocker != nil {
						blocker.Drop()
						blocker = nil
					}
					require.Equal(t, before, availableBytes(t, ctx), "normalization must return all of its reservations")
				})
			}
		})
	}
}

func TestIsOpenAINativeCompactionV2ReadsOnlyRequestMarker(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	require.False(t, IsOpenAINativeCompactionV2(c))

	MarkOpenAINativeCompactionV2(c)
	require.True(t, IsOpenAINativeCompactionV2(c))
	require.False(t, IsOpenAINativeCompactionV2(nil))
}

func TestHasCompactionTriggerInInput_DetectsCompactSignal(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"stream":true,
		"input":[
			{"type":"message","role":"user","content":"hello"},
			{"type":"compaction_trigger"}
		]
	}`)
	require.True(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_NoTrigger(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{"type":"message","role":"user","content":"hello"}
		]
	}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_EmptyInput(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[]}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_NoInputField(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5"}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_EmptyBody(t *testing.T) {
	require.False(t, HasCompactionTriggerInInput(nil))
	require.False(t, HasCompactionTriggerInInput([]byte{}))
}

func TestHasCompactionTriggerInInput_StringInput(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":"compaction_trigger"}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_CompactTriggerOnly(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	require.True(t, HasCompactionTriggerInInput(body))
}

func TestNormalizeCompactionTriggerInputOrder_MovesAndCollapsesTriggers(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"},{"type":"message","role":"user","content":"tail"},{"type":"compaction_trigger"}]}`)
	normalized, changed, err := NormalizeCompactionTriggerInputOrder(body)
	require.NoError(t, err)
	require.True(t, changed)
	items := gjson.GetBytes(normalized, "input").Array()
	require.Len(t, items, 2)
	require.Equal(t, "message", items[0].Get("type").String())
	require.Equal(t, "compaction_trigger", items[1].Get("type").String())
}

func TestNormalizeCompactionTriggerInputOrder_AlreadyFinalPreservesBytes(t *testing.T) {
	body := []byte(`{"input":[{"type":"message"},{"type":"compaction_trigger"}]}`)
	normalized, changed, err := NormalizeCompactionTriggerInputOrder(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, string(body), string(normalized))
}

func TestNormalizeCompactionTriggerInputOrder_PreservesHistoryAndLargeNumbers(t *testing.T) {
	body := []byte(`{"input":[{"type":"compaction_trigger"},{"type":"message","id":"msg_1","content":"visible"},{"type":"function_call_output","call_id":"call_1","output":"result","sequence":9007199254740993}]}`)

	normalized, changed, err := NormalizeCompactionTriggerInputOrder(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(normalized, "input.0.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(normalized, "input.1.type").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(normalized, "input.1.sequence").Raw)
	require.Equal(t, "compaction_trigger", gjson.GetBytes(normalized, "input.2.type").String())

	second, changedAgain, err := NormalizeCompactionTriggerInputOrder(normalized)
	require.NoError(t, err)
	require.False(t, changedAgain)
	require.Equal(t, normalized, second)
}

func TestWebSocketCompatibilityNormalizesTriggerAfterPairedOutputCleanup(t *testing.T) {
	body := []byte(`{"type":"response.create","model":"gpt-5.4","input":[{"type":"compaction_trigger"},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"},{"type":"message","role":"user","content":"visible"}]}`)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	normalized, changed, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
	require.NoError(t, err)
	require.True(t, changed)
	items := gjson.GetBytes(normalized, "input").Array()
	require.Len(t, items, 4)
	require.Equal(t, "function_call", items[0].Get("type").String())
	require.Equal(t, "function_call_output", items[1].Get("type").String())
	require.Equal(t, "message", items[2].Get("type").String())
	require.Equal(t, "compaction_trigger", items[3].Get("type").String())
}

func TestNormalizeCompactionTriggerInputOrder_PrecheckKeepsContract(t *testing.T) {
	plain := []byte(`{"model":"gpt-5","input":[{"type":"message","role":"user","content":"` + strings.Repeat("x", 4096) + `"}]}`)
	allocs := testing.AllocsPerRun(10, func() {
		got, changed, err := NormalizeCompactionTriggerInputOrder(plain)
		if err != nil || changed || &got[0] != &plain[0] {
			t.Fatal("body without a trigger must be returned unchanged")
		}
	})
	require.Zero(t, allocs)

	for _, invalid := range []string{`{"input":[`, `[{"type":"message"}]`, `{"input":[]} {}`} {
		_, _, err := NormalizeCompactionTriggerInputOrder([]byte(invalid))
		require.Error(t, err, invalid)
	}

	escaped := []byte(`{"input":[{"type":"\u0063ompaction_trigger"},{"type":"message","role":"user","content":"hi"}]}`)
	normalized, changed, err := NormalizeCompactionTriggerInputOrder(escaped)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "compaction_trigger", gjson.GetBytes(normalized, "input.1.type").String())
}
