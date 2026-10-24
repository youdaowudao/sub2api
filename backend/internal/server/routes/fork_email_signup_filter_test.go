package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForkEmailSignupFilterRoutesRejectUnauthenticatedAndNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{PromptAudit: securityaudit.NewPromptAdminHandler(nil)}}
	adminAuth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			servermiddleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
			return
		}
		servermiddleware.AbortWithError(c, http.StatusForbidden, "FORBIDDEN", "Admin access required")
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), h, adminAuth, auditLog, stepUp, nil, nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, authorized := range []bool{false, true} {
			req := httptest.NewRequest(method, "/api/v1/admin/fork/email-signup-filter", nil)
			want := http.StatusUnauthorized
			if authorized {
				req.Header.Set("Authorization", "Bearer unit-test-user")
				want = http.StatusForbidden
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, want, w.Code, "%s authorized=%t", method, authorized)
		}
	}
}
