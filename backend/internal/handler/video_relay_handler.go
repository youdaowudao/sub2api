package handler

import (
	"io"
	"net/http"
	"strconv"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type VideoRelayHandler struct {
	relay   *service.VideoRelayService
	gateway *OpenAIGatewayHandler
}

func NewVideoRelayHandler(relay *service.VideoRelayService, gateway *OpenAIGatewayHandler) *VideoRelayHandler {
	return &VideoRelayHandler{relay: relay, gateway: gateway}
}

func (h *VideoRelayHandler) requestKey(c *gin.Context) (*service.APIKey, bool) {
	if h == nil || h.relay == nil || h.gateway == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "api_error", "message": "Video relay is temporarily unavailable."}})
		return nil, false
	}
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		h.gateway.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	if h.gateway.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.gateway.errorPassthroughService)
	}
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	return key, true
}

func (h *VideoRelayHandler) Submit(c *gin.Context) {
	key, ok := h.requestKey(c)
	if !ok {
		return
	}
	gateway := h.gateway
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		gateway.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(c, "handler.video_relay")
	if !gateway.ensureResponsesDependencies(c, reqLog) {
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if !handleRequestBodyReadError(c, reqLog, err) {
			status, message := requestBodyReadFailure(reqLog, c.Request, err)
			gateway.errorResponse(c, status, "invalid_request_error", message)
		}
		return
	}
	input, inputError := service.ParseVideoRelayRequest(c.GetHeader("Content-Type"), body)
	if inputError != nil {
		h.writeError(c, inputError)
		return
	}
	setOpsRequestContext(c, input.Model, false)
	if failure := h.relay.CheckSubmitMode(key); failure != nil {
		h.writeError(c, failure)
		return
	}
	if !service.GroupAllowsImageGeneration(key.Group) {
		gateway.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	decision := gateway.checkSecurityAudit(c, reqLog, key, subject, service.ContentModerationProtocolOpenAIImages, input.Model, input.ModerationBody())
	if decision != nil && !decision.AllowNextStage {
		gateway.openAISecurityAuditError(c, decision)
		return
	}
	streamStarted := false
	imageRelease, ok := gateway.acquireImageGenerationSlot(c, streamStarted)
	if !ok {
		return
	}
	if imageRelease != nil {
		defer imageRelease()
	}
	userRelease, ok := gateway.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !ok {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}
	ctx := c.Request.Context()
	if err := gateway.billingCacheService.CheckBillingEligibility(ctx, key.User, key, key.Group, nil, service.QuotaPlatform(ctx, key)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		gateway.errorResponse(c, status, code, message)
		return
	}
	task, failure := h.relay.Submit(c, key, input)
	if failure != nil {
		h.writeError(c, failure)
		return
	}
	writeVideoRelayTask(c, task)
}

func (h *VideoRelayHandler) Lookup(c *gin.Context) {
	key, ok := h.requestKey(c)
	if !ok {
		return
	}
	task, failure := h.relay.Lookup(c, key, c.Param("request_id"))
	if failure != nil {
		h.writeError(c, failure)
		return
	}
	setOpsRequestContext(c, task.Model, false)
	writeVideoRelayTask(c, task)
}

func (h *VideoRelayHandler) Download(c *gin.Context) {
	key, ok := h.requestKey(c)
	if !ok {
		return
	}
	download, failure := h.relay.Download(c, key, c.Param("request_id"), c.Request.Header.Values("Range"))
	if failure != nil {
		h.writeError(c, failure)
		return
	}
	defer download.Close()
	c.Header("Content-Type", "video/mp4")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", `attachment; filename="`+download.TaskID+`.mp4"`)
	c.Header("Cache-Control", "private, no-store")
	if download.ContentRange != "" {
		c.Header("Content-Range", download.ContentRange)
	}
	if download.Length >= 0 {
		c.Header("Content-Length", strconv.FormatInt(download.Length, 10))
	}
	c.Status(download.Status)
	c.Writer.WriteHeaderNow()
	var err error
	if download.Length >= 0 {
		_, err = io.CopyN(c.Writer, download.Reader, download.Length)
	} else {
		_, err = io.Copy(c.Writer, download.Reader)
	}
	if err != nil && c.Request.Context().Err() == nil {
		middleware2.AbortHTTPResponse(c)
	}
}

func (h *VideoRelayHandler) writeError(c *gin.Context, failure *service.VideoRelayError) {
	status, code := failure.Status, failure.Code
	if failure.UpstreamStatus != 0 {
		status, code, _ = h.gateway.mapUpstreamError(failure.UpstreamStatus)
	}
	if failure.ContentRange != "" {
		c.Header("Content-Range", failure.ContentRange)
	}
	h.gateway.errorResponse(c, status, code, failure.Message)
}

func writeVideoRelayTask(c *gin.Context, task *service.VideoRelayTask) {
	type failure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	status := task.Status
	var message *failure
	var completedAt *int64
	switch task.BillingState {
	case service.VideoRelayCharged:
		status = service.VideoRelayStatusCompleted
	case service.VideoRelayRefunded:
		status = service.VideoRelayStatusFailed
		message = &failure{Code: "video_generation_failed", Message: service.VideoRelayFailureMessage(task.FailureReason)}
	default:
		if status != service.VideoRelayStatusInProgress {
			status = service.VideoRelayStatusQueued
		}
	}
	if task.SettledAt != nil && (task.BillingState == service.VideoRelayCharged || task.BillingState == service.VideoRelayRefunded) {
		at := task.SettledAt.Unix()
		completedAt = &at
	}
	c.JSON(http.StatusOK, gin.H{
		"id": task.TaskID, "object": "video", "model": task.Model, "status": status,
		"progress": task.Progress, "created_at": task.CreatedAt.Unix(), "completed_at": completedAt,
		"seconds": strconv.Itoa(task.Seconds), "resolution": task.Resolution, "error": message,
	})
}
