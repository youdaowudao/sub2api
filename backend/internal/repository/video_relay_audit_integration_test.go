//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func setupVideoRelayAuditRoute(t *testing.T, task service.VideoRelayTask, upstreamURL, fixtureKey string) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	accounts := NewAccountRepository(client, integrationDB, nil)
	account, err := accounts.GetByID(ctx, task.AccountID)
	require.NoError(t, err)
	account.Credentials = map[string]any{
		"base_url": upstreamURL, "api_key": fixtureKey, "openai_capabilities": []string{"videos"},
		"openai_videos": map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "default_seconds": 5, "max_seconds": 30},
		"model_mapping": map[string]any{task.Model: task.UpstreamModel},
	}
	require.NoError(t, accounts.Update(ctx, account))
	require.NoError(t, accounts.BindGroups(ctx, account.ID, []int64{task.GroupID}))
	groups := NewGroupRepository(client, integrationDB)
	group, err := groups.GetByID(ctx, task.GroupID)
	require.NoError(t, err)
	group.RateMultiplier = 1
	group.AllowImageGeneration = true
	group.VideoRateIndependent = true
	group.VideoRateMultiplier = 1
	pricePerSecond := 0.1
	group.ModelPricing = []service.ChannelModelPricing{{
		Platform: service.PlatformOpenAI, Models: []string{task.Model}, BillingMode: service.BillingModeVideo, PerRequestPrice: &pricePerSecond,
	}}
	require.NoError(t, groups.Update(ctx, group))
}

func newVideoRelayAuditRouter(t *testing.T, db *sql.DB, repo service.VideoRelayRepository, upstream ...service.HTTPUpstream) *gin.Engine {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxBodySize = 64 * 1024
	cfg.Gateway.TextMaxBodySize = 64 * 1024
	client := testEntClient(t)
	accounts := NewAccountRepository(client, db, nil)
	users := NewUserRepository(client, db)
	groups := NewGroupRepository(client, db)
	keyRepo := NewAPIKeyRepository(client, db)
	keys := service.NewAPIKeyService(keyRepo, users, groups, nil, nil, nil, cfg)
	concurrency := service.NewConcurrencyService(NewConcurrencyCache(testRedis(t), 1, 10))
	cache := service.NewBillingCacheService(NewBillingCache(testRedis(t)), users, nil, keyRepo, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	billing := service.NewBillingService(cfg, nil)
	transport := NewHTTPUpstream(cfg)
	if len(upstream) != 0 {
		transport = upstream[0]
	}
	openAI := service.NewOpenAIGatewayService(accounts, nil, nil, users, nil, nil, nil, cfg, nil, concurrency,
		billing, nil, cache, transport, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil)
	gateway := handler.NewOpenAIGatewayHandler(openAI, concurrency, cache, keys, nil, nil, nil, nil, cfg)
	catalog := service.NewGatewayService(accounts, groups, nil, nil, users, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, cache, nil, transport, nil, nil, nil, nil, nil, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil)
	models := handler.NewGatewayHandler(catalog, openAI, nil, nil, nil, concurrency, cache, nil, keys, nil, nil, nil, nil, cfg, nil)
	relay := service.NewVideoRelayService(repo, openAI, keys)
	router := gin.New()
	routes.RegisterGatewayRoutes(router,
		&handler.Handlers{Gateway: models, OpenAIGateway: gateway, VideoRelay: handler.NewVideoRelayHandler(relay, gateway)},
		middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
	return router
}

func videoRelayAuditRequest(router http.Handler, key, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

type videoRelayConcurrentAuditReserves struct {
	service.VideoRelayRepository
	arrivals *atomic.Int32
	both     chan struct{}
}

func (r videoRelayConcurrentAuditReserves) Reserve(ctx context.Context, task *service.VideoRelayTask) error {
	if r.arrivals.Add(1) == 2 {
		close(r.both)
	}
	select {
	case <-r.both:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("both requested video routes must reach the real balance transaction")
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.VideoRelayRepository.Reserve(ctx, task)
}

func TestVideoRelayRepoConcurrentGatewayBalanceAllowsOnePost(t *testing.T) {
	ctx := context.Background()
	first := newVideoRelayTask(t, 0.5, 0.5)
	second := newVideoRelayTask(t, 10, 0.5)
	var posts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/videos" {
			http.NotFound(w, req)
			return
		}
		id := posts.Add(1)
		_, _ = fmt.Fprintf(w, `{"id":"audit-video-%d","status":"queued"}`, id)
	}))
	t.Cleanup(provider.Close)
	setupVideoRelayAuditRoute(t, first, provider.URL, "audit-first-provider")
	setupVideoRelayAuditRoute(t, second, provider.URL, "audit-second-provider")
	client := testEntClient(t)
	keys := NewAPIKeyRepository(client, integrationDB)
	firstKey, err := keys.GetByID(ctx, first.APIKeyID)
	require.NoError(t, err)
	secondKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: first.UserID, GroupID: &second.GroupID, Key: "audit-second-" + first.TaskID,
	})
	secondDB := videoRelayIndependentDB(t)
	databases := []*sql.DB{integrationDB, secondDB}
	tokens := []string{firstKey.Key, secondKey.Key}
	wallet := NewBillingCache(testRedis(t))
	require.NoError(t, wallet.SetUserBalance(ctx, first.UserID, 0.5))
	both := make(chan struct{})
	var arrivals atomic.Int32
	responses := make(chan *httptest.ResponseRecorder, 2)
	const body = `{"model":"video-public","prompt":"workbench","seconds":5,"resolution":"480p"}`
	for i, database := range databases {
		router := newVideoRelayAuditRouter(t, database, videoRelayConcurrentAuditReserves{
			VideoRelayRepository: NewVideoRelayRepository(database), arrivals: &arrivals, both: both,
		})
		go func() {
			responses <- videoRelayAuditRequest(router, tokens[i], http.MethodPost, "/v1/videos", body)
		}()
	}
	a, b := <-responses, <-responses
	require.EqualValues(t, 2, arrivals.Load(), "two individually eligible lines must enter the balance transactions")
	require.EqualValues(t, 1, posts.Load(), "one available balance may fund just one real upstream POST")
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusPaymentRequired}, []int{a.Code, b.Code})
	var count int
	var balance, frozen float64
	require.NoError(t, secondDB.QueryRowContext(ctx, "SELECT count(*) FROM video_relay_tasks WHERE user_id=$1", first.UserID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, secondDB.QueryRowContext(ctx, "SELECT balance,frozen_balance FROM users WHERE id=$1", first.UserID).Scan(&balance, &frozen))
	require.InDelta(t, 0, balance, 1e-8)
	require.InDelta(t, 0.5, frozen, 1e-8)
}

func TestVideoRelayRepoRedisUnavailablePreservesOriginalOrder(t *testing.T) {
	ctx := context.Background()
	var gets atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/v1/videos/tracking-fixture-video" {
			http.NotFound(w, req)
			return
		}
		gets.Add(1)
		_, _ = io.WriteString(w, `{"id":"tracking-fixture-video","status":"completed","delivery_status":"ready"}`)
	}))
	t.Cleanup(provider.Close)
	redisDir, err := os.MkdirTemp("", "relay-redis-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.Remove(redisDir)) })
	disconnectedRedis := redis.NewClient(&redis.Options{
		Network: "unix", Addr: filepath.Join(redisDir, "unavailable.sock"),
		MaxRetries: -1, DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { _ = disconnectedRedis.Close() })
	require.ErrorIs(t, disconnectedRedis.Ping(ctx).Err(), os.ErrNotExist, "the actual isolated Redis connection must fail")
	task, repo, old := newTrackingVideoRelayFixture(t, provider.URL, time.Minute, trackingVideoRelayFixtureOptions{
		concurrency: service.NewConcurrencyService(NewConcurrencyCache(disconnectedRedis, 1, 10)),
	})
	claim, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, task.TaskID, claim.TaskID)
	old.ProcessClaim(ctx, claim)
	reconnected := videoRelayIndependentDB(t)
	recoveredRepo := NewVideoRelayRepository(reconnected)
	unchanged, err := recoveredRepo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Zero(t, gets.Load(), "a Redis failure must not send an unchecked upstream request")
	require.Equal(t, claim.UpstreamTaskID, unchanged.UpstreamTaskID)
	require.Equal(t, service.VideoRelayHeld, unchanged.BillingState)
	assertVideoRelayFunds(t, reconnected, task, 5, 5, 0)

	require.NoError(t, testRedis(t).Ping(ctx).Err())
	freshGateway := newTrackingVideoRelayFixtureGateway(t, NewAccountRepository(testEntClient(t), reconnected, nil), trackingVideoRelayFixtureOptions{})
	fresh := service.NewVideoRelayTracker(service.NewVideoRelayService(recoveredRepo, freshGateway, nil))
	makeVideoRelayTaskDue(t, reconnected, task.TaskID)
	resumed, err := recoveredRepo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, resumed)
	require.Equal(t, task.TaskID, resumed.TaskID)
	fresh.ProcessClaim(ctx, resumed)
	require.EqualValues(t, 1, gets.Load())
	final, err := recoveredRepo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayCharged, final.BillingState)
	require.Equal(t, claim.UpstreamTaskID, final.UpstreamTaskID)
	assertVideoRelayFunds(t, reconnected, task, 5, 0, 1)
}

func TestVideoRelayRepoReopenedGatewayKeepsDownloadAndOwnership(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 0.5)
	const receivedSize, rangeEnd = 32, 55
	resumeRange := fmt.Sprintf("bytes=%d-%d", receivedSize, rangeEnd)
	clip := make([]byte, 128*1024)
	for offset := 0; offset < len(clip); offset += 4 {
		binary.LittleEndian.PutUint32(clip[offset:], uint32(offset))
	}
	copy(clip, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	upstreamHeaders := []string{"X-SL-Test", "X-New-Api-Test", "X-Oneapi-Request-Id", "CF-Cache-Status", "Nel", "Report-To", "Location"}
	assertHeaders := func(header http.Header) {
		t.Helper()
		for _, name := range upstreamHeaders {
			require.Empty(t, header.Get(name), "a customer response cannot expose header %s", name)
		}
	}
	cancelled := make(chan struct{}, 1)
	var posts, contentRequests, statusRequests atomic.Int32
	const privateMediaURL = "https://private-fixture.invalid/exclusive-file.mp4"
	writeProviderStatus := func(w http.ResponseWriter, status, delivery string) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "audit-reopen-video", "status": status, "delivery_status": delivery,
			"model": task.UpstreamModel, "url": privateMediaURL,
		})
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		for _, name := range upstreamHeaders {
			w.Header().Set(name, "provider-only-header-fixture")
		}
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v1/videos":
			posts.Add(1)
			writeProviderStatus(w, "queued", "")
		case req.Method == http.MethodGet && req.URL.Path == "/v1/videos/audit-reopen-video":
			statusRequests.Add(1)
			writeProviderStatus(w, "completed", "ready")
		case req.Method == http.MethodGet && req.URL.Path == "/v1/videos/audit-reopen-video/content":
			requestNumber := contentRequests.Add(1)
			w.Header().Set("Content-Type", "video/mp4")
			if req.Header.Get("Range") == resumeRange {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", receivedSize, rangeEnd, len(clip)))
				w.Header().Set("Content-Length", strconv.Itoa(rangeEnd-receivedSize+1))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(clip[receivedSize : rangeEnd+1])
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(clip)))
			if requestNumber == 1 {
				_, _ = w.Write(clip[:64*1024])
				w.(http.Flusher).Flush()
				<-req.Context().Done()
				cancelled <- struct{}{}
				return
			}
			_, _ = w.Write(clip)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(provider.Close)
	setupVideoRelayAuditRoute(t, task, provider.URL, "audit-reopen-provider")
	client := testEntClient(t)
	key, err := NewAPIKeyRepository(client, integrationDB).GetByID(ctx, task.APIKeyID)
	require.NoError(t, err)
	sameUser := mustCreateApiKey(t, client, &service.APIKey{
		UserID: task.UserID, GroupID: &task.GroupID, Key: "audit-same-user-" + task.TaskID,
	})
	other := newVideoRelayTask(t, 10, 0)
	otherKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: other.UserID, GroupID: &task.GroupID, Key: "audit-other-owner-" + task.TaskID,
	})
	oldDB := videoRelayIndependentDB(t)
	old := newVideoRelayAuditRouter(t, oldDB, NewVideoRelayRepository(oldDB))
	accepted := videoRelayAuditRequest(old, key.Key, http.MethodPost, "/v1/videos",
		`{"model":"video-public","prompt":"reopen","seconds":5,"resolution":"480p"}`)
	assertPublicStatus := func(result *httptest.ResponseRecorder, status string) string {
		t.Helper()
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		var output map[string]any
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &output))
		require.ElementsMatch(t, []string{"id", "object", "model", "status", "progress", "created_at", "completed_at", "seconds", "resolution", "error"},
			slices.Collect(maps.Keys(output)))
		id, ok := output["id"].(string)
		require.True(t, ok)
		require.True(t, service.ValidVideoRelayTaskID(id))
		require.Equal(t, status, output["status"])
		require.Equal(t, task.Model, output["model"])
		for _, secret := range []string{"audit-reopen-video", provider.URL, privateMediaURL, task.UpstreamModel} {
			require.NotContains(t, result.Body.String(), secret)
		}
		assertHeaders(result.Header())
		return id
	}
	task.TaskID = assertPublicStatus(accepted, service.VideoRelayStatusQueued)
	assertVideoRelayFunds(t, integrationDB, task, 9.5, 0.5, 0)
	taskPath := "/v1/videos/" + task.TaskID
	for _, foreignKey := range []string{sameUser.Key, otherKey.Key} {
		foreign := videoRelayAuditRequest(old, foreignKey, http.MethodGet, taskPath, "")
		require.Equal(t, http.StatusNotFound, foreign.Code, "a valid key with different task ownership cannot query")
	}
	require.Zero(t, statusRequests.Load(), "foreign keys must make zero upstream status requests for a pending order")
	completed := videoRelayAuditRequest(old, key.Key, http.MethodGet, taskPath, "")
	require.Equal(t, task.TaskID, assertPublicStatus(completed, service.VideoRelayStatusCompleted))
	require.EqualValues(t, 1, statusRequests.Load(), "the original key must receive the real upstream status before settlement")
	assertVideoRelayFunds(t, integrationDB, task, 9.5, 0, 1)
	contentPath := taskPath + "/content"
	oldServer := httptest.NewServer(old)
	t.Cleanup(oldServer.Close)
	stream, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(stream, http.MethodGet, oldServer.URL+contentPath, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key.Key)
	partial, err := oldServer.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, partial.StatusCode)
	assertHeaders(partial.Header)
	received := make([]byte, receivedSize)
	_, err = io.ReadFull(partial.Body, received)
	require.NoError(t, err)
	require.Equal(t, clip[:receivedSize], received)
	cancel()
	require.NoError(t, partial.Body.Close())
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider never observed the old gateway's interrupted download")
	}
	oldServer.Close()
	require.NoError(t, oldDB.Close())

	freshDB := videoRelayIndependentDB(t)
	fresh := newVideoRelayAuditRouter(t, freshDB, NewVideoRelayRepository(freshDB))
	for _, foreignKey := range []string{sameUser.Key, otherKey.Key} {
		foreignLookup := videoRelayAuditRequest(fresh, foreignKey, http.MethodGet, "/v1/videos/"+task.TaskID, "")
		require.Equal(t, http.StatusNotFound, foreignLookup.Code, "a valid key with different task ownership cannot query")
		foreignDownload := videoRelayAuditRequest(fresh, foreignKey, http.MethodGet, contentPath, "")
		require.Equal(t, http.StatusNotFound, foreignDownload.Code, "a valid key with different task ownership cannot download")
	}
	require.EqualValues(t, 1, statusRequests.Load())
	require.EqualValues(t, 1, contentRequests.Load(), "foreign keys must make zero upstream downloads")

	owned := videoRelayAuditRequest(fresh, key.Key, http.MethodGet, "/v1/videos/"+task.TaskID, "")
	require.Equal(t, http.StatusOK, owned.Code, owned.Body.String())
	require.Contains(t, owned.Body.String(), service.VideoRelayStatusCompleted)
	assertHeaders(owned.Header())
	resume := httptest.NewRequest(http.MethodGet, contentPath, nil)
	resume.Header.Set("Authorization", "Bearer "+key.Key)
	resume.Header.Set("Range", resumeRange)
	resumed := httptest.NewRecorder()
	fresh.ServeHTTP(resumed, resume)
	require.Equal(t, http.StatusPartialContent, resumed.Code, "the reopened gateway must resume a valid single range")
	require.Equal(t, fmt.Sprintf("bytes %d-%d/%d", receivedSize, rangeEnd, len(clip)), resumed.Header().Get("Content-Range"))
	require.Equal(t, clip[receivedSize:rangeEnd+1], resumed.Body.Bytes())
	assertHeaders(resumed.Header())
	full := videoRelayAuditRequest(fresh, key.Key, http.MethodGet, contentPath, "")
	require.Equal(t, http.StatusOK, full.Code, full.Body.String())
	require.Equal(t, clip, full.Body.Bytes())
	require.Equal(t, full.Body.Bytes()[receivedSize:rangeEnd+1], resumed.Body.Bytes())
	assertHeaders(full.Header())
	require.EqualValues(t, 3, contentRequests.Load())
	require.EqualValues(t, 1, posts.Load())
	assertVideoRelayFunds(t, freshDB, task, 9.5, 0, 1)
}

func TestVideoRelayRepoPriceSnapshotAndSubscriptionGuard(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 1)
	var posts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v1/videos":
			id := posts.Add(1)
			_, _ = fmt.Fprintf(w, `{"id":"audit-price-%d","status":"queued"}`, id)
		case req.Method == http.MethodGet && req.URL.Path == "/v1/videos/audit-price-1":
			_, _ = io.WriteString(w, `{"id":"audit-price-1","status":"completed","delivery_status":"ready"}`)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(provider.Close)
	setupVideoRelayAuditRoute(t, task, provider.URL, "audit-price-provider")
	client := testEntClient(t)
	groups := NewGroupRepository(client, integrationDB)
	group, err := groups.GetByID(ctx, task.GroupID)
	require.NoError(t, err)
	group.VideoRateMultiplier = 2
	require.NoError(t, groups.Update(ctx, group))
	key, err := NewAPIKeyRepository(client, integrationDB).GetByID(ctx, task.APIKeyID)
	require.NoError(t, err)
	repo := NewVideoRelayRepository(integrationDB)
	router := newVideoRelayAuditRouter(t, integrationDB, repo)
	const body = `{"model":"video-public","prompt":"price snapshot","seconds":5,"resolution":"480p"}`
	accepted := videoRelayAuditRequest(router, key.Key, http.MethodPost, "/v1/videos", body)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	var output struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &output))
	require.True(t, service.ValidVideoRelayTaskID(output.ID))
	task.TaskID = output.ID
	assertVideoRelayFunds(t, integrationDB, task, 9, 1, 0)

	newPrice := 0.4
	group.ModelPricing[0].PerRequestPrice = &newPrice
	group.VideoRateMultiplier = 3
	require.NoError(t, groups.Update(ctx, group))
	freshDB := videoRelayIndependentDB(t)
	router = newVideoRelayAuditRouter(t, freshDB, NewVideoRelayRepository(freshDB))
	status := videoRelayAuditRequest(router, key.Key, http.MethodGet, "/v1/videos/"+task.TaskID, "")
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.Contains(t, status.Body.String(), service.VideoRelayStatusCompleted)
	var balance, actual, total, multiplier float64
	require.NoError(t, freshDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", task.UserID).Scan(&balance))
	require.InDelta(t, 9, balance, 1e-8, "charging after a price change must consume the original frozen amount")
	require.NoError(t, freshDB.QueryRowContext(ctx, `
		SELECT actual_cost,total_cost,rate_multiplier FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`,
		task.UsageRequestID(), task.APIKeyID).Scan(&actual, &total, &multiplier))
	require.InDelta(t, 1, actual, 1e-8)
	require.InDelta(t, 0.5, total, 1e-8)
	require.InDelta(t, 2, multiplier, 1e-8)
	assertVideoRelayFunds(t, freshDB, task, 9, 0, 1)

	group.SubscriptionType = service.SubscriptionTypeSubscription
	require.NoError(t, groups.Update(ctx, group))
	router = newVideoRelayAuditRouter(t, freshDB, NewVideoRelayRepository(freshDB))
	blocked := videoRelayAuditRequest(router, key.Key, http.MethodPost, "/v1/videos", body)
	require.EqualValues(t, 1, posts.Load(), "subscription video requests must make zero upstream POSTs")
	require.Equal(t, http.StatusForbidden, blocked.Code, blocked.Body.String())
	require.Contains(t, blocked.Body.String(), "requires a balance-billed group")
	var orders int
	require.NoError(t, freshDB.QueryRowContext(ctx, "SELECT count(*) FROM video_relay_tasks WHERE user_id=$1", task.UserID).Scan(&orders))
	require.Equal(t, 1, orders)
	assertVideoRelayFunds(t, freshDB, task, 9, 0, 1)
}

func TestVideoRelayRepoCustomerAndTrackerCanFinishTogether(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 0.5)
	var statuses atomic.Int32
	all := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v1/videos":
			_, _ = io.WriteString(w, `{"id":"audit-concurrent-result","status":"queued"}`)
		case req.Method == http.MethodGet && req.URL.Path == "/v1/videos/audit-concurrent-result":
			if statuses.Add(1) == 3 {
				close(all)
			}
			select {
			case <-all:
			case <-time.After(5 * time.Second):
				http.Error(w, "the three original requests did not overlap", http.StatusServiceUnavailable)
				return
			}
			_, _ = io.WriteString(w, `{"id":"audit-concurrent-result","status":"completed","delivery_status":"ready"}`)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(provider.Close)
	setupVideoRelayAuditRoute(t, task, provider.URL, "audit-concurrent-provider")
	ent := testEntClient(t)
	key, err := NewAPIKeyRepository(ent, integrationDB).GetByID(ctx, task.APIKeyID)
	require.NoError(t, err)
	router := newVideoRelayAuditRouter(t, integrationDB, NewVideoRelayRepository(integrationDB))
	submit := videoRelayAuditRequest(router, key.Key, http.MethodPost, "/v1/videos",
		`{"model":"video-public","prompt":"common completion","seconds":5,"resolution":"480p"}`)
	require.Equal(t, http.StatusOK, submit.Code, submit.Body.String())
	var body struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(submit.Body.Bytes(), &body))
	require.True(t, service.ValidVideoRelayTaskID(body.ID))
	task.TaskID = body.ID

	backgroundDB := videoRelayIndependentDB(t)
	repo := NewVideoRelayRepository(backgroundDB)
	gateway := newTrackingVideoRelayFixtureGateway(t, NewAccountRepository(ent, backgroundDB, nil), trackingVideoRelayFixtureOptions{})
	tracker := service.NewVideoRelayTracker(service.NewVideoRelayService(repo, gateway, nil))
	makeVideoRelayTaskDue(t, backgroundDB, task.TaskID)
	claim, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, task.TaskID, claim.TaskID)
	responses := make(chan *httptest.ResponseRecorder, 2)
	done := make(chan struct{})
	go func() {
		tracker.ProcessClaim(ctx, claim)
		close(done)
	}()
	for range 2 {
		go func() {
			responses <- videoRelayAuditRequest(router, key.Key, http.MethodGet, "/v1/videos/"+task.TaskID, "")
		}()
	}
	for range 2 {
		response := <-responses
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), service.VideoRelayStatusCompleted)
	}
	<-done
	require.EqualValues(t, 3, statuses.Load())
	current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayCharged, current.BillingState)
	require.Nil(t, current.NextCheckAt)
	assertVideoRelayFunds(t, backgroundDB, task, 9.5, 0, 1)
}

func TestVideoRelayRepoGatewayHTTP2SubmitBodyCannotReplay(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 0.5)
	setupVideoRelayAuditRoute(t, task, "http://video-transport-fixture.invalid", "audit-transport-provider")
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	var posts atomic.Int32
	peerDone := make(chan error, 1)
	go func() { peerDone <- serveVideoRelayRefusingPeer(serverConn, &posts) }()

	transportCfg := &config.Config{}
	transportCfg.Gateway.OpenAIHTTP2.Enabled = true
	upstream := NewHTTPUpstream(transportCfg).(*httpUpstreamService)
	entry, err := upstream.getClientEntry("", task.AccountID, 3, service.HTTPUpstreamProfileOpenAI, false, false)
	require.NoError(t, err)
	wire := entry.client.Transport.(*http.Transport)
	wire.Protocols.SetHTTP1(false)
	wire.Protocols.SetUnencryptedHTTP2(true)
	wire.DialContext = func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }
	t.Cleanup(wire.CloseIdleConnections)
	key, err := NewAPIKeyRepository(testEntClient(t), integrationDB).GetByID(ctx, task.APIKeyID)
	require.NoError(t, err)
	router := newVideoRelayAuditRouter(t, integrationDB, NewVideoRelayRepository(integrationDB), upstream)
	submit := videoRelayAuditRequest(router, key.Key, http.MethodPost, "/v1/videos",
		`{"model":"video-public","prompt":"transport refusal","seconds":5,"resolution":"480p"}`)
	require.EqualValues(t, 1, posts.Load(), "the actual gateway request must make one HTTP/2 POST after a refused stream")
	require.Equal(t, http.StatusOK, submit.Code, submit.Body.String())
	var response struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(submit.Body.Bytes(), &response))
	require.True(t, service.ValidVideoRelayTaskID(response.ID))
	task.TaskID = response.ID
	assertVideoRelayFunds(t, integrationDB, task, 9.5, 0.5, 0)
	require.NoError(t, clientConn.Close())
	select {
	case err := <-peerDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP/2 peer did not exit after the fixture connection closed")
	}
}

func TestVideoRelayRepoOriginalConfigurationControlsProviderRequests(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 0.5)
	var gets atomic.Int32
	media := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 8, 8, 8, 8}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost && req.URL.Path == "/v1/videos" {
			_, _ = io.WriteString(w, `{"id":"audit-bound-video","status":"queued"}`)
			return
		}
		gets.Add(1)
		http.NotFound(w, req)
	}))
	t.Cleanup(provider.Close)
	setupVideoRelayAuditRoute(t, task, provider.URL, "audit-original-route")
	client := testEntClient(t)
	key, err := NewAPIKeyRepository(client, integrationDB).GetByID(ctx, task.APIKeyID)
	require.NoError(t, err)
	accounts := NewAccountRepository(client, integrationDB, nil)
	account, err := accounts.GetByID(ctx, task.AccountID)
	require.NoError(t, err)
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{"X-Video-Fixture": "original"}
	require.NoError(t, accounts.Update(ctx, account))
	credentials := maps.Clone(account.Credentials)
	repo := NewVideoRelayRepository(integrationDB)
	router := newVideoRelayAuditRouter(t, integrationDB, repo)
	submit := videoRelayAuditRequest(router, key.Key, http.MethodPost, "/v1/videos",
		`{"model":"video-public","prompt":"original configuration","seconds":5,"resolution":"480p"}`)
	require.Equal(t, http.StatusOK, submit.Code, submit.Body.String())
	var body struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(submit.Body.Bytes(), &body))
	require.True(t, service.ValidVideoRelayTaskID(body.ID))
	task.TaskID = body.ID
	lookup := "/v1/videos/" + task.TaskID
	content := lookup + "/content"

	changes := []struct {
		field string
		value any
	}{
		{"base_url", provider.URL + "/changed"},
		{"api_key", "audit-rotated-provider"},
		{"header_overrides", map[string]any{"X-Video-Fixture": "changed"}},
	}
	changeRoute := func(field string, value any) {
		t.Helper()
		account.Credentials = maps.Clone(credentials)
		account.Credentials[field] = value
		require.NoError(t, accounts.Update(ctx, account))
	}
	for _, change := range changes {
		changeRoute(change.field, change.value)
		status := videoRelayAuditRequest(router, key.Key, http.MethodGet, lookup, "")
		require.Equal(t, http.StatusOK, status.Code, status.Body.String())
		require.Contains(t, status.Body.String(), service.VideoRelayStatusQueued)
		require.Zero(t, gets.Load(), "changed original route configuration must make zero status or log GETs")
		_, err = integrationDB.ExecContext(ctx, "UPDATE video_relay_tasks SET upstream_task_id='',last_lookup_at=NULL WHERE task_id=$1", task.TaskID)
		require.NoError(t, err)
		status = videoRelayAuditRequest(router, key.Key, http.MethodGet, lookup, "")
		require.Equal(t, http.StatusOK, status.Code, status.Body.String())
		require.Zero(t, gets.Load(), "changed original route configuration must make zero status or log GETs")
		_, err = integrationDB.ExecContext(ctx, "UPDATE video_relay_tasks SET upstream_task_id='audit-bound-video' WHERE task_id=$1", task.TaskID)
		require.NoError(t, err)
	}
	account.Credentials = maps.Clone(credentials)
	require.NoError(t, accounts.Update(ctx, account))
	accepted, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	_, _, err = repo.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true})
	require.NoError(t, err)
	for _, change := range changes {
		changeRoute(change.field, change.value)
		download := videoRelayAuditRequest(router, key.Key, http.MethodGet, content, "")
		require.Equal(t, http.StatusBadGateway, download.Code, download.Body.String())
		require.Zero(t, gets.Load(), "changed original route configuration must make zero download GETs")
	}

	providerURL, err := url.Parse(provider.URL)
	require.NoError(t, err)
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxyRequests.Add(1)
		if req.URL.Host != providerURL.Host || req.URL.Path != "/v1/videos/audit-bound-video/content" ||
			req.Header.Get("X-Video-Fixture") != "original" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(media)
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(proxyURL.Port())
	require.NoError(t, err)
	proxyRow := mustCreateProxy(t, client, &service.Proxy{
		Name: "audit-route-proxy-" + task.TaskID, Protocol: "http", Host: proxyURL.Hostname(), Port: port,
	})
	account.Credentials = maps.Clone(credentials)
	account.ProxyID = &proxyRow.ID
	require.NoError(t, accounts.Update(ctx, account))
	download := videoRelayAuditRequest(router, key.Key, http.MethodGet, content, "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.Equal(t, media, download.Body.Bytes())
	require.EqualValues(t, 1, proxyRequests.Load())
	require.Zero(t, gets.Load())
	assertVideoRelayFunds(t, integrationDB, task, 9.5, 0, 1)
}
