package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	openAIWSClientReadLimitBytesDefault     int64 = 64 * 1024 * 1024
	openAIWSHTTPBridgeThresholdBytesDefault int64 = 15 * 1024 * 1024
	openAIWSHTTPBridgeErrorBodyLimitBytes         = 64 * 1024
)

const openAIWSHTTPBridgeToolStateContextKey = "openai_ws_http_bridge_tool_state"

type openAIWSHTTPBridgeToolState struct {
	ClientMapping apicompat.ResponsesClientToolMapping
	LoweredTools  json.RawMessage
	memory        *pkghttputil.WSBodyMemoryOwner
}

func openAIWSHTTPBridgeToolStateFromContext(c *gin.Context) (openAIWSHTTPBridgeToolState, bool) {
	if c == nil {
		return openAIWSHTTPBridgeToolState{}, false
	}
	value, ok := c.Get(openAIWSHTTPBridgeToolStateContextKey)
	state, typed := value.(openAIWSHTTPBridgeToolState)
	return state, ok && typed
}

func setOpenAIWSHTTPBridgeToolState(c *gin.Context, state openAIWSHTTPBridgeToolState) {
	if c == nil {
		return
	}
	state.LoweredTools = append(json.RawMessage(nil), state.LoweredTools...)
	c.Set(openAIWSHTTPBridgeToolStateContextKey, state)
}

func setWSOwnedToolState(ctx context.Context, c *gin.Context, state openAIWSHTTPBridgeToolState) error {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		setOpenAIWSHTTPBridgeToolState(c, state)
		return nil
	}
	if c == nil {
		return nil
	}
	var size = int64(len(state.LoweredTools))
	var err error
	for key := range state.ClientMapping.CustomTools {
		size, err = wsMemoryAdd(size, int64(len(key)))
		if err != nil {
			return err
		}
	}
	for key, name := range state.ClientMapping.NamespaceTools {
		for _, part := range []string{key, name.Namespace, name.Name} {
			size, err = wsMemoryAdd(size, int64(len(part)))
			if err != nil {
				return err
			}
		}
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, size)
	if err != nil {
		return err
	}
	if state.LoweredTools != nil {
		raw := make(json.RawMessage, len(state.LoweredTools))
		copy(raw, state.LoweredTools)
		state.LoweredTools = raw
	}
	if state.ClientMapping.CustomTools != nil {
		custom := make(map[string]bool, len(state.ClientMapping.CustomTools))
		for key, value := range state.ClientMapping.CustomTools {
			custom[strings.Clone(key)] = value
		}
		state.ClientMapping.CustomTools = custom
	}
	if state.ClientMapping.NamespaceTools != nil {
		names := make(map[string]apicompat.ResponsesNamespaceName, len(state.ClientMapping.NamespaceTools))
		for key, value := range state.ClientMapping.NamespaceTools {
			names[strings.Clone(key)] = apicompat.ResponsesNamespaceName{Namespace: strings.Clone(value.Namespace), Name: strings.Clone(value.Name)}
		}
		state.ClientMapping.NamespaceTools = names
	}
	old, _ := openAIWSHTTPBridgeToolStateFromContext(c)
	state.memory = owner
	c.Set(openAIWSHTTPBridgeToolStateContextKey, state)
	old.memory.Drop()
	return nil
}

// ReleaseOpenAIWSHTTPBridgeToolMemory ends the connection-wide state, including
// retries that reuse the same gin context.
func ReleaseOpenAIWSHTTPBridgeToolMemory(c *gin.Context) {
	if c == nil {
		return
	}
	state, _ := openAIWSHTTPBridgeToolStateFromContext(c)
	if state.memory != nil {
		c.Set(openAIWSHTTPBridgeToolStateContextKey, openAIWSHTTPBridgeToolState{})
		state.memory.Drop()
	}
}

func decodeOpenAIWSHTTPBridgeLoweredTools(raw json.RawMessage) []any {
	if len(raw) == 0 {
		return nil
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil
	}
	return tools
}

// Borrow the actual last raw field from the existing valid request. The
// former map[RawMessage] decode copied every value (including all history)
// just to inspect tools. Use the existing bridge lexical scanner instead;
// last-value and escaped-key behavior match encoding/json's map decode.
func openAIWSHTTPBridgeRawField(body []byte, name string) (json.RawMessage, bool) {
	return openAIWSRawFieldView(body, name, true)
}

func openAIWSRawFieldView(body []byte, name string, last bool) (json.RawMessage, bool) {
	if !json.Valid(body) {
		return nil, false
	}
	return openAIWSRawFieldViewValid(body, name, last)
}

// The caller has already validated the immutable JSON value.
func openAIWSRawFieldViewValid(body []byte, name string, last bool) (json.RawMessage, bool) {
	i := skipOpenAIWSJSONSpace(body, 0)
	if i >= len(body) || body[i] != '{' {
		return nil, false
	}
	i++
	var raw json.RawMessage
	present := false
	for {
		i = skipOpenAIWSJSONSpace(body, i)
		if i >= len(body) || body[i] == '}' {
			return raw, present
		}
		keyStart := i
		if body[i] != '"' {
			return nil, false
		}
		keyEnd := scanOpenAIWSJSONString(body, i)
		i = skipOpenAIWSJSONSpace(body, keyEnd)
		if i >= len(body) || body[i] != ':' {
			return nil, false
		}
		i++
		i = skipOpenAIWSJSONSpace(body, i)
		valueStart := i
		if i >= len(body) {
			return nil, false
		}
		i = skipOpenAIWSJSONValue(body, i)
		if keyEnd-keyStart <= 6*len(name)+2 {
			var key string
			if json.Unmarshal(body[keyStart:keyEnd], &key) == nil && key == name {
				raw = json.RawMessage(bytes.TrimSpace(body[valueStart:i]))
				present = true
				if !last {
					return raw, true
				}
			}
		}
		i = skipOpenAIWSJSONSpace(body, i)
		if i >= len(body) {
			return nil, false
		}
		if body[i] == ',' {
			i++
		} else if body[i] != '}' {
			return nil, false
		}
	}
}

func openAIWSHTTPBridgeToolUpstreamName(account *Account) string {
	if account != nil && account.Platform == PlatformGrok {
		return "Grok WS HTTP bridge"
	}
	return "OpenAI WS HTTP bridge"
}

// ResolveOpenAIWSClientFirstMessageTimeout returns the effective client ingress deadline.
func ResolveOpenAIWSClientFirstMessageTimeout(cfg *config.Config) time.Duration {
	seconds := config.DefaultOpenAIWSClientFirstMessageTimeoutSeconds
	if cfg != nil && cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds > 0 {
		seconds = cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func ResolveOpenAIWSClientReadLimitBytes(cfg *config.Config) int64 {
	if cfg == nil || cfg.Gateway.OpenAIWS.ClientReadLimitBytes <= 0 {
		return openAIWSClientReadLimitBytesDefault
	}
	return cfg.Gateway.OpenAIWS.ClientReadLimitBytes
}

func (s *OpenAIGatewayService) openAIWSHTTPBridgeEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.HTTPBridgeEnabled
}

func (s *OpenAIGatewayService) openAIWSHTTPBridgeThresholdBytes() int64 {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes <= 0 {
		return openAIWSHTTPBridgeThresholdBytesDefault
	}
	return s.cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes
}

func (s *OpenAIGatewayService) shouldBridgeOpenAIWSHTTP(account *Account, payloadBytes int, previousResponseID string) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	if !s.openAIWSHTTPBridgeEnabled() {
		return false
	}
	if strings.TrimSpace(previousResponseID) != "" {
		return false
	}
	threshold := s.openAIWSHTTPBridgeThresholdBytes()
	return threshold > 0 && int64(payloadBytes) >= threshold
}

func (s *OpenAIGatewayService) shouldBridgeOpenAIWSPassthroughFirstMessage(account *Account, payload []byte) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	if !s.openAIWSHTTPBridgeEnabled() || int64(len(payload)) < s.openAIWSHTTPBridgeThresholdBytes() {
		return false
	}
	if !json.Valid(payload) {
		return false
	}

	i := skipOpenAIWSJSONSpace(payload, 0)
	if i >= len(payload) || payload[i] != '{' {
		return false
	}
	i++
	eventType := "response.create"
	previousResponseID := ""
	typeSeen, previousResponseIDSeen := false, false
	for {
		i = skipOpenAIWSJSONSpace(payload, i)
		if payload[i] == '}' {
			break
		}
		keyStart := i
		keyEnd := scanOpenAIWSJSONString(payload, keyStart)
		i = skipOpenAIWSJSONSpace(payload, keyEnd)
		i++ // json.Valid guarantees the colon.
		i = skipOpenAIWSJSONSpace(payload, i)
		valueStart := i
		i = skipOpenAIWSJSONValue(payload, i)

		key := ""
		// A critical key is at most 20 decoded bytes. The generous encoded bound
		// covers escaped spellings without allocating attacker-sized key strings.
		if keyEnd-keyStart <= 128 {
			_ = json.Unmarshal(payload[keyStart:keyEnd], &key)
		}
		switch key {
		case "type":
			if typeSeen {
				return false
			}
			typeSeen = true
			var value *string
			if err := json.Unmarshal(payload[valueStart:i], &value); err != nil {
				return false
			}
			if value == nil || strings.TrimSpace(*value) == "" {
				eventType = "response.create"
			} else {
				eventType = strings.TrimSpace(*value)
			}
		case "previous_response_id":
			if previousResponseIDSeen {
				return false
			}
			previousResponseIDSeen = true
			var value *string
			if err := json.Unmarshal(payload[valueStart:i], &value); err != nil {
				return false
			}
			if value != nil {
				previousResponseID = strings.TrimSpace(*value)
			}
		}
		i = skipOpenAIWSJSONSpace(payload, i)
		if payload[i] == ',' {
			i++
		}
	}
	return eventType == "response.create" && previousResponseID == ""
}

func skipOpenAIWSJSONSpace(payload []byte, i int) int {
	for i < len(payload) {
		switch payload[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func scanOpenAIWSJSONString(payload []byte, i int) int {
	for i++; i < len(payload); i++ {
		switch payload[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(payload)
}

func skipOpenAIWSJSONValue(payload []byte, i int) int {
	if payload[i] == '"' {
		return scanOpenAIWSJSONString(payload, i)
	}
	if payload[i] != '{' && payload[i] != '[' {
		for i < len(payload) && payload[i] != ',' && payload[i] != '}' {
			i++
		}
		return i
	}
	depth := 0
	for ; i < len(payload); i++ {
		switch payload[i] {
		case '"':
			i = scanOpenAIWSJSONString(payload, i) - 1
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(payload)
}

func prepareOpenAIWSHTTPBridgeBody(account *Account, payload []byte) ([]byte, error) {
	var body map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, errors.New("response.create payload must be a JSON object")
	}
	delete(body, "type")
	delete(body, "generate")
	delete(body, "previous_response_id")
	deleteOpenAIResponsesNoneReasoningEffortFromObject(account, body)
	body["stream"] = true
	return json.Marshal(body)
}

func prepareOpenAIWSHTTPBridgeBodyWithMemory(ctx context.Context, account *Account, payload []byte) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		body, err := prepareOpenAIWSHTTPBridgeBody(account, payload)
		return body, nil, err
	}
	release, err := wsJSONDecodeScope(ctx, payload)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	var body map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &body); err != nil {
		return nil, nil, err
	}
	if body == nil {
		return nil, nil, errors.New("response.create payload must be a JSON object")
	}
	delete(body, "type")
	delete(body, "generate")
	delete(body, "previous_response_id")
	deleteOpenAIResponsesNoneReasoningEffortFromObject(account, body)
	body["stream"] = true
	return marshalWSJSON(ctx, body, true)
}

type openAIWSToolCallReplayCollector struct {
	items    []json.RawMessage
	seen     map[string]struct{}
	allItems []json.RawMessage
	allSeen  map[string]struct{}
	memory   *wsMemoryCollector
}

func newWSReplayCollector(ctx context.Context) *openAIWSToolCallReplayCollector {
	c := &openAIWSToolCallReplayCollector{}
	if pkghttputil.HasWSRequestBodyMemory(ctx) {
		c.memory = &wsMemoryCollector{ctx: ctx}
	}
	return c
}
func (c *openAIWSToolCallReplayCollector) release() {
	if c.memory != nil {
		c.memory.drop()
	}
}
func (c *openAIWSToolCallReplayCollector) attachResult(result *OpenAIForwardResult, all bool, successful bool) {
	if c.memory == nil {
		items := c.Items()
		if all {
			items = c.AllItems()
		}
		if len(items) > 0 {
			result.wsReplayInput = items
			result.wsReplayInputExists = true
		}
		return
	}
	if c.memory.err != nil {
		result.wsHistoryMemoryErr = c.memory.err
		return
	}
	if !successful {
		return
	}
	replay := c.memory.tools
	if all {
		replay = c.memory.all
	}
	if len(replay.items) > 0 {
		owned := replay.hold()
		result.wsReplayMemory = &owned
		result.wsReplayInput = owned.items
		result.wsReplayInputExists = true
	}
}
func (c *openAIWSToolCallReplayCollector) AddEvent(eventType string, message []byte) {
	if c.memory != nil {
		c.memory.event(eventType, message)
		return
	}
	switch strings.TrimSpace(eventType) {
	case "response.output_item.done":
		item := gjson.GetBytes(message, "item")
		c.addAllItem(item)
		c.addItem(item)
	case "response.completed", "response.done":
		output := gjson.GetBytes(message, "response.output")
		if !output.IsArray() {
			return
		}
		for _, item := range output.Array() {
			c.addAllItem(item)
			c.addItem(item)
		}
	}
}

// Items/AllItems 返回浅拷贝头数组；正文由 collector 独立分配且此后不可变，
// 调用方按 replay 所有权不变式共享持有。
func (c *openAIWSToolCallReplayCollector) Items() []json.RawMessage {
	return slices.Clone(c.items)
}

func (c *openAIWSToolCallReplayCollector) AllItems() []json.RawMessage {
	return slices.Clone(c.allItems)
}

func (c *openAIWSToolCallReplayCollector) addAllItem(item gjson.Result) {
	if !item.Exists() || item.Type != gjson.JSON {
		return
	}
	raw := strings.TrimSpace(item.Raw)
	if raw == "" || !strings.HasPrefix(raw, "{") || strings.TrimSpace(item.Get("type").String()) == "" {
		return
	}
	key := strings.TrimSpace(item.Get("id").String())
	if key == "" {
		key = strings.TrimSpace(item.Get("call_id").String())
	}
	if key == "" {
		key = raw
	}
	if c.allSeen == nil {
		c.allSeen = make(map[string]struct{})
	}
	if _, ok := c.allSeen[key]; ok {
		return
	}
	c.allSeen[key] = struct{}{}
	c.allItems = append(c.allItems, json.RawMessage(raw))
}

func (c *openAIWSToolCallReplayCollector) addItem(item gjson.Result) {
	if !item.Exists() || item.Type != gjson.JSON {
		return
	}
	raw := strings.TrimSpace(item.Raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return
	}
	if !isCodexToolCallContextItemType(item.Get("type").String()) {
		return
	}
	key := strings.TrimSpace(item.Get("id").String())
	if key == "" {
		key = strings.TrimSpace(item.Get("call_id").String())
	}
	if key == "" {
		key = raw
	}
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	if _, ok := c.seen[key]; ok {
		return
	}
	c.seen[key] = struct{}{}
	c.items = append(c.items, json.RawMessage(raw))
}

func buildOpenAIWSHTTPBridgeErrorEvent(statusCode int, message string) []byte {
	message = strings.TrimSpace(message)
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if message == "" {
		message = "upstream request failed"
	}
	event := map[string]any{
		"type":            "error",
		"sequence_number": 0,
		"status":          statusCode,
		"error": map[string]any{
			"type":    "upstream_error",
			"message": message,
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		return []byte(`{"type":"error","sequence_number":0,"error":{"type":"upstream_error","message":"upstream request failed"}}`)
	}
	return body
}

// buildOpenAIWSHTTPBridgeFailedEvent synthesizes response.failed for a turn that
// ended on a bare error. created_at and sequence_number are always written:
// strict clients (grok-build, Codex) reject the frame without them.
func buildOpenAIWSHTTPBridgeFailedEvent(responseID, model string, createdAt int64, source []byte, fallbackMessage string) []byte {
	errorType := strings.TrimSpace(gjson.GetBytes(source, "error.type").String())
	if errorType == "" {
		errorType = strings.TrimSpace(gjson.GetBytes(source, "response.error.type").String())
	}
	code := strings.TrimSpace(gjson.GetBytes(source, "error.code").String())
	if code == "" {
		code = strings.TrimSpace(gjson.GetBytes(source, "response.error.code").String())
	}
	if code == "" {
		code = "upstream_error"
	}
	message := extractOpenAISSEErrorMessage(source)
	if message == "" {
		message = strings.TrimSpace(fallbackMessage)
	}
	if message == "" {
		message = "Upstream response failed"
	}
	errorBody := map[string]any{"code": code, "message": message}
	if errorType != "" {
		errorBody["type"] = errorType
	}
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	sequenceNumber := openAIResponsesFailedSequenceNumber(source)
	response := map[string]any{
		"id": responseID, "object": "response", "created_at": createdAt, "status": "failed",
		"output": []any{}, "error": errorBody,
	}
	if model = strings.TrimSpace(model); model != "" {
		response["model"] = model
	}
	body, err := json.Marshal(map[string]any{"type": "response.failed", "sequence_number": sequenceNumber, "response": response})
	if err != nil {
		return []byte(`{"type":"response.failed","sequence_number":` + strconv.FormatInt(sequenceNumber, 10) +
			`,"response":{"object":"response","created_at":` + strconv.FormatInt(createdAt, 10) +
			`,"status":"failed","output":[],"error":{"code":"upstream_error","message":"Upstream response failed"}}}`)
	}
	return body
}

func (s *OpenAIGatewayService) proxyOpenAIWSHTTPBridgeTurn(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	payload []byte,
	payloadBytes int,
	originalModel string,
	imageBillingModel string,
	imageSizeTier string,
	imageInputSize string,
	grokCacheIdentity string,
	turn int,
	writeClientMessage func([]byte) error,
) (*OpenAIForwardResult, error) {
	if s == nil {
		return nil, errors.New("service is nil")
	}
	if s.httpUpstream == nil {
		return nil, errors.New("openai http upstream is nil")
	}
	if account == nil {
		return nil, errors.New("account is nil")
	}
	if writeClientMessage == nil {
		return nil, errors.New("client websocket writer is nil")
	}
	responseModelObserver := &upstreamResponseModelObserver{}

	body, preparedOwner, err := prepareOpenAIWSHTTPBridgeBodyWithMemory(ctx, account, payload)
	if err != nil {
		if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
			return nil, NewOpenAIWSBodyMemoryCloseError(err)
		}
		return nil, fmt.Errorf("prepare http bridge body: %w", err)
	}
	bodyOwner := preparedOwner
	defer func() { bodyOwner.Drop() }()
	applyBody := func(run func(*wsMaterializer) ([]byte, error)) error {
		next, owner, err := transformWSBody(ctx, body, run)
		if err != nil {
			return err
		}
		if owner != nil {
			bodyOwner.Drop()
			bodyOwner = owner
		}
		body = next
		return nil
	}
	// Codex fingerprint convergence, same as Forward / forwardOpenAIPassthrough. Builders only read
	// staged IDs; without staging, the bridge sends no account-level session_id, so clients without
	// prompt_cache_key get random upstream prompt-cache routing and rarely hit the cache.
	stageCodexFingerprintIDs(c, nil)
	if account.UsesOpenAICodexProtocol() {
		var clientHeaders http.Header
		if c != nil && c.Request != nil {
			clientHeaders = c.Request.Header
		}
		fpIDs := resolveCodexFingerprintIDsFromRequest(account, clientHeaders)
		if fpIDs != nil {
			if fpErr := applyBody(func(mem *wsMaterializer) ([]byte, error) {
				next, _, err := applyCodexFingerprintClientMetadataRaw(body, fpIDs, mem)
				return next, err
			}); fpErr != nil {
				return nil, fmt.Errorf("apply http bridge codex fingerprint: %w", fpErr)
			}
		}
		stageCodexFingerprintIDs(c, fpIDs)
	}
	grokIntentSourceBody := body
	intentOwner := bodyOwner.Hold()
	defer func() { intentOwner.Drop() }()
	_, grokExplicitToolsField := openAIWSHTTPBridgeRawField(grokIntentSourceBody, "tools")
	intentMemory := newWSMaterializer(ctx)
	grokExplicitToolIntent := account.Platform == PlatformGrok && hasGrokResponsesToolIntent(grokIntentSourceBody, intentMemory)
	if intentMemory != nil && intentMemory.err != nil {
		intentMemory.close()
		return nil, intentMemory.err
	}
	intentMemory.close()
	var clientToolMapping apicompat.ResponsesClientToolMapping
	functionToolUpstream := (account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey) || account.Platform == PlatformGrok
	priorState, _ := openAIWSHTTPBridgeToolStateFromContext(c)
	if account.Platform == PlatformOpenAI && !grokExplicitToolsField && !hasResponsesClientToolMapping(priorState.ClientMapping) && len(priorState.LoweredTools) == 0 {
		functionToolUpstream = false
	}
	if functionToolUpstream {
		if account.Platform == PlatformGrok {
			if err := applyBody(func(mem *wsMaterializer) ([]byte, error) { return sanitizeGrokResponsesInput(body, mem) }); err != nil {
				return nil, fmt.Errorf("sanitize Grok WS HTTP bridge input: %w", err)
			}
		}
		inheritedState, _ := openAIWSHTTPBridgeToolStateFromContext(c)
		var inheritedToolCount int
		if err := applyBody(func(mem *wsMaterializer) ([]byte, error) {
			var inheritedTools []any
			if len(inheritedState.LoweredTools) > 0 {
				if mem == nil {
					inheritedTools = decodeOpenAIWSHTTPBridgeLoweredTools(inheritedState.LoweredTools)
				} else if err := mem.unmarshal(inheritedState.LoweredTools, &inheritedTools); err != nil {
					if mem != nil && mem.err != nil {
						return nil, mem.err
					}
					inheritedTools = nil
				}
			}
			inheritedToolCount = len(inheritedTools)
			next, mapping, err := adaptResponsesClientToolsForFunctionUpstreamWithMemory(body, openAIWSHTTPBridgeToolUpstreamName(account), inheritedState.ClientMapping, mem, inheritedTools)
			if err != nil {
				return nil, err
			}
			lowered := inheritedState.LoweredTools
			if raw, ok := openAIWSHTTPBridgeRawField(next, "tools"); ok {
				lowered = raw
			}
			if err := setWSOwnedToolState(ctx, c, openAIWSHTTPBridgeToolState{ClientMapping: mapping, LoweredTools: lowered}); err != nil {
				return nil, err
			}
			state, _ := openAIWSHTTPBridgeToolStateFromContext(c)
			clientToolMapping = state.ClientMapping
			return next, nil
		}); err != nil {
			return nil, fmt.Errorf("adapt %s client tools: %w", openAIWSHTTPBridgeToolUpstreamName(account), err)
		}
		intentMemory := newWSMaterializer(ctx)
		inheritIntent := account.Platform == PlatformGrok && !grokExplicitToolsField && !grokExplicitToolIntent && inheritedToolCount > 0 && hasGrokResponsesToolIntent(body, intentMemory)
		if intentMemory != nil && intentMemory.err != nil {
			intentMemory.close()
			return nil, intentMemory.err
		}
		intentMemory.close()
		if inheritIntent {
			intentOwner.Drop()
			intentOwner = bodyOwner.Hold()
			grokIntentSourceBody = body
		}
	}
	if account.Platform != PlatformGrok && isOpenAIResponsesLiteWebSocketPayload(payload) {
		if err := applyBody(func(mem *wsMaterializer) ([]byte, error) {
			next, _, err := normalizeOpenAIResponsesLitePayloadForAccount(body, account, mem)
			return next, err
		}); err != nil {
			return nil, fmt.Errorf("normalize responses Lite payload: %w", err)
		}
	}

	var requestMemory *wsMaterializer
	defer func() { requestMemory.close() }()
	buildUpstreamRequest := func(requestBody []byte) (*http.Request, error) {
		requestMemory.close()
		requestMemory = newWSMaterializer(ctx)
		upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
		defer releaseUpstreamCtx()
		var upstreamReq *http.Request
		var buildErr error
		if account.Platform == PlatformGrok {
			upstreamReq, buildErr = buildGrokResponsesRequest(upstreamCtx, c, account, requestBody, token, grokCacheIdentity, s.cfg, s.settingService)
		} else {
			upstreamReq, buildErr = s.buildUpstreamRequestOpenAIPassthrough(upstreamCtx, c, account, requestBody, token, requestMemory)
		}
		if buildErr != nil {
			return nil, buildErr
		}
		if account.Platform != PlatformGrok && isOpenAIResponsesLiteWebSocketPayload(payload) {
			upstreamReq.Header.Set(responsesLiteHeader, "true")
		}
		if err := applyMappedGPT55LiteCompatibility(upstreamReq, account, requestBody, requestMemory); err != nil {
			return nil, err
		}
		if requestMemory != nil && requestMemory.err != nil {
			return nil, requestMemory.err
		}
		return upstreamReq, nil
	}
	if account.Platform == PlatformGrok {
		upstreamModel := resolveGrokWSUpstreamModel(account, body, originalModel)
		if err := applyBody(func(mem *wsMaterializer) ([]byte, error) { return patchGrokResponsesBody(body, upstreamModel, mem) }); err != nil {
			return nil, err
		}
		mixedBody := body
		mixedOwner := bodyOwner.Hold()
		if err := applyBody(func(mem *wsMaterializer) ([]byte, error) {
			return applyGrokResponsesCacheIdentity(body, grokIntentSourceBody, grokCacheIdentity, account.IsGrokOAuth(), mem)
		}); err != nil {
			mixedOwner.Drop()
			return nil, fmt.Errorf("apply grok prompt cache identity: %w", err)
		}
		err := applyBody(func(mem *wsMaterializer) ([]byte, error) {
			return applyGrokFreeRequestToolCacheRoute(c, body, mixedBody, account, grokCacheIdentity, mem)
		})
		mixedOwner.Drop()
		if err != nil {
			return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", err)
		}
	}
	intentOwner.Drop()
	intentOwner = nil
	grokIntentSourceBody = nil

	actualModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if actualModel == "" {
		actualModel = canonicalOpenAIAccountSchedulingModel(account, originalModel)
	}
	SetOpsUpstreamModel(c, actualModel)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if c != nil {
		c.Set("openai_passthrough", true)
		c.Set("openai_ws_http_bridge", true)
	}

	turnStart := time.Now()
	rejectedFieldRetryState := newOpenAIResponsesRejectedFieldRetryState(body)
	var resp *http.Response
	for {
		upstreamReq, buildErr := buildUpstreamRequest(body)
		if buildErr != nil {
			return nil, buildErr
		}
		resp, err = s.doOpenAIUpstream(upstreamReq, proxyURL, account)
		if err != nil {
			if turn == 1 {
				return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
			}
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			clientError := buildOpenAIWSHTTPBridgeErrorEvent(http.StatusBadGateway, "Upstream request failed")
			if writeErr := writeClientMessage(clientError); writeErr == nil {
				markOpenAIWSClientVisibleFailure(c, "error", clientError)
			}
			return nil, fmt.Errorf("upstream http bridge request failed: %s", safeErr)
		}
		if resp.StatusCode < 400 {
			break
		}

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, openAIWSHTTPBridgeErrorBodyLimitBytes))
		_ = resp.Body.Close()
		markOpenAICyberPolicyEvent(c, respBody, resp.StatusCode, nil)
		if resp.StatusCode == http.StatusBadRequest &&
			extractUpstreamErrorCode(respBody) == openAIWSFallbackReasonInvalidEncryptedContent {
			lineageMemory := newWSMaterializer(ctx)
			s.markOpenAIWSInvalidEncryptedContentLineageFromPayload(c, body, "ingress_ws_http_bridge_invalid_encrypted_lineage_mark", account.ID, turn, lineageMemory)
			lineageMemory.close()
			if lineageMemory != nil && lineageMemory.err != nil {
				return nil, lineageMemory.err
			}
		}
		retryMem := newWSMaterializer(ctx)
		retryBody, retryReason, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(resp.StatusCode, body, respBody, retryMem)
		if retryMem != nil && retryMem.err != nil {
			retryErr = retryMem.err
		}
		retryOwner := retryMem.take(retryBody)
		retryMem.close()
		if retryErr != nil {
			retryOwner.Drop()
			return nil, fmt.Errorf("normalize websocket http bridge rejected field retry: %w", retryErr)
		}
		if changed && rejectedFieldRetryState.Allow(retryBody) {
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_rejected_field_retry account_id=%d turn=%d reason=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(retryReason, openAIWSLogValueMaxLen),
			)
			requestMemory.close()
			requestMemory = nil
			if retryOwner != nil {
				bodyOwner.Drop()
				bodyOwner = retryOwner
			}
			body = retryBody
			payloadBytes = len(body)
			continue
		}
		retryOwner.Drop()

		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if upstreamMsg == "" {
			upstreamMsg = http.StatusText(resp.StatusCode)
		}
		shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMsg, respBody)
		if account.Platform == PlatformGrok {
			shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody)
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, resolveGrokWSUpstreamModel(account, body, originalModel)), account, resp.StatusCode, resp.Header, respBody)
			if shouldFailover && (turn == 1 || resp.StatusCode == http.StatusTooManyRequests) {
				return nil, newOpenAIUpstreamFailoverError(resp.StatusCode, resp.Header, respBody, upstreamMsg, false)
			}
		} else if shouldFailover && (turn == 1 || resp.StatusCode == http.StatusTooManyRequests) {
			return nil, s.handleFailoverErrorResponsePassthrough(ctx, resp, c, account, body, respBody)
		}
		if account.Platform != PlatformGrok && (shouldFailover || shouldCooldownOpenAITransientUpstreamError(resp.StatusCode, respBody)) {
			s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, actualModel)
		}
		clientError := buildOpenAIWSHTTPBridgeErrorEvent(resp.StatusCode, upstreamMsg)
		if writeErr := writeClientMessage(clientError); writeErr == nil {
			markOpenAIWSClientVisibleFailure(c, "error", clientError)
		}
		return nil, fmt.Errorf("upstream http bridge error: status=%d message=%s", resp.StatusCode, upstreamMsg)
	}
	var activity *openAIWSBridgeActivity
	defer func() {
		if activity != nil {
			activity.close()
		} else {
			_ = resp.Body.Close()
		}
	}()
	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, resolveGrokWSUpstreamModel(account, body, originalModel)), account, resp.Header, resp.StatusCode)
	}

	responseID := ""
	completedResponseID := ""
	meteredResponseID := ""
	meteredResponseIDConflict := false
	usage := OpenAIUsage{}
	imageCounter := newOpenAIImageOutputCounter()
	var firstTokenMs *int
	reqStream := openAIWSPayloadBoolFromRaw(body, "stream", true)
	eventCount := 0
	tokenEventCount := 0
	terminalEventCount := 0
	replayCollector := newWSReplayCollector(ctx)
	defer replayCollector.release()
	firstEventType := ""
	lastEventType := ""
	upstreamTerminalEvent := ""
	sawDone := false
	wroteDownstream := false
	var pendingClientMessages []json.RawMessage
	var pendingMemory wsMemoryReplay
	defer pendingMemory.drop()
	memoryEnabled := pkghttputil.HasWSRequestBodyMemory(ctx)
	if !memoryEnabled {
		pendingClientMessages = make([]json.RawMessage, 0, 4)
	}
	var localMemoryErr error
	releaseStaged := func() {
		pendingMemory.drop()
		pendingClientMessages = nil
	}

	pendingClientMessageBytes := int64(0)
	capacityFailoverSuppressedLogged := false
	officialOpenAIResponses := account != nil && account.Platform == PlatformOpenAI
	bareErrorPending := false
	var bareErrorPayload []byte
	var bareErrorOwner *pkghttputil.WSBodyMemoryOwner
	defer func() { bareErrorOwner.Drop() }()
	var createdAtStamper responsesCreatedAtStamper
	bareErrorMessage := ""
	failureAccountSideEffectsApplied := false
	mappedModel := actualModel
	needModelReplace := false
	var mappedModelBytes []byte
	if originalModel != "" {
		needModelReplace = mappedModel != "" && mappedModel != originalModel
		if needModelReplace {
			mappedModelBytes = []byte(mappedModel)
		}
	}

	reasoningEffort := ApplyThinkingEnabledFallback(extractOpenAIReasoningEffortFromBody(body, mappedModel, originalModel), body, mappedModel)
	firstOutputTimeout := time.Duration(0)
	if officialOpenAIResponses {
		effort := ""
		if reasoningEffort != nil {
			effort = *reasoningEffort
		}
		firstOutputTimeout = s.openAIFirstOutputTimeout(effort)
	}
	resultWithUsage := func(isError bool) *OpenAIForwardResult {
		imageCount := imageCounter.Count()
		result := &OpenAIForwardResult{
			RequestID:                     responseID,
			ResponseID:                    completedResponseID,
			Usage:                         usage,
			Model:                         originalModel,
			UpstreamModel:                 mappedModel,
			UpstreamResponseModel:         responseModelObserver.Model(),
			UpstreamResponseModelConflict: responseModelObserver.Conflict(),
			UpstreamResponseServiceTier:   responseModelObserver.ServiceTier(),
			ServiceTier:                   resolvedOpenAIUpstreamServiceTierFromObserver(responseModelObserver, extractOpenAIServiceTierFromBody(body)),
			ReasoningEffort:               reasoningEffort,
			Stream:                        reqStream,
			OpenAIWSMode:                  true,
			UpstreamTerminalEvent:         upstreamTerminalEvent,
			ResponseHeaders:               cloneHeader(resp.Header),
			Duration:                      time.Since(turnStart),
			FirstTokenMs:                  firstTokenMs,
		}
		replayCollector.attachResult(result, true, !isError)
		if imageCount > 0 {
			result.ImageCount = imageCount
			result.ImageSize = imageSizeTier
			result.ImageInputSize = imageInputSize
			result.ImageOutputSizes = imageCounter.Sizes()
			result.BillingModel = imageBillingModel
		}
		if isError && imageCount == 0 && GetOpsCyberPolicy(c) == nil {
			switch {
			case !openAIUsageHasTokens(&result.Usage):
				logOpenAIWSModeInfo("ingress_ws_http_bridge_error_usage_unavailable account_id=%d turn=%d reason=no_metered_usage", account.ID, turn)
			case meteredResponseID == "" || meteredResponseIDConflict:
				logOpenAIWSModeInfo("ingress_ws_http_bridge_error_usage_unavailable account_id=%d turn=%d reason=unconfirmed_response_id", account.ID, turn)
			default:
				result.RequestID = meteredResponseID
				result.WSBridgeMeteredError = true
			}
		}
		return result
	}

	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	if hasResponsesClientToolMapping(clientToolMapping) {
		resp.Body = newResponsesClientToolStreamBody(resp.Body, clientToolMapping, maxLineSize, s.cfg)
	}
	activity = newOpenAIWSBridgeActivity(ctx, resp.Body, s.cfg, maxLineSize, account.Platform, turnStart, firstOutputTimeout)
	writeActive := func(message []byte) error { return activity.write(message, writeClientMessage) }
	stopForMemory := func(err error) {
		localMemoryErr = err
		releaseStaged()
		activity.startDrain()
	}

	pendingSSEEventType := ""
	finalizeBareError := func() error {
		if !bareErrorPending {
			return nil
		}
		if !failureAccountSideEffectsApplied && localMemoryErr == nil {
			failureAccountSideEffectsApplied = s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, resp.Header, bareErrorPayload)
		}
		upstreamTerminalEvent = "response.failed"
		if activity.disconnected {
			return nil
		}
		clientMessage := buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel, createdAtStamper.Value(), bareErrorPayload, bareErrorMessage)
		if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
			clientMessage = rewritten
		}
		messages := pendingClientMessages
		defer releaseStaged()
		pendingClientMessageBytes = 0
		for i := 0; i <= len(messages); i++ {
			message := clientMessage
			if i < len(messages) {
				message = messages[i]
			}
			if err := writeActive(message); err != nil {
				if controlErr := activity.observeControl(); controlErr != nil {
					return controlErr
				}
				if activity.disconnected {
					return nil
				}
				return fmt.Errorf("write synthesized websocket response.failed: %w", err)
			}
			wroteDownstream = true
		}
		markOpenAIWSClientVisibleFailure(c, "response.failed", clientMessage)
		return nil
	}
	disconnectedResult := func(cause error) (*OpenAIForwardResult, error) {
		if localMemoryErr != nil {
			return resultWithUsage(true), NewOpenAIWSBodyMemoryCloseError(localMemoryErr)
		}
		return resultWithUsage(true), NewOpenAIWSClientCloseError(coderws.StatusNormalClosure, "client disconnected", errors.Join(context.Canceled, cause))
	}
	finishDisconnected := func(cause error) (*OpenAIForwardResult, error) {
		activity.close()
		if controlErr := activity.observeControl(); controlErr != nil {
			return resultWithUsage(true), controlErr
		}
		if bareErrorPending {
			if err := finalizeBareError(); err != nil {
				cause = errors.Join(cause, err)
			}
		}
		if controlErr := activity.observeControl(); controlErr != nil {
			return resultWithUsage(true), controlErr
		}
		return disconnectedResult(cause)
	}
	finishBoundary := func(reason openAIWSBridgeStreamEnd, cause error) (*OpenAIForwardResult, error) {
		// Stop the upstream before attempting a final bounded client write.
		activity.close()
		if reason == openAIWSBridgeStreamControl {
			return resultWithUsage(true), cause
		}
		if controlErr := activity.observeControl(); controlErr != nil {
			return resultWithUsage(true), controlErr
		}
		if activity.disconnected {
			return finishDisconnected(errors.New("upstream usage incomplete after stream deadline"))
		}
		message := "Upstream response stream timed out (no upstream data); request was not replayed"
		closeReason := "upstream stream timed out"
		if reason == openAIWSBridgeStreamFirstOutput {
			message = "Upstream produced no model output before the deadline; request was not replayed"
			closeReason = "upstream first output timed out"
		}
		cause = errors.New(message)
		if bareErrorPending {
			cause = errors.New(bareErrorMessage)
			if err := finalizeBareError(); err != nil {
				cause = err
			}
		} else {
			releaseStaged()
			pendingClientMessageBytes = 0
			clientError := buildOpenAIWSHTTPBridgeErrorEvent(http.StatusGatewayTimeout, message)
			if err := writeActive(clientError); err == nil {
				markOpenAIWSClientVisibleFailure(c, "error", clientError)
			}
		}
		if controlErr := activity.observeControl(); controlErr != nil {
			return resultWithUsage(true), controlErr
		}
		if activity.disconnected {
			return disconnectedResult(cause)
		}
		return resultWithUsage(true), NewOpenAIWSClientCloseError(coderws.StatusInternalError, closeReason, cause)
	}
	var scanErr error
	for {
		event, reason, cause := activity.next()
		if reason != openAIWSBridgeStreamContinue {
			return finishBoundary(reason, cause)
		}
		if event.keepalive {
			if err := writeActive([]byte(`{"type":"keepalive"}`)); err != nil {
				if controlErr := activity.observeControl(); controlErr != nil {
					return resultWithUsage(true), controlErr
				}
				if !activity.disconnected {
					return resultWithUsage(true), wrapOpenAIWSIngressTurnError("write_client", fmt.Errorf("write client websocket keepalive: %w", err), wroteDownstream)
				}
			}
			continue
		}
		if event.eof {
			scanErr = event.err
			break
		}
		line := event.line
		if eventType, ok := extractOpenAISSEEventLine(line); ok {
			pendingSSEEventType = eventType
			continue
		}
		if strings.TrimSpace(line) == "" {
			pendingSSEEventType = ""
			continue
		}
		data, ok := extractOpenAISSEDataLine(line)
		if !ok {
			continue
		}
		trimmedData := strings.TrimSpace(data)
		if trimmedData == "" {
			continue
		}
		if trimmedData == "[DONE]" {
			sawDone = true
			continue
		}

		upstreamMessage := []byte(openAICompatPayloadWithEventType(trimmedData, pendingSSEEventType))
		if normalized, changed := normalizeCompletedImageGenerationStatus(upstreamMessage); changed {
			upstreamMessage = normalized
		}
		eventType, eventResponseID, response := parseOpenAIWSEventEnvelope(upstreamMessage)
		// Event IDs are not billing identities. Observe only an explicitly
		// identified response and reject conflicting IDs within this turn.
		if eventResponseID != "" && !meteredResponseIDConflict {
			confirmedID := ""
			if id := response.Get("id"); id.Type == gjson.String {
				confirmedID = strings.TrimSpace(id.String())
			}
			if confirmedID == "" && gjson.GetBytes(upstreamMessage, "object").String() == "response" {
				if id := gjson.GetBytes(upstreamMessage, "id"); id.Type == gjson.String {
					confirmedID = strings.TrimSpace(id.String())
				}
			}
			if confirmedID != "" {
				if meteredResponseID == "" {
					meteredResponseID = confirmedID
				} else if meteredResponseID != confirmedID {
					meteredResponseIDConflict = true
				}
			}
		}
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
		if isOpenAIWSTokenEvent(eventType) {
			tokenEventCount++
			if firstTokenMs == nil {
				ms := int(time.Since(turnStart).Milliseconds())
				firstTokenMs = &ms
			}
		}
		if openAIWSMessageShouldParseUsage(eventType, upstreamMessage) {
			parseOpenAIWSResponseUsageFromCompletedEvent(upstreamMessage, &usage)
		}
		if eventType == "error" || eventType == "response.failed" {
			markOpenAICyberPolicyEvent(c, upstreamMessage, http.StatusOK, &usage)
		}
		imageCounter.AddSSEData(upstreamMessage)

		if needModelReplace && len(mappedModelBytes) > 0 && openAIWSEventMayContainModel(eventType) && strings.Contains(trimmedData, mappedModel) {
			upstreamMessage = replaceOpenAIWSMessageModel(upstreamMessage, mappedModel, originalModel)
		}
		if s.toolCorrector != nil && openAIWSEventMayContainToolCalls(eventType) && openAIWSMessageLikelyContainsToolCalls(upstreamMessage) {
			if corrected, changed := s.toolCorrector.CorrectToolCallsInSSEBytes(upstreamMessage); changed {
				upstreamMessage = corrected
			}
		}
		if stamped, changed := createdAtStamper.Stamp(upstreamMessage, eventType); changed {
			upstreamMessage = stamped
		}
		replayCollector.AddEvent(eventType, upstreamMessage)

		var upstreamEventErr error
		if officialOpenAIResponses && bareErrorPending && (eventType == "response.completed" || eventType == "response.done") {
			// Some upstreams emit a recoverable bare error before the authoritative
			// successful terminal. Do not replace that terminal with a synthetic
			// failure or retain side effects from the superseded error.
			bareErrorPending = false
			bareErrorPayload = nil
			bareErrorOwner.Drop()
			bareErrorOwner = nil
			bareErrorMessage = ""
		}
		suppressClientMessage := officialOpenAIResponses && bareErrorPending && eventType != "response.failed"
		if eventType == "error" || eventType == "response.failed" {
			errMessage := extractOpenAISSEErrorMessage(upstreamMessage)
			if errMessage == "" {
				errMessage = "upstream error event"
			}
			statusCode := openAIStreamFailureStatus(upstreamMessage, errMessage)
			shouldFailover := openAIStreamFailedEventShouldFailover(upstreamMessage, errMessage)
			if eventType == "error" {
				errCodeRaw, errTypeRaw, _ := parseOpenAIWSErrorEventFields(upstreamMessage)
				shouldFailover = openAIStreamErrorEventShouldFailover(upstreamMessage, errMessage)
				if account.Platform == PlatformGrok {
					statusCode = openAIWSErrorHTTPStatusFromRaw(errCodeRaw, errTypeRaw)
				}
				if reason, _ := classifyOpenAIWSErrorEventFromRaw(errCodeRaw, errTypeRaw, errMessage); reason == openAIWSFallbackReasonInvalidEncryptedContent {
					lineageMemory := newWSMaterializer(ctx)
					s.markOpenAIWSInvalidEncryptedContentLineageFromPayload(c, body, "ingress_ws_http_bridge_invalid_encrypted_lineage_mark", account.ID, turn, lineageMemory)
					lineageMemory.close()
					if lineageMemory != nil && lineageMemory.err != nil {
						// The attempt has already been sent: reuse the bounded
						// usage drain instead of abandoning its real terminal.
						stopForMemory(lineageMemory.err)
					}
				}
			}
			requestScopedCapacity := isOpenAIUpstreamCapacityShedEvent(upstreamMessage)
			if account.Platform == PlatformGrok && eventType == "error" {
				// SSE error events do not carry an HTTP status. The local status
				// mapper therefore defaults unknown xAI codes (for example
				// new_sensitive) to 502; classify the body as a request-scoped
				// 403 before applying status-based failover or account state.
				if isGrokContentPolicyRejection(http.StatusForbidden, upstreamMessage) {
					shouldFailover = false
				} else {
					shouldFailover = s.shouldFailoverGrokUpstreamError(statusCode, upstreamMessage)
					s.handleGrokAccountUpstreamError(ctx, account, statusCode, resp.Header, upstreamMessage)
				}
			}
			// A disconnected client needs this attempt drained for usage, not replayed,
			// even when only non-semantic heartbeats were delivered.
			if controlErr := activity.observeControl(); controlErr != nil {
				return resultWithUsage(true), controlErr
			}
			if !activity.disconnected && !wroteDownstream && shouldFailover && (turn == 1 || statusCode == http.StatusTooManyRequests) {
				if account.Platform == PlatformGrok {
					return nil, newOpenAIUpstreamFailoverError(statusCode, resp.Header, upstreamMessage, errMessage, false)
				}
				return nil, s.newOpenAIStreamFailoverErrorWithModel(c, account, true, resp.Header.Get("x-request-id"), upstreamMessage, errMessage, mappedModel, resp.Header)
			}
			if account.Platform != PlatformGrok && !failureAccountSideEffectsApplied {
				if eventType == "response.failed" || (!officialOpenAIResponses && shouldFailover && !requestScopedCapacity) {
					failureAccountSideEffectsApplied = s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, resp.Header, upstreamMessage)
				}
			}
			if wroteDownstream && requestScopedCapacity && !capacityFailoverSuppressedLogged {
				logOpenAICapacityFailoverSuppressed(ctx, account, "ws_http_bridge", resp.Header.Get("x-request-id"), eventType)
				capacityFailoverSuppressedLogged = true
			}
			if eventType == "error" && !officialOpenAIResponses {
				upstreamEventErr = errors.New(errMessage)
			} else if eventType == "error" {
				bareErrorPending = true
				if memoryEnabled {
					payload, owner, err := pkghttputil.CloneWSBody(ctx, upstreamMessage)
					if err != nil {
						bareErrorOwner.Drop()
						bareErrorOwner = nil
						bareErrorPayload = nil
						stopForMemory(err)
					} else {
						bareErrorOwner.Drop()
						bareErrorPayload, bareErrorOwner = payload, owner
					}
				} else {
					bareErrorPayload = append(bareErrorPayload[:0], upstreamMessage...)
				}
				bareErrorMessage = errMessage
				suppressClientMessage = true
			} else {
				bareErrorPending = false
			}
		}

		// 客户端写出副本改写容量降载码：Codex 对 error/response.failed 中的
		// server_is_overloaded / slow_down 判致命并终止会话，改写后走客户端内置
		// 重试。账号状态与终止事件判定（下方 handleOpenAIWSTerminalTransientFailure）
		// 仍使用未改写的 upstreamMessage。
		clientMessage := upstreamMessage
		if eventType == "error" || eventType == "response.failed" {
			if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
				clientMessage = rewritten
			}
		}
		stageBeforeSemanticOutput := turn == 1 && officialOpenAIResponses && !wroteDownstream
		startsClientOutput := false
		if activity.firstTimer != nil || (stageBeforeSemanticOutput && !activity.disconnected && !suppressClientMessage) {
			semanticData := string(clientMessage)
			startsClientOutput = openAIStreamDataStartsClientOutput(semanticData, eventType) &&
				!isOpenAIHTTPRelayReadyMetadata(account, semanticData, eventType, pendingSSEEventType)
			if startsClientOutput {
				activity.stopFirstOutput()
			}
		}
		if !activity.disconnected && !suppressClientMessage {
			isKeepalive := eventType == "keepalive"
			commitStagedMessages := !stageBeforeSemanticOutput ||
				startsClientOutput ||
				isOpenAIWSTerminalEvent(eventType)
			if stageBeforeSemanticOutput && !commitStagedMessages && !isKeepalive {
				if pendingClientMessageBytes+int64(len(clientMessage)) > openAIFirstOutputStageMaxBytes {
					return nil, s.newOpenAIStreamFailoverError(
						c,
						account,
						true,
						resp.Header.Get("x-request-id"),
						nil,
						"OpenAI WS HTTP bridge first-output staging limit exceeded",
						resp.Header,
					)
				}
				if memoryEnabled {
					err := pendingMemory.grow(ctx)
					if err == nil {
						var copyBody []byte
						var owner *pkghttputil.WSBodyMemoryOwner
						copyBody, owner, err = pkghttputil.CloneWSBody(ctx, clientMessage)
						if err == nil {
							pendingMemory.items = append(pendingMemory.items, copyBody)
							pendingMemory.owners = append(pendingMemory.owners, owner)
							pendingClientMessages = pendingMemory.items
						}
					}
					if err != nil {
						stopForMemory(err)
					}
				} else {
					pendingClientMessages = append(pendingClientMessages, append([]byte(nil), clientMessage...))
				}
				if localMemoryErr == nil {
					pendingClientMessageBytes += int64(len(clientMessage))
				}
			} else {
				// Keep the client connection alive without committing this attempt
				// or exposing its staged lifecycle metadata.
				var messages []json.RawMessage
				if !isKeepalive {
					messages = pendingClientMessages
					pendingClientMessageBytes = 0
				}
				for i := 0; i <= len(messages); i++ {
					message := clientMessage
					if i < len(messages) {
						message = messages[i]
					}
					if err := writeActive(message); err != nil {
						if controlErr := activity.observeControl(); controlErr != nil {
							return resultWithUsage(true), controlErr
						}
						if activity.disconnected {
							closeStatus, closeReason := summarizeOpenAIWSReadCloseError(err)
							logOpenAIWSModeInfo(
								"ingress_ws_http_bridge_client_disconnected_drain account_id=%d turn=%d close_status=%s close_reason=%s",
								account.ID,
								turn,
								closeStatus,
								truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
							)
							break
						}
						return nil, wrapOpenAIWSIngressTurnError(
							"write_client",
							fmt.Errorf("write client websocket event: %w", err),
							wroteDownstream,
						)
					}
					if !isKeepalive {
						wroteDownstream = true
					}
				}
				if !isKeepalive {
					releaseStaged()
				}
			}
		}
		if !activity.disconnected && !suppressClientMessage {
			markOpenAIWSClientVisibleFailure(c, eventType, upstreamMessage)
		}

		if upstreamEventErr != nil {
			if localMemoryErr != nil {
				return resultWithUsage(true), NewOpenAIWSBodyMemoryCloseError(localMemoryErr)
			}
			return resultWithUsage(true), upstreamEventErr
		}
		if isOpenAIWSTerminalEvent(eventType) && !bareErrorPending {
			if eventType == "response.failed" {
				upstreamTerminalEvent = "response.failed"
			} else {
				upstreamTerminalEvent = s.handleOpenAIWSTerminalTransientFailure(ctx, account, mappedModel, resp.Header, upstreamMessage)
			}
			if upstreamTerminalEvent == "response.completed" || upstreamTerminalEvent == "response.done" {
				status := strings.TrimSpace(gjson.GetBytes(upstreamMessage, "response.status").String())
				if status == "" || status == "completed" {
					completedResponseID = strings.TrimSpace(gjson.GetBytes(upstreamMessage, "response.id").String())
				}
			}
			terminalEventCount++
			firstTokenMsValue := -1
			if firstTokenMs != nil {
				firstTokenMsValue = *firstTokenMs
			}
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_turn_completed account_id=%d turn=%d response_id=%s payload_bytes=%d duration_ms=%d events=%d token_events=%d terminal_events=%d first_event=%s last_event=%s first_token_ms=%d client_disconnected=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
				payloadBytes,
				time.Since(turnStart).Milliseconds(),
				eventCount,
				tokenEventCount,
				terminalEventCount,
				truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
				truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
				firstTokenMsValue,
				activity.disconnected,
			)
			if localMemoryErr != nil {
				return resultWithUsage(true), NewOpenAIWSBodyMemoryCloseError(localMemoryErr)
			}
			return resultWithUsage(false), nil
		}
	}
	if controlErr := activity.observeControl(); controlErr != nil {
		return resultWithUsage(true), controlErr
	}
	if activity.disconnected {
		return finishDisconnected(scanErr)
	}
	if bareErrorPending {
		finalizeErr := finalizeBareError()
		if controlErr := activity.observeControl(); controlErr != nil {
			return resultWithUsage(true), controlErr
		}
		if activity.disconnected {
			return disconnectedResult(errors.Join(errors.New(bareErrorMessage), scanErr, finalizeErr))
		}
		if finalizeErr != nil {
			return resultWithUsage(true), finalizeErr
		}
		if scanErr != nil {
			return resultWithUsage(true), fmt.Errorf("read upstream http bridge stream after error event: %w", scanErr)
		}
		return resultWithUsage(true), errors.New(bareErrorMessage)
	}
	if scanErr != nil {
		streamErr := fmt.Errorf("read upstream http bridge stream: %w", scanErr)
		if turn == 1 && !activity.disconnected && !wroteDownstream {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, streamErr, true)
		}
		return resultWithUsage(true), streamErr
	}
	terminalErr := errors.New("upstream http bridge stream ended before terminal event")
	if sawDone {
		terminalErr = errors.New("upstream http bridge stream sent [DONE] before terminal event")
	}
	if turn == 1 && !activity.disconnected && !wroteDownstream {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, terminalErr, true)
	}
	return resultWithUsage(true), terminalErr
}

func resolveGrokWSCacheIdentity(c *gin.Context, account *Account, seedPayload, currentPayload []byte, originalModel string) (string, error) {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	body, owner, err := prepareOpenAIWSHTTPBridgeBodyWithMemory(ctx, account, seedPayload)
	if err != nil {
		return "", err
	}
	defer owner.Drop()
	upstreamModel := resolveGrokWSUpstreamModel(account, currentPayload, originalModel)
	patched, patchedOwner, err := transformWSBody(ctx, body, func(mem *wsMaterializer) ([]byte, error) { return patchGrokResponsesBody(body, upstreamModel, mem) })
	if err != nil {
		return "", err
	}
	defer patchedOwner.Drop()
	mem := newWSMaterializer(ctx)
	defer mem.close()
	identity := resolveGrokCacheIdentity(c, patched, "", upstreamModel, mem)
	if mem != nil && mem.err != nil {
		return "", mem.err
	}
	return identity, nil
}

func resolveGrokWSUpstreamModel(account *Account, body []byte, originalModel string) string {
	upstreamModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	originalModel = strings.TrimSpace(originalModel)
	// Shared ingress has already applied channel and account mappings when the
	// body model differs from the client-facing model. Only resolve from the
	// original model when the body still carries that original value.
	if account != nil && originalModel != "" && (upstreamModel == "" || upstreamModel == originalModel) {
		if mappedModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(originalModel)); mappedModel != "" {
			upstreamModel = mappedModel
		}
	}
	if upstreamModel == "" {
		upstreamModel = grokDefaultResponsesModel
	}
	return upstreamModel
}
