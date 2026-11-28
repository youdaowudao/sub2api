//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayRepoUsageRecoveryAfterLostSubmit(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 0.5)
	other := newVideoRelayTask(t, 10, 0)
	providerHasRequest := make(chan struct{})
	releaseProvider := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseProvider) })
	defer release()
	var posts, statusReads, downloads atomic.Int32
	media := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 8, 8, 8, 8}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v1/videos":
			if posts.Add(1) == 1 {
				close(providerHasRequest)
				select {
				case <-releaseProvider:
				case <-req.Context().Done():
					return
				}
				_, _ = io.WriteString(w, `{"id":"audit-recovered-video","status":"queued"}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"audit-refunded-video","status":"failed"}`)
		case req.Method == http.MethodGet && req.URL.Path == "/v1/videos/audit-recovered-video":
			statusReads.Add(1)
			_, _ = io.WriteString(w, `{"id":"audit-recovered-video","status":"completed","delivery_status":"ready"}`)
		case req.Method == http.MethodGet && req.URL.Path == "/v1/videos/audit-recovered-video/content":
			downloads.Add(1)
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(media)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(func() {
		release()
		provider.Close()
	})
	setupVideoRelayAuditRoute(t, task, provider.URL, "audit-recovery-provider")
	ent := testEntClient(t)
	key, err := NewAPIKeyRepository(ent, integrationDB).GetByID(ctx, task.APIKeyID)
	require.NoError(t, err)
	wrongKey := mustCreateApiKey(t, ent, &service.APIKey{
		UserID: task.UserID, GroupID: &task.GroupID, Key: "audit-recovery-wrong-key-" + task.TaskID,
	})

	oldDB := videoRelayIndependentDB(t)
	old := newVideoRelayAuditRouter(t, oldDB, NewVideoRelayRepository(oldDB))
	finished := make(chan struct{}, 1)
	downstreamClosed := make(chan struct{})
	oldHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		context.AfterFunc(req.Context(), func() { close(downstreamClosed) })
		old.ServeHTTP(w, req)
		finished <- struct{}{}
	}))
	t.Cleanup(oldHTTP.Close)
	clientCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const body = `{"model":"video-public","prompt":"recover","seconds":5,"resolution":"480p"}`
	submit, err := http.NewRequestWithContext(clientCtx, http.MethodPost, oldHTTP.URL+"/v1/videos", strings.NewReader(body))
	require.NoError(t, err)
	submit.Header.Set("Content-Type", "application/json")
	submit.Header.Set("Authorization", "Bearer "+key.Key)
	clientResult := make(chan error, 1)
	go func() {
		response, err := oldHTTP.Client().Do(submit)
		if response != nil {
			_ = response.Body.Close()
		}
		clientResult <- err
	}()
	select {
	case <-providerHasRequest:
	case <-time.After(5 * time.Second):
		t.Fatal("the normal video route did not send the first request")
	}
	cancel()
	select {
	case err := <-clientResult:
		require.ErrorIs(t, err, context.Canceled, "the client never receives the task number")
	case <-time.After(5 * time.Second):
		t.Fatal("the test client did not disconnect")
	}
	select {
	case <-downstreamClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("the original gateway did not observe the disconnect")
	}
	release()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the old gateway did not finish the in-flight submission")
	}
	oldHTTP.Close()
	require.NoError(t, oldDB.Close())

	freshDB := videoRelayIndependentDB(t)
	var upstreamID string
	require.NoError(t, freshDB.QueryRowContext(ctx, `
		SELECT task_id, upstream_task_id FROM video_relay_tasks WHERE user_id=$1 AND api_key_id=$2`,
		task.UserID, task.APIKeyID).Scan(&task.TaskID, &upstreamID))
	require.Equal(t, "audit-recovered-video", upstreamID, "the in-flight submit must store the provider result after client disconnect")
	require.EqualValues(t, 1, posts.Load())
	assertVideoRelayFunds(t, freshDB, task, 9.5, 0.5, 0)
	repo := NewVideoRelayRepository(freshDB)
	gateway := newTrackingVideoRelayFixtureGateway(t, NewAccountRepository(ent, freshDB, nil), trackingVideoRelayFixtureOptions{})
	tracker := service.NewVideoRelayTracker(service.NewVideoRelayService(repo, gateway, nil))
	makeVideoRelayTaskDue(t, freshDB, task.TaskID)
	claim, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, task.TaskID, claim.TaskID)
	tracker.ProcessClaim(ctx, claim)
	require.EqualValues(t, 1, statusReads.Load())
	require.EqualValues(t, 1, posts.Load())
	assertVideoRelayFunds(t, freshDB, task, 9.5, 0, 1)

	router := newVideoRelayAuditRouter(t, freshDB, repo)
	failed := videoRelayAuditRequest(router, key.Key, http.MethodPost, "/v1/videos", body)
	require.Equal(t, http.StatusOK, failed.Code, failed.Body.String())
	var rejected struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(failed.Body.Bytes(), &rejected))
	require.True(t, service.ValidVideoRelayTaskID(rejected.ID))
	require.Equal(t, service.VideoRelayStatusFailed, rejected.Status)
	refunded := task
	refunded.TaskID = rejected.ID
	assertVideoRelayFunds(t, freshDB, refunded, 9.5, 0, 0)

	users := NewUserRepository(ent, freshDB)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "isolated-video-recovery-signature", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	usersService := service.NewUserService(users, nil, nil, nil)
	usageRepo := NewUsageLogRepository(ent, freshDB)
	textBillingMode := string(service.BillingModeToken)
	textRequestID := "audit-text-" + task.TaskID
	created, err := usageRepo.Create(ctx, &service.UsageLog{
		UserID: task.UserID, APIKeyID: task.APIKeyID, AccountID: task.AccountID, RequestID: textRequestID,
		Model: "audit-text-model", BillingMode: &textBillingMode, CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	require.True(t, created)
	usage := handler.NewUsageHandler(service.NewUsageService(usageRepo, nil, nil, nil), nil, nil, nil, nil)
	routes.RegisterUserRoutes(router.Group("/api/v1"), &handler.Handlers{Usage: usage},
		middleware.NewJWTAuthMiddleware(auth, usersService, nil, nil), middleware.NewAuditLogMiddleware(nil),
		nil, middleware.NewPanelRateLimiter(nil, nil))
	type usageItem struct {
		RequestID   string `json:"request_id"`
		Model       string `json:"model"`
		BillingMode string `json:"billing_mode"`
	}
	getUsage := func(userID int64, query string) []usageItem {
		t.Helper()
		user, err := users.GetByID(ctx, userID)
		require.NoError(t, err)
		token, err := auth.GenerateToken(ctx, user)
		require.NoError(t, err)
		result := videoRelayAuditRequest(router, token, http.MethodGet, "/api/v1/usage"+query, "")
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		require.NotContains(t, result.Body.String(), task.UpstreamModel)
		var response struct {
			Data struct {
				Items []usageItem `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
		return response.Data.Items
	}
	const videoUsageQuery = "?billing_mode=video"
	videoItem := usageItem{RequestID: task.UsageRequestID(), Model: task.Model, BillingMode: string(service.BillingModeVideo)}
	owned := getUsage(task.UserID, videoUsageQuery)
	require.Len(t, owned, 1, "the ordinary and refunded orders must stay out of successful video usage")
	require.Equal(t, videoItem, owned[0], "the owner must recover the missing task number through the real usage endpoint")
	require.ElementsMatch(t, []usageItem{videoItem, {RequestID: textRequestID, Model: "audit-text-model", BillingMode: textBillingMode}},
		getUsage(task.UserID, ""), "the same user's ordinary usage must be present in the unfiltered list")
	require.Empty(t, getUsage(other.UserID, videoUsageQuery))
	recoveredID, ok := strings.CutPrefix(owned[0].RequestID, "video_relay:")
	require.True(t, ok)
	require.Equal(t, task.TaskID, recoveredID)
	models := videoRelayAuditRequest(router, key.Key, http.MethodGet, "/v1/models", "")
	require.Equal(t, http.StatusOK, models.Code, models.Body.String())
	require.Contains(t, models.Body.String(), task.Model)
	require.NotContains(t, models.Body.String(), task.UpstreamModel)
	location := "/v1/videos/" + recoveredID + "/content"
	denied := videoRelayAuditRequest(router, wrongKey.Key, http.MethodGet, location, "")
	require.Equal(t, http.StatusNotFound, denied.Code)
	require.Zero(t, downloads.Load())
	download := videoRelayAuditRequest(router, key.Key, http.MethodGet, location, "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.Equal(t, media, download.Body.Bytes())
	require.EqualValues(t, 1, downloads.Load())
	assertVideoRelayFunds(t, freshDB, task, 9.5, 0, 1)
}
