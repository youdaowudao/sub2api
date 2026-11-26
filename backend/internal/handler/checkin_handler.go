package handler

import (
	"context"
	"errors"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type checkinUserService interface {
	GetState(context.Context, int64, string) (service.CheckinStateResponse, error)
	ListMonth(context.Context, int64, string) (service.CheckinMonthResponse, error)
	Checkin(context.Context, int64, string, service.CheckinProof, string) (service.CheckinResult, error)
	SendEmailCode(context.Context, int64, string) (service.CheckinSendCodeResult, error)
}
type CheckinHandler struct{ service checkinUserService }

func NewCheckinHandler(s *service.CheckinService) *CheckinHandler { return &CheckinHandler{service: s} }
func (h *CheckinHandler) GetState(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, e := h.service.GetState(c.Request.Context(), subject.UserID, c.Query("month"))
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *CheckinHandler) ListMonth(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, e := h.service.ListMonth(c.Request.Context(), subject.UserID, c.Query("month"))
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *CheckinHandler) Submit(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req struct {
		service.CheckinProof
		EmailCode string `json:"email_code"`
	}
	if e := c.ShouldBindJSON(&req); e != nil && !errors.Is(e, io.EOF) {
		response.BadRequest(c, "签到请求格式无效")
		return
	}
	result, e := h.service.Checkin(c.Request.Context(), subject.UserID, "", req.CheckinProof, req.EmailCode)
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *CheckinHandler) SendEmailCode(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, e := h.service.SendEmailCode(c.Request.Context(), subject.UserID, c.GetHeader("Accept-Language"))
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
