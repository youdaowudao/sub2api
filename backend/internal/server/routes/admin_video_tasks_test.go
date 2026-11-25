//go:build unit

package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminVideoTasksUsesReadOnlyFullAdminRoute(t *testing.T) {
	router := gin.New()
	next := func(c *gin.Context) { c.Next() }
	RegisterAdminRoutes(router.Group("/api/v1"), &handler.Handlers{Admin: &handler.AdminHandlers{}},
		middleware.AdminAuthMiddleware(next), middleware.AuditLogMiddleware(next),
		middleware.StepUpAuthMiddleware(next), nil, nil)
	const path = "/api/v1/admin/video-tasks"
	var methods []string
	for _, route := range router.Routes() {
		if route.Path == path {
			methods = append(methods, route.Method)
		}
	}
	require.Equal(t, []string{http.MethodGet}, methods)
	require.True(t, adminaccess.RouteAllowed(adminaccess.Policy{FullAccess: true}, http.MethodGet, path))
	require.False(t, adminaccess.RouteAllowed(adminaccess.Policy{}, http.MethodGet, path))
	require.False(t, adminaccess.RouteAllowed(adminaccess.Policy{GlobalPermissions: []string{"usage.read"}}, http.MethodGet, path))
}
