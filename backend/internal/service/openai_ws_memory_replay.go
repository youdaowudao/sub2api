package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unsafe"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

// Each replay view references the allocation token specified by the producer,
// including a small item pointing into a much larger original frame.
type wsMemoryReplay struct {
	items   []json.RawMessage
	owners  []*pkghttputil.WSBodyMemoryOwner
	headers *pkghttputil.WSBodyMemoryOwner
	exists  bool
}

func newWSMemoryReplay(ctx context.Context, count int, exists bool) (wsMemoryReplay, error) {
	if count < 0 || int64(count) > math.MaxInt64/int64(unsafe.Sizeof(json.RawMessage(nil))+unsafe.Sizeof((*pkghttputil.WSBodyMemoryOwner)(nil))) {
		return wsMemoryReplay{}, pkghttputil.ErrRequestBodyMemoryBudget
	}
	size := int64(count) * int64(unsafe.Sizeof(json.RawMessage(nil))+unsafe.Sizeof((*pkghttputil.WSBodyMemoryOwner)(nil)))
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, size)
	if err != nil {
		return wsMemoryReplay{}, err
	}
	return wsMemoryReplay{items: make([]json.RawMessage, 0, count), owners: make([]*pkghttputil.WSBodyMemoryOwner, 0, count), headers: owner, exists: exists}, nil
}
func (r *wsMemoryReplay) drop() {
	for _, owner := range r.owners {
		owner.Drop()
	}
	r.headers.Drop()
	*r = wsMemoryReplay{}
}
func (r wsMemoryReplay) hold() wsMemoryReplay {
	for _, owner := range r.owners {
		owner.Hold()
	}
	r.headers.Hold()
	return r
}
func (r *wsMemoryReplay) appendView(item json.RawMessage, owner *pkghttputil.WSBodyMemoryOwner) {
	r.items = append(r.items, item)
	r.owners = append(r.owners, owner.Hold())
}
func (r *wsMemoryReplay) appendReplay(source wsMemoryReplay) {
	for i, item := range source.items {
		r.appendView(item, source.owners[i])
	}
}
func combineWSMemoryReplay(ctx context.Context, a, b wsMemoryReplay) (wsMemoryReplay, error) {
	if len(a.items) == 0 {
		return b.hold(), nil
	}
	if len(b.items) == 0 {
		return a.hold(), nil
	}
	out, err := newWSMemoryReplay(ctx, len(a.items)+len(b.items), a.exists || b.exists)
	if err != nil {
		return out, err
	}
	out.appendReplay(a)
	out.appendReplay(b)
	return out, nil
}

func extractWSMemoryReplay(ctx context.Context, payload []byte, frameOwner *pkghttputil.WSBodyMemoryOwner) (wsMemoryReplay, error) {
	input, exists := openAIWSRawFieldView(payload, "input", false)
	if !exists {
		return wsMemoryReplay{}, nil
	}
	if len(input) == 0 {
		return wsMemoryReplay{}, errors.New("input is empty")
	}
	if input[0] == '[' {
		if !json.Valid(input) {
			return wsMemoryReplay{}, errors.New("input array json is invalid")
		}
		visit := func(fn func(json.RawMessage)) {
			for i := 1; ; {
				i = skipOpenAIWSJSONSpace(input, i)
				if input[i] == ']' {
					return
				}
				start := i
				if input[i] == '"' || input[i] == '{' || input[i] == '[' {
					i = skipOpenAIWSJSONValue(input, i)
				} else {
					for input[i] != ',' && input[i] != ']' {
						i++
					}
				}
				fn(json.RawMessage(bytes.TrimSpace(input[start:i])))
				i = skipOpenAIWSJSONSpace(input, i)
				if input[i] == ',' {
					i++
				}
			}
		}
		count := 0
		visit(func(json.RawMessage) { count++ })
		out, err := newWSMemoryReplay(ctx, count, true)
		if err != nil {
			return out, err
		}
		visit(func(item json.RawMessage) { out.appendView(item, frameOwner) })
		return out, nil
	}
	out, err := newWSMemoryReplay(ctx, 1, true)
	if err != nil {
		return out, err
	}
	if input[0] == '"' {
		release, err := wsJSONDecodeScopeMode(ctx, input, false)
		if err != nil {
			out.drop()
			return wsMemoryReplay{}, err
		}
		defer release()
		var text string
		if err := json.Unmarshal(input, &text); err != nil {
			out.drop()
			return wsMemoryReplay{}, err
		}
		encoded, owner, err := marshalWSJSON(ctx, text, true)
		if err != nil {
			out.drop()
			return wsMemoryReplay{}, err
		}
		out.appendView(encoded, owner)
		owner.Drop()
		return out, nil
	}
	out.appendView(input, frameOwner)
	return out, nil
}

func equalWSMemoryRawItem(ctx context.Context, a, b []byte) (bool, error) {
	if bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) {
		return true, nil
	}
	normalize := func(raw []byte) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
		release, err := wsJSONDecodeScope(ctx, raw)
		if err != nil {
			return nil, nil, err
		}
		defer release()
		var value any
		if err := decodeOpenAIJSONUseNumber(bytes.TrimSpace(raw), &value); err != nil {
			return bytes.TrimSpace(raw), nil, nil
		}
		return marshalWSJSON(ctx, value, true)
	}
	x, xOwner, err := normalize(a)
	if err != nil {
		return false, err
	}
	defer xOwner.Drop()
	y, yOwner, err := normalize(b)
	if err != nil {
		return false, err
	}
	defer yOwner.Drop()
	return bytes.Equal(x, y), nil
}
func wsMemoryReplayHasPrefix(ctx context.Context, current, prefix wsMemoryReplay) (bool, error) {
	if len(current.items) < len(prefix.items) {
		return false, nil
	}
	for i := range prefix.items {
		same, err := equalWSMemoryRawItem(ctx, current.items[i], prefix.items[i])
		if err != nil || !same {
			return same, err
		}
	}
	return true, nil
}

// Grow a collection's exact header storage before allocating and transfer its
// existing item references, leaving the item backing allocations unchanged.
func (r *wsMemoryReplay) grow(ctx context.Context) error {
	if len(r.items) < cap(r.items) {
		return nil
	}
	n := max(1, cap(r.items)*2)
	if n <= len(r.items) {
		return pkghttputil.ErrRequestBodyMemoryBudget
	}
	next, err := newWSMemoryReplay(ctx, n, true)
	if err != nil {
		return err
	}
	next.items = append(next.items, r.items...)
	next.owners = append(next.owners, r.owners...)
	r.headers.Drop()
	*r = next
	return nil
}

func wsReplayString(ctx context.Context, raw []byte, key string) (string, *pkghttputil.WSBodyMemoryOwner, error) {
	part, ok := openAIWSRawFieldViewValid(raw, key, false)
	if !ok || len(part) == 0 || part[0] != '"' {
		return "", nil, nil
	}
	release, err := wsJSONDecodeScopeMode(ctx, part, false)
	if err != nil {
		return "", nil, err
	}
	defer release()
	var value string
	if err := json.Unmarshal(part, &value); err != nil {
		return "", nil, err
	}
	// A decoded key detached from the temporary decoder gets one independent
	// string allocation with precisely its retained length.
	if key != "encrypted_content" {
		value = strings.TrimSpace(value)
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, int64(len(value)))
	if err != nil {
		return "", nil, err
	}
	return strings.Clone(value), owner, nil
}

func buildWSMemoryReplay(ctx context.Context, previous, current wsMemoryReplay, hasPrevious bool) (wsMemoryReplay, error) {
	if !hasPrevious || !previous.exists {
		return current.hold(), nil
	}
	// Use the same existing tool pairing rules, retaining only the keys needed
	// to identify outputs. The strings are separately owned for this pass.
	outputIDs := make(map[string]*pkghttputil.WSBodyMemoryOwner)
	defer func() {
		for _, o := range outputIDs {
			o.Drop()
		}
	}()
	for _, source := range []wsMemoryReplay{previous, current} {
		for _, item := range source.items {
			typ, typOwner, err := wsReplayString(ctx, item, "type")
			if err != nil {
				return wsMemoryReplay{}, err
			}
			isOutput := isCodexToolCallOutputItemType(typ)
			typOwner.Drop()
			if !isOutput {
				continue
			}
			id, idOwner, err := wsReplayString(ctx, item, "call_id")
			if err != nil {
				return wsMemoryReplay{}, err
			}
			if id == "" {
				idOwner.Drop()
				continue
			}
			if _, ok := outputIDs[id]; ok {
				idOwner.Drop()
				continue
			}
			outputIDs[id] = idOwner
		}
	}
	// Determine kept positions once, so temporary decoded metadata is not
	// repeated during the two list passes.
	var selected wsMemoryReplay
	selected, err := newWSMemoryReplay(ctx, len(previous.items), true)
	if err != nil {
		return selected, err
	}
	defer selected.drop()
	for i, item := range previous.items {
		typ, typOwner, err := wsReplayString(ctx, item, "type")
		if err != nil {
			return wsMemoryReplay{}, err
		}
		isContext := isCodexToolCallContextItemType(typ)
		typOwner.Drop()
		if isContext {
			id, idOwner, err := wsReplayString(ctx, item, "call_id")
			if err != nil {
				return wsMemoryReplay{}, err
			}
			_, paired := outputIDs[id]
			idOwner.Drop()
			if !paired {
				continue
			}
		}
		selected.appendView(item, previous.owners[i])
	}
	if !current.exists || len(current.items) == 0 {
		return selected.hold(), nil
	}
	prefix, err := wsMemoryReplayHasPrefix(ctx, current, selected)
	if err != nil {
		return wsMemoryReplay{}, err
	}
	if prefix {
		return current.hold(), nil
	}
	return combineWSMemoryReplay(ctx, selected, current)
}

// A retained upstream item is copied once and shared by the complete and
// tool-only collections. Dedupe strings are owned until the collector ends.
type wsMemoryCollector struct {
	ctx               context.Context
	all, tools        wsMemoryReplay
	allSeen, toolSeen map[string]struct{}
	keys              []*pkghttputil.WSBodyMemoryOwner
	keyHeaders        *pkghttputil.WSBodyMemoryOwner
	err               error
}

func (c *wsMemoryCollector) drop() {
	if c == nil {
		return
	}
	c.all.drop()
	c.tools.drop()
	for _, o := range c.keys {
		o.Drop()
	}
	c.keyHeaders.Drop()
	c.keys = nil
	c.keyHeaders = nil
	c.allSeen = nil
	c.toolSeen = nil
}
func (c *wsMemoryCollector) keepKey(owner *pkghttputil.WSBodyMemoryOwner) error {
	if owner == nil {
		return nil
	}
	if len(c.keys) == cap(c.keys) {
		n := max(1, cap(c.keys)*2)
		if n <= len(c.keys) {
			return pkghttputil.ErrRequestBodyMemoryBudget
		}
		headers, err := pkghttputil.ReserveWSBodyMemoryOwner(c.ctx, int64(n)*int64(unsafe.Sizeof(owner)))
		if err != nil {
			return err
		}
		next := make([]*pkghttputil.WSBodyMemoryOwner, len(c.keys), n)
		copy(next, c.keys)
		c.keyHeaders.Drop()
		c.keyHeaders = headers
		c.keys = next
	}
	c.keys = append(c.keys, owner)
	return nil
}
func (c *wsMemoryCollector) add(item json.RawMessage) error {
	if len(item) == 0 || item[0] != '{' {
		return nil
	}
	typ, typOwner, err := wsReplayString(c.ctx, item, "type")
	if err != nil {
		return err
	}
	isTool := isCodexToolCallContextItemType(typ)
	empty := typ == ""
	typOwner.Drop()
	if empty {
		return nil
	}
	key, keyOwner, err := wsReplayString(c.ctx, item, "id")
	if err != nil {
		return err
	}
	if key == "" {
		keyOwner.Drop()
		key, keyOwner, err = wsReplayString(c.ctx, item, "call_id")
		if err != nil {
			return err
		}
	}
	if key == "" {
		keyOwner.Drop()
		keyOwner = nil
		key = openAIWSPayloadStringView(item)
	}
	_, hasAll := c.allSeen[key]
	_, hasTool := c.toolSeen[key]
	if hasAll && (!isTool || hasTool) {
		keyOwner.Drop()
		return nil
	}
	// At most one distinct raw copy per event retained in both views.
	body, owner, err := pkghttputil.CloneWSBody(c.ctx, item)
	if err != nil {
		keyOwner.Drop()
		return err
	}
	defer owner.Drop()
	if keyOwner == nil {
		key = openAIWSPayloadStringView(body)
	} else if err := c.keepKey(keyOwner); err != nil {
		keyOwner.Drop()
		return err
	}
	if !hasAll {
		if err := c.all.grow(c.ctx); err != nil {
			return err
		}
		if c.allSeen == nil {
			c.allSeen = make(map[string]struct{})
		}
		c.allSeen[key] = struct{}{}
		c.all.appendView(body, owner)
	}
	if isTool && !hasTool {
		if err := c.tools.grow(c.ctx); err != nil {
			return err
		}
		if c.toolSeen == nil {
			c.toolSeen = make(map[string]struct{})
		}
		c.toolSeen[key] = struct{}{}
		c.tools.appendView(body, owner)
	}
	return nil
}
func (c *wsMemoryCollector) event(eventType string, message []byte) {
	if c.err != nil {
		return
	}
	if !json.Valid(message) {
		return
	}
	switch strings.TrimSpace(eventType) {
	case "response.output_item.done":
		item, ok := openAIWSRawFieldViewValid(message, "item", false)
		if ok {
			c.err = c.add(item)
		}
	case "response.completed", "response.done":
		response, ok := openAIWSRawFieldViewValid(message, "response", false)
		if !ok {
			return
		}
		output, ok := openAIWSRawFieldViewValid(response, "output", false)
		if !ok || len(output) == 0 || output[0] != '[' {
			return
		}
		for i := 1; ; {
			i = skipOpenAIWSJSONSpace(output, i)
			if output[i] == ']' {
				return
			}
			start := i
			if output[i] == '"' || output[i] == '{' || output[i] == '[' {
				i = skipOpenAIWSJSONValue(output, i)
			} else {
				for output[i] != ',' && output[i] != ']' {
					i++
				}
			}
			c.err = c.add(json.RawMessage(bytes.TrimSpace(output[start:i])))
			if c.err != nil {
				return
			}
			i = skipOpenAIWSJSONSpace(output, i)
			if output[i] == ',' {
				i++
			}
		}
	}
}

func stripWSMemoryReplay(ctx context.Context, items wsMemoryReplay, invalid map[string]struct{}) (wsMemoryReplay, error) {
	if len(items.items) == 0 || len(invalid) == 0 {
		return items.hold(), nil
	}
	// Existing sanitizer first checks the raw encrypted field, then applies
	// the decoded item rules only to matched entries.
	out, err := newWSMemoryReplay(ctx, len(items.items), items.exists)
	if err != nil {
		return out, err
	}
	for i, item := range items.items {
		encrypted, keyOwner, err := wsReplayString(ctx, item, "encrypted_content")
		if err != nil {
			out.drop()
			return wsMemoryReplay{}, err
		}
		_, hit := invalid[openAIEncryptedContentDigest(encrypted)]
		hit = hit && encrypted != ""
		keyOwner.Drop()
		if !hit {
			out.appendView(item, items.owners[i])
			continue
		}
		mem := newWSMaterializer(ctx)
		var decoded map[string]any
		if err := mem.decode(item, &decoded); err != nil {
			mem.close()
			if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
				out.drop()
				return wsMemoryReplay{}, err
			}
			out.appendView(item, items.owners[i])
			continue
		}
		encrypted, _ = decoded["encrypted_content"].(string)
		if _, matched := invalid[openAIEncryptedContentDigest(encrypted)]; !matched || encrypted == "" {
			mem.close()
			out.appendView(item, items.owners[i])
			continue
		}
		next, changed, keep := sanitizeEncryptedReasoningInputItem(decoded)
		if !changed {
			mem.close()
			out.appendView(item, items.owners[i])
			continue
		}
		if !keep {
			mem.close()
			continue
		}
		rebuilt, err := mem.encode(next)
		if err != nil {
			mem.close()
			if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
				out.drop()
				return wsMemoryReplay{}, err
			}
			out.appendView(item, items.owners[i])
			continue
		}
		owner := mem.take(rebuilt)
		out.appendView(rebuilt, owner)
		owner.Drop()
		mem.close()
	}
	return out, nil
}

func stripWSMemoryInvalidFrame(ctx context.Context, payload []byte, invalid map[string]struct{}) ([]byte, *pkghttputil.WSBodyMemoryOwner, int, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		body, n, err := stripOpenAIInvalidEncryptedContentRaw(payload, invalid)
		return body, nil, n, err
	}
	items, err := extractWSMemoryReplay(ctx, payload, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	defer items.drop()
	hit := false
	for _, item := range items.items {
		typ, typOwner, err := wsReplayString(ctx, item, "type")
		if err != nil {
			return nil, nil, 0, err
		}
		enabled := openAIEncryptedLineageItemType(typ)
		typOwner.Drop()
		if !enabled {
			continue
		}
		encrypted, owner, err := wsReplayString(ctx, item, "encrypted_content")
		if err != nil {
			return nil, nil, 0, err
		}
		_, matched := invalid[openAIEncryptedContentDigest(encrypted)]
		hit = matched && encrypted != ""
		owner.Drop()
		if hit {
			break
		}
	}
	if !hit {
		return payload, nil, 0, nil
	}
	mem := newWSMaterializer(ctx)
	defer mem.close()
	var decoded map[string]any
	if err := mem.decode(payload, &decoded); err != nil {
		return nil, nil, 0, err
	}
	n := stripOpenAIInvalidEncryptedContentItems(decoded, invalid)
	if n == 0 {
		return payload, nil, 0, nil
	}
	body, err := mem.encode(decoded)
	if err != nil {
		return nil, nil, 0, err
	}
	return body, mem.take(body), n, nil
}

func (s *OpenAIGatewayService) stripWSInvalidFrameLogged(ctx context.Context, payload []byte, invalid map[string]struct{}, logKey string, accountID int64, turn int) ([]byte, *pkghttputil.WSBodyMemoryOwner, int, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		body, n := s.stripSessionInvalidEncryptedContentLogged(payload, invalid, logKey, accountID, turn)
		return body, nil, n, nil
	}
	body, owner, n, err := stripWSMemoryInvalidFrame(ctx, payload, invalid)
	if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
		return nil, nil, 0, err
	}
	if err != nil {
		logOpenAIWSModeInfo("%s_skip account_id=%d turn=%d reason=strip_error cause=%s", logKey, accountID, turn, truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen))
		return payload, nil, 0, nil
	}
	if n > 0 {
		logOpenAIWSModeInfo("%s account_id=%d turn=%d stripped_items=%d", logKey, accountID, turn, n)
	}
	return body, owner, n, nil
}
