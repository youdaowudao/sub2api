package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
)

const (
	pluginBusinessTokenPrefix = "sub2api:plugin-business-ui:v1:"
	pluginBusinessSessionTTL  = 30 * time.Minute
	pluginInvokeTimeout       = 10 * time.Second
)

// PluginCaller is constructed by the authenticated host, never by iframe JSON.
type PluginCaller struct {
	UserID int64
	Role   string
}

type PluginEntrySummary struct {
	PluginID  int64                 `json:"plugin_id"`
	PluginKey string                `json:"plugin_key"`
	Revision  string                `json:"revision"`
	EntryID   string                `json:"entry_id"`
	Label     string                `json:"label"`
	Mount     string                `json:"mount"`
	Roles     []string              `json:"roles"`
	Methods   []PluginBackendMethod `json:"methods"`
}

type PluginEntrySession struct {
	URL             string             `json:"url"`
	BridgeToken     string             `json:"bridge_token"`
	UIBridgeVersion int                `json:"ui_bridge_version"`
	ExpiresAt       time.Time          `json:"expires_at"`
	Entry           PluginEntrySummary `json:"entry"`
}

type PluginInvokeResult struct {
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

// All fields are safe for the standard HTTP error envelope. Cause and arbitrary
// plugin payloads are deliberately absent from this public error type.
type PluginExtensionError struct {
	Reason         string
	HTTPStatus     int
	Message        string
	RequestID      string
	Retryable      bool
	OutcomeUnknown bool
}

func (e *PluginExtensionError) Error() string {
	if e == nil {
		return "插件操作失败"
	}
	return e.Message
}

func pluginExtensionError(reason string, status int, message, requestID string) *PluginExtensionError {
	if requestID == "" {
		requestID = pluginRequestID()
	}
	return &PluginExtensionError{Reason: reason, HTTPStatus: status, Message: message, RequestID: requestID}
}

func pluginRequestID() string {
	id, err := pluginRandomToken()
	if err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return id
}

// HTTP has already bounded this correlation ID before placing it in context.
// Reuse it so Invoke results/errors and the existing audit entry are traceable.
func pluginRequestIDForContext(ctx context.Context) string {
	if ctx != nil {
		if id, ok := ctx.Value(ctxkey.RequestID).(string); ok && id != "" && len(id) <= 64 {
			return id
		}
	}
	return pluginRequestID()
}

func validPluginCaller(caller PluginCaller) bool {
	return caller.UserID > 0 && (caller.Role == RoleAdmin || caller.Role == RoleUser)
}

func pluginEntryVisible(entry PluginUIEntry, caller PluginCaller) bool {
	if !validPluginCaller(caller) || (entry.Mount == "admin" && caller.Role != RoleAdmin) {
		return false
	}
	for _, role := range entry.Roles {
		if role == caller.Role {
			return true
		}
	}
	return false
}

func pluginVisibleMethods(manifest PluginManifest, caller PluginCaller) []PluginBackendMethod {
	methods := make([]PluginBackendMethod, 0, len(manifest.Backend.Methods))
	for _, method := range manifest.Backend.Methods {
		if method.Role == RoleUser || caller.Role == RoleAdmin {
			methods = append(methods, method)
		}
	}
	return methods
}

func pluginEntrySummary(installation *PluginInstallation, entry PluginUIEntry, caller PluginCaller) PluginEntrySummary {
	return PluginEntrySummary{PluginID: installation.ID, PluginKey: installation.PluginKey, Revision: installation.BinarySHA256, EntryID: entry.ID, Label: entry.Label, Mount: entry.Mount, Roles: append([]string(nil), entry.Roles...), Methods: pluginVisibleMethods(installation.Manifest, caller)}
}

func (m *PluginManager) SetExtensionServices(store PluginUserCreationStore, users UserRepository, leader LeaderLockCache) {
	m.mu.Lock()
	m.creationStore, m.users, m.leader = store, users, leader
	m.mu.Unlock()
}

func (m *PluginManager) CaptureUserCreations(ctx context.Context) context.Context {
	m.mu.Lock()
	store := m.creationStore
	m.mu.Unlock()
	return WithUserCreationCapture(ctx, store)
}

func (m *PluginManager) ListEntries(ctx context.Context, caller PluginCaller) ([]PluginEntrySummary, error) {
	if !validPluginCaller(caller) {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "请登录后访问插件页面", "")
	}
	installations, err := m.repo.List(ctx)
	if err != nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件启用状态暂时无法读取，请稍后重试", "")
	}
	entries := make([]PluginEntrySummary, 0)
	for _, installation := range installations {
		if installation == nil || !installation.Manifest.IsGeneric() || installation.State != PluginStateEnabled || !EvaluatePluginCompatibility(installation.Manifest, m.hostInfo).Compatible {
			continue
		}
		for _, entry := range installation.Manifest.UI.Entries {
			if pluginEntryVisible(entry, caller) {
				entries = append(entries, pluginEntrySummary(installation, entry, caller))
			}
		}
	}
	return entries, nil
}

func (m *PluginManager) enabledGeneric(ctx context.Context, id int64, requestID string) (*PluginInstallation, error) {
	if id <= 0 {
		return nil, pluginExtensionError("PLUGIN_INVALID_REQUEST", http.StatusBadRequest, "插件安装编号无效", requestID)
	}
	installation, err := m.repo.GetByID(ctx, id)
	if err != nil || installation == nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件当前状态无法读取，请稍后重试或联系管理员", requestID)
	}
	if !installation.Manifest.IsGeneric() {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "此插件未提供通用业务入口", requestID)
	}
	if installation.State != PluginStateEnabled {
		return nil, pluginExtensionError("PLUGIN_DISABLED", http.StatusConflict, "此插件已停用，当前操作未执行。请联系管理员启用后重试", requestID)
	}
	if !EvaluatePluginCompatibility(installation.Manifest, m.hostInfo).Compatible {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件与当前宿主不兼容，当前操作未执行。请联系管理员", requestID)
	}
	return installation, nil
}

func (m *PluginManager) CreateEntrySession(ctx context.Context, id int64, entryID string, caller PluginCaller) (*PluginEntrySession, error) {
	if !validPluginCaller(caller) {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "你无权访问此插件页面", "")
	}
	installation, err := m.enabledGeneric(ctx, id, "")
	if err != nil {
		return nil, err
	}
	var selected *PluginUIEntry
	for i := range installation.Manifest.UI.Entries {
		entry := &installation.Manifest.UI.Entries[i]
		if entry.ID == entryID && pluginEntryVisible(*entry, caller) {
			selected = entry
			break
		}
	}
	if selected == nil {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "你无权访问此插件页面", "")
	}
	user, err := m.businessUIUser(ctx, caller)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(pluginBusinessSessionTTL)
	claims := pluginBusinessUIClaims{Version: 1, Purpose: "business", PluginID: id, Revision: installation.BinarySHA256, EntryID: entryID, UserID: caller.UserID, Role: caller.Role, TokenVersion: resolvedTokenVersion(user), Expires: expires.Unix()}
	raw, err := json.Marshal(claims)
	if err != nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "创建插件页面会话失败，可重试", "")
	}
	encrypted, err := m.encryptor.Encrypt(pluginBusinessTokenPrefix + string(raw))
	if err != nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "创建插件页面会话失败，可重试", "")
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(encrypted))
	bridge, err := pluginRandomToken()
	if err != nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "创建插件页面会话失败，可重试", "")
	}
	pathParts := strings.Split(strings.TrimPrefix(selected.Path, "ui/"), "/")
	for i := range pathParts {
		pathParts[i] = url.PathEscape(pathParts[i])
	}
	path := strings.Join(pathParts, "/")
	assetURL := fmt.Sprintf("/api/v1/plugin-entry-ui/%d/%s/%s#bridge_token=%s", id, token, path, url.QueryEscape(bridge))
	return &PluginEntrySession{URL: assetURL, BridgeToken: bridge, UIBridgeVersion: pluginv1.ApplicationUIBridgeVersion, ExpiresAt: expires, Entry: pluginEntrySummary(installation, *selected, caller)}, nil
}

func (m *PluginManager) businessUIUser(ctx context.Context, caller PluginCaller) (*User, error) {
	m.mu.Lock()
	users := m.users
	m.mu.Unlock()
	if users == nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件页面无法确认当前登录身份，请稍后重试", "")
	}
	user, err := users.GetByID(ctx, caller.UserID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件页面暂时无法确认当前登录身份，请稍后重试", "")
	}
	if err != nil || user == nil || user.ID != caller.UserID || user.DeletedAt != nil || !user.IsActive() || user.Role != caller.Role {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "你无权访问此插件页面，请重新登录后重试", "")
	}
	return user, nil
}

type pluginBusinessUIClaims struct {
	Version      int    `json:"version"`
	Purpose      string `json:"purpose"`
	PluginID     int64  `json:"plugin_id"`
	Revision     string `json:"revision"`
	EntryID      string `json:"entry_id"`
	UserID       int64  `json:"user_id"`
	Role         string `json:"role"`
	TokenVersion int64  `json:"token_version"`
	Expires      int64  `json:"expires"`
}

func (m *PluginManager) businessUIClaims(token string) (*pluginBusinessUIClaims, error) {
	invalid := func() (*pluginBusinessUIClaims, error) {
		return nil, errors.New("插件业务页面会话无效或已过期")
	}
	if len(token) == 0 || len(token) > 4096 {
		return invalid()
	}
	encrypted, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return invalid()
	}
	plaintext, err := m.encryptor.Decrypt(string(encrypted))
	if err != nil {
		return invalid()
	}
	plaintext, ok := strings.CutPrefix(plaintext, pluginBusinessTokenPrefix)
	if !ok {
		return invalid()
	}
	var claims pluginBusinessUIClaims
	decoder := json.NewDecoder(strings.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return invalid()
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return invalid()
	}
	now := time.Now().Unix()
	if claims.Version != 1 || claims.Purpose != "business" || claims.PluginID <= 0 || len(claims.Revision) != 64 || !pluginExtensionIdentifier.MatchString(claims.EntryID) || !validPluginCaller(PluginCaller{UserID: claims.UserID, Role: claims.Role}) || now >= claims.Expires || claims.Expires > now+int64(pluginBusinessSessionTTL/time.Second) {
		return invalid()
	}
	return &claims, nil
}

func (m *PluginManager) ResolveBusinessUIAsset(ctx context.Context, id int64, assetPath, token string) ([]byte, string, error) {
	claims, err := m.businessUIClaims(token)
	if err != nil || claims.PluginID != id {
		return nil, "", errors.New("插件业务页面会话无效或已过期")
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	installation, err := m.enabledGeneric(ctx, id, "")
	if err != nil {
		return nil, "", err
	}
	if installation.BinarySHA256 != claims.Revision {
		return nil, "", errors.New("插件页面已被新版本替换，请重新打开")
	}
	caller := PluginCaller{UserID: claims.UserID, Role: claims.Role}
	var entry *PluginUIEntry
	for i := range installation.Manifest.UI.Entries {
		candidate := &installation.Manifest.UI.Entries[i]
		if candidate.ID == claims.EntryID && pluginEntryVisible(*candidate, caller) {
			entry = candidate
			break
		}
	}
	if entry == nil {
		return nil, "", errors.New("你无权访问此插件页面")
	}
	user, err := m.businessUIUser(ctx, caller)
	if err != nil || resolvedTokenVersion(user) != claims.TokenVersion {
		return nil, "", errors.New("插件页面登录身份已变化，请重新打开")
	}
	relative := strings.TrimPrefix(assetPath, "/")
	if relative == "" {
		relative = strings.TrimPrefix(entry.Path, "ui/")
	}
	normalized, err := normalizePluginArchivePath(relative)
	if err != nil || normalized != relative {
		return nil, "", errors.New("插件页面资源路径无效")
	}
	logical := "ui/" + relative
	installation, err = m.ensureLocalInstallation(ctx, installation)
	if err != nil {
		return nil, "", err
	}
	return readDeclaredPluginUIAsset(installation, logical)
}

func pluginJSONIsObject(raw []byte) bool {
	if len(raw) == 0 || len(raw) > pluginv1.GenericMaxPayloadBytes {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func (m *PluginManager) Invoke(ctx context.Context, id int64, methodName, access string, payload json.RawMessage, caller PluginCaller) (*PluginInvokeResult, error) {
	requestID := pluginRequestIDForContext(ctx)
	if !validPluginCaller(caller) {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "插件调用缺少有效登录身份，当前操作未执行", requestID)
	}
	if !pluginExtensionIdentifier.MatchString(methodName) || (access != "read" && access != "write") || !pluginJSONIsObject(payload) {
		return nil, pluginExtensionError("PLUGIN_INVALID_REQUEST", http.StatusBadRequest, "插件调用参数必须是最多 256KiB 的 JSON 对象，当前操作未执行", requestID)
	}
	installation, err := m.enabledGeneric(ctx, id, requestID)
	if err != nil {
		return nil, err
	}
	var method *PluginBackendMethod
	for i := range installation.Manifest.Backend.Methods {
		if installation.Manifest.Backend.Methods[i].Name == methodName {
			method = &installation.Manifest.Backend.Methods[i]
			break
		}
	}
	if method == nil || method.Access != access || (method.Role == RoleAdmin && caller.Role != RoleAdmin) || !installation.Manifest.HasCapability(PluginCapabilityBackendInvoke) {
		return nil, pluginExtensionError("PLUGIN_FORBIDDEN", http.StatusForbidden, "你无权执行此插件方法，当前操作未执行", requestID)
	}
	m.mu.Lock()
	runtime := m.runtimes[id]
	m.mu.Unlock()
	if runtime == nil || runtime.backend == nil || runtime.access == nil || !runtime.alive() || runtime.access.revision != installation.BinarySHA256 || !runtime.beginRequest() {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "此操作未完成：插件未运行，请稍后重试或联系管理员", requestID)
	}
	defer runtime.finishRequest()
	invokeCtx, cancel := context.WithTimeout(ctx, pluginInvokeTimeout)
	defer cancel()
	token, callCtx, revoke, err := runtime.access.issue(invokeCtx, caller, access, method.Role == RoleUser, false)
	if err != nil {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件正在停止，当前操作未执行，请稍后重试", requestID)
	}
	defer revoke()
	latest, err := m.enabledGeneric(callCtx, id, requestID)
	if err != nil {
		return nil, err
	}
	if latest.BinarySHA256 != runtime.access.revision {
		return nil, pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "插件版本已变化，当前操作未执行。请重新打开插件页面后重试", requestID)
	}
	response, err := runtime.backend.Invoke(callCtx, &pluginv1.InvokeRequest{RequestId: requestID, Method: methodName, PayloadJson: append([]byte(nil), payload...), Caller: &pluginv1.PluginCaller{UserId: caller.UserID, Role: caller.Role}, AccessToken: token})
	if err != nil {
		revoke()
		errorResult := pluginExtensionError("PLUGIN_UNAVAILABLE", http.StatusServiceUnavailable, "调用插件 "+installation.PluginKey+" 时连接中断，请稍后重试", requestID)
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) || errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			errorResult.Reason = "PLUGIN_TIMEOUT"
			errorResult.HTTPStatus = http.StatusGatewayTimeout
			errorResult.Message = "调用插件 " + installation.PluginKey + " 等待超时，已结束等待"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			errorResult.Message = "调用插件 " + installation.PluginKey + " 已取消"
		}
		errorResult.Retryable = access == "read"
		errorResult.OutcomeUnknown = access == "write"
		if errorResult.OutcomeUnknown {
			errorResult.Message += "；操作可能已执行，请先核对结果，勿自动重试"
		} else {
			errorResult.Message += "；可重试读取"
		}
		if !runtime.alive() {
			m.failGenericRuntime(id, runtime, errorResult.Message)
		}
		return nil, errorResult
	}
	if response == nil || (response.Error == nil && !pluginJSONIsObject(response.PayloadJson)) {
		failure := pluginExtensionError("PLUGIN_FAILED", http.StatusBadGateway, "插件 "+installation.PluginKey+" 未返回有效且大小受限的 JSON 对象", requestID)
		failure.Retryable = access == "read"
		failure.OutcomeUnknown = access == "write"
		if failure.OutcomeUnknown {
			failure.Message += "；操作结果无法确认，请先核对，勿自动重试"
		}
		return nil, failure
	}
	if response.Error != nil {
		failure := pluginExtensionError("PLUGIN_FAILED", http.StatusBadGateway, "插件 "+installation.PluginKey+" 报告操作失败", requestID)
		code := pluginSafeErrorText(response.Error.Code, 64, token)
		message := pluginSafeErrorText(response.Error.Message, 500, token)
		if code != "" {
			failure.Message += " [" + code + "]"
		}
		if message != "" {
			failure.Message += ": " + message
		}
		failure.Retryable = access == "read" && response.Error.Retryable
		failure.OutcomeUnknown = access == "write" && response.Error.OutcomeUnknown
		if access == "write" {
			failure.Message += "；请核对结果，勿自动重试"
		} else if failure.Retryable {
			failure.Message += "；可重试读取"
		}
		return nil, failure
	}
	return &PluginInvokeResult{RequestID: requestID, Payload: append(json.RawMessage(nil), response.PayloadJson...)}, nil
}

func pluginSafeErrorText(value string, limit int, secret string) string {
	if secret != "" {
		value = strings.ReplaceAll(value, secret, "***")
	}
	value = logredact.RedactText(value, "token", "secret", "api_key", "authorization", "cookie", "credential", "proxy_url")
	lower := strings.ToLower(value)
	if strings.Contains(lower, "goroutine ") || strings.Contains(lower, "stack trace") || strings.Contains(lower, ".go:") || strings.HasPrefix(lower, "panic:") {
		return "插件返回了内部错误，具体堆栈已省略"
	}
	if line, _, ok := strings.Cut(value, "\n"); ok {
		value = line
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	text := []rune(strings.TrimSpace(value))
	if len(text) > limit {
		text = text[:limit]
	}
	return string(text)
}
