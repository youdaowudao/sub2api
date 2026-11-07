package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const pluginCallBodyLimit = 256*1024 + 1024

// CaptureUserCreations adds only request-local candidate tracking. Explicit
// success exits complete a matching ID; this middleware never inspects status
// codes, buffers responses, or calls plugin processes.
func (h *PluginHandler) CaptureUserCreations(c *gin.Context) {
	c.Request = c.Request.WithContext(h.manager.CaptureUserCreations(c.Request.Context()))
	c.Next()
}

func pluginCaller(c *gin.Context) (service.PluginCaller, bool) {
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	role, roleOK := servermiddleware.GetUserRoleFromContext(c)
	if !ok || !roleOK || subject.UserID <= 0 || (role != service.RoleAdmin && role != service.RoleUser) {
		response.Unauthorized(c, "请先登录后再使用插件页面")
		return service.PluginCaller{}, false
	}
	return service.PluginCaller{UserID: subject.UserID, Role: role}, true
}

func (h *PluginHandler) ListEntries(c *gin.Context) {
	caller, ok := pluginCaller(c)
	if !ok {
		return
	}
	entries, err := h.manager.ListEntries(c.Request.Context(), caller)
	if err != nil {
		pluginExtensionError(c, err)
		return
	}
	response.Success(c, entries)
}

func (h *PluginHandler) CreateEntrySession(c *gin.Context) {
	id, ok := pluginExtensionIDParam(c)
	if !ok {
		return
	}
	caller, ok := pluginCaller(c)
	if !ok {
		return
	}
	var empty struct{}
	if !decodePluginExtensionBody(c, &empty) {
		return
	}
	session, err := h.manager.CreateEntrySession(c.Request.Context(), id, c.Param("entry"), caller)
	if err != nil {
		pluginExtensionError(c, err)
		return
	}
	response.Success(c, session)
}

func (h *PluginHandler) Call(c *gin.Context)   { h.invoke(c, "read") }
func (h *PluginHandler) Action(c *gin.Context) { h.invoke(c, "write") }

func (h *PluginHandler) invoke(c *gin.Context, access string) {
	id, ok := pluginExtensionIDParam(c)
	if !ok {
		return
	}
	caller, ok := pluginCaller(c)
	if !ok {
		return
	}
	var body struct {
		Payload json.RawMessage `json:"payload"`
	}
	if !decodePluginExtensionBody(c, &body) {
		return
	}
	if len(body.Payload) == 0 || len(body.Payload) > 256*1024 {
		pluginExtensionFailure(c, http.StatusBadRequest, "插件调用内容缺失或超过允许大小", "PLUGIN_INVALID_REQUEST", "", false, false)
		return
	}
	result, err := h.manager.Invoke(c.Request.Context(), id, c.Param("method"), access, body.Payload, caller)
	if err != nil {
		pluginExtensionError(c, err)
		return
	}
	response.Success(c, result)
}

func decodePluginExtensionBody(c *gin.Context, target any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, pluginCallBodyLimit))
	body = bytes.TrimSpace(body)
	if err != nil || len(body) == 0 || body[0] != '{' {
		pluginExtensionFailure(c, http.StatusBadRequest, "插件请求必须是允许大小内的 JSON 对象", "PLUGIN_INVALID_REQUEST", "", false, false)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		pluginExtensionFailure(c, http.StatusBadRequest, "插件请求格式无效或内容过大，请检查后重试", "PLUGIN_INVALID_REQUEST", "", false, false)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		pluginExtensionFailure(c, http.StatusBadRequest, "插件请求只能包含一个 JSON 对象", "PLUGIN_INVALID_REQUEST", "", false, false)
		return false
	}
	return true
}

func pluginExtensionError(c *gin.Context, err error) {
	var extensionErr *service.PluginExtensionError
	if errors.As(err, &extensionErr) {
		pluginExtensionFailure(c, extensionErr.HTTPStatus, extensionErr.Error(), extensionErr.Reason, extensionErr.RequestID, extensionErr.Retryable, extensionErr.OutcomeUnknown)
		return
	}
	// Unexpected storage errors must not expose an internal query or raw plugin
	// payload. Detailed runtime diagnostics are handled by the manager.
	write := strings.Contains(c.FullPath(), "/actions/")
	pluginExtensionFailure(c, http.StatusServiceUnavailable, "插件操作结果暂时无法确认，请稍后核对", "PLUGIN_UNAVAILABLE", "", !write, write)
}

func pluginExtensionFailure(c *gin.Context, status int, message, reason, requestID string, retryable, unknown bool) {
	if requestID == "" {
		if id, ok := c.Request.Context().Value(ctxkey.RequestID).(string); ok && id != "" && len(id) <= 64 {
			requestID = id
		}
	}
	if requestID == "" {
		requestID, _ = randomPluginToken()
		if requestID == "" {
			requestID = "plugin-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		}
	}
	response.ErrorWithDetails(c, status, message, reason, map[string]string{"request_id": requestID, "retryable": strconv.FormatBool(retryable), "outcome_unknown": strconv.FormatBool(unknown)})
}

func pluginExtensionIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		pluginExtensionFailure(c, http.StatusBadRequest, "插件 ID 无效，请从插件入口重新打开", "PLUGIN_INVALID_REQUEST", "", false, false)
		return 0, false
	}
	return id, true
}

func (h *PluginHandler) ServeBusinessUIAsset(c *gin.Context) {
	id, ok := pluginIDParam(c)
	if !ok {
		return
	}
	data, logicalPath, err := h.manager.ResolveBusinessUIAsset(c.Request.Context(), id, strings.TrimPrefix(c.Param("path"), "/"), strings.TrimSpace(c.Param("token")))
	if err != nil {
		c.Status(http.StatusGone)
		return
	}
	servePluginUIAsset(c, data, logicalPath)
}
