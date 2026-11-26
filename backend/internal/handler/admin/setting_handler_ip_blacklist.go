package admin

import (
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type RegistrationIPBlacklistResponse struct {
	IPs []string `json:"ips"`
}

func (h *SettingHandler) GetRegistrationIPBlacklist(c *gin.Context) {
	ips, err := h.settingService.GetRegistrationIPBlacklist(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, RegistrationIPBlacklistResponse{IPs: ips})
}

func (h *SettingHandler) UpdateRegistrationIPBlacklist(c *gin.Context) {
	var req struct {
		IPs json.RawMessage `json:"ips"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("REGISTRATION_IP_BLACKLIST_INVALID", "Request must contain an ips array"))
		return
	}
	ips, err := service.ParseRegistrationIPBlacklist(string(req.IPs))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	saved, err := h.settingService.SetRegistrationIPBlacklist(c.Request.Context(), ips)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, RegistrationIPBlacklistResponse{IPs: saved})
}
