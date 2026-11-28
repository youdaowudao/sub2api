//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The real handler's normal handshake uses a shared ordinary request budget.
// No real upstream or external service is contacted by this fixture.
func wsBodyBudgetHandshake(t *testing.T, limit, budgetBytes int64) (*httptest.Server, *pkghttputil.BodyMemoryBudget, <-chan struct{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := newOpenAIHandlerForPreviousResponseIDValidation(t, nil)
	h.cfg = &config.Config{RunMode: config.RunModeSimple}
	h.cfg.Gateway.OpenAIWS.ClientReadLimitBytes = limit
	h.cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds = 30
	budget := pkghttputil.NewBodyMemoryBudget(budgetBytes)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit, budget))
	apiKey := &service.APIKey{ID: 101, User: &service.User{ID: 201, Status: service.StatusActive}}
	router.GET("/v1/responses", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		h.ResponsesWebSocket(c)
	})
	done := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)
		done <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	return srv, budget, done
}

func wsBodyBudgetDial(t *testing.T, srv *httptest.Server) (*coderws.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return coderws.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/responses", nil)
}

func wsBodyBudgetWaitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("websocket handler did not release its body scope")
	}
}

// Same old reservation interface as the measurement helper, parameterized
// for the ordinary small-budget correctness fixture.
func wsBodyBudgetUsedAtLimit(budget *pkghttputil.BodyMemoryBudget, limit int64) int64 {
	ctx, release := pkghttputil.WithRequestBodyMemoryBudget(context.Background(), budget)
	defer release()
	lo, hi := int64(0), limit
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if pkghttputil.ReserveRequestBodyMemory(ctx, mid) == nil {
			pkghttputil.ReleaseRequestBodyMemory(ctx, mid)
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return limit - lo
}

// RED: an ordinary successful handshake must reserve real reading capacity.
// The observation uses the pre-existing shared HTTP accounting interface.
func TestOpenAIResponsesWebSocket_BodyMemoryPreUpgradeCredit(t *testing.T) {
	const limit = int64(64 << 10)
	srv, budget, done := wsBodyBudgetHandshake(t, limit, wsMeasureB)
	conn, res, err := wsBodyBudgetDial(t, srv)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, res.StatusCode)
	defer conn.CloseNow()
	used := wsMeasureBudgetUsed(budget)
	_ = conn.CloseNow()
	wsBodyBudgetWaitDone(t, done)
	require.Zero(t, wsMeasureBudgetUsed(budget), "empty connection must return credit")
	require.GreaterOrEqual(t, used, 2*limit, "normal handshake has a reading reservation")
}

// RED: the declared limit is the real configured maximum, not the current
// available budget. The test uses a normal successful handshake only.
func TestOpenAIResponsesWebSocket_AdvertisesClientMessageLimit(t *testing.T) {
	for _, limit := range []int64{32 << 10, 64 << 10} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			srv, _, done := wsBodyBudgetHandshake(t, limit, wsMeasureB)
			conn, res, err := wsBodyBudgetDial(t, srv)
			require.NoError(t, err)
			defer conn.CloseNow()
			got := res.Header.Get("x-codex-websocket-max-message-bytes")
			_ = conn.CloseNow()
			wsBodyBudgetWaitDone(t, done)
			require.Equal(t, strconv.FormatInt(limit, 10), got)
		})
	}
}

// RED: two ordinary successful turns retain complete replay and return the
// shared body budget after the client closes. The fixture never fills capacity.
func TestOpenAIResponsesWebSocket_BodyBudgetOwnershipAndRefund(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const budgetBytes = int64(8 << 20)
	const limit = int64(64 << 10)
	budget := pkghttputil.NewBodyMemoryBudget(budgetBytes)
	fixtureCache := &wsMeasureCache{ctx: ctx, commits: make(chan chan struct{})}
	upstream := &wsMeasureUpstream{ctx: ctx, observed: make(chan wsMeasureUpstreamObservation)}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.RequestBodyMemoryBudgetBytes = budgetBytes
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.ClientReadLimitBytes = limit
	cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 30
	groupID := int64(4201)
	account := service.Account{ID: 9901, Name: "memory-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture-only", "base_url": "https://example.invalid"}, Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}}
	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billingCache.Stop()
	cache := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }}
	concurrency := service.NewConcurrencyService(cache)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
	gateway := service.NewOpenAIGatewayService(accountRepo, usageRepo, nil, nil, nil, nil, fixtureCache, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	defer gateway.CloseOpenAIWSPool()
	handler := &OpenAIGatewayHandler{cfg: cfg, gatewayService: gateway, billingCacheService: billingCache, apiKeyService: &service.APIKeyService{}, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Second)}
	apiKey := &service.APIKey{ID: 1801, GroupID: &groupID, User: &service.User{ID: 1701, Status: service.StatusActive}}
	done := make(chan struct{}, 1)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit, budget))
	router.GET("/v1/responses", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		handler.ResponsesWebSocket(c)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)
		done <- struct{}{}
	}))
	defer func() { cancel(); server.Close() }()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	first := wsMeasureItem{text: wsMeasureText{'a', 24 << 10}}
	second := wsMeasureItem{text: wsMeasureText{'b', 24 << 10}}
	var used [2]int64
	for i, item := range []wsMeasureItem{first, second} {
		previous := ""
		expected := []wsMeasureItem{first}
		if i == 1 {
			previous = wsMeasureID(1)
			expected = append(expected, wsMeasureOutput(1), second)
		}
		require.NoError(t, conn.Write(ctx, coderws.MessageText, wsMeasureFrame([]wsMeasureItem{item}, previous)))
		var obs wsMeasureUpstreamObservation
		select {
		case obs = <-upstream.observed:
		case <-ctx.Done():
			t.Fatal("ordinary upstream turn did not arrive")
		}
		wantLength, wantHash := wsMeasureExpectedBody(expected)
		require.Equal(t, wantLength, obs.length)
		require.Equal(t, wantHash, obs.sum)
		close(obs.proceed)
		for {
			_, event, err := conn.Read(ctx)
			require.NoError(t, err)
			if gjson.GetBytes(event, "type").String() == "response.completed" {
				require.Equal(t, wsMeasureID(i+1), gjson.GetBytes(event, "response.id").String())
				break
			}
		}
		select {
		case proceed := <-fixtureCache.commits:
			used[i] = wsBodyBudgetUsedAtLimit(budget, budgetBytes)
			close(proceed)
		case <-ctx.Done():
			t.Fatal("history commit missing")
		}
	}
	require.NoError(t, conn.CloseNow())
	wsBodyBudgetWaitDone(t, done)
	require.Zero(t, wsBodyBudgetUsedAtLimit(budget, budgetBytes))
	require.Positive(t, used[0], "committed history holds a reservation")
	require.Greater(t, used[1], used[0], "new unique history is accounted")
}

// A configuration whose allowed WS share cannot hold one reading credit
// must decline the upgrade without requesting any client body.
func TestOpenAIResponsesWebSocket_ConfiguredBodyBudgetFallback(t *testing.T) {
	const limit = int64(64 << 10)
	srv, budget, done := wsBodyBudgetHandshake(t, limit, limit)
	conn, res, err := wsBodyBudgetDial(t, srv)
	if conn != nil {
		defer conn.CloseNow()
	}
	require.Error(t, err)
	require.NotNil(t, res)
	require.Equal(t, http.StatusUpgradeRequired, res.StatusCode)
	require.Empty(t, res.Header.Get("x-codex-websocket-max-message-bytes"))
	wsBodyBudgetWaitDone(t, done)
	require.Zero(t, wsBodyBudgetUsedAtLimit(budget, limit))
}

func TestOpenAIResponsesWebSocket_DisabledBodyBudgetPreservesHandshake(t *testing.T) {
	const limit = int64(64 << 10)
	srv, _, done := wsBodyBudgetHandshake(t, limit, 0)
	conn, res, err := wsBodyBudgetDial(t, srv)
	require.NoError(t, err)
	defer conn.CloseNow()
	require.Equal(t, strconv.FormatInt(limit, 10), res.Header.Get("x-codex-websocket-max-message-bytes"))
	_ = conn.CloseNow()
	wsBodyBudgetWaitDone(t, done)
}
