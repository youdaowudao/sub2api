package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterPluginExtensionRoutes keeps the existing user/admin route contracts
// intact. Capability assets use their own per-entry token, never a panel JWT.
func RegisterPluginExtensionRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	stepUpAuth middleware.StepUpAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settings *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	v1.GET("/plugin-entry-ui/:id/:token/*path", h.Admin.Plugin.ServeBusinessUIAsset)
	plugins := v1.Group("/plugins")
	plugins.Use(gin.HandlerFunc(jwtAuth))
	plugins.Use(middleware.BackendModeUserGuard(settings))
	plugins.Use(gin.HandlerFunc(auditLog))
	plugins.Use(panelRateLimiter.Global())
	plugins.GET("/entries", h.Admin.Plugin.ListEntries)
	plugins.POST("/:id/entries/:entry/session", h.Admin.Plugin.CreateEntrySession)
	plugins.POST("/:id/calls/:method", h.Admin.Plugin.Call)
	plugins.POST("/:id/actions/:method", gin.HandlerFunc(stepUpAuth), h.Admin.Plugin.Action)
}
