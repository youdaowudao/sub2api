//go:build integration

package repository

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type videoRelayAdminSettingProbe struct {
	service.SettingRepository
	panelReads, bindingReads int
}

type videoRelayAdminListProbe struct {
	service.VideoRelayRepository
	reads int
}

func (r *videoRelayAdminListProbe) ListAdmin(ctx context.Context, filter service.VideoRelayAdminFilter, offset, limit int) ([]service.VideoRelayAdminRow, int64, error) {
	r.reads++
	return r.VideoRelayRepository.ListAdmin(ctx, filter, offset, limit)
}

func (r *videoRelayAdminSettingProbe) GetValue(ctx context.Context, key string) (string, error) {
	switch key {
	case service.SettingKeyPanelRateLimitSettings:
		r.panelReads++
	case service.SettingKeySessionBindingEnabled:
		r.bindingReads++
	}
	return r.SettingRepository.GetValue(ctx, key)
}

func TestVideoRelayRepoVideoTaskListUsesOnlyNecessaryAdminChecks(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	client := testEntClient(t)
	operator := mustCreateUser(t, client, &service.User{
		Email: "video-admin-cost-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin,
	})
	probe := &videoRelayAdminSettingProbe{SettingRepository: NewSettingRepository(client)}
	require.NoError(t, probe.Set(ctx, service.SettingKeyPanelRateLimitSettings,
		`{"enabled":true,"user_rpm":1,"exempt_admin":false}`))
	require.NoError(t, probe.Delete(ctx, service.SettingKeySessionBindingEnabled))
	t.Cleanup(func() {
		require.NoError(t, probe.Delete(ctx, service.SettingKeyPanelRateLimitSettings))
		require.NoError(t, probe.Delete(ctx, service.SettingKeySessionBindingEnabled))
	})
	settings := service.NewSettingService(probe, nil)
	users := service.NewUserService(NewUserRepository(client, integrationDB), nil, nil, nil)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "isolated-fixture-signing-value", ExpireHour: 1}}
	authService := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	auth := middleware.NewAdminAuthMiddleware(authService, users, settings, nil)
	redisClient := testRedis(t)
	router := gin.New()
	pass := func(c *gin.Context) { c.Next() }
	list := &videoRelayAdminListProbe{VideoRelayRepository: repo}
	routes.RegisterAdminRoutes(router.Group("/api/v1"), &handler.Handlers{
		Admin: &handler.AdminHandlers{VideoTask: admin.NewVideoTaskHandler(list)},
	}, auth, middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), settings, middleware.NewPanelRateLimiter(redisClient, settings))
	request := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "different-fixture-browser")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	bound := service.WithSessionBinding(ctx, &service.SessionBinding{IP: "192.0.2.10", UserAgent: "original-fixture-browser"})
	token, err := authService.GenerateToken(bound, operator)
	require.NoError(t, err)
	legacy, err := authService.GenerateToken(ctx, operator)
	require.NoError(t, err)
	videoURL := "/api/v1/admin/video-tasks?task_id=" + task.TaskID
	bucket := fmt.Sprintf("rate_limit:panel:global:user:%d", operator.ID)
	resp := request(videoURL, token)
	require.Equal(t, http.StatusOK, resp.Code, "video list must work without a compliance acknowledgement")
	require.Contains(t, resp.Body.String(), task.TaskID)
	require.Zero(t, probe.panelReads)
	require.Zero(t, probe.bindingReads)
	require.ErrorIs(t, redisClient.Get(ctx, bucket).Err(), redis.Nil)
	require.Equal(t, http.StatusLocked, request("/api/v1/admin/settings", token).Code)
	require.Equal(t, 1, probe.panelReads)
	require.Equal(t, "1", redisClient.Get(ctx, bucket).Val())
	require.Equal(t, http.StatusTooManyRequests, request("/api/v1/admin/settings", token).Code)
	require.Equal(t, "2", redisClient.Get(ctx, bucket).Val())
	require.Equal(t, http.StatusOK, request(videoURL, token).Code)
	require.Equal(t, 1, probe.panelReads)
	require.Equal(t, "2", redisClient.Get(ctx, bucket).Val())

	require.NoError(t, probe.Set(ctx, service.SettingKeySessionBindingEnabled, "true"))
	require.Equal(t, http.StatusUnauthorized, request(videoURL, token).Code)
	require.Equal(t, http.StatusOK, request(videoURL, legacy).Code)
	require.NoError(t, probe.Set(ctx, service.SettingKeySessionBindingEnabled, "false"))
	require.Equal(t, http.StatusOK, request(videoURL, token).Code)
	require.NoError(t, probe.Delete(ctx, service.SettingKeySessionBindingEnabled))
	require.Equal(t, http.StatusOK, request(videoURL, token).Code)
	// Only the two requests to the existing settings route read the old
	// standalone setting. The video route uses the snapshot with the user.
	require.Equal(t, 2, probe.bindingReads)

	reads := list.reads
	require.NoError(t, client.User.UpdateOneID(operator.ID).SetStatus(service.StatusDisabled).Exec(ctx))
	disabled := request(videoURL, token)
	require.Equal(t, http.StatusUnauthorized, disabled.Code)
	require.Contains(t, disabled.Body.String(), "USER_INACTIVE")
	require.NotContains(t, disabled.Body.String(), task.TaskID)
	require.Equal(t, reads, list.reads, "a disabled administrator must not reach the video list query")
	require.NoError(t, client.User.UpdateOneID(operator.ID).SetStatus(service.StatusActive).Exec(ctx))
	require.Equal(t, http.StatusOK, request(videoURL, token).Code)
	reads = list.reads
	require.NoError(t, client.User.UpdateOneID(operator.ID).AddTokenVersion(1).Exec(ctx))
	revoked := request(videoURL, token)
	require.Equal(t, http.StatusUnauthorized, revoked.Code)
	require.Contains(t, revoked.Body.String(), "TOKEN_REVOKED")
	require.NotContains(t, revoked.Body.String(), task.TaskID)
	require.Equal(t, reads, list.reads, "a revoked token must not reach the video list query")
	assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)
}
