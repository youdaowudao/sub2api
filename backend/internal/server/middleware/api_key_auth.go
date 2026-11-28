package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const maxAPIKeyAuthorizationHeaderBytes = service.MaxAPIKeyCredentialBytes + 128

// statusClientClosedRequest 非标准状态码 499：客户端在响应前断开。
const statusClientClosedRequest = 499

// NewAPIKeyAuthMiddleware 创建 API Key 认证中间件
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, cfg))
}

// apiKeyAuthWithSubscription API Key认证中间件（支持订阅验证）
//
// 中间件职责分为两层：
//   - 鉴权（Authentication）：验证 Key 有效性、用户状态、IP 限制 —— 始终执行
//   - 计费执行（Billing Enforcement）：过期/配额/订阅/余额检查 —— skipBilling 时整块跳过
//
// /v1/usage、/v1/sub2api/billing 端点与异步生图任务查询只需鉴权，不需要计费执行。
// usage 允许过期/配额耗尽的 Key 查询自身用量，billing 用于读取当前 Key 的倍率配置，
// 异步生图查询允许已耗尽额度的 Key 拉取自身任务结果。
func apiKeyAuthWithSubscription(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── 1. 提取 API Key ──────────────────────────────────────────
		if rejectInvalidAuthAbuse(c, apiKeyService) {
			AbortWithError(c, http.StatusTooManyRequests, "INVALID_AUTH_RATE_LIMITED", "Too many invalid authentication attempts; retry later")
			return
		}

		if apiKeyHeadersTooLarge(c) {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key（API Key 无效，请检查是否填写正确）")
			return
		}

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectQueryAPIKeyDeprecated)
			AbortWithError(c, 400, "api_key_in_query_deprecated", "API key in query parameter is deprecated. Please use Authorization header instead.")
			return
		}

		// 尝试从Authorization header中提取API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string

		if authHeader != "" {
			// 验证Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
			}
		}

		// 如果Authorization header中没有，尝试从x-api-key header中提取
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
		}
		if len(apiKeyString) > service.MaxAPIKeyCredentialBytes {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key（API Key 无效，请检查是否填写正确）")
			return
		}

		// 如果x-api-key header中没有，尝试从x-goog-api-key header中提取（Gemini CLI兼容）
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
		}

		// 如果所有header都没有API key
		if apiKeyString == "" {
			recordInvalidAuthFailure(c, apiKeyService)
			if hasAPIKeyCredentialInput(c) {
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			} else {
				MarkIngressRejected(c, IngressRejectAPIKeyRequired)
			}
			AbortWithError(c, 401, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}

		// ── 2. 验证 Key 存在 ─────────────────────────────────────────

		apiKey, err := apiKeyService.GetByKey(c.Request.Context(), apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				recordInvalidAuthFailure(c, apiKeyService)
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
				AbortWithError(c, 401, "INVALID_API_KEY", "Invalid API key（API Key 无效，请检查是否填写正确）")
				return
			}
			if errors.Is(err, service.ErrAPIKeyAuthOverloaded) {
				MarkIngressRejected(c, IngressRejectAPIKeyAuthOverloaded)
				AbortWithError(c, http.StatusServiceUnavailable, "API_KEY_AUTH_OVERLOADED", "API key authentication is temporarily unavailable")
				return
			}
			if abortIfAPIKeyLookupClientGone(c, err) {
				return
			}
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to validate API key")
			return
		}

		// apiKey 已加载（含 User/Group）。即便后续因分组停用/Key 停用/用户停用/
		// IP 限制等早退中断，也让 Ops 错误日志能回退取到 user/group/platform。
		SetOpsFallbackAPIKey(c, apiKey)

		// ── 3. 基础鉴权（始终执行） ─────────────────────────────────

		clientIP := ""
		if apiKey != nil && (len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0) {
			clientIP = ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL())
		}
		if rejected := validateAPIKeyIdentity(apiKey, clientIP); rejected != nil {
			rejected.abortHTTP(c)
			return
		}
		ctx := context.WithValue(c.Request.Context(), ctxkey.UserID, apiKey.User.ID)
		ctx = service.WithUserAccountBlockUserID(ctx, apiKey.User.ID)
		// 请求头覆写变量（{{user_id}} 等）的取值来源：只在此处由已认证的 Key 建立。
		ctx = service.WithHeaderOverrideRequestScope(ctx, apiKey.User.ID, apiKey.ID, apiKey.GroupID)
		c.Request = c.Request.WithContext(ctx)
		billingInfoRequest := c.Request.URL.Path == "/v1/sub2api/billing"
		// Async image task polling only reads data that already belongs to the
		// authenticated key and must remain available after the completed
		// generation consumes the key's remaining balance.
		skipBilling := c.Request.URL.Path == "/v1/usage" || billingInfoRequest || isAsyncImageTaskRead(c.Request.Method, c.Request.URL.Path)
		videoRead := c.Request.Method == http.MethodGet && service.IsOpenAIVideoRelayRoute(apiKey, c.Request.Method, c.FullPath())
		if videoRead && (apiKey.Status == service.StatusAPIKeyExpired || apiKey.IsExpired()) {
			AbortWithError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
			return
		}

		// ── 4. SimpleMode → early return ─────────────────────────────

		if cfg.RunMode == config.RunModeSimple {
			c.Set(string(ContextKeyAPIKey), apiKey)
			c.Set(string(ContextKeyUser), AuthSubject{
				UserID:      apiKey.User.ID,
				Concurrency: apiKey.User.Concurrency,
			})
			c.Set(string(ContextKeyUserRole), apiKey.User.Role)
			setGroupContext(c, apiKey.Group)
			if !billingInfoRequest && !videoRead {
				_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
			}
			c.Next()
			return
		}

		// ── 5. 按端点需要加载订阅 ───────────────────────────────────

		var subscription *service.UserSubscription
		isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()

		// 倍率自省不需要订阅数据；/v1/usage 仍保留原有订阅读取行为。
		if isSubscriptionType && subscriptionService != nil && !billingInfoRequest && !videoRead {
			sub, subErr := subscriptionService.GetActiveSubscription(
				c.Request.Context(),
				apiKey.User.ID,
				apiKey.Group.ID,
			)
			if subErr != nil {
				if !skipBilling {
					AbortWithError(c, 403, "SUBSCRIPTION_NOT_FOUND", "No active subscription found for this group（当前分组没有有效订阅）")
					return
				}
				// skipBilling: 订阅不存在也放行，handler 会返回可用的数据
			} else {
				subscription = sub
			}
		}

		// ── 6. 计费执行（skipBilling 时整块跳过） ────────────────────

		if !skipBilling && !videoRead {
			var rejected *WSGenerationAuthRejection
			subscription, rejected = validateAPIKeyGenerationBilling(c.Request.Context(), apiKey, subscription, subscriptionService, cfg)
			if rejected != nil {
				rejected.abortHTTP(c)
				return
			}
		}
		if isResponsesWebSocketRoute(c) {
			bindWSGenerationAuthRefresh(c, apiKeyService, subscriptionService, cfg, apiKeyString)
		}

		// ── 7. 设置上下文 → Next ─────────────────────────────────────

		if subscription != nil {
			c.Set(string(ContextKeySubscription), subscription)
		}
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), apiKey.User.Role)
		setGroupContext(c, apiKey.Group)
		if !billingInfoRequest && !videoRead {
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
		}

		c.Next()
	}
}

// WSGenerationAuthRejection is a safe policy decision, shared by HTTP
// authentication and the per-generation WebSocket authorization check.
type WSGenerationAuthRejection struct {
	Status    int
	Code      string
	Message   string
	ingress   IngressRejectReason
	opsReason string
}

func (r *WSGenerationAuthRejection) abortHTTP(c *gin.Context) {
	if r.ingress != "" {
		MarkIngressRejected(c, r.ingress)
	}
	if r.opsReason != "" {
		service.MarkOpsClientBusinessLimited(c, r.opsReason)
	}
	if r.Code == "API_KEY_QUOTA_EXHAUSTED" {
		abortWithAPIKeyQuotaError(c)
		return
	}
	AbortWithError(c, r.Status, r.Code, r.Message)
}

func validateAPIKeyIdentity(key *service.APIKey, clientIP string) *WSGenerationAuthRejection {
	if key == nil {
		return &WSGenerationAuthRejection{Status: 401, Code: "INVALID_API_KEY", Message: "Invalid API key（API Key 无效，请检查是否填写正确）"}
	}
	if !key.IsActive() && key.Status != service.StatusAPIKeyExpired && key.Status != service.StatusAPIKeyQuotaExhausted {
		return &WSGenerationAuthRejection{Status: 401, Code: "API_KEY_DISABLED", Message: "API key is disabled（API Key 已被停用）", ingress: IngressRejectAPIKeyDisabled}
	}
	if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, key.CompiledIPWhitelist, key.CompiledIPBlacklist)
		if !allowed {
			if clientIP == "" {
				clientIP = "unknown"
			}
			return &WSGenerationAuthRejection{Status: 403, Code: "ACCESS_DENIED", Message: fmt.Sprintf("Access denied. Your IP is %s", clientIP), ingress: IngressRejectIPRestricted, opsReason: service.OpsClientBusinessLimitedReasonIPRestriction}
		}
	}
	if key.User == nil {
		return &WSGenerationAuthRejection{Status: 401, Code: "USER_NOT_FOUND", Message: "User associated with API key not found"}
	}
	if !key.User.IsActive() {
		return &WSGenerationAuthRejection{Status: 401, Code: "USER_INACTIVE", Message: "User account is not active（账户已停用，请联系管理员）", ingress: IngressRejectUserInactive}
	}
	if code, message, ok := validateAPIKeyGroupAvailable(key); !ok {
		reason := IngressRejectGroupDisabled
		if code == "GROUP_DELETED" {
			reason = IngressRejectGroupDeleted
		}
		return &WSGenerationAuthRejection{Status: 403, Code: code, Message: message, ingress: reason, opsReason: service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable}
	}
	if !validateAPIKeyGroupAllowed(key) {
		return &WSGenerationAuthRejection{Status: 403, Code: "GROUP_NOT_ALLOWED", Message: "API Key 所属专属分组不再允许当前用户使用", ingress: IngressRejectGroupNotAllowed, opsReason: service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable}
	}
	return nil
}

func validateAPIKeyGenerationBilling(ctx context.Context, key *service.APIKey, sub *service.UserSubscription, subSvc *service.SubscriptionService, cfg *config.Config) (*service.UserSubscription, *WSGenerationAuthRejection) {
	if key.Status == service.StatusAPIKeyQuotaExhausted {
		return nil, &WSGenerationAuthRejection{Status: 429, Code: "API_KEY_QUOTA_EXHAUSTED", Message: "API key 额度已用完"}
	}
	if key.Status == service.StatusAPIKeyExpired || key.IsExpired() {
		return nil, &WSGenerationAuthRejection{Status: 403, Code: "API_KEY_EXPIRED", Message: "API key 已过期"}
	}
	if key.IsQuotaExhausted() {
		return nil, &WSGenerationAuthRejection{Status: 429, Code: "API_KEY_QUOTA_EXHAUSTED", Message: "API key 额度已用完"}
	}
	if sub != nil {
		needsMaintenance, validateErr := subSvc.ValidateAndCheckLimits(sub, key.Group)
		if needsMaintenance {
			refreshed, err := subSvc.EnsureWindowMaintenance(ctx, sub)
			if err != nil {
				return nil, &WSGenerationAuthRejection{Status: 500, Code: "SUBSCRIPTION_MAINTENANCE_FAILED", Message: "Failed to maintain subscription usage windows"}
			}
			sub = refreshed
			_, validateErr = subSvc.ValidateAndCheckLimits(sub, key.Group)
		}
		if validateErr != nil {
			code, status := "SUBSCRIPTION_INVALID", 403
			if errors.Is(validateErr, service.ErrDailyLimitExceeded) || errors.Is(validateErr, service.ErrWeeklyLimitExceeded) || errors.Is(validateErr, service.ErrMonthlyLimitExceeded) {
				code, status = "USAGE_LIMIT_EXCEEDED", 429
			}
			return nil, &WSGenerationAuthRejection{Status: status, Code: code, Message: validateErr.Error()}
		}
	} else if apiKeyBalanceBelowAuthThreshold(key.User.Balance, cfg) {
		return nil, &WSGenerationAuthRejection{Status: 403, Code: "INSUFFICIENT_BALANCE", Message: "Insufficient account balance（账户余额不足，请及时充值）"}
	}
	return sub, nil
}

const wsGenerationAuthRefreshContextKey = "ws_generation_auth_refresh"
const wsUngroupedPolicyContextKey = "ws_ungrouped_policy"

type WSGenerationAuthRefresh func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, *WSGenerationAuthRejection)

func GetWSGenerationAuthRefresh(c *gin.Context) (WSGenerationAuthRefresh, bool) {
	value, ok := c.Get(wsGenerationAuthRefreshContextKey)
	if !ok {
		return nil, false
	}
	refresh, ok := value.(WSGenerationAuthRefresh)
	return refresh, ok && refresh != nil
}

// Compatible snapshots keep connection-scoped routes and policy immutable.
func wsGenerationSamePolicy(initial, latest *service.APIKey) bool {
	if initial == nil || latest == nil || initial.User == nil || latest.User == nil || initial.ID != latest.ID || initial.User.ID != latest.User.ID {
		return false
	}
	if (initial.GroupID == nil) != (latest.GroupID == nil) {
		return false
	}
	if initial.GroupID == nil {
		return initial.Group == nil && latest.Group == nil
	}
	if *initial.GroupID != *latest.GroupID || initial.Group == nil || latest.Group == nil {
		return false
	}
	a, b := initial.Group, latest.Group
	return a.ID == b.ID && a.Platform == b.Platform && a.SubscriptionType == b.SubscriptionType &&
		a.MaxReasoningEffort == b.MaxReasoningEffort && a.MaxReasoningEffortOverLimit == b.MaxReasoningEffortOverLimit &&
		slices.Equal(a.ReasoningEffortMappings, b.ReasoningEffortMappings) &&
		a.AllowImageGeneration == b.AllowImageGeneration && a.RequirePrivacySet == b.RequirePrivacySet &&
		a.ForceOpenAIFast == b.ForceOpenAIFast && a.ForceOpenAIUltrafast == b.ForceOpenAIUltrafast
}

func bindWSGenerationAuthRefresh(c *gin.Context, keySvc *service.APIKeyService, subSvc *service.SubscriptionService, cfg *config.Config, credential string) {
	clientIP := ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL())
	c.Set(wsGenerationAuthRefreshContextKey, WSGenerationAuthRefresh(func(ctx context.Context, initial *service.APIKey) (*service.APIKey, *service.UserSubscription, *WSGenerationAuthRejection) {
		if keySvc == nil || credential == "" {
			return nil, nil, &WSGenerationAuthRejection{Status: 503, Code: "API_KEY_AUTH_OVERLOADED", Message: "API key authentication is temporarily unavailable"}
		}
		latest, err := keySvc.GetByKey(ctx, credential)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				return nil, nil, &WSGenerationAuthRejection{Status: 401, Code: "INVALID_API_KEY", Message: "Invalid API key（API Key 无效，请检查是否填写正确）"}
			}
			return nil, nil, &WSGenerationAuthRejection{Status: 503, Code: "API_KEY_AUTH_OVERLOADED", Message: "API key authentication is temporarily unavailable"}
		}
		if rejected := validateAPIKeyIdentity(latest, clientIP); rejected != nil {
			if rejected.Code == "ACCESS_DENIED" {
				rejected.Message = "Access denied"
			}
			return nil, nil, rejected
		}
		if !wsGenerationSamePolicy(initial, latest) {
			return nil, nil, &WSGenerationAuthRejection{Status: 403, Code: "RECONNECT_REQUIRED", Message: "Connection authorization changed; please reconnect"}
		}
		if latest.GroupID == nil {
			v, _ := c.Get(wsUngroupedPolicyContextKey)
			allow, ok := v.(func(context.Context) bool)
			if !ok || allow == nil || !allow(ctx) {
				return nil, nil, &WSGenerationAuthRejection{Status: 403, Code: "GROUP_NOT_ALLOWED", Message: "API Key is not assigned to an available group"}
			}
		}
		var sub *service.UserSubscription
		if latest.Group != nil && latest.Group.IsSubscriptionType() {
			if subSvc != nil {
				sub, err = subSvc.GetActiveSubscription(ctx, latest.User.ID, latest.Group.ID)
			}
			if err != nil || sub == nil {
				return nil, nil, &WSGenerationAuthRejection{Status: 403, Code: "SUBSCRIPTION_NOT_FOUND", Message: "No active subscription found for this group（当前分组没有有效订阅）"}
			}
		}
		sub, rejected := validateAPIKeyGenerationBilling(ctx, latest, sub, subSvc, cfg)
		if rejected != nil {
			return nil, nil, rejected
		}
		return latest, sub, nil
	}))
}

func apiKeyHeadersTooLarge(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return len(c.GetHeader("Authorization")) > maxAPIKeyAuthorizationHeaderBytes ||
		len(c.GetHeader("x-api-key")) > service.MaxAPIKeyCredentialBytes ||
		len(c.GetHeader("x-goog-api-key")) > service.MaxAPIKeyCredentialBytes
}

func hasAPIKeyCredentialInput(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return c.GetHeader("Authorization") != "" ||
		c.GetHeader("x-api-key") != "" ||
		c.GetHeader("x-goog-api-key") != ""
}

func abortWithAPIKeyQuotaError(c *gin.Context) {
	const message = "API key 额度已用完"
	if isOpenAICompatibleAPIKeyRequest(c) {
		abortWithOpenAIQuotaError(c, http.StatusTooManyRequests, message)
		return
	}
	AbortWithError(c, http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED", message)
}

func isOpenAICompatibleAPIKeyRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}

	path := strings.TrimRight(c.Request.URL.Path, "/")
	for _, root := range []string{
		"/v1/responses",
		"/openai/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func isAsyncImageTaskRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(path, "/v1/images/tasks/") || strings.HasPrefix(path, "/images/tasks/")
}

// GetAPIKeyFromContext 从上下文中获取API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时回退使用。
// 与 ContextKeyAPIKey 区分：写入它不代表请求已通过鉴权，因此不影响 handler、
// 审计日志等对“已鉴权”的判断。
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey 读取 Ops 错误日志专用的回退 API Key。
func GetOpsFallbackAPIKey(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext 从上下文中获取订阅信息
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	value, exists := c.Get(string(ContextKeySubscription))
	if !exists {
		return nil, false
	}
	subscription, ok := value.(*service.UserSubscription)
	return subscription, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}

// apiKeyBalanceBelowAuthThreshold 保持鉴权层的历史语义：仅在余额耗尽（<=0）时拒绝。
// MinimumBalanceReserve 只作为 billing-cache 预检的保守下限，不得复用为鉴权硬门槛，
// 否则已配置该值的存量部署升级后，0 < balance < reserve 的用户会在所有端点被静默 403。
func apiKeyBalanceBelowAuthThreshold(balance float64, _ *config.Config) bool {
	return balance <= 0
}

func validateAPIKeyGroupAllowed(apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.GroupID == nil || apiKey.User == nil || apiKey.Group == nil {
		return true
	}
	group := apiKey.Group
	if group.IsSubscriptionType() {
		return true
	}
	return apiKey.User.CanBindGroup(group.ID, group.IsExclusive)
}

func validateAPIKeyGroupAvailable(apiKey *service.APIKey) (string, string, bool) {
	if apiKey == nil || apiKey.GroupID == nil {
		return "", "", true
	}
	group := apiKey.Group
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return "GROUP_DELETED", "API Key 所属分组已删除", false
	}
	if !group.IsActive() {
		return "GROUP_DISABLED", "API Key 所属分组已停用", false
	}
	return "", "", true
}

// abortIfAPIKeyLookupClientGone 处理 GetByKey 的非业务错误。
// 客户端已断开（连接关闭、HTTP/2 流被取消）时请求 ctx 被取消，查 Key 拿到的只是
// context 错误，不是服务端故障：以 499 结束并返回 true。其余错误记一行日志（不含 key），
// 由调用方按 500 处理。
func abortIfAPIKeyLookupClientGone(c *gin.Context, err error) bool {
	ctx := c.Request.Context()
	if ctx.Err() != nil {
		c.AbortWithStatus(statusClientClosedRequest)
		return true
	}
	logger.FromContext(ctx).Warn("api key lookup failed", zap.Error(err))
	return false
}
