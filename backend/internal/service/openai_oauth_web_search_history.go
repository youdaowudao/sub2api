package service

import (
	"bytes"
	"fmt"
	"strings"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/tidwall/gjson"
)

// The ChatGPT internal Codex endpoint rejects a request whose input replays a
// hosted web_search_call item unless the request also declares the web_search
// tool; the stream then fails with "response protection is unavailable".
// Codex's local context compaction sends the full history with tools:[], so
// every compaction after a web search fails (#7927).
//
// Declare a cached-only web_search tool for such requests. Responses Lite
// rejects hosted tools at the top level, so Lite requests carry it in an
// input additional_tools item instead. When the caller declared no tools at
// all, also pin tool_choice to "none" so the injected tool cannot be invoked
// and the request keeps its no-tools semantics.

const (
	openAIWebSearchCallItemType      = "web_search_call"
	openAIAdditionalToolsItemType    = "additional_tools"
	openAICompactionTriggerItemType  = "compaction_trigger"
	openAIAdditionalToolsDefaultRole = "developer"
)

var openAIWebSearchHistoryTool = map[string]any{
	"type":                "web_search",
	"external_web_access": false,
}

func isOpenAIWebSearchToolType(toolType string) bool {
	return strings.HasPrefix(strings.TrimSpace(toolType), "web_search")
}

func openAIToolsContainWebSearch(rawTools any) bool {
	tools, ok := rawTools.([]any)
	if !ok {
		return false
	}
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if ok && isOpenAIWebSearchToolType(firstNonEmptyString(tool["type"])) {
			return true
		}
	}
	return false
}

// shouldPinOpenAIWebSearchHistoryToolChoice reports whether tool_choice may be
// forced to "none": only when the caller offered no tools, so the choice
// cannot meaningfully be anything else.
func shouldPinOpenAIWebSearchHistoryToolChoice(choice any) bool {
	switch typed := choice.(type) {
	case nil:
		return true
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "" || normalized == "auto" || normalized == "none"
	default:
		return false
	}
}

// openAIAdditionalToolsInsertIndex keeps a trailing compaction trigger last,
// as required by the remote compaction v2 wire format.
func openAIAdditionalToolsInsertIndex(itemTypes []string) int {
	if n := len(itemTypes); n > 0 && itemTypes[n-1] == openAICompactionTriggerItemType {
		return n - 1
	}
	return len(itemTypes)
}

// ensureOpenAIOAuthWebSearchToolForHistory is the map variant used by the
// Codex OAuth transform.
func ensureOpenAIOAuthWebSearchToolForHistory(reqBody map[string]any, responsesLite bool) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	hasWebSearchCall := false
	callerDeclaredTools := false
	additionalToolsIndex := -1
	itemTypes := make([]string, len(input))
	for i, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemTypes[i] = strings.TrimSpace(firstNonEmptyString(item["type"]))
		switch itemTypes[i] {
		case openAIWebSearchCallItemType:
			hasWebSearchCall = true
		case openAIAdditionalToolsItemType:
			if openAIToolsContainWebSearch(item["tools"]) {
				return false
			}
			if tools, _ := item["tools"].([]any); len(tools) > 0 {
				callerDeclaredTools = true
			}
			if additionalToolsIndex < 0 {
				additionalToolsIndex = i
			}
		}
	}
	if !hasWebSearchCall || openAIToolsContainWebSearch(reqBody["tools"]) {
		return false
	}
	tools, _ := reqBody["tools"].([]any)
	callerDeclaredTools = callerDeclaredTools || len(tools) > 0

	switch {
	case !responsesLite:
		reqBody["tools"] = append(tools, cloneOpenAIWebSearchHistoryTool())
	case additionalToolsIndex >= 0:
		// additionalToolsIndex is only recorded for map items.
		item, _ := input[additionalToolsIndex].(map[string]any)
		existing, _ := item["tools"].([]any)
		item["tools"] = append(existing, cloneOpenAIWebSearchHistoryTool())
	default:
		at := openAIAdditionalToolsInsertIndex(itemTypes)
		additional := map[string]any{
			"type":  openAIAdditionalToolsItemType,
			"role":  openAIAdditionalToolsDefaultRole,
			"tools": []any{cloneOpenAIWebSearchHistoryTool()},
		}
		next := make([]any, 0, len(input)+1)
		next = append(next, input[:at]...)
		next = append(next, additional)
		next = append(next, input[at:]...)
		reqBody["input"] = next
	}
	if !callerDeclaredTools {
		if choice, exists := reqBody["tool_choice"]; !exists || shouldPinOpenAIWebSearchHistoryToolChoice(choice) {
			reqBody["tool_choice"] = "none"
		}
	}
	return true
}

// ensureOpenAIOAuthWebSearchToolForHistoryBody is the raw-body variant used by
// the passthrough and WebSocket paths; it avoids decoding the whole body.
func ensureOpenAIOAuthWebSearchToolForHistoryBody(body []byte, responsesLite bool, memories ...*wsMaterializer) ([]byte, bool, error) {
	wsMem := firstWSMaterializer(memories)

	if len(body) == 0 || !bytes.Contains(body, []byte(openAIWebSearchCallItemType)) {
		return body, false, nil
	}
	input := wsMem.get(body, "input")
	if !input.IsArray() {
		return body, false, nil
	}
	items := wsMem.array(input)
	hasWebSearchCall := false
	callerDeclaredTools := false
	additionalToolsIndex := -1
	itemTypes := make([]string, len(items))
	for i, item := range items {
		itemTypes[i] = strings.TrimSpace(wsMem.resultGet(item, "type").String())
		switch itemTypes[i] {
		case openAIWebSearchCallItemType:
			hasWebSearchCall = true
		case openAIAdditionalToolsItemType:
			itemTools := wsMem.resultGet(item, "tools")
			if gjsonToolsContainWebSearch(itemTools, wsMem) {
				return body, false, nil
			}
			if itemTools.IsArray() && len(wsMem.array(itemTools)) > 0 {
				callerDeclaredTools = true
			}
			if additionalToolsIndex < 0 {
				additionalToolsIndex = i
			}
		}
	}
	if !hasWebSearchCall {
		return body, false, nil
	}
	tools := wsMem.get(body, "tools")
	if gjsonToolsContainWebSearch(tools, wsMem) {
		return body, false, nil
	}
	topLevelTools := tools.IsArray() && len(wsMem.array(tools)) > 0
	callerDeclaredTools = callerDeclaredTools || topLevelTools

	var (
		next []byte
		err  error
	)
	switch {
	case !responsesLite && topLevelTools:
		next, err = wsMem.set(body, "tools.-1", cloneOpenAIWebSearchHistoryTool())
	case !responsesLite:
		next, err = wsMem.set(body, "tools", []any{cloneOpenAIWebSearchHistoryTool()})
	case additionalToolsIndex >= 0 && items[additionalToolsIndex].Get("tools").IsArray():
		next, err = wsMem.set(body, fmt.Sprintf("input.%d.tools.-1", additionalToolsIndex), cloneOpenAIWebSearchHistoryTool())
	case additionalToolsIndex >= 0:
		next, err = wsMem.set(body, fmt.Sprintf("input.%d.tools", additionalToolsIndex), []any{cloneOpenAIWebSearchHistoryTool()})
	default:
		next, err = insertOpenAIAdditionalToolsItemRaw(body, items, openAIAdditionalToolsInsertIndex(itemTypes), wsMem)
	}
	if err != nil {
		return body, false, fmt.Errorf("declare web_search tool for web_search_call history: %w", err)
	}
	if !callerDeclaredTools {
		choice := wsMem.get(next, "tool_choice")
		if !choice.Exists() || choice.Type == gjson.Null || (choice.Type == gjson.String && shouldPinOpenAIWebSearchHistoryToolChoice(choice.String())) {
			next, err = wsMem.set(next, "tool_choice", "none")
			if err != nil {
				return body, false, fmt.Errorf("pin tool_choice for web_search_call history: %w", err)
			}
		}
	}
	return next, true, nil
}

func insertOpenAIAdditionalToolsItemRaw(body []byte, items []gjson.Result, at int, memories ...*wsMaterializer) ([]byte, error) {
	wsMem := firstWSMaterializer(memories)

	additional, err := wsMem.encode(map[string]any{
		"type":  openAIAdditionalToolsItemType,
		"role":  openAIAdditionalToolsDefaultRole,
		"tools": []any{cloneOpenAIWebSearchHistoryTool()},
	})
	if err != nil {
		return nil, err
	}
	rawItems := make([][]byte, 0, len(items)+1)
	for i, item := range items {
		if i == at {
			rawItems = append(rawItems, additional)
		}
		rawItems = append(rawItems, wsMem.rawView(item.Raw))
	}
	if at >= len(items) {
		rawItems = append(rawItems, additional)
	}
	var raw []byte
	if wsMem == nil {
		raw = append([]byte{'['}, bytes.Join(rawItems, []byte{','})...)
		raw = append(raw, ']')
	} else {
		n := int64(2 + max(0, len(rawItems)-1))
		for _, item := range rawItems {
			n, err = wsMemoryAdd(n, int64(len(item)))
			if err != nil {
				return nil, err
			}
		}
		owner, reserveErr := pkghttputil.ReserveWSBodyMemoryOwner(wsMem.ctx, n)
		if reserveErr != nil {
			return nil, reserveErr
		}
		defer owner.Drop()
		raw = make([]byte, 0, int(n))
		raw = append(raw, '[')
		for i, item := range rawItems {
			if i != 0 {
				raw = append(raw, ',')
			}
			raw = append(raw, item...)
		}
		raw = append(raw, ']')
	}
	return wsMem.setRaw(body, "input", raw)
}

func gjsonToolsContainWebSearch(tools gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	if !tools.IsArray() {
		return false
	}
	for _, tool := range mem.array(tools) {
		if isOpenAIWebSearchToolType(mem.resultGet(tool, "type").String()) {
			return true
		}
	}
	return false
}

func cloneOpenAIWebSearchHistoryTool() map[string]any {
	tool := make(map[string]any, len(openAIWebSearchHistoryTool))
	for key, value := range openAIWebSearchHistoryTool {
		tool[key] = value
	}
	return tool
}
