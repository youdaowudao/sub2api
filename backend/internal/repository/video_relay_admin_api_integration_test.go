//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayRepoAdminEndpointIsReadOnly(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	_, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE video_relay_tasks SET
			upstream_task_id='https://example.invalid/test-private-url',
			upstream_request_id='test-private-request',
			error_message='test-private-body'
		WHERE task_id=$1`, task.TaskID)
	require.NoError(t, err)

	client := testEntClient(t)
	operator := mustCreateUser(t, client, &service.User{
		Email: "video-admin-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin,
	})
	settings := service.NewSettingService(NewSettingRepository(client), nil)
	settings.SetScopedAdminKeyRepository(NewScopedAdminKeyRepository(integrationDB))
	issueKey := func(owner int64, full bool) string {
		t.Helper()
		input := service.ScopedAdminKeyInput{
			Name: "fixture-" + uuid.NewString(), Enabled: true, ConfirmFullAccess: full,
			Policy: adminaccess.Policy{FullAccess: full},
		}
		key, _, err := settings.CreateScopedAdminKey(ctx, input, owner)
		require.NoError(t, err)
		return key
	}
	full := issueKey(operator.ID, true)
	limited := issueKey(operator.ID, false)
	nonAdmin := issueKey(task.UserID, true)
	users := service.NewUserService(NewUserRepository(client, integrationDB), nil, nil, nil)
	auth := middleware.NewAdminAuthMiddleware(nil, users, settings, nil)
	router := gin.New()
	routes.RegisterAdminRoutes(router.Group("/api/v1"), &handler.Handlers{
		Admin: &handler.AdminHandlers{VideoTask: admin.NewVideoTaskHandler(repo)},
	}, auth, middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	path := "/api/v1/admin/video-tasks"
	do := func(method, suffix, key string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path+suffix, nil)
		if key != "" {
			request.Header.Set("x-api-key", key)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	require.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "", "").Code)
	require.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "", nonAdmin).Code)
	require.Equal(t, http.StatusForbidden, do(http.MethodGet, "", limited).Code)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		require.Equal(t, http.StatusNotFound, do(method, "", full).Code)
	}
	for _, param := range []string{"status=queued", "user_id=0", "task_id=invalid", "risk_only=yes"} {
		response := do(http.MethodGet, "?"+param, full)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}

	response := do(http.MethodGet,
		fmt.Sprintf("?status=confirmation&user_id=%d&task_id=%s&page_size=250", task.UserID, task.TaskID), full)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Data struct {
			Items    []map[string]json.RawMessage `json:"items"`
			Total    int                          `json:"total"`
			PageSize int                          `json:"page_size"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 1, envelope.Data.Total)
	require.Equal(t, 200, envelope.Data.PageSize)
	require.Len(t, envelope.Data.Items, 1)
	item := envelope.Data.Items[0]
	require.ElementsMatch(t, []string{
		"task_id", "user_id", "user_email", "group_id", "account_id", "model", "upstream_model", "seconds", "resolution",
		"hold_amount", "status", "billing_state", "failure_reason", "upstream_task_id", "created_at", "settled_at", "risk",
	}, slices.Collect(maps.Keys(item)))
	require.JSONEq(t, `"confirmation"`, string(item["status"]))
	require.JSONEq(t, `""`, string(item["upstream_task_id"]))
	require.NotContains(t, response.Body.String(), "test-private")
	assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)
}
