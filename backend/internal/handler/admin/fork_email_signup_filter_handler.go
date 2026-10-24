package admin

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ForkEmailSignupFilterHandler struct {
	settingService *service.SettingService
}

func NewForkEmailSignupFilterHandler(settingService *service.SettingService) *ForkEmailSignupFilterHandler {
	return &ForkEmailSignupFilterHandler{settingService: settingService}
}

type forkEmailSignupFilterResponse struct {
	service.ForkEmailSignupFilterConfig
	ServerTimezone string `json:"server_timezone"`
	ConfigInvalid  bool   `json:"config_invalid"`
}

func (h *ForkEmailSignupFilterHandler) Get(c *gin.Context) {
	if h.settingService == nil {
		response.ErrorFrom(c, infraerrors.InternalServer("FORK_EMAIL_SIGNUP_FILTER_LOAD_FAILED", "Failed to read email signup filter settings"))
		return
	}
	cfg, invalid, err := h.settingService.GetForkEmailSignupFilterConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, infraerrors.InternalServer("FORK_EMAIL_SIGNUP_FILTER_LOAD_FAILED", "Failed to read email signup filter settings").WithCause(err))
		return
	}
	response.Success(c, forkEmailSignupFilterResponse{ForkEmailSignupFilterConfig: cfg, ServerTimezone: timezone.Name(), ConfigInvalid: invalid})
}

func (h *ForkEmailSignupFilterHandler) Update(c *gin.Context) {
	var req service.ForkEmailSignupFilterConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.settingService == nil {
		response.ErrorFrom(c, infraerrors.InternalServer("FORK_EMAIL_SIGNUP_FILTER_SAVE_FAILED", "Failed to write email signup filter settings"))
		return
	}
	cfg, err := h.settingService.UpdateForkEmailSignupFilterConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, forkEmailSignupFilterResponse{ForkEmailSignupFilterConfig: cfg, ServerTimezone: timezone.Name(), ConfigInvalid: false})
}
