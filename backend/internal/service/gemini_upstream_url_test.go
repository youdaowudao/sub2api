package service

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildGeminiAIStudioModelActionURL(t *testing.T) {
	const base = "https://generativelanguage.googleapis.com"

	got, err := buildGeminiAIStudioModelActionURL(base, "gemini-2.5-pro", "generateContent", false)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/gemini-2.5-pro:generateContent", got)

	got, err = buildGeminiAIStudioModelActionURL(base+"/", " gemini-2.5-flash ", "streamGenerateContent", true)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", got)

	got, err = buildGeminiAIStudioModelActionURL(base, "gemini-2.5-pro", "countTokens", false)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/gemini-2.5-pro:countTokens", got)
}

// TestBuildGeminiAIStudioModelActionURLRejectsNonConformingModel 锁定不变式：
// 模型名来自客户端（native 路由的 URL 片段 / compat 路由的请求体），
// 只有合规的路径片段才允许拼进上游 URL。
func TestBuildGeminiAIStudioModelActionURLRejectsNonConformingModel(t *testing.T) {
	const base = "https://generativelanguage.googleapis.com"

	for _, model := range []string{
		"../../x/y",
		"..",
		".",
		"gemini-2.5-pro/../../x",
		`..\..\x`,
		"gemini-2.5-pro?a=b",
		"gemini-2.5-pro#frag",
		"gemini-2.5-pro%2f..",
		"gemini 2.5 pro",
		"gemini\x00pro",
		"gemini-2.5-pro@001",
		"gemini~pro",
		"models/gemini-2.5-pro",
		"...",
		"",
		"   ",
	} {
		t.Run("model_"+model, func(t *testing.T) {
			_, err := buildGeminiAIStudioModelActionURL(base, model, "generateContent", false)
			require.Error(t, err, "model %q must be rejected", model)
			require.False(t, IsSafeGeminiModelPathSegment(model))
		})
	}

	// action 只允许已知取值，避免未来把可变字符串拼进 path。
	_, err := buildGeminiAIStudioModelActionURL(base, "gemini-2.5-pro", "deleteModel", false)
	require.Error(t, err)

	_, err = buildGeminiAIStudioModelActionURL("", "gemini-2.5-pro", "generateContent", false)
	require.Error(t, err)
}

// TestBuildGeminiAIStudioModelActionURLEncodesProviderAlias 锁定：账号模型映射里
// 配置的供应商别名（含 ASCII 方括号与中文）百分号编码后拼进上游 URL，Go HTTP 客户端
// 实际发出的路径与编码结果一致；入站校验仍拒绝同一个名字。
func TestBuildGeminiAIStudioModelActionURLEncodesProviderAlias(t *testing.T) {
	const base = "https://upstream.example.com"
	const alias = "[官逆]gemini-3.1-flash-lite"
	const encodedPath = "/v1beta/models/%5B%E5%AE%98%E9%80%86%5Dgemini-3.1-flash-lite"

	got, err := buildGeminiAIStudioModelActionURL(base, alias, "generateContent", false)
	require.NoError(t, err)
	require.Equal(t, base+encodedPath+":generateContent", got)

	got, err = buildGeminiAIStudioModelActionURL(base+"/", " "+alias+" ", "streamGenerateContent", true)
	require.NoError(t, err)
	require.Equal(t, base+encodedPath+":streamGenerateContent?alt=sse", got)

	req, err := http.NewRequest(http.MethodPost, got, nil)
	require.NoError(t, err)
	require.Equal(t, "upstream.example.com", req.URL.Host)
	require.Equal(t, "/v1beta/models/"+alias+":streamGenerateContent", req.URL.Path)
	require.Equal(t, encodedPath+":streamGenerateContent?alt=sse", req.URL.RequestURI())

	got, err = buildGeminiAIStudioModelActionURL(base, "[preview]gemini-2.5-pro", "countTokens", false)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/%5Bpreview%5Dgemini-2.5-pro:countTokens", got)

	got, err = buildGeminiAIStudioModelActionURL(base, "模型", "generateContent", false)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/%E6%A8%A1%E5%9E%8B:generateContent", got)

	// 入站校验不随之放宽：客户端仍只能在 URL 里书写合规片段。
	require.False(t, IsSafeGeminiModelPathSegment(alias))
	require.False(t, IsSafeGeminiModelPathSegment("模型"))
}

// TestBuildGeminiAIStudioModelActionURLRejectsUnsafeProviderAlias 锁定：额外放行的
// 只有 '[' ']' 与汉字，带上它们也绕不过路径结构字符、相似符号、不可见字母与
// 其他文字的拒绝。
func TestBuildGeminiAIStudioModelActionURLRejectsUnsafeProviderAlias(t *testing.T) {
	const base = "https://upstream.example.com"

	longAlias := "[官逆]"
	for len(longAlias) <= maxUpstreamPathSegmentLen {
		longAlias += "a"
	}

	for i, model := range []string{
		"[官逆]../x",
		"[官逆]gemini/x",
		`[官逆]gemini\x`,
		"[官逆]gemini?a=b",
		"[官逆]gemini#frag",
		"[官逆]gemini%2f..",
		"[官逆]gemini%5D",
		"[官逆] gemini",
		"[官逆]\tgemini",
		"[官逆]gemini:streamGenerateContent",
		"[官逆]gemini@001",
		"[官逆]gemini~pro",
		"[官逆]gemini;x",
		"[官逆]gemini\x00",
		"[官逆]gemini\nX-Injected: 1",
		"[官逆]gemini\u202e", // RIGHT-TO-LEFT OVERRIDE
		"[官逆]gemini\u200b", // ZERO WIDTH SPACE
		"[官逆]\u3000gemini", // IDEOGRAPHIC SPACE（首尾的会被 TrimSpace 裁掉，放在中间）
		"模型／x",             // FULLWIDTH SOLIDUS
		"模型∕x",             // DIVISION SLASH
		"模型＼x",             // FULLWIDTH REVERSE SOLIDUS
		"模型．．",             // FULLWIDTH FULL STOP
		"模型。x",             // IDEOGRAPHIC FULL STOP
		"【官逆】gemini",       // 全角方括号是标点，不在放行范围内
		"（官逆）gemini",
		"gemini\u3164-pro",                 // HANGUL FILLER（Lo 类别但不可见）
		"gemini\u115f-pro",                 // HANGUL CHOSEONG FILLER
		"gemini\u1160-pro",                 // HANGUL JUNGSEONG FILLER
		"gemini\uffa0-pro",                 // HALFWIDTH HANGUL FILLER
		"[官逆]\uff21gemini",                 // FULLWIDTH LATIN CAPITAL LETTER A
		"[\u3042]gemini",                   // HIRAGANA LETTER A
		"[\uac00]gemini",                   // HANGUL SYLLABLE GA
		"[\u0639\u0631\u0628\u064a]gemini", // ARABIC
		"[\u0939\u093f]gemini",             // DEVANAGARI（含组合元音符号）
		"[官\u0301]gemini",                  // COMBINING ACUTE ACCENT
		"[官逆]gemini\xff",
		"\xe5\xae",
		"🙂gemini",
		longAlias,
	} {
		// 用例含 '/'、控制字符与非法 UTF-8，子测试名只用序号，原值见失败信息。
		t.Run(fmt.Sprintf("case_%02d", i), func(t *testing.T) {
			_, err := buildGeminiAIStudioModelActionURL(base, model, "generateContent", false)
			require.Error(t, err, "model %q must be rejected", model)
			require.NotContains(t, err.Error(), model, "error must not echo raw input")
			require.False(t, IsSafeGeminiModelPathSegment(model))
		})
	}
}
