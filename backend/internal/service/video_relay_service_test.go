//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayPriceUsesFullDurationAndGroupPrice(t *testing.T) {
	secondsPrice, fallback := 0.2, 0.3
	group := &Group{
		ID: 30, Platform: PlatformOpenAI, RateMultiplier: 2,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"fixture-video"}, BillingMode: BillingModeVideo, PerRequestPrice: &fallback,
			Intervals: []PricingInterval{{TierLabel: "480p", PerRequestPrice: &secondsPrice}},
		}},
	}
	billing := &BillingService{}
	gateway := &OpenAIGatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	key := &APIKey{ID: 100, UserID: 1, Group: group, GroupID: &group.ID}
	cost, multiplier, err := gateway.videoRelayPrice(context.Background(), key, "fixture-video", 30, "480p")
	require.Nil(t, err)
	require.InDelta(t, 6.0, cost.TotalCost, 1e-8)
	require.InDelta(t, 12.0, cost.ActualCost, 1e-8)
	require.Equal(t, 2.0, multiplier)
	fallbackCost, _, err := gateway.videoRelayPrice(context.Background(), key, "fixture-video", 30, "720p")
	require.Nil(t, err)
	require.InDelta(t, 18.0, fallbackCost.ActualCost, 1e-8)
	group.ModelPricing[0].PerRequestPrice = nil
	_, _, err = gateway.videoRelayPrice(context.Background(), key, "fixture-video", 30, "720p")
	require.NotNil(t, err)

	group.VideoRateIndependent = true
	group.VideoRateMultiplier = 0
	free, multiplier, err := gateway.videoRelayPrice(context.Background(), key, "fixture-video", 30, "480p")
	require.Nil(t, err)
	require.Zero(t, multiplier)
	require.Zero(t, free.ActualCost)
	require.InDelta(t, 6.0, free.TotalCost, 1e-8)
}

func TestVideoRelayErrorRuleKeepsSafeMessageAndValidStatus(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, test := range []struct {
		status int
		valid  bool
	}{
		{200, false}, {399, false}, {400, true}, {599, true}, {600, false},
	} {
		for _, step := range []struct {
			upstream int
			message  string
		}{{429, videoRelayRejectedMessage}, {503, videoRelayDownloadMessage}} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			message := "private fixture-task at https://fixture.test/secret"
			rules := &ErrorPassthroughService{}
			rules.setLocalCache([]*model.ErrorPassthroughRule{{
				Enabled: true, Platforms: []string{PlatformOpenAI}, ErrorCodes: []int{step.upstream},
				MatchMode: model.MatchModeAny, ResponseCode: &test.status, CustomMessage: &message, SkipMonitoring: true,
			}})
			BindErrorPassthroughService(c, rules)
			failure := videoRelayUpstreamError(c, account, step.upstream, []byte(message), step.message)
			require.Equal(t, step.message, failure.Message)
			require.True(t, c.GetBool(OpsSkipPassthroughKey))
			if test.valid {
				require.Equal(t, test.status, failure.Status)
				require.Zero(t, failure.UpstreamStatus)
			} else {
				require.Equal(t, step.upstream, failure.UpstreamStatus, "handler must map the upstream status")
			}
		}
	}
}

func TestVideoRelayRouteFingerprintTiesRequestsToOriginalConfiguration(t *testing.T) {
	cfg := &config.Config{}
	gateway := &OpenAIGatewayService{cfg: cfg}
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://FIXTURE.test:443/provider/v1/", "api_key": "local-original",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides:       map[string]any{"X-Partner": "original", "Idempotency-Key": "unused"},
	}}
	bound, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	require.Equal(t, "https://fixture.test/provider/v1/videos", bound.collection.String())
	require.Equal(t, "https://fixture.test/provider/api/log/token", bound.logURL)
	req, err := bound.request(context.Background(), http.MethodGet, bound.collection.String(), nil)
	require.NoError(t, err)
	require.Empty(t, req.Header.Get("Idempotency-Key"))
	require.Equal(t, []string{"original"}, req.Header["x-partner"])
	task := &VideoRelayTask{SiteFingerprint: bound.site, CredentialFingerprint: bound.credential, RouteFingerprint: bound.fingerprint}
	account.Credentials["base_url"] = "https://fixture.test/provider"
	account.Schedulable = false
	proxyID := int64(22)
	account.ProxyID, account.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 2080}
	compatible, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	require.True(t, compatible.matches(task))
	require.Equal(t, "http://127.0.0.1:2080", compatible.proxyURL)
	for _, changed := range []struct {
		key   string
		value any
	}{
		{"base_url", "https://fixture.test/different-provider"},
		{"api_key", "local-rotated"},
		{credKeyHeaderOverrides, map[string]any{"x-partner": "modified"}},
	} {
		original := account.Credentials[changed.key]
		account.Credentials[changed.key] = changed.value
		current, err := gateway.videoRelayRoute(account)
		require.NoError(t, err)
		require.False(t, current.matches(task), changed.key)
		account.Credentials[changed.key] = original
	}
}

func TestVideoRelayRouteUsesEffectiveBearerAndDropsIdempotencyOverrides(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://fixture.test", "api_key": "synthetic-video-key",
	}}
	original, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	account.Credentials["api_key"] = "  synthetic-video-key \t"
	account.Credentials[credKeyHeaderOverrideEnabled] = true
	account.Credentials[credKeyHeaderOverrides] = map[string]any{
		"Authorization": "synthetic-override", "idempotency-key": "first", "X-IDEMPOTENCY-KEY": "second",
	}
	route, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	req, err := route.request(context.Background(), http.MethodPost, route.collection.String(), strings.NewReader("{}"))
	require.NoError(t, err)
	t.Run("effective bearer", func(t *testing.T) {
		require.True(t, req.Header.Get("Authorization") == "Bearer synthetic-video-key", "send the normalized account credential")
		require.Equal(t, original.credential, route.credential)
	})
	t.Run("excluded overrides", func(t *testing.T) {
		for name := range req.Header {
			require.False(t, strings.EqualFold(name, "Idempotency-Key") || strings.EqualFold(name, "X-Idempotency-Key"), name)
		}
	})
}

func TestVideoRelayRequestsReportIdentityPerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	received := make(chan http.Header, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received <- req.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": upstream.URL, "api_key": "fixture-video-key",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides: map[string]any{
			"x-end-user": "{{user_id}}", "x-key": "{{api_key_id}}", "x-group": "{{group_id}}",
			"x-trace": "{{request_id}}", "x-combined": "private-fixture-{{user_id}}", "x-static": "constant",
			"accept": "{{user_id}}", "idempotency-key": "{{request_id}}", "x-idempotency-key": "{{request_id}}",
		},
	}}
	gateway := &OpenAIGatewayService{cfg: cfg}
	route, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	task := &VideoRelayTask{UpstreamModel: "fixture-video", Seconds: 5, Resolution: "480p"}
	for _, identity := range []struct {
		userID, keyID, groupID             int64
		requestID, user, key, group, trace string
	}{
		{7, 31, 9, "req-a;unsafe", "7", "31", "9", "req-a_unsafe"},
		{8, 32, 10, "req-b", "8", "32", "10", "req-b"},
	} {
		ctx := context.WithValue(context.Background(), ctxkey.RequestID, identity.requestID)
		ctx = WithHeaderOverrideRequestScope(ctx, identity.userID, identity.keyID, &identity.groupID)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/videos/owned", nil)
		for _, operation := range []string{"submit", "status", "download", "charges"} {
			t.Run(identity.requestID+"/"+operation, func(t *testing.T) {
				if operation == "charges" {
					_, err = route.queryCharges(ctx, videoRelayTestTransport{client: upstream.Client()})
					require.NoError(t, err)
				} else {
					var req *http.Request
					if operation == "submit" {
						req, err = route.submitRequest(ctx, VideoRelayRequest{Prompt: "Waves"}, task)
					} else {
						req, err = route.taskRequest(ctx, "provider-fixture", operation == "download")
					}
					require.NoError(t, err)
					resp, sendErr := upstream.Client().Do(req)
					require.NoError(t, sendErr)
					require.NoError(t, resp.Body.Close())
				}
				headers := <-received
				require.Equal(t, identity.user, headers.Get("X-End-User"), "actual upstream user identity")
				require.Equal(t, identity.key, headers.Get("X-Key"))
				require.Equal(t, identity.group, headers.Get("X-Group"))
				require.Equal(t, identity.trace, headers.Get("X-Trace"))
				require.Equal(t, "private-fixture-"+identity.user, headers.Get("X-Combined"))
				require.Equal(t, "constant", headers.Get("X-Static"))
				require.Equal(t, "application/json", headers.Get("Accept"))
				require.Equal(t, "Bearer fixture-video-key", headers.Get("Authorization"))
				require.Empty(t, headers.Get("Idempotency-Key"))
				require.Empty(t, headers.Get("X-Idempotency-Key"))
				for _, values := range headers {
					for _, value := range values {
						require.NotContains(t, value, "{{", "never send an unresolved template")
					}
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: account.ID})
				events, ok := c.Get(OpsUpstreamErrorsKey)
				require.True(t, ok)
				reported := events.([]*OpsUpstreamErrorEvent)
				actual := reported[len(reported)-1].ReportedHeaders
				require.Equal(t, identity.user, actual["x-end-user"])
				require.Equal(t, "…user_id="+identity.user, actual["x-combined"])
				require.NotContains(t, actual, "accept", "fixed priority replaced this variable")
				require.NotContains(t, actual, "idempotency-key")
				require.NotContains(t, actual, "x-idempotency-key")
				require.NotContains(t, actual, "x-static")
			})
		}
		rebuilt, rebuildErr := gateway.videoRelayRoute(account)
		require.NoError(t, rebuildErr)
		require.Equal(t, route.fingerprint, rebuilt.fingerprint, "identity is not account configuration")
	}
}

func TestVideoRelayRouteRetainsLegacyFixedHeaderFingerprint(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://FIXTURE.test:443/provider/v1/", "api_key": "local-original",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides: map[string]any{
			"user-agent": "gold-agent", "aaa": "a", "X-Partner": "b",
			"Idempotency-Key": "ignored", "X-IDEMPOTENCY-KEY": "ignored", "Authorization": "blocked",
		},
	}}
	route, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	// Captures the pre-upgrade length-prefixed SHA256 including wire-case sort
	// (User-Agent sorts before aaa), normalized URL and excluded headers.
	require.Equal(t, "c27aa20d5193820e098a2fdf224642f18037fc3b853ea453721acddf806dd0e9", route.fingerprint)
	task := &VideoRelayTask{SiteFingerprint: route.site, CredentialFingerprint: route.credential, RouteFingerprint: route.fingerprint}
	require.True(t, route.matches(task))
	account.Credentials[credKeyHeaderOverrides] = map[string]any{"x-user": "{{user_id}}"}
	templated, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	require.False(t, templated.matches(task))
	task.RouteFingerprint = templated.fingerprint
	account.Credentials[credKeyHeaderOverrides] = map[string]any{"x-user": "{{api_key_id}}"}
	changed, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	require.False(t, changed.matches(task), "changing template semantics changes configuration")
}

func TestVideoRelayRequestSkipsUnavailableVariables(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://fixture.test", "api_key": "fixture",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides: map[string]any{
			"x-user": "prefix-{{user_id}}", "x-group": "{{group_id}}", "x-unknown": "{{unknown}}", "x-static": "constant",
		},
	}}
	route, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	for _, ctx := range []context.Context{context.Background(), WithHeaderOverrideRequestScope(context.Background(), 7, 31, nil)} {
		req, err := route.taskRequest(ctx, "provider-task", false)
		require.NoError(t, err)
		require.NotContains(t, req.Header, "x-group")
		require.NotContains(t, req.Header, "x-unknown")
		require.Equal(t, []string{"constant"}, req.Header["x-static"])
		if headerOverrideScopeFromContext(ctx) == nil {
			require.NotContains(t, req.Header, "x-user")
		} else {
			require.Equal(t, []string{"prefix-7"}, req.Header["x-user"])
		}
	}
}

type videoRelayIdentityAccounts struct {
	AccountRepository
	account *Account
	reads   int
}

func (r *videoRelayIdentityAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	r.reads++
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return r.account, nil
}

func TestVideoRelayDownloadReportsOnlySentVariableHeaders(t *testing.T) {
	received := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received <- req.Header.Clone()
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"fixture unavailable"}}`)
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": upstream.URL, "api_key": "fixture",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides:       map[string]any{"x-user": "{{user_id}}", "accept": "{{user_id}}", "range": "{{user_id}}"},
	}}
	accounts := &videoRelayIdentityAccounts{account: account}
	gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: accounts, httpUpstream: videoRelayTestTransport{client: upstream.Client()}}
	route, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	store := &videoRelaySubmissionStore{task: VideoRelayTask{
		TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 7, APIKeyID: 31, AccountID: account.ID,
		UpstreamTaskID: "provider-task", Status: VideoRelayStatusCompleted, BillingState: VideoRelayCharged,
		SiteFingerprint: route.site, CredentialFingerprint: route.credential, RouteFingerprint: route.fingerprint,
	}}
	svc := NewVideoRelayService(store, gateway, nil)
	key := &APIKey{ID: 31, UserID: 7}
	for _, wanted := range []string{"", "bytes=0-11"} {
		t.Run(wanted, func(t *testing.T) {
			ctx := WithHeaderOverrideRequestScope(context.Background(), 7, 31, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/videos/"+store.task.TaskID+"/content", nil)
			c.Request.Header.Set("X-User", "forged-client-identity")
			var ranges []string
			if wanted != "" {
				ranges = []string{wanted}
			}
			download, failure := svc.Download(c, key, store.task.TaskID, ranges)
			require.Nil(t, download)
			require.NotNil(t, failure)
			headers := <-received
			require.Equal(t, wanted, headers.Get("Range"))
			require.Equal(t, "video/mp4", headers.Get("Accept"))
			require.Equal(t, "identity", headers.Get("Accept-Encoding"))
			require.Equal(t, "7", headers.Get("X-User"))
			events, exists := c.Get(OpsUpstreamErrorsKey)
			require.True(t, exists)
			require.Len(t, events, 1)
			reported := events.([]*OpsUpstreamErrorEvent)[0].ReportedHeaders
			require.Equal(t, "7", reported["x-user"])
			require.NotContains(t, reported, "range")
			require.NotContains(t, reported, "accept")
			require.Equal(t, VideoRelayCharged, store.task.BillingState)
		})
	}
	require.Equal(t, 2, accounts.reads, "only the existing original-account read per download")
	require.Equal(t, 2, store.reads, "only the existing ownership read per download")
}

func TestVideoRelayRouteRecognizesEscapedVersionPath(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		enabled bool
	}{{"default validation", false}, {"with host allowlist", true}} {
		t.Run(fixture.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = fixture.enabled
			cfg.Security.URLAllowlist.UpstreamHosts = []string{"fixture.test"}
			gateway := &OpenAIGatewayService{cfg: cfg}
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url": "https://fixture.test/tenant/v1", "api_key": "synthetic-video-key",
			}}
			plain, err := gateway.videoRelayRoute(account)
			require.NoError(t, err)
			account.Credentials["base_url"] = "https://fixture.test/tenant/%76%31"
			escaped, err := gateway.videoRelayRoute(account)
			require.NoError(t, err)
			require.Equal(t, plain.collection.String(), escaped.collection.String())
			require.Equal(t, plain.site, escaped.site)
			require.Equal(t, "https://fixture.test/tenant/api/log/token", escaped.logURL)
			account.Credentials["base_url"] = "https://fixture.test/tenant%2Fother"
			prefixed, err := gateway.videoRelayRoute(account)
			require.NoError(t, err)
			require.Equal(t, "https://fixture.test/tenant%2Fother/v1/videos", prefixed.collection.String())
			require.Equal(t, "https://fixture.test/tenant%2Fother/api/log/token", prefixed.logURL)
			account.Credentials["base_url"] = "https://fixture.test/tenant/other"
			separate, err := gateway.videoRelayRoute(account)
			require.NoError(t, err)
			require.NotEqual(t, prefixed.site, separate.site)
			if fixture.enabled {
				account.Credentials["base_url"] = "https://disallowed.test/tenant%2Fother"
				_, err = gateway.videoRelayRoute(account)
				require.Error(t, err, "the existing host allowlist still applies")
			}
		})
	}
}

func TestVideoRelaySelectRouteAppliesRestrictionToTheVideoModels(t *testing.T) {
	const requested, channelModel, accountModel = "public-video", "channel-video", "account-video"
	for _, tc := range []struct {
		name, billingModelSource, allowedModel string
		passthrough, wantRoute                 bool
	}{
		{"requested model permits the two mappings", BillingModelSourceRequested, requested, false, true},
		{"a permitted mapped model cannot replace a denied requested model", BillingModelSourceRequested, channelModel, false, false},
		{"upstream restriction rejects the actual video model", BillingModelSourceUpstream, channelModel, true, false},
		{"upstream restriction allows the actual video model", BillingModelSourceUpstream, accountModel, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price := 0.25
			group := &Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
				{Models: []string{requested}, BillingMode: BillingModeVideo, PerRequestPrice: &price},
			}}
			channel := newTestChannelService(makeStandardRepo(Channel{
				ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID},
				RestrictModels: true, BillingModelSource: tc.billingModelSource,
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{tc.allowedModel}}},
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {requested: channelModel}},
			}, map[int64]string{group.ID: PlatformOpenAI}))
			account := Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
				Schedulable: true, Concurrency: 1, GroupIDs: []int64{group.ID},
				Credentials: map[string]any{
					"base_url": "https://fixture.test", "api_key": "synthetic-video-key",
					"openai_capabilities": []any{"videos"},
					"model_mapping":       map[string]any{channelModel: accountModel},
					OpenAIVideoRelayCredentialsKey: OpenAIVideoRelaySettings{
						Resolutions: []string{"480p"}, MinSeconds: 5, DefaultSeconds: 5, MaxSeconds: 30,
					},
				},
				Extra: map[string]any{"openai_passthrough": tc.passthrough},
			}
			billing := &BillingService{}
			svc := &VideoRelayService{gateway: &OpenAIGatewayService{
				cfg: &config.Config{}, channelService: channel, accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				billingService: billing, resolver: NewModelPricingResolver(nil, billing),
			}}
			_, task, release, err := svc.selectRoute(context.Background(), &APIKey{ID: 3, UserID: 4, Group: group, GroupID: &group.ID},
				VideoRelayRequest{Model: requested, Seconds: 5, Resolution: "480p"})
			defer releaseVideoRelaySlot(release)
			if tc.wantRoute {
				require.Nil(t, err)
				require.Equal(t, accountModel, task.UpstreamModel)
			} else {
				require.NotNil(t, err)
				require.Nil(t, task)
			}
		})
	}
}

type videoRelaySchedulingFixture struct {
	stubOpenAIAccountRepo
	groupReads int
}

func (r *videoRelaySchedulingFixture) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	r.groupReads++
	return r.stubOpenAIAccountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
}

func TestVideoRelaySelectRouteFiltersUnsupportedSpecsInOneSchedulingPass(t *testing.T) {
	const requestedModel = "fixture-video"
	price := 0.25
	group := &Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
		{Models: []string{requestedModel}, BillingMode: BillingModeVideo, PerRequestPrice: &price},
	}}
	newAccount := func(id int64, priority int, resolution string) Account {
		return Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Concurrency: 1, Priority: priority, GroupIDs: []int64{group.ID},
			Credentials: map[string]any{
				"base_url": "https://fixture.test", "api_key": "synthetic-video-key",
				"openai_capabilities": []any{"videos"},
				OpenAIVideoRelayCredentialsKey: OpenAIVideoRelaySettings{
					Resolutions: []string{resolution}, MinSeconds: 5, DefaultSeconds: 5, MaxSeconds: 30,
				},
			},
		}
	}
	unavailable, matching := newAccount(20, 1, "720p"), newAccount(21, 2, "480p")
	for _, tc := range []struct {
		name     string
		accounts []Account
		match    bool
		advanced bool
	}{
		{"use the compatible account", []Account{unavailable, matching}, true, false},
		{"all accounts have incompatible specs", []Account{unavailable}, false, false},
		{"advanced scheduling uses the compatible account", []Account{unavailable, matching}, true, true},
		{"advanced scheduling has no compatible account", []Account{unavailable}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := &videoRelaySchedulingFixture{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: tc.accounts}}
			billing := &BillingService{}
			svc := &VideoRelayService{gateway: &OpenAIGatewayService{
				cfg: &config.Config{}, accountRepo: accounts,
				billingService: billing, resolver: NewModelPricingResolver(nil, billing),
			}}
			if tc.advanced {
				svc.gateway.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
			}
			_, task, release, failure := svc.selectRoute(context.Background(), &APIKey{ID: 3, UserID: 4, Group: group, GroupID: &group.ID},
				VideoRelayRequest{Model: requestedModel, Seconds: 5, Resolution: "480p"})
			defer releaseVideoRelaySlot(release)
			if tc.match {
				require.Nil(t, failure)
				require.Equal(t, matching.ID, task.AccountID)
			} else {
				require.NotNil(t, failure)
				require.Equal(t, http.StatusBadRequest, failure.Status)
			}
			require.Equal(t, 1, accounts.groupReads, "video specs can be checked on the already loaded scheduling candidates")
		})
	}
}

type videoRelaySubmissionStore struct {
	VideoRelayRepository
	task  VideoRelayTask
	reads int
}

func (r *videoRelaySubmissionStore) Reserve(_ context.Context, task *VideoRelayTask) error {
	task.Status, task.BillingState, task.CreatedAt = VideoRelayStatusSubmitting, VideoRelayHeld, time.Now()
	r.task = *task
	return nil
}

func (r *videoRelaySubmissionStore) AcquireSend(_ context.Context, task *VideoRelayTask) (*VideoRelayTask, error) {
	now := time.Now()
	r.task.SentAt = &now
	return r.Get(context.Background(), task.TaskID, r.task.UserID, r.task.APIKeyID)
}

func (r *videoRelaySubmissionStore) Get(_ context.Context, id string, userID, keyID int64) (*VideoRelayTask, error) {
	r.reads++
	if id != r.task.TaskID || userID != r.task.UserID || keyID != r.task.APIKeyID {
		return nil, ErrVideoRelayNotFound
	}
	task := r.task
	return &task, nil
}

func (r *videoRelaySubmissionStore) RecordSubmission(_ context.Context, sent *VideoRelayTask, requestID, upstreamID string) (*VideoRelayTask, error) {
	if requestID != "" {
		r.task.UpstreamRequestID = requestID
	}
	if upstreamID != "" {
		r.task.UpstreamTaskID, r.task.Status = upstreamID, VideoRelayStatusQueued
	} else {
		r.task.Status = VideoRelayStatusUnconfirmed
	}
	return r.Get(context.Background(), sent.TaskID, sent.UserID, sent.APIKeyID)
}

func (r *videoRelaySubmissionStore) Settle(ctx context.Context, task *VideoRelayTask, change VideoRelaySettlement) (*VideoRelayTask, *UsageBillingApplyResult, error) {
	r.task.FailureReason, r.task.ErrorMessage = change.FailureReason, change.ErrorMessage
	r.task.Status, r.task.BillingState = VideoRelayStatusFailed, VideoRelayRefunded
	current, err := r.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	return current, &UsageBillingApplyResult{Applied: true}, err
}

type videoRelayTestTransport struct {
	client  *http.Client
	request func(*http.Request)
}

func (t videoRelayTestTransport) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if t.request != nil {
		t.request(req)
	}
	return t.client.Do(req)
}

func (t videoRelayTestTransport) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return t.Do(req, proxyURL, accountID, accountConcurrency)
}

type videoRelayTracedTestTransport struct{ videoRelayTestTransport }

func (videoRelayTracedTestTransport) SupportsVideoRelayConnectionTrace() bool { return true }

func TestVideoRelayQueryAndDownloadReportInvalidUpstreamResponses(t *testing.T) {
	const upstreamTaskID = "owned-fixture-video"
	var downloadMediaType atomic.Value
	downloadMediaType.Store("application/json")
	var downloadStatus atomic.Int32
	downloadStatus.Store(http.StatusOK)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "synthetic-response-id")
		switch r.URL.Path {
		case "/v1/videos/" + upstreamTaskID:
			_, _ = io.WriteString(w, `{"id":"wrong-fixture-video","status":"completed"}`)
		case "/v1/videos/" + upstreamTaskID + "/content":
			mediaType := downloadMediaType.Load().(string)
			w.Header().Set("Content-Type", mediaType)
			status := int(downloadStatus.Load())
			w.WriteHeader(status)
			if mediaType != "application/json" {
				_, _ = io.WriteString(w, "<h1>invalid media fixture marker</h1>")
				if status == http.StatusBadGateway {
					_, _ = io.WriteString(w, strings.Repeat("X", videoRelayResponseLimit))
				}
				return
			}
			if status == http.StatusBadGateway {
				_, _ = io.WriteString(w, `{"error":{"code":"account_disabled","message":"invalid media fixture marker"},"diagnostic":"`+
					strings.Repeat("X", 2*maxBodyMatchLen)+`"}`)
				return
			}
			_, _ = io.WriteString(w, `{"error":{"message":"invalid media fixture marker"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	account := Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": upstream.URL, "api_key": "synthetic-video-key",
	}}
	gateway := &OpenAIGatewayService{
		cfg: cfg, accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: videoRelayTestTransport{client: upstream.Client()},
	}
	route, err := gateway.videoRelayRoute(&account)
	require.NoError(t, err)
	store := &videoRelaySubmissionStore{task: VideoRelayTask{
		TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 1, APIKeyID: 2, AccountID: account.ID,
		UpstreamTaskID: upstreamTaskID, UpstreamModel: "fixture-video", Status: VideoRelayStatusQueued, BillingState: VideoRelayHeld,
		SiteFingerprint: route.site, CredentialFingerprint: route.credential, RouteFingerprint: route.fingerprint,
	}}
	svc := NewVideoRelayService(store, gateway, nil)
	key := &APIKey{ID: store.task.APIKeyID, UserID: store.task.UserID}
	t.Run("mismatched upstream result", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+store.task.TaskID, nil)
		task, failure := svc.Lookup(c, key, store.task.TaskID)
		require.Nil(t, failure)
		require.Equal(t, VideoRelayHeld, task.BillingState)
		require.Equal(t, upstreamTaskID, task.UpstreamTaskID)
		events, exists := c.Get(OpsUpstreamErrorsKey)
		require.True(t, exists, "a rejected upstream reply must be recorded by the existing Ops path")
		require.Len(t, events, 1)
		require.Equal(t, 1, store.reads, "a read-only failure already has the owned task")
	})
	for _, fixture := range []struct {
		name, mediaType  string
		status           int
		wantAccountBlock bool
	}{
		{"JSON error matches safe rule", "application/json", http.StatusOK, false},
		{"HTML error matches safe rule", "text/html; charset=utf-8", http.StatusOK, false},
		{"mislabeled HTML error matches safe rule", "video/mp4", http.StatusOK, false},
		{"large 502 matches the existing prefix rule", "text/html", http.StatusBadGateway, false},
		{"complete structured error still blocks account scheduling", "application/json", http.StatusBadGateway, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			downloadMediaType.Store(fixture.mediaType)
			downloadStatus.Store(int32(fixture.status))
			store.task.Status, store.task.BillingState = VideoRelayStatusCompleted, VideoRelayCharged
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+store.task.TaskID+"/content", nil)
			rules := &ErrorPassthroughService{}
			status := http.StatusUnprocessableEntity
			rules.setLocalCache([]*model.ErrorPassthroughRule{{
				Enabled: true, Platforms: []string{PlatformOpenAI}, ErrorCodes: []int{fixture.status},
				Keywords: []string{"invalid media fixture marker"}, MatchMode: model.MatchModeAll, ResponseCode: &status,
				SkipMonitoring: true,
			}})
			BindErrorPassthroughService(c, rules)
			download, failure := svc.Download(c, key, store.task.TaskID, nil)
			require.Nil(t, download)
			require.NotNil(t, failure)
			require.Equal(t, status, failure.Status)
			require.Equal(t, videoRelayDownloadMessage, failure.Message)
			require.True(t, c.GetBool(OpsSkipPassthroughKey))
			require.Equal(t, VideoRelayCharged, store.task.BillingState)
			require.Equal(t, fixture.wantAccountBlock, gateway.isOpenAIAccountRuntimeBlocked(&account),
				"complete structured errors must keep the existing account scheduling behavior")
		})
	}
}

func TestVideoRelayRefundsOnlyWhenTransportNeverGetsConnection(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	var received atomic.Int32
	disconnected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/videos" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		received.Add(1)
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(disconnected.Close)
	for _, test := range []struct {
		name string
		url  string
		sent bool
	}{{"connection refused", closedURL, false}, {"disconnect after request", disconnected.URL, true}} {
		t.Run(test.name, func(t *testing.T) {
			collection, err := canonicalVideoRelayCollectionURL(test.url)
			require.NoError(t, err)
			route := &videoRelayRoute{collection: collection, bearer: "fixture-only",
				account: &Account{ID: 51, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}}
			client := &http.Client{Timeout: time.Second}
			upstream := videoRelayTracedTestTransport{videoRelayTestTransport{client: client}}
			store := &videoRelaySubmissionStore{}
			svc := NewVideoRelayService(store, &OpenAIGatewayService{httpUpstream: upstream}, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			task := &VideoRelayTask{TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 1, APIKeyID: 2,
				Seconds: 5, Resolution: "480p", UpstreamModel: "fixture-video"}
			result, publicErr := svc.submitSelected(c, VideoRelayRequest{Prompt: "Waves"}, route, task, nil)
			if test.sent {
				require.Nil(t, publicErr)
				require.Equal(t, VideoRelayHeld, result.BillingState)
				require.Equal(t, VideoRelayStatusUnconfirmed, result.Status)
				require.Equal(t, int32(1), received.Load())
				return
			}
			require.Nil(t, result)
			require.NotNil(t, publicErr)
			require.Equal(t, VideoRelayRefunded, store.task.BillingState)
			require.Equal(t, VideoRelayNotSent, store.task.FailureReason)
			require.Zero(t, received.Load())
		})
	}
}

func TestVideoRelaySubmitEvidenceAndRequestContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, requestID, body string
		status                int
		wantTask, wantReason  string
		publicError           bool
		truncated             bool
	}{
		{"accepted", "r1", `{"status":"processing","id":"provider-1","task_id":"provider-1"}`, 200, "provider-1", "", false, false},
		{"truncated accepted response", "r2", `{"id":"provider-1"`, 200, "", "", false, true},
		{"conflicting ids", "r3", `{"status":"queued","id":"provider-1","task_id":"other"}`, 200, "", "", false, false},
		{"malformed status", "r31", `{"status":123,"id":"provider-1"}`, 200, "", "", false, false},
		{"missing status with an accepted id", "r32", `{"id":"provider-1"}`, 200, "provider-1", "", false, false},
		{"explicit rejection", "r4", `{"error":{"message":"provider only diagnostic"}}`, 400, "", VideoRelayUpstreamRejected, true, false},
		{"explicit authentication rejection", "r41", `{"error":{"type":"unauthorized"}}`, 401, "", VideoRelayUpstreamRejected, true, false},
		{"explicit permission rejection", "r43", `{"code":"policy","message":"denied","data":null}`, 403, "", VideoRelayUpstreamRejected, true, false},
		{"explicit rate limit rejection", "r49", `{"error":{"code":"rate_limit"}}`, 429, "", VideoRelayUpstreamRejected, true, false},
		{"request timeout response", "r5", `{"error":{"message":"provider only diagnostic"}}`, 408, "", "", false, false},
		{"conflict response", "r51", `{"error":{"message":"conflict"}}`, 409, "", "", false, false},
		{"too early response", "r52", `{"error":{"message":"retry"}}`, 425, "", "", false, false},
		{"client disconnect response", "r53", `{"error":{"message":"closed"}}`, 499, "", "", false, false},
		{"error with a null task field", "r54", `{"error":{"message":"denied"},"task_id":null}`, 400, "", "", false, false},
		{"error with an invalid task field", "r55", `{"error":{"message":"denied"},"data":{"id":"a/b"}}`, 400, "", "", false, false},
		{"error with an empty error object", "r56", `{"error":{}}`, 400, "", "", false, false},
		{"non JSON error", "r57", `<html>temporary response</html>`, 400, "", "", false, false},
		{"incomplete error", "r6", `{"error":{"message":"provider only diagnostic"}`, 400, "", "", false, true},
		{"rejection without request id", "", `{"error":{"message":"provider only diagnostic"}}`, 400, "", "", false, false},
		{"upstream unavailable", "r7", `{"error":{"message":"provider only diagnostic"}}`, 503, "", "", false, false},
		{"redirect", "", "", 302, "", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type observed struct {
				method, path string
				body         []byte
			}
			seen := make(chan observed, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				seen <- observed{method: r.Method, path: r.URL.Path, body: body}
				if tc.requestID != "" {
					w.Header().Set("X-Oneapi-Request-Id", tc.requestID)
				}
				if tc.truncated {
					w.Header().Set("Content-Length", "999")
				}
				if tc.status == 302 {
					w.Header().Set("Location", "/redirected")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			collection, err := canonicalVideoRelayCollectionURL(server.URL + "/tenant/v1/")
			require.NoError(t, err)
			account := &Account{ID: 30, Type: AccountTypeAPIKey, Platform: PlatformOpenAI}
			route := &videoRelayRoute{collection: collection, bearer: "fixture", account: account}
			repo := &videoRelaySubmissionStore{}
			client := server.Client()
			client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				return http.ErrUseLastResponse
			}
			upstream := videoRelayTestTransport{client: client, request: func(req *http.Request) {
				require.Nil(t, req.GetBody, "the admitted POST body must not be replayable")
				require.Empty(t, req.Header.Get("Idempotency-Key"))
			}}
			svc := NewVideoRelayService(repo, &OpenAIGatewayService{httpUpstream: upstream}, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			task := &VideoRelayTask{
				TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 51, APIKeyID: 61,
				Seconds: 30, Resolution: "480p", UpstreamModel: "fixture-upstream-model",
			}
			var releases atomic.Int32
			result, publicErr := svc.submitSelected(c, VideoRelayRequest{Prompt: "Waves", Images: []string{"https://fixture.test/ref.png"}}, route, task,
				func() { releases.Add(1) })
			require.Equal(t, int32(1), releases.Load())
			require.Len(t, seen, 1)
			request := <-seen
			require.Equal(t, "POST", request.method)
			require.Equal(t, "/tenant/v1/videos", request.path)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(request.body, &payload))
			require.Equal(t, "fixture-upstream-model", payload["model"])
			require.Equal(t, "30", payload["seconds"])
			require.Equal(t, "480p", payload["resolution"])
			require.Equal(t, []any{"https://fixture.test/ref.png"}, payload["images"])
			require.Equal(t, tc.requestID, repo.task.UpstreamRequestID)
			require.Equal(t, tc.wantTask, repo.task.UpstreamTaskID)
			require.Equal(t, tc.wantReason, repo.task.FailureReason)
			if tc.publicError {
				require.Nil(t, result)
				require.NotNil(t, publicErr)
				require.Equal(t, videoRelayRejectedMessage, publicErr.Message)
				require.Equal(t, VideoRelayRefunded, repo.task.BillingState)
			} else {
				require.Nil(t, publicErr)
				require.Equal(t, VideoRelayHeld, result.BillingState)
				require.Contains(t, []string{VideoRelayStatusQueued, VideoRelayStatusUnconfirmed}, result.Status)
			}
		})
	}
}

func TestVideoRelaySubmitKeepsAccountSlotAfterClientDisconnect(t *testing.T) {
	received := make(chan struct{}, 1)
	reply := make(chan struct{})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		received <- struct{}{}
		select {
		case <-reply:
			w.Header().Set("X-Oneapi-Request-Id", "provider-request")
			_, _ = io.WriteString(w, `{"id":"provider-task","status":"queued"}`)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/videos", nil)
	collection, err := canonicalVideoRelayCollectionURL(server.URL)
	require.NoError(t, err)
	route := &videoRelayRoute{collection: collection, bearer: "fixture",
		account: &Account{ID: 30, Type: AccountTypeAPIKey, Platform: PlatformOpenAI}}
	store := &videoRelaySubmissionStore{}
	svc := NewVideoRelayService(store, &OpenAIGatewayService{httpUpstream: videoRelayTestTransport{client: server.Client()}}, nil)
	task := &VideoRelayTask{TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 31, APIKeyID: 32,
		Seconds: 5, Resolution: "480p", UpstreamModel: "fixture-upstream"}
	type completion struct {
		task *VideoRelayTask
		err  *VideoRelayError
	}
	completed, released := make(chan completion, 1), make(chan struct{}, 1)
	go func() {
		persisted, err := svc.submitSelected(c, VideoRelayRequest{Prompt: "Waves"}, route, task, func() { released <- struct{}{} })
		completed <- completion{persisted, err}
	}()
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		close(reply)
		t.Fatal("submitting request did not reach the upstream")
	}
	cancel()
	select {
	case <-released:
		close(reply)
		t.Fatal("client disconnect released the account slot before the upstream replied")
	case <-time.After(50 * time.Millisecond):
	}
	close(reply)
	select {
	case got := <-completed:
		require.Nil(t, got.err)
		require.Equal(t, "provider-task", got.task.UpstreamTaskID)
		require.Equal(t, VideoRelayHeld, got.task.BillingState)
	case <-time.After(2 * time.Second):
		t.Fatal("submission did not persist the upstream reply after the client disconnected")
	}
	require.Len(t, released, 1)
	require.Equal(t, int32(1), posts.Load())
}
