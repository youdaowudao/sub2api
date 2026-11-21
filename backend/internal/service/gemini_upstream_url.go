package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// errInvalidGeminiUpstreamModel 与 validateUpstreamPathSegment 的报错文案保持一致，
// 同样不回显原始输入。
var errInvalidGeminiUpstreamModel = errors.New("invalid gemini model for upstream url path")

// geminiAIStudioActions 是允许出现在上游 URL 里的 action 集合，与 ForwardNative
// 的入站校验保持一致，同时避免 compat 侧把可变字符串直接拼进 path。
var geminiAIStudioActions = map[string]struct{}{
	"generateContent":       {},
	"streamGenerateContent": {},
	"countTokens":           {},
}

// buildGeminiAIStudioModelActionURL 组装 AI Studio 的
// /v1beta/models/{model}:{action} 上游 URL。
//
// model 是客户端可控的（native 路由取自 URL 片段，compat 路由取自请求体的 model
// 字段，之后可能再经渠道映射），因此必须先过路径片段护栏才能拼进 path，
// 见 upstream_path_guard.go 与 encodeGeminiUpstreamModelPathSegment。
// 新增 AI Studio 端点请一律走本函数。
func buildGeminiAIStudioModelActionURL(baseURL, model, action string, stream bool) (string, error) {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBase == "" {
		return "", errors.New("gemini base url is required")
	}
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return "", errors.New("gemini model is required")
	}
	modelSegment, err := encodeGeminiUpstreamModelPathSegment(trimmedModel)
	if err != nil {
		return "", err
	}
	trimmedAction := strings.TrimSpace(action)
	if _, ok := geminiAIStudioActions[trimmedAction]; !ok {
		return "", fmt.Errorf("unsupported gemini action: %s", trimmedAction)
	}

	fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", trimmedBase, modelSegment, trimmedAction)
	if stream {
		fullURL += "?alt=sse"
	}
	return fullURL, nil
}

// encodeGeminiUpstreamModelPathSegment 把模型名转成可拼进上游 URL 的单个 path 片段。
//
// 合规片段（isSafeUpstreamPathSegment）原样返回，与收紧后的既有行为逐字节一致。
//
// 此外额外放行一个闭集：ASCII 的 '[' ']' 与汉字（unicode.Han 中属于字母的码点），
// 并对其做百分号编码后再拼接。用途是账号模型映射里配置的供应商别名
// （如 "[官逆]gemini-3.1-flash-lite"）；compat 路由在账号未配映射时会直接使用
// 请求体里的 model，同类名字也按同一规则编码转发。编码是无损序列化，上游解码后
// 得到与原名完全相同的字符串，不属于 upstream_path_guard.go 所说的"改写"，
// 且放行的字符编码后都不会改变上游路径结构。
//
// 路径结构字符（/ \ ? # % : 等）、空白、控制字符、非法 UTF-8、只由点组成的片段，
// 以及汉字以外的一切非 ASCII 字符（全角斜杠/句点、【】、表情、零宽字符、
// Hangul filler 这类不可见字母、其他文字等）仍一律拒绝。只放行汉字是因为它们
// 在 NFC/NFD/NFKC/NFKD 下都不会规范化出路径符号、空白或不可见字符，而全角
// 斜杠/句点这类符号可能被上游规范化成真正的 '/' 或 '.'。
//
// 入站校验 IsSafeGeminiModelPathSegment 不受影响，native 路由的客户端仍只能在
// URL 里书写合规片段。
func encodeGeminiUpstreamModelPathSegment(model string) (string, error) {
	if isSafeUpstreamPathSegment(model) {
		return model, nil
	}
	if !isEncodableGeminiUpstreamModel(model) {
		return "", errInvalidGeminiUpstreamModel
	}
	return url.PathEscape(model), nil
}

// isEncodableGeminiUpstreamModel 是 encodeGeminiUpstreamModelPathSegment 的闭集
// 允许清单（默认拒绝）：合规字节 + '[' ']' + 汉字。
func isEncodableGeminiUpstreamModel(model string) bool {
	if model == "" || len(model) > maxUpstreamPathSegmentLen || !utf8.ValidString(model) {
		return false
	}
	dotsOnly := true
	for _, r := range model {
		switch {
		case r < utf8.RuneSelf:
			if r != '[' && r != ']' && !isSafeUpstreamPathSegmentByte(byte(r)) {
				return false
			}
		case !unicode.Is(unicode.Han, r) || !unicode.IsLetter(r):
			return false
		}
		if r != '.' {
			dotsOnly = false
		}
	}
	return !dotsOnly
}

// IsSafeGeminiModelPathSegment 供 handler 层在解析出 URL 里的模型名后立刻校验，
// 让客户端拿到明确的 400，而不是等到构造上游请求时才报错。
func IsSafeGeminiModelPathSegment(model string) bool {
	return isSafeUpstreamPathSegment(strings.TrimSpace(model))
}
