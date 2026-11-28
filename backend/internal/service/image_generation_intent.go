package service

import (
	"bytes"
	"context"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	openAIResponsesEndpoint          = "/v1/responses"
	openAIResponsesCompactEndpoint   = "/v1/responses/compact"
	responsesLiteHeader              = "X-OpenAI-Internal-Codex-Responses-Lite"
	responsesLiteHeaderKey           = "x-openai-internal-codex-responses-lite"
	responsesLiteWSMetadataKey       = "ws_request_header_x_openai_internal_codex_responses_lite"
	imageGenerationPermissionMessage = "Image generation is not enabled for this group"
)

func isOpenAIResponsesLiteHeader(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func isOpenAIResponsesLiteWebSocketPayload(body []byte, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	return isOpenAIResponsesLiteHeader(mem.get(body, "client_metadata."+responsesLiteWSMetadataKey).String())
}

// ImageGenerationPermissionMessage returns the stable end-user error text for disabled groups.
func ImageGenerationPermissionMessage() string {
	return imageGenerationPermissionMessage
}

// GroupAllowsImageGeneration preserves ungrouped-key behavior and enforces the flag when a group is present.
func GroupAllowsImageGeneration(group *Group) bool {
	return group == nil || group.AllowImageGeneration
}

var (
	// imageGenLiteralBytes 覆盖扫描侧的全部非模型分支：tool type image_generation
	// 自带该前缀，namespace / tool_choice / 函数引用的名字都是 image_gen。
	imageGenLiteralBytes = []byte("image_gen")
	// imageModelFoldBytes 覆盖 model 分支：gpt-image-* 与 grok-imagine* 都含 imag，
	// 模型名按 ToLower 比较，这里做 ASCII 折叠匹配。
	imageModelFoldBytes = []byte("imag")
)

// openAIBodyMayDeclareImageGeneration 是生图意图扫描的字节级必要条件。
//
// 扫描侧每个能让意图成立的取值都要求 body 里出现 image_gen 字面量，或 model 值本身
// 是图片模型；因此不满足本判定的 body 一定不会命中扫描，可以整段跳过
// gjson.ValidBytes 全量校验与根对象遍历（大 body 上这两步各占数百微秒）。
// 只做字节级预检查，宁多算不漏判：含 \u 转义时交给完整扫描兜底，因为 JSON 字符串
// 允许把 image_gen 的任意字节写成 unicode 转义。
//
// 唯一的理论缺口：模型名比较走 strings.ToLower（Unicode 简易映射），把非 ASCII
// 字母折叠成 ASCII 的拼写（如 İ→i）不在字节折叠覆盖内；这类拼写不是真实模型 id，
// 且以 \u 形式出现时仍由上面的转义兜底放行。
func openAIBodyMayDeclareImageGeneration(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	if bytes.Contains(body, imageGenLiteralBytes) {
		return true
	}
	if containsASCIIFold(body, imageModelFoldBytes) {
		return true
	}
	return bytes.Contains(body, jsonUnicodeEscapeBytes)
}

// IsImageGenerationIntent classifies requests that can produce generated images.
func IsImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
	return openAIImageGenerationIntent(endpoint, requestedModel, body, false, true)
}

// IsExplicitImageGenerationIntent 仅检测原生 image_generation 工具、图片模型和显式 tool_choice，
// 不检测被动的 image_gen namespace 声明。用于 capability 路由决策——被动 namespace 不应
// 强制要求原生 Responses 能力，否则 Chat Completions-only 账号会被误过滤（#4476）。
func IsExplicitImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
	return openAIImageGenerationIntent(endpoint, requestedModel, body, true, true)
}

// IsImageGenerationIntentForPlatform applies platform-specific intent rules.
//
// Codex advertises the image_gen namespace on ordinary Responses requests so
// that it is available if the model needs it. Grok strips namespace and
// Responses Lite additional_tools declarations before forwarding, so those
// declarations alone must not turn every Codex request into an image request.
// Native image_generation tools, explicit image selection and image models
// remain image intent. Other platforms retain the original declaration rule.
func IsImageGenerationIntentForPlatform(endpoint string, requestedModel string, body []byte, platform string) bool {
	return openAIImageGenerationIntentForPlatform(endpoint, requestedModel, body, platform, true)
}

// IsImageGenerationIntentForPlatformValidated 与 IsImageGenerationIntentForPlatform 判定相同，
// 但要求调用方已经确认 body 是合法 JSON（例如 handler 已对同一份字节做过 gjson.ValidBytes）。
// 它跳过这次全量校验——大 body 上这一项占判定耗时的六成以上。非法 JSON 的语义与未校验
// 版本不同，只允许在校验过同一份字节、且后续改写只经由 sjson / json.Marshal 的调用点使用。
func IsImageGenerationIntentForPlatformValidated(endpoint string, requestedModel string, body []byte, platform string) bool {
	return openAIImageGenerationIntentForPlatform(endpoint, requestedModel, body, platform, false)
}

func openAIImageGenerationIntentForPlatform(endpoint string, requestedModel string, body []byte, platform string, validate bool, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !strings.EqualFold(strings.TrimSpace(platform), PlatformGrok) {
		return openAIImageGenerationIntent(endpoint, requestedModel, body, false, validate, mem)
	}
	// Grok removes namespace catalogs before forwarding. Native image_generation
	// remains an explicit capability request.
	return openAIImageGenerationIntent(endpoint, requestedModel, body, true, validate, mem)
}

// openAIImageGenerationIntent 统一入口：先做 O(1) 端点/模型判断，再用字节级预检查
// 挡住不可能命中扫描的 body，最后才按需校验并遍历根对象。
func openAIImageGenerationIntent(endpoint string, requestedModel string, body []byte, explicit bool, validate bool, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if IsImageGenerationEndpoint(endpoint) || isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if !openAIBodyMayDeclareImageGeneration(body) {
		return false
	}
	if validate && !gjson.ValidBytes(body) {
		return false
	}
	root := parseRawJSONView(body)
	if explicit {
		return explicitImageGenerationDeclarationsFromRoot(root, mem)
	}
	return imageGenerationDeclarationsFromRoot(root, mem)
}

// imageGenerationDeclarationsFromRoot 走一遍根对象，保留「重复键取第一个」语义，
// 覆盖 model、tools、input.additional_tools 与 tool_choice。调用方负责 JSON 合法性。
func imageGenerationDeclarationsFromRoot(root gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	var modelSeen, toolsSeen, inputSeen, toolChoiceSeen bool
	imageIntent := false
	mem.forEachArray(root, func(key, value gjson.Result) bool {
		// GetBytes returns the first duplicate key; retain that behavior while walking the root once.
		switch key.Str {
		case "model":
			if !modelSeen {
				modelSeen = true
				imageIntent = isOpenAIImageGenerationModel(strings.TrimSpace(value.String()))
			}
		case "tools":
			if !toolsSeen {
				toolsSeen = true
				imageIntent = openAIJSONToolsContainImageGeneration(value, mem)
			}
		case "input":
			if !inputSeen {
				inputSeen = true
				imageIntent = openAIJSONInputContainsImageGenTool(value, mem)
			}
		case "tool_choice":
			if !toolChoiceSeen {
				toolChoiceSeen = true
				imageIntent = openAIJSONToolChoiceSelectsImageGeneration(value, mem)
			}
		}
		return !imageIntent && (!modelSeen || !toolsSeen || !inputSeen || !toolChoiceSeen)
	})
	return imageIntent
}

// explicitImageGenerationDeclarationsFromRoot 只认原生 image_generation 工具、图片模型
// 与显式 tool_choice，不认被动的 image_gen namespace 声明。
func explicitImageGenerationDeclarationsFromRoot(root gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	var modelSeen, toolsSeen, toolChoiceSeen bool
	imageIntent := false
	mem.forEachArray(root, func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			if !modelSeen {
				modelSeen = true
				imageIntent = isOpenAIImageGenerationModel(strings.TrimSpace(value.String()))
			}
		case "tools":
			if !toolsSeen {
				toolsSeen = true
				imageIntent = openAIJSONToolsContainNativeImageGeneration(value, mem)
			}
		case "tool_choice":
			if !toolChoiceSeen {
				toolChoiceSeen = true
				imageIntent = openAIJSONToolChoiceSelectsExplicitImageGeneration(value, mem)
			}
		}
		return !imageIntent && (!modelSeen || !toolsSeen || !toolChoiceSeen)
	})
	return imageIntent
}

// IsImageGenerationIntentMap is the map-backed variant used after service-side request mutation.
func IsImageGenerationIntentMap(endpoint string, requestedModel string, reqBody map[string]any) bool {
	if IsImageGenerationEndpoint(endpoint) {
		return true
	}
	if isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if reqBody == nil {
		return false
	}
	if isOpenAIImageGenerationModel(firstNonEmptyString(reqBody["model"])) {
		return true
	}
	if hasOpenAIImageGenerationTool(reqBody) {
		return true
	}
	return openAIAnyToolChoiceSelectsImageGeneration(reqBody["tool_choice"])
}

// IsImageGenerationEndpoint identifies dedicated generated-image endpoints.
func IsImageGenerationEndpoint(endpoint string) bool {
	switch normalizeImageGenerationEndpoint(endpoint) {
	case "/v1/images/generations", "/v1/images/edits", "/images/generations", "/images/edits":
		return true
	default:
		return false
	}
}

func normalizeImageGenerationEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(strings.ToLower(endpoint))
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimPrefix(endpoint, "https://api.openai.com")
	if idx := strings.IndexByte(endpoint, '?'); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return strings.TrimRight(endpoint, "/")
}

func openAIJSONToolsContainImageGeneration(tools gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !tools.IsArray() {
		return false
	}
	found := false
	mem.forEachArray(tools, func(_, item gjson.Result) bool {
		if isOpenAIImageGenerationType(openAIJSONString(mem.resultGet(item, "type"))) {
			found = true
			return false
		}
		if isImageGenNamespaceTool(item, mem) {
			found = true
			return false
		}
		return true
	})
	return found
}

func openAIJSONToolsContainNativeImageGeneration(tools gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !tools.IsArray() {
		return false
	}
	found := false
	mem.forEachArray(tools, func(_, item gjson.Result) bool {
		found = isOpenAIImageGenerationType(openAIJSONString(mem.resultGet(item, "type")))
		return !found
	})
	return found
}

func isOpenAIImageGenerationType(value string) bool {
	return strings.TrimSpace(value) == "image_generation"
}

func isOpenAIImageGenNamespaceName(value string) bool {
	return strings.TrimSpace(value) == "image_gen"
}

// isImageGenNamespaceTool detects the namespace advertised by Codex's built-in
// image-generation extension instead of a hosted image_generation tool.
func isImageGenNamespaceTool(tool gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	return openAIJSONString(mem.resultGet(tool, "type")) == "namespace" &&
		isOpenAIImageGenNamespaceName(openAIJSONString(mem.resultGet(tool, "name")))
}

// openAIJSONInputContainsImageGenTool scans Responses input items for
// additional_tools entries that declare the image_gen namespace. This covers
// the "Responses Lite" format where tools are embedded inside input items
// rather than top-level tools.
func openAIJSONInputContainsImageGenTool(input gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !input.IsArray() {
		return false
	}
	found := false
	mem.forEachArray(input, func(_, item gjson.Result) bool {
		if openAIJSONString(mem.resultGet(item, "type")) != "additional_tools" {
			return true
		}
		found = openAIJSONToolsContainImageGeneration(mem.resultGet(item, "tools"), mem)
		return !found
	})
	return found
}

func openAIRequestBodyHasImageGenerationDeclaration(body []byte, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if len(body) == 0 || !openAIBodyMayDeclareImageGeneration(body) || !gjson.ValidBytes(body) {
		return false
	}
	return openAIJSONToolsContainImageGeneration(mem.get(body, "tools"), mem) ||
		openAIJSONInputContainsImageGenTool(mem.get(body, "input"), mem) ||
		openAIJSONToolChoiceSelectsImageGeneration(mem.get(body, "tool_choice"), mem)
}

func openAIRequestBodyImageGenerationToolNeedsNormalization(body []byte, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if len(body) == 0 || !openAIBodyMayDeclareImageGeneration(body) || !gjson.ValidBytes(body) {
		return false
	}
	tools := mem.get(body, "tools")
	if !tools.IsArray() {
		return false
	}
	needsNormalization := false
	mem.forEachArray(tools, func(_, item gjson.Result) bool {
		if openAIJSONString(mem.resultGet(item, "type")) != "image_generation" {
			return true
		}
		// 只有旧字段或明确的模型不兼容字段需要修正时才进入 map 修改。
		if mem.resultGet(item, "format").Exists() || mem.resultGet(item, "compression").Exists() {
			needsNormalization = true
			return false
		}
		imageModel := strings.ToLower(strings.TrimSpace(mem.resultGet(item, "model").String()))
		if strings.HasPrefix(imageModel, "gpt-image-2") && mem.resultGet(item, "input_fidelity").Exists() {
			needsNormalization = true
			return false
		}
		return true
	})
	return needsNormalization
}

func openAIJSONToolChoiceSelectsImageGeneration(choice gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !choice.Exists() {
		return false
	}
	if choice.Type == gjson.String {
		return isOpenAIImageGenerationType(choice.String())
	}
	if !choice.IsObject() {
		return false
	}
	choiceType := openAIJSONString(mem.resultGet(choice, "type"))
	if isOpenAIImageGenerationType(choiceType) {
		return true
	}
	if choiceType == "namespace" &&
		(isOpenAIImageGenNamespaceName(openAIJSONString(mem.resultGet(choice, "name"))) ||
			isOpenAIImageGenNamespaceName(openAIJSONString(mem.resultGet(choice, "namespace")))) {
		return true
	}
	if tool := mem.resultGet(choice, "tool"); tool.IsObject() && openAIJSONToolChoiceSelectsImageGeneration(tool, mem) {
		return true
	}
	if isOpenAIImageGenerationType(openAIJSONString(mem.resultGet(choice, "function.name"))) {
		return true
	}
	return false
}

func openAIJSONToolChoiceSelectsExplicitImageGeneration(choice gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if openAIJSONToolChoiceSelectsImageGeneration(choice, mem) {
		return true
	}
	if !choice.IsObject() {
		return false
	}
	if tool := mem.resultGet(choice, "tool"); tool.IsObject() && openAIJSONToolChoiceSelectsExplicitImageGeneration(tool, mem) {
		return true
	}
	if isOpenAIImageGenFunctionReference(
		openAIJSONString(mem.resultGet(choice, "namespace")),
		openAIJSONString(mem.resultGet(choice, "name")),
	) {
		return true
	}
	if fn := mem.resultGet(choice, "function"); fn.IsObject() {
		return isOpenAIImageGenFunctionReference(
			openAIJSONString(mem.resultGet(fn, "namespace")),
			openAIJSONString(mem.resultGet(fn, "name")),
		)
	}
	return false
}

func isOpenAIImageGenFunctionReference(namespace string, name string) bool {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "image_gen" && name == "imagegen" {
		return true
	}
	switch name {
	case "image_gen.imagegen", "image_gen__imagegen":
		return true
	default:
		return false
	}
}

func openAIAnyToolChoiceSelectsImageGeneration(choice any) bool {
	switch v := choice.(type) {
	case string:
		return isOpenAIImageGenerationType(v)
	case map[string]any:
		choiceType := strings.TrimSpace(firstNonEmptyString(v["type"]))
		if isOpenAIImageGenerationType(choiceType) {
			return true
		}
		if choiceType == "namespace" &&
			(isOpenAIImageGenNamespaceName(firstNonEmptyString(v["name"])) ||
				isOpenAIImageGenNamespaceName(firstNonEmptyString(v["namespace"]))) {
			return true
		}
		if tool, ok := v["tool"].(map[string]any); ok && openAIAnyToolChoiceSelectsImageGeneration(tool) {
			return true
		}
		if fn, ok := v["function"].(map[string]any); ok && isOpenAIImageGenerationType(firstNonEmptyString(fn["name"])) {
			return true
		}
	}
	return false
}

func getAPIKeyFromContext(c interface{ Get(string) (any, bool) }) *APIKey {
	if c == nil {
		return nil
	}
	v, exists := c.Get("api_key")
	if !exists {
		return nil
	}
	apiKey, _ := v.(*APIKey)
	return apiKey
}

func apiKeyGroup(apiKey *APIKey) *Group {
	if apiKey == nil {
		return nil
	}
	return apiKey.Group
}

type OpenAIResponsesImageBillingConfig struct {
	Model     string
	SizeTier  string
	InputSize string
}

func resolveOpenAIResponsesImageBillingConfigDetailed(reqBody map[string]any, fallbackModel string) (OpenAIResponsesImageBillingConfig, error) {
	imageModel := ""
	imageSize := ""
	hasImageTool := false
	if reqBody != nil {
		rawTools, _ := reqBody["tools"].([]any)
		for _, rawTool := range rawTools {
			toolMap, ok := rawTool.(map[string]any)
			if !ok || strings.TrimSpace(firstNonEmptyString(toolMap["type"])) != "image_generation" {
				continue
			}
			hasImageTool = true
			imageModel = strings.TrimSpace(firstNonEmptyString(toolMap["model"]))
			imageSize = strings.TrimSpace(firstNonEmptyString(toolMap["size"]))
			break
		}
		if imageSize == "" {
			imageSize = strings.TrimSpace(firstNonEmptyString(reqBody["size"]))
		}
	}
	if imageModel == "" && reqBody != nil {
		bodyModel := strings.TrimSpace(firstNonEmptyString(reqBody["model"]))
		if isOpenAIImageBillingModelAlias(bodyModel) || !hasImageTool {
			imageModel = bodyModel
		}
	}
	if imageModel == "" && hasImageTool {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = strings.TrimSpace(fallbackModel)
	}
	sizeTier := normalizeOpenAIImageSizeTier(imageSize)
	return OpenAIResponsesImageBillingConfig{
		Model:     imageModel,
		SizeTier:  sizeTier,
		InputSize: imageSize,
	}, nil
}

func resolveOpenAIResponsesImageBillingConfigFromBody(body []byte, fallbackModel string) (string, string, error) {
	cfg, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, fallbackModel)
	if err != nil {
		return "", "", err
	}
	return cfg.Model, cfg.SizeTier, nil
}

func resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body []byte, fallbackModel string, memories ...*wsMaterializer) (OpenAIResponsesImageBillingConfig, error) {
	mem := firstWSMaterializer(memories)
	imageModel := ""
	imageSize := ""
	hasImageTool := false
	if len(body) > 0 && gjson.ValidBytes(body) {
		tools := mem.get(body, "tools")
		if tools.IsArray() {
			mem.forEachArray(tools, func(_, item gjson.Result) bool {
				if openAIJSONString(mem.resultGet(item, "type")) != "image_generation" {
					return true
				}
				hasImageTool = true
				imageModel = openAIJSONString(mem.resultGet(item, "model"))
				imageSize = openAIJSONString(mem.resultGet(item, "size"))
				return false
			})
		}
		if imageSize == "" {
			imageSize = openAIJSONString(mem.get(body, "size"))
		}
		if imageModel == "" {
			bodyModel := openAIJSONString(mem.get(body, "model"))
			if isOpenAIImageBillingModelAlias(bodyModel) || !hasImageTool {
				imageModel = bodyModel
			}
		}
	}
	if imageModel == "" && hasImageTool {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = strings.TrimSpace(fallbackModel)
	}
	return OpenAIResponsesImageBillingConfig{
		Model:     imageModel,
		SizeTier:  normalizeOpenAIImageSizeTier(imageSize),
		InputSize: imageSize,
	}, nil
}

func isOpenAIImageBillingModelAlias(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return false
	}
	return isOpenAIImageGenerationModel(normalized) || strings.Contains(normalized, "image")
}

func openAIJSONString(value gjson.Result) string {
	if value.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func IsWSExplicitImageGenerationIntent(ctx context.Context, endpoint, model string, body []byte) (bool, error) {
	mem := newWSMaterializer(ctx)
	defer mem.close()
	intent := openAIImageGenerationIntent(endpoint, model, body, true, true, mem)
	if mem != nil && mem.err != nil {
		return false, mem.err
	}
	return intent, nil
}
