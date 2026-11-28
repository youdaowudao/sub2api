package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ErrOpenAIWSBridgePreviousResponseUnavailable identifies a local history
// mismatch, not an upstream account failure.
var ErrOpenAIWSBridgePreviousResponseUnavailable = errors.New("websocket bridge previous response unavailable")

type openAIWSRejectedFieldRetryError struct {
	body   []byte
	reason string
	owner  *pkghttputil.WSBodyMemoryOwner
}

func (e *openAIWSRejectedFieldRetryError) Error() string {
	if e == nil || strings.TrimSpace(e.reason) == "" {
		return "retry websocket turn after rejected field normalization"
	}
	return "retry websocket turn after rejected field normalization: " + e.reason
}

func openAIWSRejectedFieldRetryHTTPStatus(message []byte) int {
	for _, value := range gjson.GetManyBytes(message, "status", "status_code", "error.status", "error.status_code") {
		status := int(value.Int())
		if status >= 100 && status <= 599 {
			return status
		}
	}
	return openAIWSErrorHTTPStatus(message)
}

func (s *OpenAIGatewayService) openAIWSIngressInterTurnIdleTimeout() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(s.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds) * time.Second
}

// newOpenAIWSDownstreamWriteContext binds writes directly to the client
// lifecycle while excluding the separate ingress-lease cancellation signal.
// This lets a lease-loss path finish its current client write before
// ReadOpenAIWSClientMessage sends the retryable close frame.
func newOpenAIWSDownstreamWriteContext(controlCtx context.Context, hooks *OpenAIWSIngressHooks, timeout time.Duration) (context.Context, context.CancelFunc) {
	writeParent := controlCtx
	if hooks != nil && hooks.ClientLifecycleContext != nil {
		writeParent = hooks.ClientLifecycleContext
	}
	if writeParent == nil {
		writeParent = context.Background()
	}
	return context.WithTimeout(writeParent, timeout)
}

func (s *OpenAIGatewayService) ProxyResponsesWebSocketFromClient(
	ctx context.Context,
	c *gin.Context,
	clientConn *coderws.Conn,
	account *Account,
	token string,
	firstClientMessage []byte,
	hooks *OpenAIWSIngressHooks,
) (returnErr error) {
	defer func() {
		if errors.Is(returnErr, pkghttputil.ErrRequestBodyMemoryBudget) {
			returnErr = NewOpenAIWSBodyMemoryCloseError(returnErr)
		}
	}()

	if s == nil {
		return errors.New("service is nil")
	}
	if c == nil {
		return errors.New("gin context is nil")
	}
	if clientConn == nil {
		return errors.New("client websocket is nil")
	}
	if account == nil {
		return errors.New("account is nil")
	}
	// A handler may reuse the same gin context across account failover attempts.
	// Never let an OAuth attempt's response aliases leak into the next account.
	setCodexToolNameReverse(c, nil)
	if _, err := s.prepareCodexAccountIdentitySource(ctx, c, account); err != nil {
		return err
	}
	if err := validateOpenAIWSBearerToken(account, token); err != nil {
		return err
	}

	// 预取一次 OpenAI Fast Policy settings，绑定到 ctx，让该 WS session
	// 内所有帧的 evaluateOpenAIFastPolicy 调用复用同一份快照，避免每帧
	// 进入 DB / settingRepo。Trade-off 见 withOpenAIFastPolicyContext 注释。
	if s.settingService != nil {
		if settings, err := s.settingService.GetOpenAIFastPolicySettings(ctx); err == nil && settings != nil {
			ctx = withOpenAIFastPolicyContext(ctx, settings)
		}
	}

	// The handler normally owns this registration across retry attempts. Direct
	// callers still get the same session-scoped preemption behavior here.
	if preemptCtx, cleanupPreempt, armed := s.BeginOpenAIWSIngressSessionPreemptionWithClient(ctx, c, account, firstClientMessage, clientConn); armed {
		ctx = preemptCtx
		defer cleanupPreempt()
		defer func() {
			if isOpenAIWSSessionPreempted(ctx) {
				_, owner, _ := TakeOpenAIWSCurrentTurnRetryBody(returnErr)
				owner.Drop()
				returnErr = errOpenAIWSSessionPreempted
			}
		}()
	}

	wsDecision := s.getOpenAIWSProtocolResolver().Resolve(account)
	forceHTTPBridge := account.Platform == PlatformGrok ||
		(s.pluginManager != nil && s.pluginManager.ShouldRouteOpenAIOAuth(account))
	modeRouterV2Enabled := s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled
	ingressMode := OpenAIWSIngressModeCtxPool
	if modeRouterV2Enabled && !forceHTTPBridge {
		ingressMode = account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault)
		if ingressMode == OpenAIWSIngressModeOff {
			return NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"websocket mode is disabled for this account",
				nil,
			)
		}
		switch ingressMode {
		case OpenAIWSIngressModePassthrough:
			if wsDecision.Transport != OpenAIUpstreamTransportResponsesWebsocketV2 {
				return fmt.Errorf("websocket ingress requires ws_v2 transport, got=%s", wsDecision.Transport)
			}
			if s.shouldBridgeOpenAIWSPassthroughFirstMessage(account, firstClientMessage) {
				forceHTTPBridge = true
				break
			}
			// 首轮准入由握手路径完成；后续 response.create 会在写入上游前
			// 依次回调 BeforeRequest 和 BeforeTurn，并在终止或失败时回调
			// AfterTurn，从而覆盖 turn 级利润复核、定价冻结和并发槽位释放。
			return s.proxyResponsesWebSocketV2Passthrough(
				ctx,
				c,
				clientConn,
				account,
				token,
				firstClientMessage,
				hooks,
				wsDecision,
			)
		case OpenAIWSIngressModeHTTPBridge:
			forceHTTPBridge = true
		case OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeShared, OpenAIWSIngressModeDedicated:
			// continue
		default:
			return NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"websocket mode only supports ctx_pool/passthrough/http_bridge",
				nil,
			)
		}
	}
	if !forceHTTPBridge && wsDecision.Transport != OpenAIUpstreamTransportResponsesWebsocketV2 {
		return fmt.Errorf("websocket ingress requires ws_v2 transport, got=%s", wsDecision.Transport)
	}
	dedicatedMode := modeRouterV2Enabled && ingressMode == OpenAIWSIngressModeDedicated

	wsURL := ""
	wsHost := "-"
	wsPath := "-"
	if forceHTTPBridge {
		wsHost = "xai-http-bridge"
		wsPath = "/v1/responses"
	} else {
		var err error
		wsURL, err = s.buildOpenAIResponsesWSURL(account)
		if err != nil {
			return fmt.Errorf("build ws url: %w", err)
		}
		if parsedURL, parseErr := url.Parse(wsURL); parseErr == nil && parsedURL != nil {
			wsHost = normalizeOpenAIWSLogValue(parsedURL.Host)
			wsPath = normalizeOpenAIWSLogValue(parsedURL.Path)
		}
	}
	debugEnabled := isOpenAIWSModeDebugEnabled()
	isCodexCLI := openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator")) || (s.cfg != nil && s.cfg.Gateway.ForceCodexCLI)

	type openAIWSClientPayload struct {
		payloadRaw               []byte
		accountIdentitySourceRaw []byte
		rawForHash               []byte
		promptCacheKey           string
		previousResponseID       string
		originalModel            string
		imageBillingModel        string
		imageSizeTier            string
		imageInputSize           string
		payloadBytes             int
		requestedReasoningEffort *string
		responsesLite            bool
		memory                   wsParsedBodyMemory
	}
	ingressSessionOriginalModel := ""
	var ingressSessionModelOwner *pkghttputil.WSBodyMemoryOwner
	defer func() { ingressSessionModelOwner.Drop() }()

	applyPayloadMutation := func(current []byte, path string, value any) ([]byte, error) {
		next, err := sjson.SetBytes(current, path, value)
		if err == nil {
			return next, nil
		}

		// 仅在确实需要修改 payload 且 sjson 失败时，退回 map 路径确保兼容性。
		payload := make(map[string]any)
		if unmarshalErr := json.Unmarshal(current, &payload); unmarshalErr != nil {
			return nil, err
		}
		switch path {
		case "type", "model":
			payload[path] = value
		case "client_metadata." + openAIWSTurnMetadataHeader:
			setOpenAIWSTurnMetadata(payload, fmt.Sprintf("%v", value))
		default:
			return nil, err
		}
		rebuilt, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return nil, marshalErr
		}
		return rebuilt, nil
	}

	parseClientPayload := func(turn int, raw []byte, rawOwner *pkghttputil.WSBodyMemoryOwner) (parsed openAIWSClientPayload, parseErr error) {
		memory := wsParsedBodyMemory{raw: rawOwner, normalized: rawOwner.Hold()}
		defer func() {
			if parseErr != nil {
				memory.drop()
			}
		}()
		transform := func(current []byte, run func(*wsMaterializer) ([]byte, error)) ([]byte, error) {
			next, owner, err := transformWSBody(ctx, current, run)
			if err != nil {
				return nil, err
			}
			if owner != nil {
				memory.replace(owner)
			}
			return next, nil
		}
		mutate := func(current []byte, path string, value any) ([]byte, error) {
			if !pkghttputil.HasWSRequestBodyMemory(ctx) {
				return applyPayloadMutation(current, path, value)
			}
			next, owner, err := setWSBodyField(ctx, current, path, value)
			if err != nil && !errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
				originalErr := err
				next, owner, err = transformWSBody(ctx, current, func(mem *wsMaterializer) ([]byte, error) {
					payload := make(map[string]any)
					if decodeErr := mem.unmarshal(current, &payload); decodeErr != nil {
						return nil, originalErr
					}
					switch path {
					case "type", "model":
						payload[path] = value
					case "client_metadata." + openAIWSTurnMetadataHeader:
						setOpenAIWSTurnMetadata(payload, fmt.Sprintf("%v", value))
					default:
						return nil, originalErr
					}
					return mem.marshal(payload)
				})
			}
			if err != nil {
				return nil, err
			}
			memory.replace(owner)
			return next, nil
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "empty websocket request payload", nil)
		}
		if !gjson.ValidBytes(trimmed) {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", errors.New("invalid json"))
		}
		// 帧内重复字段会让这里 gjson 读到的首个值与后续重编码/上游读到的值分叉
		// （GHSA-p3r4-jcm3-2hph），在读取任何字段之前拒绝。
		if field, conflict := requestmodel.ConflictingField("application/json", trimmed); conflict {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, fmt.Sprintf("websocket request payload declares field %.64q more than once", field), nil)
		}

		values, metadataOwner, fieldErr := readWSRoutingFields(ctx, trimmed, "type", "model", "prompt_cache_key", "previous_response_id")
		if fieldErr != nil {
			return openAIWSClientPayload{}, fieldErr
		}
		memory.metadata = metadataOwner
		eventType := strings.TrimSpace(values[0].String())
		normalized := trimmed
		switch eventType {
		case "":
			eventType = "response.create"
			next, setErr := mutate(normalized, "type", eventType)
			if setErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", setErr)
			}
			normalized = next
		case "response.create":
		case "response.append":
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"response.append is not supported in ws v2; use response.create with previous_response_id",
				nil,
			)
		default:
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				fmt.Sprintf("unsupported websocket request type: %s", eventType),
				nil,
			)
		}
		effortMemory := newWSMaterializer(ctx)
		requestedReasoningEffort := canonicalRequestedReasoningEffortWithMemory(normalized, effortMemory, strings.TrimSpace(values[1].String()))
		if effortMemory != nil && effortMemory.err != nil {
			effortMemory.close()
			return openAIWSClientPayload{}, effortMemory.err
		}
		effortMemory.close()
		if next, policyErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
			return applyOpenAIWSReasoningEffortPolicy(normalized, hooks, mem)
		}); policyErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, policyErr.Error(), policyErr)
		} else {
			normalized = next
		}
		liteMemory := newWSMaterializer(ctx)
		responsesLite := isOpenAIResponsesLiteWebSocketPayload(normalized, liteMemory)
		if liteMemory != nil && liteMemory.err != nil {
			liteMemory.close()
			return openAIWSClientPayload{}, liteMemory.err
		}
		defer liteMemory.close()
		var compatibilityChanged bool
		if compatibilityBody, compatibilityErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
			next, changed, err := normalizeOpenAIResponsesWebSocketCompatibilityBodyWithConfig(s.cfg, normalized, account, responsesLite, mem)
			compatibilityChanged = changed
			return next, err
		}); compatibilityErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", compatibilityErr)
		} else if compatibilityChanged {
			normalized = compatibilityBody
		}
		if account.IsOpenAIOAuthLike() {
			aliasSource := memory.normalized.Hold()
			defer aliasSource.Drop()
			var reverse map[string]string
			var aliased bool
			aliasedBody, aliasErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
				next, r, changed, err := aliasOpenAIOAuthReservedToolNamesBody(normalized, mem)
				reverse, aliased = r, changed
				return next, err
			})
			if aliasErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, aliasErr.Error(), aliasErr)
			}
			aliasMemory := newWSMaterializer(ctx)
			updateCodexToolNameReverseForWSFrame(c, normalized, reverse, aliasMemory)
			if aliasMemory != nil && aliasMemory.err != nil {
				aliasMemory.close()
				return openAIWSClientPayload{}, aliasMemory.err
			}
			aliasMemory.close()
			if aliased {
				normalized = aliasedBody
			}
		}

		originalModel := strings.TrimSpace(values[1].String())
		modelMissing := originalModel == ""
		if originalModel == "" {
			// 入站 WS 长会话里，部分客户端只在第一轮 response.create 上声明
			// model，后续 turn 复用同一 session-level model。为避免因省略
			// model 直接断开用户连接，这里回落到上一轮已通过校验的客户端模型，
			// 并在下方写回上游 payload，保证账号模型映射/fast policy/图片权限
			// 仍按同一模型执行。
			originalModel = ingressSessionOriginalModel
			if originalModel == "" {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
					coderws.StatusPolicyViolation,
					"model is required in response.create payload",
					nil,
				)
			}
		}
		promptCacheKey := strings.TrimSpace(values[2].String())
		previousResponseID := strings.TrimSpace(values[3].String())
		previousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
		if previousResponseID != "" && previousResponseIDKind == OpenAIPreviousResponseIDKindMessageID {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"previous_response_id must be a response.id (resp_*), not a message id",
				nil,
			)
		}
		if turnMetadata := strings.TrimSpace(c.GetHeader(openAIWSTurnMetadataHeader)); turnMetadata != "" {
			next, setErr := mutate(normalized, "client_metadata."+openAIWSTurnMetadataHeader, turnMetadata)
			if setErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", setErr)
			}
			normalized = next
		}
		// Immutable source shared with retry; account rewriting, when needed,
		// creates a separate allocation and keeps this owner alive.
		accountIdentitySourceRaw := normalized
		memory.identity = memory.normalized.Hold()
		var accountScoped bool
		accountScopedPayload, scopeErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
			next, changed, err := applyCodexAccountIdentityClientMetadataRaw(normalized, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), codexClientSessionID(c), mem)
			accountScoped = changed
			return next, err
		})
		if scopeErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket identity metadata", scopeErr)
		}
		if accountScoped {
			normalized = accountScopedPayload
		}
		if responsesLite {
			litePayload, liteErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
				next, _, err := normalizeOpenAIResponsesLitePayloadForAccount(normalized, account, mem)
				return next, err
			})
			if liteErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
					coderws.StatusPolicyViolation,
					liteErr.Error(),
					liteErr,
				)
			}
			normalized = litePayload
		}
		apiKey := getAPIKeyFromContext(c)
		imageGenerationAllowed := GroupAllowsImageGeneration(apiKeyGroup(apiKey))
		codexImageGenerationExplicitToolPolicy := codexImageGenerationExplicitToolPolicyAllow
		if isCodexCLI {
			codexImageGenerationExplicitToolPolicy = account.CodexImageGenerationExplicitToolPolicy()
		}
		codexBridgeEnabled := isCodexCLI &&
			!isOpenAIResponsesLiteWebSocketPayload(normalized, liteMemory) &&
			imageGenerationAllowed &&
			codexImageGenerationExplicitToolPolicy != codexImageGenerationExplicitToolPolicyStrip &&
			s.isCodexImageGenerationBridgeEnabled(ctx, account, apiKey)
		if liteMemory != nil && liteMemory.err != nil {
			return openAIWSClientPayload{}, liteMemory.err
		}
		if codexBridgeEnabled {
			next, transformErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
				payloadMap := make(map[string]any)
				if err := mem.decode(normalized, &payloadMap); err != nil {
					return nil, err
				}
				bridgeModified := false
				if ensureOpenAIResponsesImageGenerationTool(payloadMap) {
					bridgeModified = true
					logOpenAIWSModeInfo("ingress_ws_codex_image_tool_injected account_id=%d", account.ID)
				}
				if ensureOpenAIResponsesImageGenerationToolChoiceAuto(payloadMap) {
					bridgeModified = true
					logOpenAIWSModeInfo("ingress_ws_codex_image_tool_choice_auto account_id=%d", account.ID)
				}
				if normalizeOpenAIResponsesImageGenerationTools(payloadMap) {
					bridgeModified = true
				}
				if applyCodexImageGenerationBridgeInstructions(payloadMap, mem) {
					bridgeModified = true
					logOpenAIWSModeInfo("ingress_ws_codex_image_bridge_instructions_added account_id=%d", account.ID)
				}
				if bridgeModified {
					rebuilt, marshalErr := mem.marshal(payloadMap)
					if marshalErr != nil {
						return nil, marshalErr
					}
					return rebuilt, nil
				}
				return normalized, nil
			})
			if transformErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", transformErr)
			}
			normalized = next
		}
		requestModel := originalModel
		if hooks != nil && hooks.MapRequestModel != nil {
			mappedModel, mapErr := hooks.MapRequestModel(turn, originalModel)
			if mapErr != nil {
				return openAIWSClientPayload{}, mapErr
			}
			if mappedModel = strings.TrimSpace(mappedModel); mappedModel != "" {
				requestModel = mappedModel
			}
		}
		upstreamModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(requestModel))
		if modelMissing || upstreamModel != originalModel {
			next, setErr := mutate(normalized, "model", upstreamModel)
			if setErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", setErr)
			}
			normalized = next
		}
		SetOpsUpstreamModel(c, upstreamModel)
		if isCodexCLI && codexImageGenerationExplicitToolPolicy == codexImageGenerationExplicitToolPolicyStrip {
			var changed bool
			if stripped, stripErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
				next, didChange, err := stripOpenAIImageGenerationToolsFromRawPayload(normalized, mem)
				changed = didChange
				return next, err
			}); stripErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", stripErr)
			} else if changed {
				normalized = stripped
				logOpenAIWSModeInfo("ingress_ws_codex_image_tool_stripped_by_policy account_id=%d", account.ID)
			}
		}
		var sparkChanged bool
		if stripped, stripErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
			next, changed, err := stripCodexSparkImageGenerationToolFromRawPayload(normalized, upstreamModel, mem)
			sparkChanged = changed
			return next, err
		}); stripErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", stripErr)
		} else if sparkChanged {
			normalized = stripped
			logOpenAIWSModeInfo("ingress_ws_codex_spark_image_tool_stripped account_id=%d", account.ID)
		}
		// normalized 由 parseClientPayload 中已通过 gjson.ValidBytes 校验的字节派生，
		// 后续改写只走 sjson 或 decode→json.Marshal，合法性保持，因此跳过重复校验。
		imageMemory := newWSMaterializer(ctx)
		imageIntent := openAIImageGenerationIntentForPlatform(openAIResponsesEndpoint, originalModel, normalized, account.Platform, false, imageMemory)
		if imageMemory != nil && imageMemory.err != nil {
			imageMemory.close()
			return openAIWSClientPayload{}, imageMemory.err
		}
		defer imageMemory.close()
		if imageIntent && !imageGenerationAllowed {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, ImageGenerationPermissionMessage(), nil)
		}
		imageBillingModel := ""
		imageSizeTier := ""
		imageInputSize := ""
		if imageIntent {
			var imageCfgErr error
			imageCfg, imageCfgErr := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(normalized, originalModel, imageMemory)
			if imageMemory != nil && imageMemory.err != nil {
				return openAIWSClientPayload{}, imageMemory.err
			}
			if imageCfgErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, imageCfgErr.Error(), imageCfgErr)
			}
			imageBillingModel = imageCfg.Model
			imageSizeTier = imageCfg.SizeTier
			imageInputSize = imageCfg.InputSize
		}

		// Apply OpenAI Fast Policy on the response.create frame using the same
		// evaluator/normalize/scope rules as the HTTP entrypoints. This is the
		// single integration point for all WS ingress turns (first + follow-up
		// frames flow through here).
		//
		// Model fallback: first turn still requires model at the handler layer；
		// follow-up response.create frames may omit it and then reuse
		// ingressSessionOriginalModel. We always write a concrete upstream model
		// before evaluating policy, so whitelist / filter behavior remains stable.
		var blocked *OpenAIFastBlockedError
		policyApplied, policyErr := transform(normalized, func(mem *wsMaterializer) ([]byte, error) {
			next, b, err := s.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, upstreamModel, normalized, mem)
			blocked = b
			return next, err
		})
		if policyErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", policyErr)
		}
		if blocked != nil {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			// Send a Realtime-style error event to the client first, then
			// signal the handler to close the connection with PolicyViolation.
			// We intentionally do NOT forward this frame upstream.
			//
			// coder/websocket@v1.8.14 Conn.Write is synchronous and flushes
			// the underlying bufio writer before returning (write.go:42 →
			// 307-311), and the subsequent close handshake re-acquires the
			// same writeFrameMu, so the error event is guaranteed to reach
			// the kernel send buffer before any close frame is queued.
			eventBytes := buildOpenAIFastPolicyBlockedWSEvent(blocked)
			if eventBytes != nil {
				writeCtx, cancel := newOpenAIWSDownstreamWriteContext(ctx, hooks, s.openAIWSWriteTimeout())
				_ = clientConn.Write(writeCtx, coderws.MessageText, eventBytes)
				cancel()
			}
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				blocked.Message,
				blocked,
			)
		}
		normalized = policyApplied
		if pkghttputil.HasWSRequestBodyMemory(ctx) {
			if modelMissing {
				memory.model = ingressSessionModelOwner.Hold()
			} else {
				owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, int64(len(originalModel)))
				if err != nil {
					return openAIWSClientPayload{}, err
				}
				originalModel = strings.Clone(originalModel)
				ingressSessionModelOwner.Drop()
				ingressSessionModelOwner = owner
				memory.model = owner.Hold()
			}
		}
		ingressSessionOriginalModel = originalModel

		return openAIWSClientPayload{
			payloadRaw:               normalized,
			accountIdentitySourceRaw: accountIdentitySourceRaw,
			rawForHash:               trimmed,
			promptCacheKey:           promptCacheKey,
			previousResponseID:       previousResponseID,
			originalModel:            originalModel,
			imageBillingModel:        imageBillingModel,
			imageSizeTier:            imageSizeTier,
			imageInputSize:           imageInputSize,
			payloadBytes:             len(normalized),
			requestedReasoningEffort: requestedReasoningEffort,
			responsesLite:            responsesLite,
			memory:                   memory,
		}, nil
	}

	writeClientMessage := func(message []byte) error {
		writeCtx, cancel := newOpenAIWSDownstreamWriteContext(ctx, hooks, s.openAIWSWriteTimeout())
		defer cancel()
		message = restoreCodexToolNamesFromContext(c, message)
		return clientConn.Write(writeCtx, coderws.MessageText, message)
	}

	readClientMessage := func() ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
		idleTimeout := s.openAIWSIngressInterTurnIdleTimeout()
		msgType, payload, owner, readErr := ReadOpenAIWSClientMessageWithBodyMemory(
			ctx, ctx, ResolveOpenAIWSClientReadLimitBytes(s.cfg),
			clientConn,
			idleTimeout,
			coderws.StatusNormalClosure,
			"websocket idle timeout",
		)
		if readErr != nil {
			var closeErr *OpenAIWSClientCloseError
			if errors.As(readErr, &closeErr) && closeErr.StatusCode() == coderws.StatusNormalClosure {
				logOpenAIWSModeInfo("ingress_ws_inter_turn_idle_timeout account_id=%d timeout_seconds=%d", account.ID, int(idleTimeout.Seconds()))
			}
			owner.Drop()
			return nil, nil, readErr
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			owner.Drop()
			return nil, nil, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				fmt.Sprintf("unsupported websocket client message type: %s", msgType.String()),
				nil,
			)
		}
		return payload, owner, nil
	}

	firstPayload, err := parseClientPayload(1, firstClientMessage, pkghttputil.WSBodyMemorySource(ctx).Hold())
	if err != nil {
		return err
	}

	defer firstPayload.memory.drop()
	useHTTPBridge := forceHTTPBridge || s.shouldBridgeOpenAIWSHTTP(account, firstPayload.payloadBytes, firstPayload.previousResponseID)
	turnState := strings.TrimSpace(c.GetHeader(openAIWSTurnStateHeader))
	stateStore := s.getOpenAIWSStateStore()
	groupID := getOpenAIGroupIDFromContext(c)
	apiKeyID := getAPIKeyIDFromContext(c)
	storeDisabledConnMode := s.openAIWSStoreDisabledConnMode()
	sessionHash := ""
	preferredConnID := ""
	storeDisabled := false
	refreshIngressRouteState := func(payload openAIWSClientPayload) error {
		// 会话级状态按执行作用域隔离：codex 多智能体共用 session-id，只有线程标识能把
		// 父线程与子智能体区分开；没有声明身份时沿用原会话哈希。账号粘性仍由 handler 决定。
		var hashErr error
		sessionHash, hashErr = s.GenerateWSSessionHash(ctx, c, payload.rawForHash, "")
		if hashErr != nil {
			return hashErr
		}
		if scope, _ := resolveOpenAIWSExecutionScope(c, payload.rawForHash, apiKeyID); scope != "" {
			sessionHash = scope
		}
		preferredConnID = ""
		storeDisabled = s.isOpenAIWSStoreDisabledInRequestRaw(payload.payloadRaw, account)
		if useHTTPBridge {
			// Sticky account affinity may be shared, but an HTTP bridge must not
			// inherit another connection's native WS turn state or socket binding.
			return nil
		}
		if turnState == "" && stateStore != nil && sessionHash != "" {
			if savedTurnState, ok := stateStore.GetSessionTurnState(groupID, sessionHash); ok {
				turnState = savedTurnState
			}
		}

		if stateStore != nil && payload.previousResponseID != "" {
			if connID, ok := stateStore.GetResponseConn(payload.previousResponseID); ok {
				preferredConnID = connID
			}
		}

		if stateStore != nil && storeDisabled && payload.previousResponseID == "" && sessionHash != "" {
			if connID, ok := stateStore.GetSessionConn(groupID, sessionHash); ok {
				preferredConnID = connID
			}
		}
		return nil
	}
	if hashErr := refreshIngressRouteState(firstPayload); hashErr != nil {
		return hashErr
	}

	if useHTTPBridge {
		logOpenAIWSModeInfo(
			"ingress_ws_http_bridge_start account_id=%d account_type=%s payload_bytes=%d threshold_bytes=%d has_session_hash=%v store_disabled=%v",
			account.ID,
			account.Type,
			firstPayload.payloadBytes,
			s.openAIWSHTTPBridgeThresholdBytes(),
			sessionHash != "",
			storeDisabled,
		)
		currentBridgePayload := firstPayload
		currentBridgePayload.memory = firstPayload.memory.hold()
		defer func() { currentBridgePayload.memory.drop() }()
		// Keep the first turn as the stable conversation seed. The mapped model
		// is resolved again for each turn below so an in-connection model switch
		// cannot reuse another model's upstream cache identity.
		grokCacheSeedPayload := firstPayload.payloadRaw
		var bridgeReplayInput []json.RawMessage
		bridgeReplayInputExists := false
		memoryEnabled := pkghttputil.HasWSRequestBodyMemory(ctx)
		var historyMemory, currentItemsMemory, turnMemory wsMemoryReplay
		var attemptOwner *pkghttputil.WSBodyMemoryOwner
		defer func() { attemptOwner.Drop(); turnMemory.drop(); currentItemsMemory.drop(); historyMemory.drop() }()
		lastBridgeResponseID := ""
		for turn := 1; ; turn++ {
			if turn > 1 && hooks != nil && hooks.BeforeRequest != nil {
				if err := hooks.BeforeRequest(turn, currentBridgePayload.rawForHash, currentBridgePayload.originalModel); err != nil {
					return err
				}
			}
			previousID := currentBridgePayload.previousResponseID
			if turn == 1 && hooks != nil && strings.TrimSpace(hooks.InitialPreviousResponseID) != "" {
				previousID = strings.TrimSpace(hooks.InitialPreviousResponseID)
			}
			if previousID != "" && (turn == 1 || previousID != lastBridgeResponseID) {
				writeErr := writeClientMessage([]byte(`{"type":"error","sequence_number":0,"status":400,"error":{"type":"invalid_request_error","code":"previous_response_not_found","message":"Previous response is unavailable on this connection. This request was not sent upstream. Reconnect and resend the full input."}}`))
				// Preserve local attribution even if writing the explanatory frame fails.
				cause := error(ErrOpenAIWSBridgePreviousResponseUnavailable)
				if writeErr != nil {
					cause = errors.Join(cause, writeErr)
				}
				return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "previous response unavailable; reconnect with full input", cause)
			}
			if hooks != nil && hooks.BeforeTurn != nil {
				if err := hooks.BeforeTurn(turn); err != nil {
					return err
				}
			}
			if turnState != "" && c != nil && c.Request != nil {
				c.Request.Header.Set(openAIWSTurnStateHeader, turnState)
			}
			if c != nil && sessionHash != "" {
				c.Set(openAIWSIngressSessionHashContextKey, sessionHash)
			}
			// 剥离本会话已知失效的加密项，阻断同一失效密文随历史反复触发上游拒绝。
			// 历史序列须同步剥离，否则与已剥离的当前 input 项错位，prefix 复用失配。
			if invalidDigests := s.sessionInvalidEncryptedContentDigests(groupID, sessionHash); len(invalidDigests) > 0 {
				strippedPayload, strippedOwner, strippedCount, stripErr := s.stripWSInvalidFrameLogged(ctx, currentBridgePayload.payloadRaw, invalidDigests, "ingress_ws_http_bridge_invalid_encrypted_lineage_strip", account.ID, turn)
				if stripErr != nil {
					return stripErr
				}
				if strippedCount > 0 {
					if strippedOwner != nil {
						currentBridgePayload.memory.replace(strippedOwner)
					}
					currentBridgePayload.payloadRaw = strippedPayload
					currentBridgePayload.payloadBytes = len(strippedPayload)
				}
				if bridgeReplayInputExists {
					if memoryEnabled {
						next, err := stripWSMemoryReplay(ctx, historyMemory, invalidDigests)
						if err != nil {
							return err
						}
						historyMemory.drop()
						historyMemory = next
						bridgeReplayInput = historyMemory.items
					} else {
						bridgeReplayInput, _ = stripOpenAIInvalidEncryptedContentFromReplayItems(bridgeReplayInput, invalidDigests)
					}
				}
			}
			bridgePayloadRaw := currentBridgePayload.payloadRaw
			bridgePayloadBytes := currentBridgePayload.payloadBytes
			toolOutputCoverage, coverageErr := AnalyzeWSBodyToolCoverage(ctx, currentBridgePayload.payloadRaw)
			if coverageErr != nil {
				return coverageErr
			}
			needsBridgeReplay := currentBridgePayload.previousResponseID != "" ||
				(toolOutputCoverage.HasFunctionCallOutput && !toolOutputCoverage.ContextCoversAllCallIDs)
			// 一次解析当前 input，正常 replay 和换号复用同一份完整历史。
			var bridgeCurrentItems, turnReplayInput []json.RawMessage
			var bridgeCurrentItemsExist, turnReplayInputExists bool
			if memoryEnabled {
				var extractErr error
				currentItemsMemory, extractErr = extractWSMemoryReplay(ctx, currentBridgePayload.payloadRaw, currentBridgePayload.memory.normalized)
				if extractErr != nil {
					return NewOpenAIWSBodyMemoryCloseError(extractErr)
				}
				turnMemory, extractErr = buildWSMemoryReplay(ctx, historyMemory, currentItemsMemory, needsBridgeReplay)
				if extractErr != nil {
					return NewOpenAIWSBodyMemoryCloseError(extractErr)
				}
				bridgeCurrentItems, bridgeCurrentItemsExist = currentItemsMemory.items, currentItemsMemory.exists
				turnReplayInput, turnReplayInputExists = turnMemory.items, turnMemory.exists
			} else {
				var extractErr error
				bridgeCurrentItems, bridgeCurrentItemsExist, extractErr = openAIWSExtractNormalizedInputSequence(
					currentBridgePayload.payloadRaw,
				)
				if extractErr != nil {
					return fmt.Errorf("build websocket http bridge replay input: %w", extractErr)
				}
				turnReplayInput, turnReplayInputExists = buildOpenAIWSReplayInputSequenceFromItems(
					bridgeReplayInput,
					bridgeReplayInputExists,
					bridgeCurrentItems,
					bridgeCurrentItemsExist,
					needsBridgeReplay,
				)
			}
			mergedHistory := len(turnReplayInput) > len(bridgeCurrentItems)
			if needsBridgeReplay && mergedHistory && len(bridgeCurrentItems) > 0 && account.IsOpenAI() {
				previous := turnReplayInput[:len(turnReplayInput)-len(bridgeCurrentItems)]
				messageCount := 0
				for _, item := range previous {
					if gjson.GetBytes(item, "type").String() == "message" {
						messageCount++
					}
				}
				if messageCount <= len(bridgeCurrentItems) {
					comparable, comparableOwner, compareErr := setWSBodyInput(ctx, currentBridgePayload.payloadRaw, previous, true)
					if compareErr == nil {
						var nextOwner *pkghttputil.WSBodyMemoryOwner
						comparable, nextOwner, compareErr = transformWSBody(ctx, comparable, func(mem *wsMaterializer) ([]byte, error) {
							return normalizeOpenAIWSBridgeReplayItems(comparable, account, mem)
						})
						if nextOwner != nil {
							comparableOwner.Drop()
							comparableOwner = nextOwner
						}
					}
					if compareErr != nil {
						comparableOwner.Drop()
						return fmt.Errorf("normalize websocket http bridge replay prefix: %w", compareErr)
					}
					var prefixMatch bool
					if memoryEnabled {
						prefix, err := extractWSMemoryReplay(ctx, comparable, comparableOwner)
						if err == nil {
							prefixMatch, err = wsMemoryReplayHasPrefix(ctx, currentItemsMemory, prefix)
						}
						prefix.drop()
						comparableOwner.Drop()
						if err != nil {
							return NewOpenAIWSBodyMemoryCloseError(err)
						}
					} else {
						normalizedPrefix, _, err := openAIWSExtractNormalizedInputSequence(comparable)
						if err != nil {
							return fmt.Errorf("extract websocket http bridge replay prefix: %w", err)
						}
						prefixMatch = openAIWSRawItemsHasPrefix(bridgeCurrentItems, normalizedPrefix)
					}
					if prefixMatch {
						if memoryEnabled {
							turnMemory.drop()
							turnMemory = currentItemsMemory.hold()
						}
						turnReplayInput, turnReplayInputExists = bridgeCurrentItems, bridgeCurrentItemsExist
						mergedHistory = false
					}
				}
			}
			if needsBridgeReplay && turnReplayInputExists {
				updatedPayload, owner, setInputErr := setWSBodyInput(ctx, currentBridgePayload.payloadRaw, turnReplayInput, true)
				attemptOwner = owner
				if setInputErr != nil {
					return fmt.Errorf("set websocket http bridge replay input: %w", setInputErr)
				}
				if mergedHistory {
					var nextOwner *pkghttputil.WSBodyMemoryOwner
					updatedPayload, nextOwner, setInputErr = transformWSBody(ctx, updatedPayload, func(mem *wsMaterializer) ([]byte, error) {
						return normalizeOpenAIWSBridgeReplayBody(updatedPayload, account, currentBridgePayload.responsesLite, mem)
					})
					if nextOwner != nil {
						attemptOwner.Drop()
						attemptOwner = nextOwner
					}
					if setInputErr != nil {
						return fmt.Errorf("normalize websocket http bridge replay input: %w", setInputErr)
					}
				}
				bridgePayloadRaw = updatedPayload
				bridgePayloadBytes = len(updatedPayload)
				logOpenAIWSModeInfo(
					"ingress_ws_http_bridge_replay_input account_id=%d turn=%d input_items=%d previous_response_id_present=%v has_tool_output=%v",
					account.ID,
					turn,
					len(turnReplayInput),
					currentBridgePayload.previousResponseID != "",
					toolOutputCoverage.HasFunctionCallOutput,
				)
			}
			grokCacheIdentity := ""
			if account.Platform == PlatformGrok {
				grokCacheIdentity, err = resolveGrokWSCacheIdentity(
					c,
					account,
					grokCacheSeedPayload,
					currentBridgePayload.payloadRaw,
					currentBridgePayload.originalModel,
				)
				if err != nil {
					return fmt.Errorf("resolve Grok websocket cache identity: %w", err)
				}
			}
			if hooks != nil && hooks.BeforeForward != nil {
				if err := hooks.BeforeForward(turn, bridgePayloadRaw, currentBridgePayload.originalModel); err != nil {
					return err
				}
			}
			result, bridgeErr := s.proxyOpenAIWSHTTPBridgeTurn(
				ctx,
				c,
				account,
				token,
				bridgePayloadRaw,
				bridgePayloadBytes,
				currentBridgePayload.originalModel,
				currentBridgePayload.imageBillingModel,
				currentBridgePayload.imageSizeTier,
				currentBridgePayload.imageInputSize,
				grokCacheIdentity,
				turn,
				writeClientMessage,
			)
			attemptOwner.Drop()
			attemptOwner = nil
			if result != nil {
				// The HTTP payload can contain a policy rewrite or replayed configuration from an earlier turn.
				result.RequestedReasoningEffort = currentBridgePayload.requestedReasoningEffort
			}
			if bridgeErr != nil && isOpenAIWSSessionPreempted(ctx) {
				if result != nil && result.WSBridgeMeteredError && hooks != nil && hooks.AfterTurn != nil {
					hooks.AfterTurn(turn, result, bridgeErr)
				}
				result.releaseWSReplayMemory()
				return errOpenAIWSSessionPreempted
			}
			if hooks != nil && hooks.AfterTurn != nil {
				hooks.AfterTurn(turn, result, bridgeErr)
			}
			if bridgeErr != nil {
				result.releaseWSReplayMemory()
				var failoverErr *UpstreamFailoverError
				if turn > 1 && errors.As(bridgeErr, &failoverErr) && failoverErr != nil {
					retryPayload, retryOwner, retrySafe, retryPayloadErr := buildWSOwnedRetryPayload(ctx, currentBridgePayload.accountIdentitySourceRaw, turnReplayInput, turnReplayInputExists, currentBridgePayload.originalModel)
					if retryPayloadErr != nil {
						return fmt.Errorf("build websocket current-turn failover payload: %w", retryPayloadErr)
					}
					if !retrySafe {
						retryOwner.Drop()
						retryPayload = nil
						retryOwner = nil
					}
					if memoryEnabled {
						return &openAIWSCurrentTurnFailoverError{cause: bridgeErr, retryPayload: retryPayload, retryOwner: retryOwner}
					}
					return newOpenAIWSCurrentTurnFailoverError(bridgeErr, retryPayload)
				}
				return bridgeErr
			}
			if result == nil {
				return errors.New("websocket http bridge turn result is nil")
			}
			if memoryEnabled {
				if result.wsHistoryMemoryErr != nil {
					result.releaseWSReplayMemory()
					return NewOpenAIWSBodyMemoryCloseError(result.wsHistoryMemoryErr)
				}
				nextHistory := turnMemory.hold()
				if result.wsReplayMemory != nil {
					combined, err := combineWSMemoryReplay(ctx, nextHistory, *result.wsReplayMemory)
					nextHistory.drop()
					if err != nil {
						result.releaseWSReplayMemory()
						return NewOpenAIWSBodyMemoryCloseError(err)
					}
					nextHistory = combined
				}
				result.releaseWSReplayMemory()
				historyMemory.drop()
				historyMemory = nextHistory
				bridgeReplayInput, bridgeReplayInputExists = historyMemory.items, historyMemory.exists
				turnMemory.drop()
				currentItemsMemory.drop()
			} else {
				// 保存历史必须经 combine 新建头，禁止就地 append 或改写共享正文。
				bridgeReplayInput = turnReplayInput
				bridgeReplayInputExists = turnReplayInputExists
				if result.wsReplayInputExists {
					bridgeReplayInput = combineOpenAIWSReplayItems(bridgeReplayInput, result.wsReplayInput)
					bridgeReplayInputExists = true
				}
			}
			lastBridgeResponseID = strings.TrimSpace(result.ResponseID)
			if bridgeTurnState := strings.TrimSpace(result.ResponseHeaders.Get(openAIWSTurnStateHeader)); bridgeTurnState != "" {
				// Follow-up turns on this bridge retain their own upstream state;
				// publishing it by session hash would leak it to independent bridges.
				turnState = bridgeTurnState
			}
			responseID := strings.TrimSpace(result.RequestID)
			if responseID != "" && stateStore != nil {
				ttl := s.openAIWSResponseStickyTTL()
				logOpenAIWSBindResponseAccountWarn(groupID, account.ID, responseID, stateStore.BindResponseAccount(ctx, groupID, responseID, account.ID, ttl))
			}
			nextClientMessage, nextOwner, readErr := readClientMessage()
			if readErr != nil {
				if isOpenAIWSSessionPreempted(ctx) {
					return errOpenAIWSSessionPreempted
				}
				if isOpenAIWSClientDisconnectError(readErr) {
					closeStatus, closeReason := summarizeOpenAIWSReadCloseError(readErr)
					logOpenAIWSModeInfo(
						"ingress_ws_http_bridge_client_closed account_id=%d close_status=%s close_reason=%s",
						account.ID,
						closeStatus,
						truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
					)
					return nil
				}
				return fmt.Errorf("read client websocket request: %w", readErr)
			}
			nextPayload, parseErr := parseClientPayload(turn+1, nextClientMessage, nextOwner)
			if parseErr != nil {
				return parseErr
			}
			currentBridgePayload.memory.drop()
			currentBridgePayload = nextPayload
		}
	}

	firstRoutingFields := gjson.GetManyBytes(firstPayload.payloadRaw, "model", "service_tier")
	wsHeaders, _, buildHdrErr := s.buildOpenAIWSHeaders(
		ctx,
		c,
		account,
		token,
		wsDecision,
		isCodexCLI,
		turnState,
		strings.TrimSpace(c.GetHeader(openAIWSTurnMetadataHeader)),
		firstPayload.promptCacheKey,
		firstRoutingFields[0].String(),
		firstRoutingFields[1].String(),
	)
	if buildHdrErr != nil {
		return fmt.Errorf("build ws headers: %w", buildHdrErr)
	}
	baseAcquireReq := openAIWSAcquireRequest{
		Account: account,
		WSURL:   wsURL,
		Headers: wsHeaders,
		HeadersFactory: func(factoryCtx context.Context, headers http.Header) (http.Header, error) {
			return s.refreshOpenAIAgentIdentityHeaders(factoryCtx, account, headers)
		},
		ProxyURL: func() string {
			if account.ProxyID != nil && account.Proxy != nil {
				return account.Proxy.URL()
			}
			return ""
		}(),
		ForceNewConn: false,
	}
	pool := s.getOpenAIWSConnPool()
	if pool == nil {
		return errors.New("openai ws conn pool is nil")
	}

	logOpenAIWSModeInfo(
		"ingress_ws_protocol_confirm account_id=%d account_type=%s transport=%s ws_host=%s ws_path=%s ws_mode=%s store_disabled=%v has_session_hash=%v has_previous_response_id=%v",
		account.ID,
		account.Type,
		normalizeOpenAIWSLogValue(string(wsDecision.Transport)),
		wsHost,
		wsPath,
		normalizeOpenAIWSLogValue(ingressMode),
		storeDisabled,
		sessionHash != "",
		firstPayload.previousResponseID != "",
	)

	if debugEnabled {
		logOpenAIWSModeDebug(
			"ingress_ws_start account_id=%d account_type=%s transport=%s ws_host=%s preferred_conn_id=%s has_session_hash=%v has_previous_response_id=%v store_disabled=%v",
			account.ID,
			account.Type,
			normalizeOpenAIWSLogValue(string(wsDecision.Transport)),
			wsHost,
			truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
			sessionHash != "",
			firstPayload.previousResponseID != "",
			storeDisabled,
		)
	}
	if firstPayload.previousResponseID != "" {
		firstPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(firstPayload.previousResponseID)
		logOpenAIWSModeInfo(
			"ingress_ws_continuation_probe account_id=%d turn=%d previous_response_id=%s previous_response_id_kind=%s preferred_conn_id=%s session_hash=%s header_session_id=%s header_conversation_id=%s has_turn_state=%v turn_state_len=%d has_prompt_cache_key=%v store_disabled=%v",
			account.ID,
			1,
			truncateOpenAIWSLogValue(firstPayload.previousResponseID, openAIWSIDValueMaxLen),
			normalizeOpenAIWSLogValue(firstPreviousResponseIDKind),
			truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
			truncateOpenAIWSLogValue(sessionHash, 12),
			openAIWSHeaderValueForLog(baseAcquireReq.Headers, "session_id"),
			openAIWSHeaderValueForLog(baseAcquireReq.Headers, "conversation_id"),
			turnState != "",
			len(turnState),
			firstPayload.promptCacheKey != "",
			storeDisabled,
		)
	}

	acquireTimeout := s.openAIWSAcquireTimeout()
	if acquireTimeout <= 0 {
		acquireTimeout = 30 * time.Second
	}

	agentTaskRecoveryTried := false
	var acquireTurnLease func(int, string, bool, bool) (*openAIWSConnLease, error)
	acquireTurnLease = func(turn int, preferred string, forcePreferredConn bool, forceNewConn bool) (*openAIWSConnLease, error) {
		req := cloneOpenAIWSAcquireRequest(baseAcquireReq)
		req.PreferredConnID = strings.TrimSpace(preferred)
		req.ForcePreferredConn = forcePreferredConn
		// dedicated 模式下每次获取均新建连接，避免跨会话复用残留上下文；
		// 上游读写失败后的重试同样新建，避免再拿到同批陈旧的空闲连接。
		req.ForceNewConn = dedicatedMode || forceNewConn
		acquireCtx, acquireCancel := context.WithTimeout(ctx, acquireTimeout)
		lease, acquireErr := pool.Acquire(acquireCtx, req)
		acquireCancel()
		var dialErr *openAIWSDialError
		if acquireErr != nil && s.isAgentIdentityAccount(ctx, account) && errors.As(acquireErr, &dialErr) && isAgentIdentityTaskInvalidWSDialError(dialErr) && !agentTaskRecoveryTried {
			agentTaskRecoveryTried = true
			if recoveryErr := s.recoverAgentIdentityTask(ctx, account, account.GetCredential("task_id")); recoveryErr != nil {
				return nil, fmt.Errorf("agent identity task recovery failed: %w", recoveryErr)
			}
			return acquireTurnLease(turn, preferred, forcePreferredConn, forceNewConn)
		}
		if acquireErr != nil {
			if isOpenAIWSSessionPreempted(ctx) {
				return nil, errOpenAIWSSessionPreempted
			}
			canonicalModel := canonicalOpenAIAccountSchedulingModel(account, ingressSessionOriginalModel)
			s.handleOpenAIWSDialTransientFailure(ctx, account, canonicalModel, acquireErr)
			dialStatus, dialClass, dialCloseStatus, dialCloseReason, dialRespServer, dialRespVia, dialRespCFRay, dialRespReqID := summarizeOpenAIWSDialError(acquireErr)
			logOpenAIWSModeInfo(
				"ingress_ws_upstream_acquire_fail account_id=%d turn=%d reason=%s dial_status=%d dial_class=%s dial_close_status=%s dial_close_reason=%s dial_resp_server=%s dial_resp_via=%s dial_resp_cf_ray=%s dial_resp_x_request_id=%s cause=%s preferred_conn_id=%s force_preferred_conn=%v ws_host=%s ws_path=%s proxy_enabled=%v",
				account.ID,
				turn,
				normalizeOpenAIWSLogValue(classifyOpenAIWSAcquireError(acquireErr)),
				dialStatus,
				dialClass,
				dialCloseStatus,
				truncateOpenAIWSLogValue(dialCloseReason, openAIWSHeaderValueMaxLen),
				dialRespServer,
				dialRespVia,
				dialRespCFRay,
				dialRespReqID,
				truncateOpenAIWSLogValue(acquireErr.Error(), openAIWSLogValueMaxLen),
				truncateOpenAIWSLogValue(preferred, openAIWSIDValueMaxLen),
				forcePreferredConn,
				wsHost,
				wsPath,
				account.ProxyID != nil && account.Proxy != nil,
			)
			var dialErr *openAIWSDialError
			if errors.As(acquireErr, &dialErr) && dialErr != nil && dialErr.StatusCode == http.StatusTooManyRequests {
				s.persistOpenAIWSRateLimitSignal(ctx, account, dialErr.ResponseHeaders, nil, "rate_limit_exceeded", "rate_limit_error", strings.TrimSpace(acquireErr.Error()), canonicalModel)
				return nil, s.newOpenAIWSRateLimitFailoverError(account, dialErr.ResponseHeaders, nil, acquireErr.Error())
			}
			if errors.Is(acquireErr, errOpenAIWSPreferredConnUnavailable) {
				return nil, NewOpenAIWSClientCloseError(
					coderws.StatusPolicyViolation,
					"upstream continuation connection is unavailable; please restart the conversation",
					acquireErr,
				)
			}
			if errors.Is(acquireErr, context.DeadlineExceeded) || errors.Is(acquireErr, errOpenAIWSConnQueueFull) {
				return nil, NewOpenAIWSClientCloseError(
					coderws.StatusTryAgainLater,
					"upstream websocket is busy, please retry later",
					acquireErr,
				)
			}
			return nil, acquireErr
		}
		connID := strings.TrimSpace(lease.ConnID())
		if handshakeTurnState := strings.TrimSpace(lease.HandshakeHeader(openAIWSTurnStateHeader)); handshakeTurnState != "" {
			turnState = handshakeTurnState
			if stateStore != nil && sessionHash != "" {
				stateStore.BindSessionTurnState(groupID, sessionHash, handshakeTurnState, s.openAIWSSessionStickyTTL())
			}
			updatedHeaders := cloneHeader(baseAcquireReq.Headers)
			if updatedHeaders == nil {
				updatedHeaders = make(http.Header)
			}
			updatedHeaders.Set(openAIWSTurnStateHeader, handshakeTurnState)
			baseAcquireReq.Headers = updatedHeaders
		}
		logOpenAIWSModeInfo(
			"ingress_ws_upstream_connected account_id=%d turn=%d conn_id=%s conn_reused=%v conn_idle_ms=%d conn_age_ms=%d upstream_pings=%d conn_pick_ms=%d queue_wait_ms=%d preferred_conn_id=%s",
			account.ID,
			turn,
			truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
			lease.Reused(),
			lease.IdleBefore().Milliseconds(),
			lease.AgeBefore().Milliseconds(),
			lease.UpstreamPingCount(),
			lease.ConnPickDuration().Milliseconds(),
			lease.QueueWaitDuration().Milliseconds(),
			truncateOpenAIWSLogValue(preferred, openAIWSIDValueMaxLen),
		)
		return lease, nil
	}

	var rejectedFieldRetryState *openAIResponsesRejectedFieldRetryState
	sendAndRelay := func(turn int, lease *openAIWSConnLease, payload []byte, payloadBytes int, originalModel string, imageBillingModel string, imageSizeTier string, imageInputSize string, requestedReasoningEffort *string) (*OpenAIForwardResult, error) {
		responseModelObserver := &upstreamResponseModelObserver{}
		if lease == nil {
			return nil, errors.New("upstream websocket lease is nil")
		}
		turnStart := time.Now()
		turnReadMemory := newWSMaterializer(ctx)
		turnHasFunctionCallOutput := openAIWSRawPayloadHasToolCallOutput(payload, turnReadMemory)
		if turnReadMemory != nil && turnReadMemory.err != nil {
			turnReadMemory.close()
			return nil, turnReadMemory.err
		}
		turnReadMemory.close()
		_, releaseEncoder, encodeErr := reserveWSJSONEncodingWork(ctx, json.RawMessage(payload), true)
		if encodeErr != nil {
			return nil, encodeErr
		}
		wroteDownstream := false
		writeErr := lease.WriteJSONWithContextTimeout(ctx, json.RawMessage(payload), s.openAIWSWriteTimeout())
		releaseEncoder()
		if err := writeErr; err != nil {
			return nil, wrapOpenAIWSIngressTurnError(
				"write_upstream",
				fmt.Errorf("write upstream websocket request: %w", err),
				false,
			)
		}
		if debugEnabled {
			logOpenAIWSModeDebug(
				"ingress_ws_turn_request_sent account_id=%d turn=%d conn_id=%s payload_bytes=%d",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
				payloadBytes,
			)
		}

		responseID := ""
		usage := OpenAIUsage{}
		imageCounter := newOpenAIImageOutputCounter()
		var firstTokenMs *int
		reqStream := openAIWSPayloadBoolFromRaw(payload, "stream", true)
		turnPreviousResponseID := openAIWSPayloadStringFromRaw(payload, "previous_response_id")
		turnPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(turnPreviousResponseID)
		turnPromptCacheKey := openAIWSPayloadStringFromRaw(payload, "prompt_cache_key")
		turnStoreDisabled := s.isOpenAIWSStoreDisabledInRequestRaw(payload, account)

		eventCount := 0
		tokenEventCount := 0
		terminalEventCount := 0
		replayCollector := newWSReplayCollector(ctx)
		defer replayCollector.release()
		firstEventType := ""
		lastEventType := ""
		needModelReplace := false
		clientDisconnected := false
		mappedModel := ""
		var mappedModelBytes []byte
		if originalModel != "" {
			mappedModel = strings.TrimSpace(gjson.GetBytes(payload, "model").String())
			if mappedModel == "" {
				mappedModel = normalizeOpenAIModelForUpstream(account, account.GetMappedModel(originalModel))
			}
			needModelReplace = mappedModel != "" && mappedModel != originalModel
			if needModelReplace {
				mappedModelBytes = []byte(mappedModel)
			}
		}
		for {
			upstreamMessage, readErr := lease.ReadMessageWithContextTimeout(ctx, s.openAIWSReadTimeout())
			if readErr != nil {
				lease.MarkBroken()
				return nil, wrapOpenAIWSIngressTurnError(
					"read_upstream",
					fmt.Errorf("read upstream websocket event: %w", readErr),
					wroteDownstream,
				)
			}
			if normalized, changed := normalizeCompletedImageGenerationStatus(upstreamMessage); changed {
				upstreamMessage = normalized
			}

			eventType, eventResponseID, _ := parseOpenAIWSEventEnvelope(upstreamMessage)
			responseModelObserver.ObserveOpenAI(upstreamMessage, eventType)
			if responseID == "" && eventResponseID != "" {
				responseID = eventResponseID
			}
			if eventType != "" {
				eventCount++
				if firstEventType == "" {
					firstEventType = eventType
				}
				lastEventType = eventType
			}
			if openAIWSMessageShouldParseUsage(eventType, upstreamMessage) {
				parseOpenAIWSResponseUsageFromCompletedEvent(upstreamMessage, &usage)
			}
			if eventType == "error" || eventType == "response.failed" {
				markOpenAICyberPolicyEvent(c, upstreamMessage, http.StatusOK, &usage)
			}
			if eventType == "error" {
				s.handleOpenAIWSErrorEventTransientFailure(ctx, account, mappedModel, lease.HandshakeHeaders(), upstreamMessage)
				errCodeRaw, errTypeRaw, errMsgRaw := parseOpenAIWSErrorEventFields(upstreamMessage)
				statusCode := openAIWSRejectedFieldRetryHTTPStatus(upstreamMessage)
				if !wroteDownstream && statusCode == http.StatusBadRequest && rejectedFieldRetryState != nil {
					retryMem := newWSMaterializer(ctx)
					retryBody, retryReason, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(statusCode, payload, upstreamMessage, retryMem)
					if retryMem != nil && retryMem.err != nil {
						retryErr = retryMem.err
					}
					retryOwner := retryMem.take(retryBody)
					retryMem.close()
					if retryErr != nil {
						retryOwner.Drop()
						return nil, fmt.Errorf("normalize websocket rejected field retry: %w", retryErr)
					}
					if changed && rejectedFieldRetryState.Allow(retryBody) {
						logOpenAIWSModeInfo(
							"ingress_ws_rejected_field_retry account_id=%d turn=%d conn_id=%s reason=%s",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(retryReason, openAIWSLogValueMaxLen),
						)
						return nil, &openAIWSRejectedFieldRetryError{
							body:   retryBody,
							reason: retryReason,
							owner:  retryOwner,
						}
					}
					retryOwner.Drop()
				}
				s.persistOpenAIWSRateLimitSignal(ctx, account, lease.HandshakeHeaders(), upstreamMessage, errCodeRaw, errTypeRaw, errMsgRaw, mappedModel)
				fallbackReason, _ := classifyOpenAIWSErrorEventFromRaw(errCodeRaw, errTypeRaw, errMsgRaw)
				if fallbackReason == openAIWSFallbackReasonInvalidEncryptedContent {
					// 记录被上游拒绝的密文摘要；错误照旧透传，下一轮进场时按摘要预剥离。
					lineageMemory := newWSMaterializer(ctx)
					digests := collectOpenAIEncryptedContentDigestsRaw(payload, lineageMemory)
					lineageMemory.close()
					if lineageMemory != nil && lineageMemory.err != nil {
						return nil, lineageMemory.err
					}
					if len(digests) > 0 {
						s.markOpenAIWSInvalidEncryptedContentLineage(groupID, sessionHash, digests)
						logOpenAIWSModeInfo(
							"ingress_ws_invalid_encrypted_lineage_mark account_id=%d turn=%d digests=%d",
							account.ID,
							turn,
							len(digests),
						)
					}
				}
				errCode, errType, errMessage := summarizeOpenAIWSErrorEventFieldsFromRaw(errCodeRaw, errTypeRaw, errMsgRaw)
				recoverablePrevNotFound := fallbackReason == openAIWSIngressStagePreviousResponseNotFound &&
					turnPreviousResponseID != "" &&
					!turnHasFunctionCallOutput &&
					s.openAIWSIngressPreviousResponseRecoveryEnabled() &&
					!wroteDownstream
				if recoverablePrevNotFound {
					// 可恢复场景使用非 error 关键字日志，避免被 LegacyPrintf 误判为 ERROR 级别。
					logOpenAIWSModeInfo(
						"ingress_ws_prev_response_recoverable account_id=%d turn=%d conn_id=%s idx=%d reason=%s code=%s type=%s message=%s previous_response_id=%s previous_response_id_kind=%s response_id=%s store_disabled=%v has_prompt_cache_key=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
						eventCount,
						truncateOpenAIWSLogValue(fallbackReason, openAIWSLogValueMaxLen),
						errCode,
						errType,
						errMessage,
						truncateOpenAIWSLogValue(turnPreviousResponseID, openAIWSIDValueMaxLen),
						normalizeOpenAIWSLogValue(turnPreviousResponseIDKind),
						truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
						turnStoreDisabled,
						turnPromptCacheKey != "",
					)
				} else {
					logOpenAIWSModeInfo(
						"ingress_ws_error_event account_id=%d turn=%d conn_id=%s idx=%d fallback_reason=%s err_code=%s err_type=%s err_message=%s previous_response_id=%s previous_response_id_kind=%s response_id=%s store_disabled=%v has_prompt_cache_key=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
						eventCount,
						truncateOpenAIWSLogValue(fallbackReason, openAIWSLogValueMaxLen),
						errCode,
						errType,
						errMessage,
						truncateOpenAIWSLogValue(turnPreviousResponseID, openAIWSIDValueMaxLen),
						normalizeOpenAIWSLogValue(turnPreviousResponseIDKind),
						truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
						turnStoreDisabled,
						turnPromptCacheKey != "",
					)
				}
				// previous_response_not_found 在 ingress 模式支持单次恢复重试：
				// 不把该 error 直接下发客户端，而是由上层去掉 previous_response_id 后重放当前 turn。
				if recoverablePrevNotFound {
					lease.MarkBroken()
					errMsg := strings.TrimSpace(errMsgRaw)
					if errMsg == "" {
						errMsg = "previous response not found"
					}
					return nil, wrapOpenAIWSIngressTurnError(
						openAIWSIngressStagePreviousResponseNotFound,
						errors.New(errMsg),
						false,
					)
				}
				if !wroteDownstream && isOpenAIWSRateLimitError(errCodeRaw, errTypeRaw, errMsgRaw) {
					lease.MarkBroken()
					return nil, s.newOpenAIWSRateLimitFailoverError(account, lease.HandshakeHeaders(), upstreamMessage, errMsgRaw)
				}
			}
			isTokenEvent := isOpenAIWSTokenEvent(eventType)
			if isTokenEvent {
				tokenEventCount++
			}
			isTerminalEvent := isOpenAIWSTerminalEvent(eventType)
			if isTerminalEvent {
				terminalEventCount++
			}
			if firstTokenMs == nil && isTokenEvent {
				ms := int(time.Since(turnStart).Milliseconds())
				firstTokenMs = &ms
			}
			imageCounter.AddSSEData(upstreamMessage)

			if !clientDisconnected {
				if needModelReplace && len(mappedModelBytes) > 0 && openAIWSEventMayContainModel(eventType) && bytes.Contains(upstreamMessage, mappedModelBytes) {
					upstreamMessage = replaceOpenAIWSMessageModel(upstreamMessage, mappedModel, originalModel)
				}
				if openAIWSEventMayContainToolCalls(eventType) && openAIWSMessageLikelyContainsToolCalls(upstreamMessage) {
					if corrected, changed := s.toolCorrector.CorrectToolCallsInSSEBytes(upstreamMessage); changed {
						upstreamMessage = corrected
					}
				}
				replayCollector.AddEvent(eventType, upstreamMessage)
				// 客户端写出副本改写容量降载码：Codex 对 error/response.failed 中的
				// server_is_overloaded / slow_down 判致命并终止会话，改写后走客户端
				// 内置退避重试。HTTP/SSE（openai_gateway_response_handling.go）与
				// http_bridge（openai_ws_http_bridge.go）两条路径早已这么做，
				// ctx_pool 的 ingress 直写路径是唯一漏掉的一条 —— 同一个上游降载
				// 事件在这里会让会话就地终止，切到 http_bridge 却能正常退避重试。
				//
				// 必须写进独立变量而不是原地改 upstreamMessage：下面的
				// markOpenAIWSClientVisibleFailure 与 handleOpenAIWSTerminalTransientFailure
				// 仍要按未改写的原始 payload 判定账号状态，这正是
				// sanitizeOpenAICapacityShedErrorCodeForClient 注释里写明的前提。
				clientMessage := upstreamMessage
				if eventType == "error" || eventType == "response.failed" {
					if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
						clientMessage = rewritten
					}
				}
				if err := writeClientMessage(clientMessage); err != nil {
					if isOpenAIWSClientDisconnectError(err) {
						clientDisconnected = true
						closeStatus, closeReason := summarizeOpenAIWSReadCloseError(err)
						logOpenAIWSModeInfo(
							"ingress_ws_client_disconnected_drain account_id=%d turn=%d conn_id=%s close_status=%s close_reason=%s",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
							closeStatus,
							truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
						)
					} else {
						return nil, wrapOpenAIWSIngressTurnError(
							"write_client",
							fmt.Errorf("write client websocket event: %w", err),
							wroteDownstream,
						)
					}
				} else {
					wroteDownstream = true
					markOpenAIWSClientVisibleFailure(c, eventType, upstreamMessage)
				}
			}
			if isTerminalEvent {
				terminalEvent := s.handleOpenAIWSTerminalTransientFailure(ctx, account, mappedModel, lease.HandshakeHeaders(), upstreamMessage)
				// 客户端已断连时，上游连接的 session 状态不可信，标记 broken 避免回池复用。
				if clientDisconnected {
					lease.MarkBroken()
				}
				firstTokenMsValue := -1
				if firstTokenMs != nil {
					firstTokenMsValue = *firstTokenMs
				}
				if debugEnabled {
					logOpenAIWSModeDebug(
						"ingress_ws_turn_completed account_id=%d turn=%d conn_id=%s response_id=%s duration_ms=%d events=%d token_events=%d terminal_events=%d first_event=%s last_event=%s first_token_ms=%d client_disconnected=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
						time.Since(turnStart).Milliseconds(),
						eventCount,
						tokenEventCount,
						terminalEventCount,
						truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
						truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
						firstTokenMsValue,
						clientDisconnected,
					)
				}
				imageCount := imageCounter.Count()
				result := &OpenAIForwardResult{
					RequestID:                     responseID,
					Usage:                         usage,
					Model:                         originalModel,
					UpstreamModel:                 mappedModel,
					UpstreamResponseModel:         responseModelObserver.Model(),
					UpstreamResponseModelConflict: responseModelObserver.Conflict(),
					UpstreamResponseServiceTier:   responseModelObserver.ServiceTier(),
					ServiceTier:                   resolvedOpenAIUpstreamServiceTierFromObserver(responseModelObserver, extractOpenAIServiceTierFromBody(payload)),
					ReasoningEffort:               ApplyThinkingEnabledFallback(extractOpenAIReasoningEffortFromBody(payload, mappedModel, originalModel), payload, mappedModel),
					RequestedReasoningEffort:      requestedReasoningEffort,
					Stream:                        reqStream,
					OpenAIWSMode:                  true,
					UpstreamTerminalEvent:         terminalEvent,
					ResponseHeaders:               lease.HandshakeHeaders(),
					Duration:                      time.Since(turnStart),
					FirstTokenMs:                  firstTokenMs,
				}
				// Keep complete output while search activation can require a new socket.
				replayCollector.attachResult(result, account.IsOpenAIOAuthLike() && lease.initializedWithoutWebSearch(), true)
				if imageCount > 0 {
					result.ImageCount = imageCount
					result.ImageSize = imageSizeTier
					result.ImageInputSize = imageInputSize
					result.ImageOutputSizes = imageCounter.Sizes()
					result.BillingModel = imageBillingModel
				}
				return result, nil
			}
		}
	}

	currentPayload := firstPayload.payloadRaw
	currentMemory := firstPayload.memory.hold()
	defer func() { currentMemory.drop() }()
	// Admission hooks must see client model candidates before upstream mapping.
	currentClientPayload := firstPayload.rawForHash
	currentOriginalModel := firstPayload.originalModel
	currentImageBillingModel := firstPayload.imageBillingModel
	currentImageSizeTier := firstPayload.imageSizeTier
	currentImageInputSize := firstPayload.imageInputSize
	currentPayloadBytes := firstPayload.payloadBytes
	currentRequestedReasoningEffort := firstPayload.requestedReasoningEffort
	isStrictAffinityTurn := func(payload []byte) bool {
		if !storeDisabled {
			return false
		}
		return strings.TrimSpace(openAIWSPayloadStringFromRaw(payload, "previous_response_id")) != ""
	}
	var sessionLease *openAIWSConnLease
	sessionConnID := ""
	pinnedSessionConnID := ""
	unpinSessionConn := func(connID string) {
		connID = strings.TrimSpace(connID)
		if connID == "" || pinnedSessionConnID != connID {
			return
		}
		pool.UnpinConn(account.ID, connID)
		pinnedSessionConnID = ""
	}
	pinSessionConn := func(connID string) {
		if !storeDisabled {
			return
		}
		connID = strings.TrimSpace(connID)
		if connID == "" || pinnedSessionConnID == connID {
			return
		}
		if pinnedSessionConnID != "" {
			pool.UnpinConn(account.ID, pinnedSessionConnID)
			pinnedSessionConnID = ""
		}
		if pool.PinConn(account.ID, connID) {
			pinnedSessionConnID = connID
		}
	}
	// lastTurnClean 标记最后一轮 sendAndRelay 是否正常完成（收到终端事件且客户端未断连）。
	// 所有异常路径（读写错误、error 事件、客户端断连）已在各自分支或上层（L3403）中 MarkBroken，
	// 因此 releaseSessionLease 中只需在非正常结束时 MarkBroken。
	lastTurnClean := false
	releaseSessionLease := func() {
		if sessionLease == nil {
			return
		}
		if !lastTurnClean {
			sessionLease.MarkBroken()
		}
		unpinSessionConn(sessionConnID)
		sessionLease.Release()
		if debugEnabled {
			logOpenAIWSModeDebug(
				"ingress_ws_upstream_released account_id=%d conn_id=%s",
				account.ID,
				truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
			)
		}
	}
	defer releaseSessionLease()

	turn := 1
	defer func() {
		var failoverErr *UpstreamFailoverError
		if turn > 1 && errors.As(returnErr, &failoverErr) {
			// Native ingress does not retain a complete conversation for account
			// migration. A nil current-turn payload makes the handler close the
			// client connection instead of replaying its original first message.
			returnErr = newOpenAIWSCurrentTurnFailoverError(returnErr, nil)
		}
	}()
	rejectedFieldRetryState = newOpenAIResponsesRejectedFieldRetryState(currentPayload)
	turnRetry := 0
	turnPrevRecoveryTried := false
	lastTurnFinishedAt := time.Time{}
	lastTurnResponseID := ""
	lastTurnWindowID := ""
	lastTurnPayload := []byte(nil)
	var lastTurnPayloadOwner, strictOwner *pkghttputil.WSBodyMemoryOwner
	var lastReplayMemory, currentReplayMemory wsMemoryReplay
	nativeOps := newWSMaterializer(ctx)
	defer func() {
		nativeOps.close()
		lastReplayMemory.drop()
		currentReplayMemory.drop()
		lastTurnPayloadOwner.Drop()
		strictOwner.Drop()
	}()
	adoptNative := func(body []byte) {
		if owner := nativeOps.take(body); owner != nil {
			currentMemory.replace(owner)
		}
	}
	setNativeInput := func(body []byte, items []json.RawMessage, exists bool) ([]byte, error) {
		out, owner, err := setWSBodyInput(ctx, body, items, exists)
		return nativeOps.track(out, owner, err)
	}
	var lastTurnStrictState *openAIWSIngressPreviousTurnStrictState
	lastTurnReplayInput := []json.RawMessage(nil)
	lastTurnReplayInputExists := false
	lastTurnReplayInputComplete := false
	currentTurnReplayInput := []json.RawMessage(nil)
	currentTurnReplayInputExists := false
	skipBeforeTurn := false
	hasCurrentOrReplayFunctionCallOutput := func(payload []byte) bool {
		if openAIWSRawPayloadHasToolCallOutput(payload, nativeOps) {
			return true
		}
		return currentTurnReplayInputExists && openAIWSRawItemsHasFunctionCallOutput(currentTurnReplayInput, nativeOps)
	}
	resetSessionLease := func(markBroken bool) {
		if sessionLease == nil {
			return
		}
		if markBroken {
			sessionLease.MarkBroken()
		}
		releaseSessionLease()
		sessionLease = nil
		sessionConnID = ""
		preferredConnID = ""
	}
	recoverIngressPrevResponseNotFound := func(relayErr error, turn int, connID string) bool {
		if !isOpenAIWSIngressPreviousResponseNotFound(relayErr) {
			return false
		}
		if turnPrevRecoveryTried || !s.openAIWSIngressPreviousResponseRecoveryEnabled() {
			return false
		}
		// 携带 function_call_output 的请求不能丢弃 previous_response_id：
		// 上游 API 需要 response chain 来匹配 tool_result 与之前的 tool_use，
		// 丢弃后会导致 "No tool call found for function call output" 400 错误。
		if hasCurrentOrReplayFunctionCallOutput(currentPayload) {
			return false
		}
		if isStrictAffinityTurn(currentPayload) {
			// Layer 2：严格亲和链路命中 previous_response_not_found 时，降级为“去掉 previous_response_id 后重放一次”。
			// 该错误说明续链锚点已失效，继续 strict fail-close 只会直接中断本轮请求。
			logOpenAIWSModeInfo(
				"ingress_ws_prev_response_recovery_layer2 account_id=%d turn=%d conn_id=%s store_disabled_conn_mode=%s action=drop_previous_response_id_retry",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				normalizeOpenAIWSLogValue(storeDisabledConnMode),
			)
		}
		turnPrevRecoveryTried = true
		updatedPayload, removed, dropErr := dropPreviousResponseIDFromRawPayload(currentPayload, nativeOps)
		if dropErr != nil || !removed {
			reason := "not_removed"
			if dropErr != nil {
				reason = "drop_error"
			}
			logOpenAIWSModeInfo(
				"ingress_ws_prev_response_recovery_skip account_id=%d turn=%d conn_id=%s reason=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				normalizeOpenAIWSLogValue(reason),
			)
			return false
		}
		updatedWithInput, setInputErr := setNativeInput(
			updatedPayload,
			currentTurnReplayInput,
			currentTurnReplayInputExists,
		)
		if setInputErr != nil {
			logOpenAIWSModeInfo(
				"ingress_ws_prev_response_recovery_skip account_id=%d turn=%d conn_id=%s reason=set_full_input_error cause=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(setInputErr.Error(), openAIWSLogValueMaxLen),
			)
			return false
		}
		logOpenAIWSModeInfo(
			"ingress_ws_prev_response_recovery account_id=%d turn=%d conn_id=%s action=drop_previous_response_id retry=1",
			account.ID,
			turn,
			truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
		)
		adoptNative(updatedWithInput)
		currentPayload = updatedWithInput
		currentPayloadBytes = len(updatedWithInput)
		resetSessionLease(true)
		skipBeforeTurn = true
		return true
	}
	retryIngressTurn := func(relayErr error, turn int, connID string) bool {
		if !isOpenAIWSIngressTurnRetryable(relayErr) || turnRetry >= 1 {
			return false
		}
		if isStrictAffinityTurn(currentPayload) {
			logOpenAIWSModeInfo(
				"ingress_ws_turn_retry_skip account_id=%d turn=%d conn_id=%s reason=strict_affinity",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
			)
			return false
		}
		turnRetry++
		logOpenAIWSModeInfo(
			"ingress_ws_turn_retry account_id=%d turn=%d retry=%d reason=%s conn_id=%s",
			account.ID,
			turn,
			turnRetry,
			truncateOpenAIWSLogValue(openAIWSIngressTurnRetryReason(relayErr), openAIWSLogValueMaxLen),
			truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
		)
		resetSessionLease(true)
		skipBeforeTurn = true
		return true
	}
	for {
		if nativeOps != nil && nativeOps.err != nil {
			return nativeOps.err
		}
		nativeOps.close()
		nativeOps = newWSMaterializer(ctx)
		if turn > 1 && !skipBeforeTurn && hooks != nil && hooks.BeforeRequest != nil {
			if err := hooks.BeforeRequest(turn, currentClientPayload, currentOriginalModel); err != nil {
				return err
			}
		}
		if !skipBeforeTurn && hooks != nil && hooks.BeforeTurn != nil {
			if err := hooks.BeforeTurn(turn); err != nil {
				return err
			}
		}
		if !skipBeforeTurn && hooks != nil && hooks.BeforeForward != nil {
			if err := hooks.BeforeForward(turn, currentPayload, currentOriginalModel); err != nil {
				return err
			}
		}
		skipBeforeTurn = false
		// 剥离本会话已知失效的加密项，阻断同一失效密文随历史反复触发上游拒绝。
		// 历史序列须同步剥离，否则与已剥离的当前 input 项错位，prefix 复用失配。
		if invalidDigests := s.sessionInvalidEncryptedContentDigests(groupID, sessionHash); len(invalidDigests) > 0 {
			strippedPayload, strippedOwner, strippedCount, stripErr := s.stripWSInvalidFrameLogged(ctx, currentPayload, invalidDigests, "ingress_ws_invalid_encrypted_lineage_strip", account.ID, turn)
			if stripErr != nil {
				return stripErr
			}
			if strippedCount > 0 {
				if strippedOwner != nil {
					currentMemory.replace(strippedOwner)
				}
				currentPayload = strippedPayload
				currentPayloadBytes = len(strippedPayload)
			}
			if lastTurnReplayInputExists {
				if nativeOps != nil {
					next, err := stripWSMemoryReplay(ctx, lastReplayMemory, invalidDigests)
					if err != nil {
						return err
					}
					lastReplayMemory.drop()
					lastReplayMemory = next
					lastTurnReplayInput = next.items
				} else {
					lastTurnReplayInput, _ = stripOpenAIInvalidEncryptedContentFromReplayItems(lastTurnReplayInput, invalidDigests)
				}
			}
		}
		boundaryPayload, contextWindowBoundary, boundaryErr := normalizeOpenAIWSContextWindowBoundary(
			currentPayload,
			lastTurnWindowID, nativeOps,
		)
		if boundaryErr != nil {
			return fmt.Errorf("normalize Codex websocket context-window boundary: %w", boundaryErr)
		}
		if contextWindowBoundary.Changed {
			adoptNative(boundaryPayload)
			currentPayload = boundaryPayload
			currentPayloadBytes = len(boundaryPayload)
			logOpenAIWSModeInfo(
				"ingress_ws_context_window_changed account_id=%d turn=%d conn_id=%s action=break_previous_response_chain previous_window_id=%s current_window_id=%s previous_response_id_removed=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(lastTurnWindowID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(contextWindowBoundary.WindowID, openAIWSIDValueMaxLen),
				contextWindowBoundary.PreviousResponseIDRemoved,
			)
		}
		currentPreviousResponseID := openAIWSPayloadStringFromRaw(currentPayload, "previous_response_id")
		expectedPrev := strings.TrimSpace(lastTurnResponseID)
		if contextWindowBoundary.Changed {
			// A context-window rollover is a new Responses root. Do not infer a
			// continuation anchor from the response produced in the old window.
			expectedPrev = ""
		}
		toolSignals := ToolContinuationSignals{
			HasFunctionCallOutput: openAIWSRawPayloadHasToolCallOutput(currentPayload, nativeOps),
		}
		if nativeOps != nil && nativeOps.err != nil {
			return nativeOps.err
		}
		if toolSignals.HasFunctionCallOutput {
			var currentReqBody map[string]any
			if err := nativeOps.unmarshal(currentPayload, &currentReqBody); err == nil {
				toolSignals = AnalyzeToolContinuationSignals(currentReqBody)
			} else if nativeOps != nil && nativeOps.err != nil {
				return NewOpenAIWSBodyMemoryCloseError(nativeOps.err)
			}
		}
		hasFunctionCallOutput := toolSignals.HasFunctionCallOutput
		// store=false + function_call_output 场景必须有续链锚点。
		// 若客户端未传 previous_response_id，优先回填上一轮响应 ID，避免上游报 call_id 无法关联。
		if shouldInferIngressFunctionCallOutputPreviousResponseID(
			storeDisabled,
			turn,
			toolSignals,
			currentPreviousResponseID,
			expectedPrev,
		) {
			updatedPayload, setPrevErr := setPreviousResponseIDToRawPayload(currentPayload, expectedPrev, nativeOps)
			if setPrevErr != nil {
				logOpenAIWSModeInfo(
					"ingress_ws_function_call_output_prev_infer_skip account_id=%d turn=%d conn_id=%s reason=set_previous_response_id_error cause=%s expected_previous_response_id=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(setPrevErr.Error(), openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				)
			} else {
				adoptNative(updatedPayload)
				currentPayload = updatedPayload
				currentPayloadBytes = len(updatedPayload)
				currentPreviousResponseID = expectedPrev
				logOpenAIWSModeInfo(
					"ingress_ws_function_call_output_prev_infer account_id=%d turn=%d conn_id=%s action=set_previous_response_id previous_response_id=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				)
			}
		}
		var nextReplayInput []json.RawMessage
		var nextReplayInputExists bool
		var replayInputErr error
		if nativeOps != nil {
			currentReplayMemory.drop()
			items, err := extractWSMemoryReplay(ctx, currentPayload, currentMemory.normalized)
			if err != nil {
				return err
			}
			currentReplayMemory, err = buildWSMemoryReplay(ctx, lastReplayMemory, items, currentPreviousResponseID != "")
			items.drop()
			if err != nil {
				return err
			}
			nextReplayInput, nextReplayInputExists = currentReplayMemory.items, currentReplayMemory.exists
		} else {
			nextReplayInput, nextReplayInputExists, replayInputErr = buildOpenAIWSReplayInputSequence(lastTurnReplayInput, lastTurnReplayInputExists, currentPayload, currentPreviousResponseID != "")
		}
		currentTurnReplayInputComplete := replayInputErr == nil && (currentPreviousResponseID == "" ||
			(lastTurnReplayInputComplete && currentPreviousResponseID == lastTurnResponseID))
		if replayInputErr != nil {
			logOpenAIWSModeInfo(
				"ingress_ws_replay_input_skip account_id=%d turn=%d conn_id=%s reason=build_error cause=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(replayInputErr.Error(), openAIWSLogValueMaxLen),
			)
			currentTurnReplayInput = nil
			currentTurnReplayInputExists = false
		} else {
			currentTurnReplayInput = nextReplayInput
			currentTurnReplayInputExists = nextReplayInputExists
		}
		replayHasFunctionCallOutput := currentTurnReplayInputExists &&
			openAIWSRawItemsHasFunctionCallOutput(currentTurnReplayInput, nativeOps)
		hasFunctionCallOutput = hasFunctionCallOutput || replayHasFunctionCallOutput
		if storeDisabled && turn > 1 && currentPreviousResponseID != "" {
			shouldKeepPreviousResponseID := false
			strictReason := ""
			var strictErr error
			if lastTurnStrictState != nil {
				shouldKeepPreviousResponseID, strictReason, strictErr = shouldKeepIngressPreviousResponseIDWithStrictState(
					lastTurnStrictState,
					currentPayload,
					lastTurnResponseID,
					hasFunctionCallOutput, nativeOps,
				)
			} else {
				shouldKeepPreviousResponseID, strictReason, strictErr = shouldKeepIngressPreviousResponseID(
					lastTurnPayload,
					currentPayload,
					lastTurnResponseID,
					hasFunctionCallOutput, nativeOps,
				)
			}
			if strictErr != nil {
				logOpenAIWSModeInfo(
					"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=keep_previous_response_id reason=%s cause=%s previous_response_id=%s expected_previous_response_id=%s has_function_call_output=%v",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					normalizeOpenAIWSLogValue(strictReason),
					truncateOpenAIWSLogValue(strictErr.Error(), openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
					hasFunctionCallOutput,
				)
			} else if !shouldKeepPreviousResponseID {
				updatedPayload, removed, dropErr := dropPreviousResponseIDFromRawPayload(currentPayload, nativeOps)
				if dropErr != nil || !removed {
					dropReason := "not_removed"
					if dropErr != nil {
						dropReason = "drop_error"
					}
					logOpenAIWSModeInfo(
						"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=keep_previous_response_id reason=%s drop_reason=%s previous_response_id=%s expected_previous_response_id=%s has_function_call_output=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
						normalizeOpenAIWSLogValue(strictReason),
						normalizeOpenAIWSLogValue(dropReason),
						truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
						hasFunctionCallOutput,
					)
				} else {
					updatedWithInput, setInputErr := setNativeInput(
						updatedPayload,
						currentTurnReplayInput,
						currentTurnReplayInputExists,
					)
					if setInputErr != nil {
						logOpenAIWSModeInfo(
							"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=keep_previous_response_id reason=%s drop_reason=set_full_input_error previous_response_id=%s expected_previous_response_id=%s cause=%s has_function_call_output=%v",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
							normalizeOpenAIWSLogValue(strictReason),
							truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(setInputErr.Error(), openAIWSLogValueMaxLen),
							hasFunctionCallOutput,
						)
					} else {
						adoptNative(updatedWithInput)
						currentPayload = updatedWithInput
						currentPayloadBytes = len(updatedWithInput)
						logOpenAIWSModeInfo(
							"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=drop_previous_response_id_full_create reason=%s previous_response_id=%s expected_previous_response_id=%s has_function_call_output=%v",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
							normalizeOpenAIWSLogValue(strictReason),
							truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
							hasFunctionCallOutput,
						)
						currentPreviousResponseID = ""
					}
				}
			}
		}
		forcePreferredConn := isStrictAffinityTurn(currentPayload)
		if sessionLease == nil {
			acquiredLease, acquireErr := acquireTurnLease(turn, preferredConnID, forcePreferredConn, turnRetry > 0)
			if acquireErr != nil {
				return fmt.Errorf("acquire upstream websocket: %w", acquireErr)
			}
			sessionLease = acquiredLease
			sessionConnID = strings.TrimSpace(sessionLease.ConnID())
			if storeDisabled {
				pinSessionConn(sessionConnID)
			} else {
				unpinSessionConn(sessionConnID)
			}
		}
		if account.IsOpenAIOAuthLike() && sessionLease.initializedWithoutWebSearch() && openAIWSWebSearchDeclared(currentPayload) {
			fullPayload, fullOwner, safe, err := buildWSOwnedRetryPayload(ctx, currentPayload, currentTurnReplayInput, currentTurnReplayInputExists, "")
			if err != nil {
				return fmt.Errorf("rebuild websocket input for web search activation: %w", err)
			}
			if !safe || !currentTurnReplayInputComplete {
				fullOwner.Drop()
				return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "web search activation requires complete input history; please reconnect with full input", nil)
			}
			logOpenAIWSModeInfo("ingress_ws_web_search_activation account_id=%d turn=%d action=reconnect", account.ID, turn)
			resetSessionLease(true)
			acquiredLease, acquireErr := acquireTurnLease(turn, "", false, true)
			if acquireErr != nil {
				fullOwner.Drop()
				return fmt.Errorf("acquire upstream websocket for web search activation: %w", acquireErr)
			}
			sessionLease = acquiredLease
			sessionConnID = strings.TrimSpace(sessionLease.ConnID())
			pinSessionConn(sessionConnID)
			if fullOwner != nil {
				currentMemory.replace(fullOwner)
			}
			currentPayload = fullPayload
			currentPayloadBytes = len(fullPayload)
			currentPreviousResponseID = ""
			forcePreferredConn = false
		}
		shouldPreflightPing := turn > 1 && sessionLease != nil && sessionLease.SupportsIdlePingWithoutReader() && turnRetry == 0
		if shouldPreflightPing && openAIWSIngressPreflightPingIdle > 0 && !lastTurnFinishedAt.IsZero() {
			if time.Since(lastTurnFinishedAt) < openAIWSIngressPreflightPingIdle {
				shouldPreflightPing = false
			}
		}
		if shouldPreflightPing {
			if pingErr := sessionLease.PingWithTimeout(openAIWSProbePingTO); pingErr != nil {
				logOpenAIWSModeInfo(
					"ingress_ws_upstream_preflight_ping_fail account_id=%d turn=%d conn_id=%s cause=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(pingErr.Error(), openAIWSLogValueMaxLen),
				)
				if forcePreferredConn {
					// 携带 function_call_output 的请求不能丢弃 previous_response_id：
					// 上游 API 需要 response chain 来匹配 tool_result 与之前的 tool_use，
					// 除非 replay input 已经包含与每个 tool_result 匹配的 tool_use 上下文。
					hasFCOutput := hasFunctionCallOutput
					hasReplayToolContext := hasFCOutput &&
						currentTurnReplayInputExists &&
						openAIWSRawItemsHaveToolCallContextForOutputs(currentTurnReplayInput, nativeOps)
					if nativeOps != nil && nativeOps.err != nil {
						return nativeOps.err
					}
					if !turnPrevRecoveryTried && currentPreviousResponseID != "" && (!hasFCOutput || hasReplayToolContext) {
						updatedPayload, removed, dropErr := dropPreviousResponseIDFromRawPayload(currentPayload, nativeOps)
						if errors.Is(dropErr, pkghttputil.ErrRequestBodyMemoryBudget) {
							return NewOpenAIWSBodyMemoryCloseError(dropErr)
						}
						if dropErr != nil || !removed {
							reason := "not_removed"
							if dropErr != nil {
								reason = "drop_error"
							}
							logOpenAIWSModeInfo(
								"ingress_ws_preflight_ping_recovery_skip account_id=%d turn=%d conn_id=%s reason=%s previous_response_id=%s",
								account.ID,
								turn,
								truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
								normalizeOpenAIWSLogValue(reason),
								truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
							)
						} else {
							updatedWithInput, setInputErr := setNativeInput(
								updatedPayload,
								currentTurnReplayInput,
								currentTurnReplayInputExists,
							)
							if errors.Is(setInputErr, pkghttputil.ErrRequestBodyMemoryBudget) {
								return NewOpenAIWSBodyMemoryCloseError(setInputErr)
							}
							if setInputErr != nil {
								logOpenAIWSModeInfo(
									"ingress_ws_preflight_ping_recovery_skip account_id=%d turn=%d conn_id=%s reason=set_full_input_error previous_response_id=%s cause=%s",
									account.ID,
									turn,
									truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
									truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
									truncateOpenAIWSLogValue(setInputErr.Error(), openAIWSLogValueMaxLen),
								)
							} else {
								logOpenAIWSModeInfo(
									"ingress_ws_preflight_ping_recovery account_id=%d turn=%d conn_id=%s action=drop_previous_response_id_retry previous_response_id=%s has_function_call_output=%v has_replay_tool_context=%v",
									account.ID,
									turn,
									truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
									truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
									hasFCOutput,
									hasReplayToolContext,
								)
								turnPrevRecoveryTried = true
								adoptNative(updatedWithInput)
								currentPayload = updatedWithInput
								currentPayloadBytes = len(updatedWithInput)
								resetSessionLease(true)
								skipBeforeTurn = true
								continue
							}
						}
					}
					if hasFCOutput && currentPreviousResponseID != "" {
						reason := "function_call_output_missing_replay_context"
						if hasReplayToolContext {
							reason = "function_call_output_replay_not_applied"
						}
						logOpenAIWSModeInfo(
							"ingress_ws_preflight_ping_recovery_skip account_id=%d turn=%d conn_id=%s reason=%s action=fail_close previous_response_id=%s has_replay_tool_context=%v",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
							reason,
							truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
							hasReplayToolContext,
						)
					}
					resetSessionLease(true)
					return NewOpenAIWSClientCloseError(
						coderws.StatusPolicyViolation,
						"upstream continuation connection is unavailable; please restart the conversation",
						pingErr,
					)
				}
				resetSessionLease(true)

				acquiredLease, acquireErr := acquireTurnLease(turn, preferredConnID, forcePreferredConn, false)
				if acquireErr != nil {
					return fmt.Errorf("acquire upstream websocket after preflight ping fail: %w", acquireErr)
				}
				sessionLease = acquiredLease
				sessionConnID = strings.TrimSpace(sessionLease.ConnID())
				if storeDisabled {
					pinSessionConn(sessionConnID)
				}
			}
		}
		connID := sessionConnID
		if currentPreviousResponseID != "" {
			chainedFromLast := expectedPrev != "" && currentPreviousResponseID == expectedPrev
			currentPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(currentPreviousResponseID)
			logOpenAIWSModeInfo(
				"ingress_ws_turn_chain account_id=%d turn=%d conn_id=%s previous_response_id=%s previous_response_id_kind=%s last_turn_response_id=%s chained_from_last=%v preferred_conn_id=%s header_session_id=%s header_conversation_id=%s has_turn_state=%v turn_state_len=%d has_prompt_cache_key=%v store_disabled=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
				normalizeOpenAIWSLogValue(currentPreviousResponseIDKind),
				truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				chainedFromLast,
				truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
				openAIWSHeaderValueForLog(baseAcquireReq.Headers, "session_id"),
				openAIWSHeaderValueForLog(baseAcquireReq.Headers, "conversation_id"),
				turnState != "",
				len(turnState),
				openAIWSPayloadStringFromRaw(currentPayload, "prompt_cache_key") != "",
				storeDisabled,
			)
		}

		if nativeOps != nil && nativeOps.err != nil {
			return nativeOps.err
		}
		nativeOps.close()
		nativeOps = newWSMaterializer(ctx)
		result, relayErr := sendAndRelay(turn, sessionLease, currentPayload, currentPayloadBytes, currentOriginalModel, currentImageBillingModel, currentImageSizeTier, currentImageInputSize, currentRequestedReasoningEffort)
		if relayErr != nil {
			result.releaseWSReplayMemory()
			lastTurnClean = false
			var rejectedFieldErr *openAIWSRejectedFieldRetryError
			_ = errors.As(relayErr, &rejectedFieldErr)
			if errors.Is(relayErr, pkghttputil.ErrRequestBodyMemoryBudget) {
				if rejectedFieldErr != nil {
					rejectedFieldErr.owner.Drop()
				}
				return relayErr
			}
			if isOpenAIWSSessionPreempted(ctx) {
				if rejectedFieldErr != nil {
					rejectedFieldErr.owner.Drop()
				}
				sessionLease.MarkBroken()
				return errOpenAIWSSessionPreempted
			}
			if rejectedFieldErr != nil && len(rejectedFieldErr.body) > 0 {
				if rejectedFieldErr.owner != nil {
					currentMemory.replace(rejectedFieldErr.owner)
					rejectedFieldErr.owner = nil
				}
				currentPayload = rejectedFieldErr.body
				currentPayloadBytes = len(currentPayload)
				skipBeforeTurn = true
				continue
			}
			if recoverIngressPrevResponseNotFound(relayErr, turn, connID) {
				continue
			}
			if nativeOps != nil && nativeOps.err != nil {
				return nativeOps.err
			}
			if retryIngressTurn(relayErr, turn, connID) {
				continue
			}
			finalErr := relayErr
			if unwrapped := errors.Unwrap(relayErr); unwrapped != nil {
				finalErr = unwrapped
			}
			if hooks != nil && hooks.AfterTurn != nil {
				hooks.AfterTurn(turn, nil, finalErr)
			}
			sessionLease.MarkBroken()
			return finalErr
		}
		turnRetry = 0
		turnPrevRecoveryTried = false
		lastTurnFinishedAt = time.Now()
		lastTurnClean = true
		if hooks != nil && hooks.AfterTurn != nil {
			hooks.AfterTurn(turn, result, nil)
		}
		if result == nil {
			return errors.New("websocket turn result is nil")
		}
		responseID := strings.TrimSpace(result.RequestID)
		lastTurnResponseID = responseID
		if contextWindowBoundary.WindowID != "" {
			lastTurnWindowID = contextWindowBoundary.WindowID
		}
		// 正文共享：currentPayload/currentTurnReplayInput 均不可变，历史直接引用；
		// collector 增量经 combine 合并（新头数组）。
		lastTurnReplayInputComplete = currentTurnReplayInputComplete
		if nativeOps != nil {
			if result.wsHistoryMemoryErr != nil {
				result.releaseWSReplayMemory()
				return NewOpenAIWSBodyMemoryCloseError(result.wsHistoryMemoryErr)
			}
			next := currentReplayMemory.hold()
			if result.wsReplayMemory != nil {
				combined, err := combineWSMemoryReplay(ctx, next, *result.wsReplayMemory)
				next.drop()
				if err != nil {
					result.releaseWSReplayMemory()
					return err
				}
				next = combined
			}
			result.releaseWSReplayMemory()
			lastReplayMemory.drop()
			lastReplayMemory = next
			currentReplayMemory.drop()
			lastTurnReplayInput, lastTurnReplayInputExists = next.items, next.exists
		} else {
			lastTurnReplayInput = currentTurnReplayInput
			lastTurnReplayInputExists = currentTurnReplayInputExists
			if result.wsReplayInputExists {
				lastTurnReplayInput = combineOpenAIWSReplayItems(lastTurnReplayInput, result.wsReplayInput)
				lastTurnReplayInputExists = true
			}
		}
		nextStrictState, strictStateErr := buildOpenAIWSIngressPreviousTurnStrictState(currentPayload, nativeOps)
		if nativeOps != nil && nativeOps.err != nil {
			return nativeOps.err
		}
		lastTurnPayloadOwner.Drop()
		lastTurnPayloadOwner = nil
		strictOwner.Drop()
		strictOwner = nil
		if strictStateErr != nil {
			lastTurnStrictState = nil
			// strict 状态不可用时保留整份上一轮 payload 供慢路径比较。
			lastTurnPayload = currentPayload
			lastTurnPayloadOwner = currentMemory.normalized.Hold()
			logOpenAIWSModeInfo(
				"ingress_ws_prev_response_strict_state_skip account_id=%d turn=%d conn_id=%s reason=build_error cause=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(strictStateErr.Error(), openAIWSLogValueMaxLen),
			)
		} else {
			lastTurnStrictState = nextStrictState
			if nextStrictState != nil {
				strictOwner = nativeOps.take(nextStrictState.nonInputComparable)
			}
			lastTurnPayload = nil
		}

		if responseID != "" && stateStore != nil {
			ttl := s.openAIWSResponseStickyTTL()
			logOpenAIWSBindResponseAccountWarn(groupID, account.ID, responseID, stateStore.BindResponseAccount(ctx, groupID, responseID, account.ID, ttl))
			stateStore.BindResponseConn(responseID, connID, ttl)
		}
		if stateStore != nil && storeDisabled && sessionHash != "" {
			stateStore.BindSessionConn(groupID, sessionHash, connID, s.openAIWSSessionStickyTTL())
		}
		if connID != "" {
			preferredConnID = connID
		}

		nativeOps.close()
		nativeOps = newWSMaterializer(ctx)
		nextClientMessage, nextOwner, readErr := readClientMessage()
		if readErr != nil {
			if isOpenAIWSSessionPreempted(ctx) {
				return errOpenAIWSSessionPreempted
			}
			if isOpenAIWSClientDisconnectError(readErr) {
				closeStatus, closeReason := summarizeOpenAIWSReadCloseError(readErr)
				logOpenAIWSModeInfo(
					"ingress_ws_client_closed account_id=%d conn_id=%s close_status=%s close_reason=%s",
					account.ID,
					truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
					closeStatus,
					truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
				)
				return nil
			}
			return fmt.Errorf("read client websocket request: %w", readErr)
		}

		nextPayload, parseErr := parseClientPayload(turn+1, nextClientMessage, nextOwner)
		if parseErr != nil {
			return parseErr
		}
		nextRoutingFields := gjson.GetManyBytes(nextPayload.payloadRaw, "model", "service_tier")
		if nextPayload.promptCacheKey != "" {
			// ingress 会话在整个客户端 WS 生命周期内复用同一上游连接；
			// prompt_cache_key 对握手头的更新仅在未来需要重新建连时生效。
			updatedHeaders, _, updHdrErr := s.buildOpenAIWSHeaders(
				ctx,
				c,
				account,
				token,
				wsDecision,
				isCodexCLI,
				turnState,
				strings.TrimSpace(c.GetHeader(openAIWSTurnMetadataHeader)),
				nextPayload.promptCacheKey,
				nextRoutingFields[0].String(),
				nextRoutingFields[1].String(),
			)
			if updHdrErr != nil {
				logOpenAIWSModeInfo("ingress_ws_update_headers_failed account_id=%d err=%v", account.ID, updHdrErr)
			} else {
				baseAcquireReq.Headers = updatedHeaders
			}
		}
		setOpenAICodexRoutingHint(baseAcquireReq.Headers, account, nextRoutingFields[0].String(), nextRoutingFields[1].String())
		if nextPayload.previousResponseID != "" {
			expectedPrev := strings.TrimSpace(lastTurnResponseID)
			chainedFromLast := expectedPrev != "" && nextPayload.previousResponseID == expectedPrev
			nextPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(nextPayload.previousResponseID)
			logOpenAIWSModeInfo(
				"ingress_ws_next_turn_chain account_id=%d turn=%d next_turn=%d conn_id=%s previous_response_id=%s previous_response_id_kind=%s last_turn_response_id=%s chained_from_last=%v has_prompt_cache_key=%v store_disabled=%v",
				account.ID,
				turn,
				turn+1,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(nextPayload.previousResponseID, openAIWSIDValueMaxLen),
				normalizeOpenAIWSLogValue(nextPreviousResponseIDKind),
				truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				chainedFromLast,
				nextPayload.promptCacheKey != "",
				storeDisabled,
			)
		}
		if stateStore != nil && nextPayload.previousResponseID != "" {
			if stickyConnID, ok := stateStore.GetResponseConn(nextPayload.previousResponseID); ok {
				if sessionConnID != "" && stickyConnID != "" && stickyConnID != sessionConnID {
					logOpenAIWSModeInfo(
						"ingress_ws_keep_session_conn account_id=%d turn=%d conn_id=%s sticky_conn_id=%s previous_response_id=%s",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(stickyConnID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(nextPayload.previousResponseID, openAIWSIDValueMaxLen),
					)
				} else {
					preferredConnID = stickyConnID
				}
			}
		}
		currentMemory.drop()
		currentMemory = nextPayload.memory
		currentPayload = nextPayload.payloadRaw
		currentClientPayload = nextPayload.rawForHash
		currentOriginalModel = nextPayload.originalModel
		currentImageBillingModel = nextPayload.imageBillingModel
		currentImageSizeTier = nextPayload.imageSizeTier
		currentImageInputSize = nextPayload.imageInputSize
		currentPayloadBytes = nextPayload.payloadBytes
		currentRequestedReasoningEffort = nextPayload.requestedReasoningEffort
		rejectedFieldRetryState = newOpenAIResponsesRejectedFieldRetryState(currentPayload)
		storeDisabled = s.isOpenAIWSStoreDisabledInRequestRaw(currentPayload, account)
		if !storeDisabled {
			unpinSessionConn(sessionConnID)
		}
		turn++
	}
}
