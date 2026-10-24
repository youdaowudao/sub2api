package routes

import (
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func registerForkEmailSignupFilterRoutes(admin *gin.RouterGroup, settingService *service.SettingService) {
	h := adminhandler.NewForkEmailSignupFilterHandler(settingService)
	admin.GET("/fork/email-signup-filter", h.Get)
	admin.PUT("/fork/email-signup-filter", h.Update)
}
