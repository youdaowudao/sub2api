package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The account mapping changes the model, not the upstream capability header.
// Apply at the final HTTP request boundary, after account/model resolution.
// Never mutate the ingress headers/body: failover may select a native account.
func applyMappedGPT55LiteCompatibility(req *http.Request, account *Account, body []byte, memories ...*wsMaterializer) error {
	wsMem := firstWSMaterializer(memories)
	if req == nil || account == nil || !account.IsOpenAIOAuthLike() {
		return nil
	}
	if strings.TrimSpace(wsMem.get(body, "model").String()) != "gpt-5.5" {
		return nil
	}
	if !isOpenAIResponsesLiteHeader(req.Header.Get(responsesLiteHeader)) && !isOpenAIResponsesLiteWebSocketPayload(body, wsMem) {
		return nil
	}
	// The Codex non-Lite endpoint accepts additional_tools, namespaces and
	// reasoning.context=all_turns. Preserve them and all history/tool results.
	if isOpenAIResponsesLiteWebSocketPayload(body, wsMem) {
		var err error
		body, err = wsMem.delete(body, "client_metadata."+responsesLiteWSMetadataKey)
		if err != nil {
			return fmt.Errorf("remove mapped GPT-5.5 Lite metadata: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		savedBody := body
		if wsMem == nil {
			savedBody = append([]byte(nil), body...)
		}
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(savedBody)), nil }
	}
	req.Header.Del(responsesLiteHeader)
	return nil
}
