package service

import (
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/tidwall/gjson"
)

// replaceOpenAIRawInput copies unchanged image strings only into the final
// request, without allocating a second complete input array first.
func replaceOpenAIRawInput(body []byte, input gjson.Result, items []string, memories ...*wsMaterializer) []byte {
	wsMem := firstWSMaterializer(memories)

	size := len(body) - len(input.Raw) + 2
	for index, item := range items {
		size += len(item)
		if index > 0 {
			size++
		}
	}
	var owner *pkghttputil.WSBodyMemoryOwner
	if wsMem != nil {
		if wsMem.err != nil {
			return body
		}
		var err error
		owner, err = pkghttputil.ReserveWSBodyMemoryOwner(wsMem.ctx, int64(size))
		if err != nil {
			wsMem.err = err
			return body
		}
	}
	result := make([]byte, 0, size)
	result = append(result, body[:input.Index]...)
	result = append(result, '[')
	for index, item := range items {
		if index > 0 {
			result = append(result, ',')
		}
		result = append(result, item...)
	}
	result = append(result, ']')
	result = append(result, body[input.Index+len(input.Raw):]...)
	result, _ = wsMem.track(result, owner, nil)
	return result
}

// The standard decoder keeps the last duplicate key; GJSON selects the first.
func hasDuplicateJSONObjectKeys(object gjson.Result, memories ...*wsMaterializer) bool {
	mem := firstWSMaterializer(memories)
	seen := make(map[string]struct{})
	duplicate := false
	mem.forEachArray(object, func(key, _ gjson.Result) bool {
		if _, exists := seen[key.Str]; exists {
			duplicate = true
			return false
		}
		seen[key.Str] = struct{}{}
		return true
	})
	return duplicate
}
