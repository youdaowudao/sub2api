//go:build unit

package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayHandlerResponseContainsOnlyCustomerFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	task := &service.VideoRelayTask{
		TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Model: "customer-model",
		UpstreamTaskID: "provider-only-task", UpstreamModel: "provider-only-model",
		UpstreamRequestID: "provider-only-request", Status: service.VideoRelayStatusUnconfirmed,
		BillingState: service.VideoRelayHeld, Seconds: 5, Resolution: "480p",
		CreatedAt: time.Unix(1795434906, 0),
	}
	writeVideoRelayTask(c, task)
	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body, 10)
	for _, key := range []string{"id", "object", "model", "status", "progress", "created_at", "completed_at", "seconds", "resolution", "error"} {
		require.Contains(t, body, key)
	}
	require.JSONEq(t, `"queued"`, string(body["status"]))
	require.JSONEq(t, `"5"`, string(body["seconds"]))
	require.JSONEq(t, `null`, string(body["completed_at"]))
	require.NotContains(t, recorder.Body.String(), "provider-only")
	require.Empty(t, recorder.Header().Get("X-Oneapi-Request-Id"))
}

func TestVideoRelayHandlerOnlyReportsRefundAfterSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	task := &service.VideoRelayTask{
		Status: service.VideoRelayStatusFailed, BillingState: service.VideoRelayHeld,
		ErrorMessage: "The frozen amount was returned.",
	}
	writeVideoRelayTask(c, task)
	require.NotContains(t, recorder.Body.String(), "The frozen amount was returned.")
	require.Contains(t, recorder.Body.String(), `"status":"queued"`)
}

func TestVideoRelayHandlerReportsTrackingAndRemovedRouteRefunds(t *testing.T) {
	for reason, message := range map[string]string{
		service.VideoRelayTrackingExpired: "Video tracking expired. The frozen amount was returned. Submit a new request if you still need the video.",
		service.VideoRelayAccountDeleted:  "The video route was removed. The frozen amount was returned. Submit a new request if you still need the video.",
	} {
		for _, state := range []string{service.VideoRelayRefunded, service.VideoRelayHeld} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			task := &service.VideoRelayTask{
				Status: service.VideoRelayStatusFailed, BillingState: state, FailureReason: reason,
				ErrorMessage: "UPSTREAM_INTERNAL_TEST_MESSAGE",
			}
			writeVideoRelayTask(c, task)
			require.NotContains(t, recorder.Body.String(), task.ErrorMessage)
			if state == service.VideoRelayRefunded {
				require.Contains(t, recorder.Body.String(), `"status":"failed"`)
				require.Contains(t, recorder.Body.String(), message)
			} else {
				require.Contains(t, recorder.Body.String(), `"status":"queued"`)
				require.NotContains(t, recorder.Body.String(), message)
			}
		}
	}
}

func TestVideoRelayHandlerUsageRecordShowsOnlyThePublicModel(t *testing.T) {
	model, mapped := "customer-video", "provider-only-video"
	log := &service.UsageLog{RequestID: "video_relay:video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Model: model, RequestedModel: model, UpstreamModel: &mapped}
	customer, err := json.Marshal(dto.UsageLogFromService(log))
	require.NoError(t, err)
	require.Contains(t, string(customer), log.RequestID)
	require.Contains(t, string(customer), model)
	require.NotContains(t, string(customer), mapped)
	admin, err := json.Marshal(dto.UsageLogFromServiceAdmin(log))
	require.NoError(t, err)
	require.Contains(t, string(admin), mapped)
}

type videoRelayHandlerStore struct {
	service.VideoRelayRepository
	task service.VideoRelayTask
}

func (repo *videoRelayHandlerStore) Reserve(_ context.Context, task *service.VideoRelayTask) error {
	task.Status, task.BillingState, task.CreatedAt = service.VideoRelayStatusSubmitting, service.VideoRelayHeld, time.Now()
	repo.task = *task
	return nil
}

func (repo *videoRelayHandlerStore) AcquireSend(_ context.Context, _ *service.VideoRelayTask) (*service.VideoRelayTask, error) {
	now := time.Now()
	repo.task.SentAt = &now
	return repo.Get(context.Background(), repo.task.TaskID, repo.task.UserID, repo.task.APIKeyID)
}

func (repo *videoRelayHandlerStore) RecordSubmission(ctx context.Context, task *service.VideoRelayTask, requestID, upstreamID string) (*service.VideoRelayTask, error) {
	repo.task.UpstreamRequestID = requestID
	if upstreamID != "" {
		repo.task.UpstreamTaskID, repo.task.Status = upstreamID, service.VideoRelayStatusQueued
	}
	return repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
}

func (repo *videoRelayHandlerStore) Get(_ context.Context, id string, userID, keyID int64) (*service.VideoRelayTask, error) {
	if id != repo.task.TaskID || userID != repo.task.UserID || keyID != repo.task.APIKeyID {
		return nil, service.ErrVideoRelayNotFound
	}
	copy := repo.task
	return &copy, nil
}

type videoRelayHandlerAccounts struct {
	service.AccountRepository
	account *service.Account
}

func (repo *videoRelayHandlerAccounts) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	if repo.account.Platform != platform {
		return nil, nil
	}
	return []service.Account{*repo.account}, nil
}

func (repo *videoRelayHandlerAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if id != repo.account.ID {
		return nil, service.ErrAccountNotFound
	}
	return repo.account, nil
}

func fixtureVideoRelayHash(fields ...string) string {
	var serialized []byte
	for _, field := range fields {
		serialized = binary.BigEndian.AppendUint64(serialized, uint64(len(field)))
		serialized = append(serialized, field...)
	}
	digest := sha256.Sum256(serialized)
	return hex.EncodeToString(digest[:])
}

type videoRelayHandlerUsers struct {
	service.UserRepository
	user *service.User
}

func (repo *videoRelayHandlerUsers) GetByID(_ context.Context, _ int64) (*service.User, error) {
	return repo.user, nil
}

type videoRelayHandlerAudit struct {
	requests []securityaudit.Request
	blocked  bool
}

func (audit *videoRelayHandlerAudit) Check(_ context.Context, request securityaudit.Request) (*securityaudit.LegacyDecision, error) {
	audit.requests = append(audit.requests, request.Clone())
	return &securityaudit.LegacyDecision{Allowed: !audit.blocked, Blocked: audit.blocked, Message: "fixture moderation blocked"}, nil
}

func TestVideoRelayHandlerSubmitValidatesBeforeFreezingAndSharesReferenceWithAudit(t *testing.T) {
	const reference = "https://fixture.test/reference.png"
	upstreamRequests := make(chan []byte, 16)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/videos" {
			http.Error(w, "unexpected fixture route", http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		upstreamRequests <- body
		w.Header().Set("X-Oneapi-Request-Id", "fixture-request")
		_, _ = io.WriteString(w, `{"id":"fixture-task","status":"queued"}`)
	}))
	t.Cleanup(upstream.Close)

	for _, tc := range []struct {
		name, fields, mediaType string
		status, wantAudit       int
		wantImage, noPrice      bool
		blocked, subscription   bool
		simple                  bool
	}{
		{name: "image array", fields: `,"seconds":"30","images":["` + reference + `"]`, status: 200, wantAudit: 1, wantImage: true},
		{name: "normalized image alias", fields: `,"seconds":30,"input_reference":"HTTPS://fixture.test/reference.png"`, status: 200, wantAudit: 1, wantImage: true},
		{name: "only configured route has shorter limit", fields: `,"seconds":31`, status: 400, wantAudit: 1},
		{name: "outside database integer", fields: `,"seconds":2147483648`, status: 400},
		{name: "unpriced", status: 400, wantAudit: 1, noPrice: true},
		{name: "subscription", status: 403, subscription: true},
		{name: "simple mode", status: 403, simple: true},
		{name: "moderation blocked", fields: `,"input_reference":"` + reference + `"`, status: 403, wantAudit: 1, wantImage: true, blocked: true},
		{name: "unrecognized resolution", fields: `,"resolution":"1440p"`, status: 400},
		{name: "size field", fields: `,"size":"480p"`, status: 400},
		{name: "duration field", fields: `,"duration":5`, status: 400},
		{name: "unknown field", fields: `,"other":"value"`, status: 400},
		{name: "two videos", fields: `,"n":2`, status: 400},
		{name: "unsupported format", mediaType: "text/plain", status: 415},
		{name: "empty image", fields: `,"input_reference":""`, status: 400},
		{name: "bad image URL", fields: `,"input_reference":"https:///reference.png"`, status: 400},
		{name: "two images", fields: `,"images":["` + reference + `","` + reference + `"]`, status: 400},
		{name: "two image forms", fields: `,"input_reference":"` + reference + `","images":["` + reference + `"]`, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			if tc.simple {
				cfg.RunMode = config.RunModeSimple
			}
			group := &service.Group{ID: 21, Platform: service.PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1}
			if tc.subscription {
				group.SubscriptionType = service.SubscriptionTypeSubscription
			}
			if !tc.noPrice {
				price := 0.1
				group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"public-video"}, BillingMode: service.BillingModeVideo, PerRequestPrice: &price}}
			}
			accounts := &videoRelayHandlerAccounts{account: &service.Account{
				ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{
					"base_url": upstream.URL, "api_key": "local-simulator", "model_mapping": map[string]any{"public-video": "mapped-video"},
					"openai_capabilities": []any{"videos"},
					"openai_videos":       map[string]any{"resolutions": []any{"480p"}, "min_seconds": 5, "max_seconds": 30, "default_seconds": 5},
				},
			}}
			user := &service.User{ID: 71, Balance: 10}
			key := &service.APIKey{ID: 81, UserID: user.ID, User: user, GroupID: &group.ID, Group: group}
			billing := service.NewBillingService(cfg, nil)
			cache := service.NewBillingCacheService(nil, &videoRelayHandlerUsers{user: user}, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(cache.Stop)
			gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, cache,
				repository.NewHTTPUpstream(cfg), nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil)
			audit := &videoRelayHandlerAudit{blocked: tc.blocked}
			concurrency := service.NewConcurrencyService(&concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			})
			g := &OpenAIGatewayHandler{
				gatewayService: gateway, billingCacheService: cache, apiKeyService: &service.APIKeyService{},
				concurrencyHelper:        NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Second),
				securityAuditCoordinator: securityaudit.NewCoordinator(audit, nil),
			}
			store := &videoRelayHandlerStore{}
			h := NewVideoRelayHandler(service.NewVideoRelayService(store, gateway, nil), g)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"public-video","prompt":"Waves"`+tc.fields+`}`))
			mediaType := tc.mediaType
			if mediaType == "" {
				mediaType = "application/json"
			}
			c.Request.Header.Set("Content-Type", mediaType)
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: user.ID, Concurrency: 1})
			h.Submit(c)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Len(t, audit.requests, tc.wantAudit)
			var auditedImage string
			if tc.wantImage {
				require.Equal(t, service.ContentModerationProtocolOpenAIImages, audit.requests[0].Protocol)
				var auditBody struct {
					Images []struct {
						URL string `json:"image_url"`
					} `json:"images"`
				}
				require.NoError(t, json.Unmarshal(audit.requests[0].Body, &auditBody))
				require.Len(t, auditBody.Images, 1)
				auditedImage = auditBody.Images[0].URL
				require.Equal(t, reference, auditedImage)
			}
			if tc.status != http.StatusOK {
				require.Empty(t, store.task.TaskID, "rejected requests must not freeze funds")
				require.Empty(t, upstreamRequests, "rejected requests must not send a POST")
				return
			}
			require.Equal(t, service.VideoRelayHeld, store.task.BillingState)
			require.Equal(t, "fixture-task", store.task.UpstreamTaskID)
			require.Len(t, upstreamRequests, 1)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(<-upstreamRequests, &payload))
			require.Equal(t, []any{auditedImage}, payload["images"])
			require.Equal(t, "mapped-video", payload["model"])
			require.Equal(t, "30", payload["seconds"])
		})
	}
}

func TestVideoRelayHandlerDownloadRejectsImplicitlyDecompressed206(t *testing.T) {
	var recoveryLog bytes.Buffer
	previousMode, previousWriter := gin.Mode(), gin.DefaultErrorWriter
	gin.SetMode(gin.DebugMode)
	gin.DefaultErrorWriter = &recoveryLog
	t.Cleanup(func() {
		gin.SetMode(previousMode)
		gin.DefaultErrorWriter = previousWriter
	})
	const clientKeyFixture = "synthetic-download-request-key"
	var compressed atomic.Bool
	var truncated atomic.Bool
	var requests atomic.Int32
	var mode atomic.Int32
	const (
		modeRedirect int32 = iota + 1
		modeInvalid416
		modeValid416
		modeExcessBytes
		modeCancellation
	)
	var redirected atomic.Int32
	streamStarted := make(chan struct{}, 1)
	streamClosed := make(chan struct{}, 1)
	var captureRange atomic.Bool
	observedRanges := make(chan []string, 2)
	mp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 8, 8, 8, 8}
	var zipped bytes.Buffer
	writer := gzip.NewWriter(&zipped)
	_, err := writer.Write(mp4)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/incorrect-target" {
			redirected.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		requests.Add(1)
		if captureRange.Load() {
			observedRanges <- r.Header.Values("Range")
		}
		if r.Method != "GET" || r.URL.Path != "/v1/videos/proxy-only-id/content" || r.Header.Get("Accept-Encoding") != "identity" {
			http.Error(w, "unexpected download request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Disposition", `attachment; filename="proxy-only-id.mp4"`)
		w.Header().Set("CF-Ray", "proxy-only")
		w.Header().Set("X-Oneapi-Request-Id", "proxy-only")
		switch mode.Load() {
		case modeRedirect:
			w.Header().Set("Location", "/incorrect-target")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		case modeInvalid416, modeValid416:
			total := "32; untrusted"
			if mode.Load() == modeValid416 {
				total = "32"
			}
			w.Header().Set("Content-Range", "bytes */"+total)
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			_, _ = io.WriteString(w, "provider-only error")
			return
		case modeExcessBytes:
			w.Header().Set("Content-Range", "bytes 0-15/32")
			w.WriteHeader(http.StatusPartialContent)
			_ = http.NewResponseController(w).Flush()
			_, _ = w.Write(mp4)
			_, _ = io.WriteString(w, "provider-only excess bytes")
			return
		case modeCancellation:
			_, _ = w.Write(mp4)
			_ = http.NewResponseController(w).Flush()
			streamStarted <- struct{}{}
			<-r.Context().Done()
			streamClosed <- struct{}{}
			return
		}
		body := mp4
		if compressed.Load() {
			w.Header().Set("Content-Encoding", "gzip")
			body = zipped.Bytes()
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Header.Get("Range") == "bytes=0-15" {
			w.Header().Set("Content-Range", "bytes 0-15/32")
			w.WriteHeader(http.StatusPartialContent)
			if truncated.Load() {
				body = body[:12]
			}
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(upstream.Close)
	const auth = "synthetic-video-upstream"
	site, credential := fixtureVideoRelayHash(upstream.URL+"/v1/videos"), fixtureVideoRelayHash(auth)
	store := &videoRelayHandlerStore{task: service.VideoRelayTask{
		TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Model: "public-video", UserID: 71, APIKeyID: 81, AccountID: 91,
		UpstreamTaskID: "proxy-only-id", Status: service.VideoRelayStatusCompleted, BillingState: service.VideoRelayCharged,
		SiteFingerprint: site, CredentialFingerprint: credential, RouteFingerprint: fixtureVideoRelayHash(site, credential),
	}}
	accounts := &videoRelayHandlerAccounts{account: &service.Account{
		ID: store.task.AccountID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"base_url": upstream.URL, "api_key": auth},
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil,
		nil, repository.NewHTTPUpstream(cfg), nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewVideoRelayHandler(service.NewVideoRelayService(store, gateway, nil), &OpenAIGatewayHandler{})
	router := gin.New()
	router.Use(middleware2.Recovery())
	router.GET("/v1/videos/:request_id/content", func(c *gin.Context) {
		keyID, _ := strconv.ParseInt(c.Request.Header.Get("X-Fixture-Key"), 10, 64)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: keyID, UserID: store.task.UserID})
		h.Download(c)
	})
	downloadWithContext := func(ctx context.Context, key, byteRange string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/videos/"+store.task.TaskID+"/content", nil)
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		req.Header.Set("X-Fixture-Key", key)
		req.Header.Set("X-Api-Key", clientKeyFixture)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	download := func(key, byteRange string) *httptest.ResponseRecorder {
		return downloadWithContext(context.Background(), key, byteRange)
	}

	const byteRange = "bytes=0-15"
	require.Equal(t, http.StatusNotFound, download("82", byteRange).Code)
	store.task.BillingState = service.VideoRelayHeld
	require.Equal(t, http.StatusConflict, download("81", byteRange).Code)
	require.Zero(t, requests.Load())
	store.task.BillingState = service.VideoRelayCharged
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, download("81", "bytes=0-15,20-24").Code)
	require.Zero(t, requests.Load())
	whole := download("81", "")
	require.Equal(t, http.StatusOK, whole.Code)
	require.Equal(t, mp4, whole.Body.Bytes())
	require.Empty(t, whole.Header().Get("Content-Range"))
	require.Equal(t, "private, no-store", whole.Header().Get("Cache-Control"))
	valid := download("81", byteRange)
	require.Equal(t, http.StatusPartialContent, valid.Code, valid.Body.String())
	require.Equal(t, mp4, valid.Body.Bytes())
	require.Equal(t, "bytes 0-15/32", valid.Header().Get("Content-Range"))
	require.Equal(t, `attachment; filename="`+store.task.TaskID+`.mp4"`, valid.Header().Get("Content-Disposition"))
	require.Empty(t, valid.Header().Get("CF-Ray"))
	require.Empty(t, valid.Header().Get("X-Oneapi-Request-Id"))
	compressed.Store(true)
	invalid := download("81", byteRange)
	require.Equal(t, http.StatusBadGateway, invalid.Code, invalid.Body.String())
	require.NotContains(t, invalid.Body.String(), "ftyp")
	require.Empty(t, invalid.Header().Get("Content-Range"))
	require.Equal(t, int32(3), requests.Load())
	compressed.Store(false)
	truncated.Store(true)
	require.PanicsWithValue(t, http.ErrAbortHandler, func() { download("81", byteRange) })
	t.Run("abort without request credentials in logs", func(t *testing.T) {
		require.False(t, strings.Contains(recoveryLog.String(), clientKeyFixture),
			"an aborted download must not log the client's request credentials")
	})
	require.Equal(t, int32(4), requests.Load())
	require.Equal(t, service.VideoRelayCharged, store.task.BillingState)
	truncated.Store(false)

	original := accounts.account.Credentials
	for _, credentials := range []map[string]any{
		{"base_url": upstream.URL + "/other-tenant", "api_key": auth},
		{"base_url": upstream.URL, "api_key": "rotated-fixture-key"},
		{"base_url": upstream.URL, "api_key": auth, "header_override_enabled": true, "header_overrides": map[string]any{"x-fixture": "modified"}},
	} {
		accounts.account.Credentials = credentials
		blocked := download("81", byteRange)
		require.Equal(t, http.StatusBadGateway, blocked.Code, blocked.Body.String())
		require.Equal(t, int32(4), requests.Load(), "changed account identity cannot contact the upstream")
	}
	accounts.account.Credentials = original
	proxyTargets := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyTargets <- r.URL.String()
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(mp4)
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(proxyURL.Port())
	require.NoError(t, err)
	proxyID := int64(99)
	accounts.account.ProxyID, accounts.account.Proxy = &proxyID, &service.Proxy{ID: proxyID, Host: proxyURL.Hostname(), Port: port, Protocol: "http"}
	throughProxy := download("81", "")
	require.Equal(t, http.StatusOK, throughProxy.Code, throughProxy.Body.String())
	require.Equal(t, mp4, throughProxy.Body.Bytes())
	require.Len(t, proxyTargets, 1)
	require.Equal(t, upstream.URL+"/v1/videos/proxy-only-id/content", <-proxyTargets)
	require.Equal(t, int32(4), requests.Load(), "the latest proxy must carry the original task request")

	t.Run("redirects broken ranges and excess bytes", func(t *testing.T) {
		accounts.account.ProxyID, accounts.account.Proxy = nil, nil
		t.Cleanup(func() { mode.Store(0) })
		mode.Store(modeRedirect)
		response := download("81", "")
		require.Equal(t, http.StatusBadGateway, response.Code)
		require.Zero(t, redirected.Load())
		require.Empty(t, response.Header().Get("Location"))

		mode.Store(modeInvalid416)
		response = download("81", "bytes=40-50")
		require.Equal(t, http.StatusBadGateway, response.Code)
		require.Empty(t, response.Header().Get("Content-Range"))
		require.NotContains(t, response.Body.String(), "provider-only")
		mode.Store(modeValid416)
		response = download("81", "bytes=40-50")
		require.Equal(t, http.StatusRequestedRangeNotSatisfiable, response.Code)
		require.Equal(t, "bytes */32", response.Header().Get("Content-Range"))
		require.NotContains(t, response.Body.String(), "provider-only")

		mode.Store(modeExcessBytes)
		response = download("81", byteRange)
		require.Equal(t, http.StatusPartialContent, response.Code)
		require.Equal(t, mp4, response.Body.Bytes())
		require.Equal(t, "16", response.Header().Get("Content-Length"))
	})
	t.Run("client cancellation releases the upstream response", func(t *testing.T) {
		mode.Store(modeCancellation)
		t.Cleanup(func() { mode.Store(0) })
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			downloadWithContext(ctx, "81", "")
		}()
		select {
		case <-streamStarted:
			cancel()
		case <-time.After(2 * time.Second):
			t.Fatal("upstream never started the response")
		}
		select {
		case <-streamClosed:
		case <-time.After(2 * time.Second):
			t.Fatal("client cancellation did not close the upstream response")
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("download did not return after cancellation")
		}
		mode.Store(0)
		retry := download("81", "")
		require.Equal(t, http.StatusOK, retry.Code)
		require.Equal(t, mp4, retry.Body.Bytes())
		require.Equal(t, service.VideoRelayCharged, store.task.BillingState)
	})
	t.Run("only the validated customer range reaches the upstream", func(t *testing.T) {
		accounts.account.ProxyID, accounts.account.Proxy = nil, nil
		const overriddenRange = "bytes=7-8"
		accounts.account.Credentials = map[string]any{
			"base_url": upstream.URL, "api_key": auth, "header_override_enabled": true,
			"header_overrides": map[string]any{"rAnGe": overriddenRange},
		}
		store.task.RouteFingerprint = fixtureVideoRelayHash(site, credential, "range", "1", overriddenRange)
		captureRange.Store(true)
		t.Cleanup(func() { captureRange.Store(false) })

		full := download("81", "")
		require.Equal(t, http.StatusOK, full.Code)
		require.Empty(t, <-observedRanges)
		partial := download("81", byteRange)
		require.Equal(t, http.StatusPartialContent, partial.Code)
		require.Equal(t, []string{byteRange}, <-observedRanges)
	})
}
