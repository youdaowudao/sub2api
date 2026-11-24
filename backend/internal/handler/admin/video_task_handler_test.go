//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminVideoTaskStore struct {
	service.VideoRelayRepository
	filter        service.VideoRelayAdminFilter
	offset, limit int
}

func (repo *adminVideoTaskStore) ListAdmin(_ context.Context, filter service.VideoRelayAdminFilter, offset, limit int) ([]service.VideoRelayAdminRow, int64, error) {
	repo.filter, repo.offset, repo.limit = filter, offset, limit
	return []service.VideoRelayAdminRow{}, 501, nil
}

func TestAdminVideoTasksFiltersAndPageLimit(t *testing.T) {
	const task = "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := &adminVideoTaskStore{}
	router := gin.New()
	router.GET("/video-tasks", NewVideoTaskHandler(repo).List)
	for _, query := range []string{"status=queued", "user_id=0", "user_id=", "task_id=bad", "risk_only=1", "risk_only="} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/video-tasks?"+query, nil))
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Zero(t, repo.limit, "invalid filters must be rejected before a database read")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/video-tasks?status=failed&user_id=42&risk_only=true&page=2&page_size=500&task_id="+task, nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, service.VideoRelayAdminFilter{UserID: 42, TaskID: task, Status: "failed", RiskOnly: true}, repo.filter)
	require.Equal(t, 200, repo.offset)
	require.Equal(t, 200, repo.limit)
	require.Contains(t, response.Body.String(), `"total":501`)
}
