//go:build integration

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayRepoUsageRecoversOwnedChargedTask(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	other := newVideoRelayTask(t, 10, 5)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	sent, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	accepted, err := repo.RecordSubmission(ctx, sent, "fixture-recovered-request", "fixture-recovered-provider")
	require.NoError(t, err)
	_, _, err = repo.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true})
	require.NoError(t, err)
	apiKeys := NewAPIKeyRepository(testEntClient(t), integrationDB)
	usage := handler.NewUsageHandler(service.NewUsageService(NewUsageLogRepository(testEntClient(t), integrationDB), nil, nil, nil), nil, nil, nil)
	read := func(userID int64) string {
		t.Helper()
		router := gin.New()
		router.GET("/api/v1/usage", func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
			usage.List(c)
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/usage?billing_mode=video", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		return recorder.Body.String()
	}
	owner := read(task.UserID)
	require.Contains(t, owner, task.UsageRequestID())
	require.NotContains(t, owner, task.UpstreamModel)
	require.NotContains(t, read(other.UserID), task.UsageRequestID())

	relay := service.NewVideoRelayService(repo, nil, nil)
	_, problem := relay.GetOwned(ctx, &service.APIKey{ID: other.APIKeyID, UserID: other.UserID}, task.TaskID)
	require.NotNil(t, problem)
	require.Equal(t, http.StatusNotFound, problem.Status)
	_, problem = relay.GetOwned(ctx, &service.APIKey{ID: other.APIKeyID, UserID: task.UserID}, task.TaskID)
	require.NotNil(t, problem)
	require.Equal(t, http.StatusNotFound, problem.Status)

	require.NoError(t, apiKeys.Delete(ctx, task.APIKeyID))
	_, err = apiKeys.GetByID(ctx, task.APIKeyID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.Contains(t, read(task.UserID), task.UsageRequestID())
	assertVideoRelayFunds(t, integrationDB, task, 5, 0, 1)
}
