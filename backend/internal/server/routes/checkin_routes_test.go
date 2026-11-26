//go:build unit

package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCheckinRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerCheckinUserRoutes(r.Group("/api/v1/user"), &handler.CheckinHandler{})
	registerCheckinAdminRoutes(r.Group("/api/v1/admin"), &handler.Handlers{Admin: &handler.AdminHandlers{Checkin: &admin.CheckinHandler{}}})
	got := map[string]bool{}
	for _, route := range r.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	expected := []string{"GET /api/v1/user/checkin", "GET /api/v1/user/checkin/records", "POST /api/v1/user/checkin", "POST /api/v1/user/checkin/email-code", "GET /api/v1/admin/checkin/config", "PUT /api/v1/admin/checkin/config", "GET /api/v1/admin/checkin/overview", "GET /api/v1/admin/checkin/users", "GET /api/v1/admin/checkin/records", "GET /api/v1/admin/checkin/ips"}
	require.Len(t, got, len(expected))
	for _, key := range expected {
		require.True(t, got[key], key)
	}
}
