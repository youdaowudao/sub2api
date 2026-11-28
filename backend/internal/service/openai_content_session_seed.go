package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// contentSessionSeedPrefix prevents collisions between content-derived seeds
// and explicit session IDs (e.g. "sess-xxx" or "compat_cc_xxx").
const contentSessionSeedPrefix = "compat_cs_"

// contentStablePrefixSessionSeedPrefix distinguishes cache identities derived
// only from request fields that remain stable across independent prompts.
const contentStablePrefixSessionSeedPrefix = "compat_csp_"

// deriveOpenAIContentSessionSeed builds a stable session seed from an
// OpenAI-format request body. Only fields constant across conversation turns
// are included: model, tools/functions definitions, the leading system/developer
// prompt prefix, instructions (Responses API), and the first user message.
// Supports both Chat Completions (messages) and Responses API (input).
func deriveOpenAIContentSessionSeed(body []byte, memories ...*wsMaterializer) string {
	mem := firstWSMaterializer(memories)
	if len(body) == 0 {
		return ""
	}

	const (
		modelField = iota
		toolsField
		functionsField
		instructionsField
		messagesField
		inputField
		contentSessionSeedFieldCount
		allContentSessionSeedFields = 1<<contentSessionSeedFieldCount - 1
	)
	var fields [contentSessionSeedFieldCount]gjson.Result
	var seen uint8
	// Match gjson.GetBytes by starting at the first root container, even when
	// malformed input has a non-JSON prefix.
	root := body
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			root = body[i:]
			goto scanRoot
		case '[':
			return ""
		}
	}
	return ""

scanRoot:
	if mem != nil {
		for i, path := range []string{"model", "tools", "functions", "instructions", "messages", "input"} {
			fields[i] = mem.get(root, path)
		}
	} else {
		nextKeyOffset := 1
		parseRawJSONView(root).ForEach(func(key, value gjson.Result) bool {
			if key.Index < nextKeyOffset || key.Index > len(root) {
				return false
			}
			// Result.ForEach can continue after the root '}' on malformed input.
			// The separator range excludes braces inside the preceding parsed value.
			if bytes.IndexByte(root[nextKeyOffset:key.Index], '}') >= 0 {
				return false
			}
			nextKeyOffset = value.Index + len(value.Raw)

			field := -1
			switch key.Str {
			case "model":
				field = modelField
			case "tools":
				field = toolsField
			case "functions":
				field = functionsField
			case "instructions":
				field = instructionsField
			case "messages":
				field = messagesField
			case "input":
				field = inputField
			}
			if field < 0 {
				return true
			}
			mask := uint8(1 << field)
			if seen&mask == 0 {
				fields[field] = value
				seen |= mask
			}
			return seen != allContentSessionSeedFields
		})
	}

	var b strings.Builder
	var parts []string
	write := func(value string) (int, error) {
		if mem == nil {
			return b.WriteString(value)
		}
		parts = append(parts, value)
		return len(value), nil
	}

	if model := fields[modelField].String(); model != "" {
		_, _ = write("model=")
		_, _ = write(model)
	}

	if tools := fields[toolsField]; tools.Exists() && tools.IsArray() && tools.Raw != "[]" {
		_, _ = write("|tools=")
		_, _ = write(normalizeCompatSeedJSON(json.RawMessage(mem.rawView(tools.Raw)), mem))
	}

	if funcs := fields[functionsField]; funcs.Exists() && funcs.IsArray() && funcs.Raw != "[]" {
		_, _ = write("|functions=")
		_, _ = write(normalizeCompatSeedJSON(json.RawMessage(mem.rawView(funcs.Raw)), mem))
	}

	if instr := fields[instructionsField].String(); instr != "" {
		_, _ = write("|instructions=")
		_, _ = write(instr)
	}

	firstUserCaptured := false

	msgs := fields[messagesField]
	if msgs.Exists() && msgs.IsArray() {
		systemPrefixOpen := true
		mem.forEachArray(msgs, func(_, msg gjson.Result) bool {
			role := mem.resultGet(msg, "role").String()
			switch role {
			case "system", "developer":
				if systemPrefixOpen {
					_, _ = write("|system=")
					if c := mem.resultGet(msg, "content"); c.Exists() {
						_, _ = write(normalizeCompatSeedJSON(json.RawMessage(mem.rawView(c.Raw)), mem))
					}
				}
			case "user":
				systemPrefixOpen = false
				if !firstUserCaptured {
					_, _ = write("|first_user=")
					if c := mem.resultGet(msg, "content"); c.Exists() {
						_, _ = write(normalizeCompatSeedJSON(json.RawMessage(mem.rawView(c.Raw)), mem))
					}
					firstUserCaptured = true
				}
			default:
				systemPrefixOpen = false
			}
			return true
		})
	} else if inp := fields[inputField]; inp.Exists() {
		if inp.Type == gjson.String {
			_, _ = write("|input=")
			_, _ = write(inp.String())
		} else if inp.IsArray() {
			systemPrefixOpen := true
			mem.forEachArray(inp, func(_, item gjson.Result) bool {
				role := mem.resultGet(item, "role").String()
				switch role {
				case "system", "developer":
					if systemPrefixOpen {
						_, _ = write("|system=")
						if c := mem.resultGet(item, "content"); c.Exists() {
							_, _ = write(normalizeCompatSeedJSON(json.RawMessage(mem.rawView(c.Raw)), mem))
						}
					}
				case "user":
					systemPrefixOpen = false
					if !firstUserCaptured {
						_, _ = write("|first_user=")
						if c := mem.resultGet(item, "content"); c.Exists() {
							_, _ = write(normalizeCompatSeedJSON(json.RawMessage(mem.rawView(c.Raw)), mem))
						}
						firstUserCaptured = true
					}
				default:
					// Match Chat: reminders appended after conversation/tool items
					// must not change the identity of an existing session.
					systemPrefixOpen = false
				}
				if !firstUserCaptured && mem.resultGet(item, "type").String() == "input_text" {
					_, _ = write("|first_user=")
					if text := mem.resultGet(item, "text").String(); text != "" {
						_, _ = write(text)
					}
					firstUserCaptured = true
				}
				return true
			})
		}
	}

	if b.Len() == 0 && len(parts) == 0 {
		return ""
	}
	if mem != nil {
		return mem.join(append([]string{contentSessionSeedPrefix}, parts...), "")
	}
	return contentSessionSeedPrefix + b.String()
}

// deriveOpenAIAnchoredContentSessionSeed returns the legacy content-derived
// seed only when it contains a meaningful user/input anchor. This preserves
// the existing session derivation while preventing model-only requests from
// becoming a tenant-wide cache routing identity.
func deriveOpenAIAnchoredContentSessionSeed(body []byte, memories ...*wsMaterializer) string {
	mem := firstWSMaterializer(memories)
	if !hasOpenAIContentSessionUserAnchor(body, mem) {
		return ""
	}
	return deriveOpenAIContentSessionSeed(body, mem)
}

func hasOpenAIContentSessionUserAnchor(body []byte, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if len(body) == 0 {
		return false
	}

	if messages := mem.get(body, "messages"); messages.Exists() && messages.IsArray() {
		anchored := false
		mem.forEachArray(messages, func(_, message gjson.Result) bool {
			if strings.TrimSpace(mem.resultGet(message, "role").String()) != "user" {
				return true
			}
			anchored = hasMeaningfulOpenAIContent(mem.resultGet(message, "content"), mem)
			return false
		})
		return anchored
	}

	input := mem.get(body, "input")
	if !input.Exists() {
		return false
	}
	if input.Type == gjson.String {
		return strings.TrimSpace(input.String()) != ""
	}
	if !input.IsArray() {
		return false
	}

	anchored := false
	mem.forEachArray(input, func(_, item gjson.Result) bool {
		if strings.TrimSpace(mem.resultGet(item, "role").String()) == "user" {
			anchored = hasMeaningfulOpenAIContent(mem.resultGet(item, "content"), mem)
			return false
		}
		if strings.TrimSpace(mem.resultGet(item, "type").String()) == "input_text" {
			anchored = strings.TrimSpace(mem.resultGet(item, "text").String()) != ""
			return false
		}
		return true
	})
	return anchored
}

func hasMeaningfulOpenAIContent(content gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !content.Exists() || content.Type == gjson.Null {
		return false
	}
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String()) != ""
	}
	if !content.IsArray() {
		normalized, ok := normalizeNonEmptyCompatSeedJSON(content, mem)
		return ok && strings.TrimSpace(normalized) != ""
	}

	meaningful := false
	mem.forEachArray(content, func(_, item gjson.Result) bool {
		if item.Type == gjson.String {
			meaningful = strings.TrimSpace(item.String()) != ""
		} else if text := mem.resultGet(item, "text"); text.Exists() {
			meaningful = strings.TrimSpace(text.String()) != ""
		} else {
			_, meaningful = normalizeNonEmptyCompatSeedJSON(item, mem)
		}
		return !meaningful
	})
	return meaningful
}

// deriveOpenAIStablePrefixSessionSeed builds a seed from the reusable prefix
// of an OpenAI-format request. User and assistant content are deliberately
// excluded so independent prompts with the same system/tool prefix can share
// an upstream prompt-cache routing identity.
//
// An empty result means the request has no meaningful stable prefix. Callers
// must then use a narrower fallback instead of grouping all requests by tenant
// and model alone.
func deriveOpenAIStablePrefixSessionSeed(body []byte, memories ...*wsMaterializer) string {
	mem := firstWSMaterializer(memories)
	if len(body) == 0 {
		return ""
	}

	var b strings.Builder
	var parts []string
	write := func(value string) (int, error) {
		if mem == nil {
			return b.WriteString(value)
		}
		parts = append(parts, value)
		return len(value), nil
	}
	hasStablePrefix := false
	appendJSON := func(label string, value gjson.Result) {
		normalized, ok := normalizeNonEmptyCompatSeedJSON(value, mem)
		if !ok {
			return
		}
		_, _ = write("|")
		_, _ = write(label)
		_, _ = write("=")
		_, _ = write(normalized)
		hasStablePrefix = true
	}

	if tools := mem.get(body, "tools"); tools.Exists() && tools.IsArray() {
		appendJSON("tools", tools)
	}
	if funcs := mem.get(body, "functions"); funcs.Exists() && funcs.IsArray() {
		appendJSON("functions", funcs)
	}
	if instructions := mem.get(body, "instructions"); strings.TrimSpace(instructions.String()) != "" {
		appendJSON("instructions", instructions)
	}

	appendSystemMessages := func(items gjson.Result) {
		mem.forEachArray(items, func(_, item gjson.Result) bool {
			role := strings.TrimSpace(mem.resultGet(item, "role").String())
			switch role {
			case "system", "developer":
				appendJSON(role, mem.resultGet(item, "content"))
			}
			return true
		})
	}

	if messages := mem.get(body, "messages"); messages.Exists() && messages.IsArray() {
		appendSystemMessages(messages)
	} else if input := mem.get(body, "input"); input.Exists() && input.IsArray() {
		appendSystemMessages(input)
	}

	if !hasStablePrefix {
		return ""
	}
	if mem != nil {
		return mem.join(append([]string{contentStablePrefixSessionSeedPrefix}, parts...), "")
	}
	return contentStablePrefixSessionSeedPrefix + b.String()
}

func normalizeNonEmptyCompatSeedJSON(value gjson.Result, memories ...*wsMaterializer) (string, bool) {
	mem := firstWSMaterializer(memories)
	if !value.Exists() || value.Type == gjson.Null {
		return "", false
	}
	normalized := normalizeCompatSeedJSON(json.RawMessage(mem.rawView(value.Raw)), mem)
	switch normalized {
	case "", `""`, "[]", "{}", "null":
		return "", false
	default:
		return normalized, true
	}
}
