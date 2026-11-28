package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/MoeForge/moego/ojson"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RemovePreviousResponseIDFromWSBody keeps the existing plain-field removal
// semantics, with a separately owned exact-length result when WS accounting
// is enabled. The one fixed-key Doc delete borrows the immutable input.
func RemovePreviousResponseIDFromWSBody(ctx context.Context, body []byte) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		return RemovePreviousResponseIDFromBody(body), nil, nil
	}
	if !gjson.Get(openAIWSPayloadStringView(body), "previous_response_id").Exists() {
		return body, nil, nil
	}
	doc := ojson.NewDoc(body)
	if err := doc.Delete("previous_response_id"); err != nil {
		return nil, nil, err
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, int64(doc.Len()))
	if err != nil {
		return nil, nil, err
	}
	out := doc.AppendTo(make([]byte, 0, doc.Len()))
	return out, owner, nil
}

// WS raw mutations use one plain-path Doc edit. The raw value copy and the
// Doc's first arena growth are reserved before SetRawBytes; the resulting
// document size is known before allocating its one independent backing array.
func setWSRawBodyField(ctx context.Context, body []byte, path string, raw []byte) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		out, err := sjson.SetRawBytes(body, path, raw)
		return out, nil, err
	}
	// All WS call sites use fixed, plain object paths. The insertion includes
	// the value, escaped keys, separators and needed nested object braces.
	// Doc.room starts with a 48-byte inline arena, then max(512, insertion).
	if strings.ContainsAny(path, "*?#|@[]:") {
		return nil, nil, errors.New("unsupported WS raw edit path")
	}
	insertion, err := wsMemoryAdd(int64(len(raw)), int64(len(path))*6+32)
	if err != nil {
		return nil, nil, err
	}
	arenaSize := insertion
	if arenaSize < 512 {
		arenaSize = 512
	}
	scratch, err := wsMemoryAdd(int64(len(raw)), arenaSize)
	if err != nil {
		return nil, nil, err
	}
	scope, release := pkghttputil.WithRequestBodyMemoryScope(ctx)
	defer release()
	if err := pkghttputil.ReserveRequestBodyMemory(scope, scratch); err != nil {
		return nil, nil, err
	}
	doc := ojson.NewDoc(body)
	if err := doc.SetRawBytes(path, raw); err != nil {
		return nil, nil, err
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, int64(doc.Len()))
	if err != nil {
		return nil, nil, err
	}
	return doc.AppendTo(make([]byte, 0, doc.Len())), owner, nil
}

func setWSBodyField(ctx context.Context, body []byte, path string, value any) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		out, err := sjson.SetBytes(body, path, value)
		return out, nil, err
	}
	raw, owner, err := marshalWSJSON(ctx, value, true)
	if err != nil {
		return nil, nil, err
	}
	defer owner.Drop()
	return setWSRawBodyField(ctx, body, path, raw)
}

func setWSBodyInput(ctx context.Context, payload []byte, items []json.RawMessage, exists bool) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if !exists {
		return payload, nil, nil
	}
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		out, err := setOpenAIWSPayloadInputSequence(payload, items, exists)
		return out, nil, err
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	raw, owner, err := marshalWSJSON(ctx, items, true)
	if err != nil {
		return nil, nil, err
	}
	defer owner.Drop()
	return setWSRawBodyField(ctx, payload, "input", raw)
}

type wsParsedBodyMemory struct {
	raw, normalized, identity, metadata, model *pkghttputil.WSBodyMemoryOwner
}

func (m wsParsedBodyMemory) hold() wsParsedBodyMemory {
	return wsParsedBodyMemory{raw: m.raw.Hold(), normalized: m.normalized.Hold(), identity: m.identity.Hold(), metadata: m.metadata.Hold(), model: m.model.Hold()}
}
func (m *wsParsedBodyMemory) drop() {
	m.model.Drop()
	m.metadata.Drop()
	m.identity.Drop()
	m.normalized.Drop()
	m.raw.Drop()
	*m = wsParsedBodyMemory{}
}
func (m *wsParsedBodyMemory) replace(owner *pkghttputil.WSBodyMemoryOwner) {
	m.normalized.Drop()
	m.normalized = owner
}

func buildWSOwnedRetryPayload(ctx context.Context, payload []byte, items []json.RawMessage, exists bool, model string) ([]byte, *pkghttputil.WSBodyMemoryOwner, bool, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		out, safe, err := buildOpenAIWSCurrentTurnRetryPayload(payload, items, exists, model)
		return out, nil, safe, err
	}
	if !exists {
		return nil, nil, false, nil
	}
	body, owner, err := setWSBodyInput(ctx, payload, items, true)
	if err != nil {
		return nil, nil, false, err
	}
	out, deleteOwner, err := RemovePreviousResponseIDFromWSBody(ctx, body)
	if err != nil {
		owner.Drop()
		return nil, nil, false, err
	}
	if deleteOwner != nil {
		owner.Drop()
		owner = deleteOwner
		body = out
	}
	if model = strings.TrimSpace(model); model != "" {
		next, nextOwner, err := setWSBodyField(ctx, body, "model", model)
		if err != nil {
			owner.Drop()
			return nil, nil, false, err
		}
		owner.Drop()
		body, owner = next, nextOwner
	}
	coverage, coverageErr := AnalyzeWSBodyToolCoverage(ctx, body)
	if coverageErr != nil {
		owner.Drop()
		return nil, nil, false, coverageErr
	}
	if coverage.HasFunctionCallOutput && !coverage.ContextCoversAllCallIDs {
		owner.Drop()
		return nil, nil, false, nil
	}
	return body, owner, true, nil
}

// wsMaterializer is a per-transformation scope, never a global allocation
// registry. Each producer records its exact independently owned output; the
// caller transfers the returned allocation and drops the remaining temporary
// results when the existing shared helper returns. Nil is the unchanged HTTP
// and budget-disabled path.
type wsMaterializedBody struct {
	body  []byte
	owner *pkghttputil.WSBodyMemoryOwner
	next  *wsMaterializedBody
}
type wsMaterializer struct {
	ctx      context.Context
	outputs  *wsMaterializedBody
	decoders []func()
	err      error
}

func newWSMaterializer(ctx context.Context) *wsMaterializer {
	if pkghttputil.HasWSRequestBodyMemory(ctx) {
		return &wsMaterializer{ctx: ctx}
	}
	return nil
}
func (m *wsMaterializer) close() {
	if m == nil {
		return
	}
	for n := m.outputs; n != nil; n = n.next {
		n.owner.Drop()
	}
	m.outputs = nil
	for _, done := range m.decoders {
		done()
	}
	m.decoders = nil
}
func (m *wsMaterializer) track(body []byte, owner *pkghttputil.WSBodyMemoryOwner, err error) ([]byte, error) {
	if err != nil {
		if m != nil && errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
			m.err = err
		}
		return nil, err
	}
	if m != nil && owner != nil {
		m.outputs = &wsMaterializedBody{body: body, owner: owner, next: m.outputs}
	}
	return body, nil
}
func (m *wsMaterializer) take(body []byte) *pkghttputil.WSBodyMemoryOwner {
	if m == nil {
		return nil
	}
	// Match the explicit output allocation returned by this helper, not
	// arbitrary process pointers or inferred lengths of earlier frames.
	for link := &m.outputs; *link != nil; link = &(*link).next {
		n := *link
		if unsafe.SliceData(body) == unsafe.SliceData(n.body) {
			*link = n.next
			return n.owner
		}
	}
	return nil
}
func (m *wsMaterializer) decode(body []byte, value any) error {
	if m != nil {
		if m.err != nil {
			return m.err
		}
		done, err := wsJSONDecodeScope(m.ctx, body)
		if err != nil {
			if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
				m.err = err
			}
			return err
		}
		m.decoders = append(m.decoders, done)
	}
	return decodeOpenAIJSONUseNumber(body, value)
}
func (m *wsMaterializer) unmarshal(body []byte, value any) error {
	if m != nil {
		if m.err != nil {
			return m.err
		}
		done, err := wsJSONDecodeScopeMode(m.ctx, body, false)
		if err != nil {
			if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
				m.err = err
			}
			return err
		}
		m.decoders = append(m.decoders, done)
		if json.Valid(body) {
			rawBytes, err := wsJSONRawCopyBound(body, reflect.TypeOf(value))
			if err == nil && rawBytes > 0 {
				_, err = m.reserveText(rawBytes)
			}
			if err != nil {
				m.err = err
				return err
			}
		}
	}
	return json.Unmarshal(body, value)
}
func (m *wsMaterializer) marshal(value any) ([]byte, error) {
	if m == nil {
		return json.Marshal(value)
	}
	if m.err != nil {
		return nil, m.err
	}
	body, owner, err := marshalWSJSON(m.ctx, value, true)
	return m.track(body, owner, err)
}
func (m *wsMaterializer) encode(value any) ([]byte, error) {
	if m == nil {
		return marshalOpenAIUpstreamJSON(value)
	}
	if m.err != nil {
		return nil, m.err
	}
	body, owner, err := marshalWSJSON(m.ctx, value, false)
	return m.track(body, owner, err)
}
func (m *wsMaterializer) set(body []byte, path string, value any) ([]byte, error) {
	if m == nil {
		return sjson.SetBytes(body, path, value)
	}
	if m.err != nil {
		return nil, m.err
	}
	out, owner, err := setWSBodyField(m.ctx, body, path, value)
	return m.track(out, owner, err)
}
func (m *wsMaterializer) setRaw(body []byte, path string, value []byte) ([]byte, error) {
	if m == nil {
		return sjson.SetRawBytes(body, path, value)
	}
	if m.err != nil {
		return nil, m.err
	}
	out, owner, err := setWSRawBodyField(m.ctx, body, path, value)
	return m.track(out, owner, err)
}
func (m *wsMaterializer) delete(body []byte, path string) ([]byte, error) {
	if m == nil {
		return sjson.DeleteBytes(body, path)
	}
	if m.err != nil {
		return nil, m.err
	}
	doc := ojson.NewDoc(body)
	if err := doc.Delete(path); err != nil {
		return nil, err
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(m.ctx, int64(doc.Len()))
	if err != nil {
		return m.track(nil, nil, err)
	}
	return m.track(doc.AppendTo(make([]byte, 0, doc.Len())), owner, nil)
}
func firstWSMaterializer(values []*wsMaterializer) *wsMaterializer {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func (m *wsMaterializer) rawView(s string) []byte {
	if m == nil {
		return []byte(s)
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// rawWSPath returns the raw, immutable value for the simple paths used by
// request transforms. Traversing an array never copies its contents.
func rawWSPath(body []byte, path string) []byte {
	for _, key := range strings.Split(path, ".") {
		body = bytes.TrimSpace(body)
		if len(body) == 0 || key == "" {
			return nil
		}
		if body[0] == '[' {
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 {
				return nil
			}
			i := 1
			for n := 0; ; n++ {
				i = skipOpenAIWSJSONSpace(body, i)
				if i >= len(body) || body[i] == ']' {
					return nil
				}
				start := i
				if body[i] == '"' || body[i] == '[' || body[i] == '{' {
					i = skipOpenAIWSJSONValue(body, i)
				} else {
					for i < len(body) && body[i] != ',' && body[i] != ']' {
						i++
					}
				}
				if n == index {
					body = body[start:i]
					break
				}
				i = skipOpenAIWSJSONSpace(body, i)
				if i >= len(body) || body[i] != ',' {
					return nil
				}
				i++
			}
		} else {
			part, ok := openAIWSRawFieldViewValid(body, key, false)
			if !ok {
				return nil
			}
			body = part
		}
	}
	return body
}

// Routing fields retained by the caller are independent small strings,
// unlike temporary borrowed GJSON results. Reserve their copies before
// parsing, then release the decoder scratch separately.
func readWSRoutingFields(ctx context.Context, body []byte, paths ...string) ([]gjson.Result, *pkghttputil.WSBodyMemoryOwner, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		return gjson.GetManyBytes(body, paths...), nil, nil
	}
	var size int64
	for _, path := range paths {
		raw := rawWSPath(body, path)
		n, err := wsMemoryAdd(int64(len(raw)), int64(len(raw)))
		if err == nil {
			size, err = wsMemoryAdd(size, n)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, size)
	if err != nil {
		return nil, nil, err
	}
	mem := newWSMaterializer(ctx)
	defer mem.close()
	results := make([]gjson.Result, len(paths))
	for i, path := range paths {
		value := mem.get(body, path)
		if mem.err != nil {
			owner.Drop()
			return nil, nil, mem.err
		}
		value.Raw = strings.Clone(value.Raw)
		value.Str = strings.Clone(value.Str)
		results[i] = value
	}
	return results, owner, nil
}

func (m *wsMaterializer) get(body []byte, path string) gjson.Result {
	if m == nil {
		return gjson.GetBytes(body, path)
	}
	if m.err != nil {
		return gjson.Result{}
	}
	raw := rawWSPath(body, path)
	if len(raw) == 0 {
		return gjson.Result{}
	}
	// The raw field is a subview of the immutable original allocation.
	offset := int(uintptr(unsafe.Pointer(unsafe.SliceData(raw))) - uintptr(unsafe.Pointer(unsafe.SliceData(body))))
	return m.parseResult(raw, offset)
}
func (m *wsMaterializer) resultGet(result gjson.Result, path string) gjson.Result {
	if m == nil {
		return result.Get(path)
	}
	value := m.get(m.rawView(result.Raw), path)
	if value.Exists() {
		value.Index += result.Index
	}
	return value
}

// Iterate a valid immutable JSON view using precisely the next item/key's
// string storage bounds before GJSON decodes it. This keeps GJSON's value and
// index semantics without materializing unrelated siblings.
func (m *wsMaterializer) parseResult(raw []byte, index int) gjson.Result {
	if len(raw) > 0 && raw[0] == '"' && bytes.ContainsRune(raw, '\\') {
		if _, err := m.reserveText(4*int64(len(raw)) + 8192); err != nil {
			return gjson.Result{}
		}
	}
	result := gjson.Parse(openAIWSPayloadStringView(raw))
	result.Index = index
	return result
}
func (m *wsMaterializer) forEachArray(result gjson.Result, visit func(gjson.Result, gjson.Result) bool) {
	if m == nil || (!result.IsArray() && !result.IsObject()) {
		result.ForEach(visit)
		return
	}
	body := m.rawView(result.Raw)
	object := result.IsObject()
	for i, index := 1, 0; ; index++ {
		i = skipOpenAIWSJSONSpace(body, i)
		if i >= len(body) || body[i] == ']' || body[i] == '}' {
			return
		}
		key := gjson.Result{Type: gjson.Number, Num: float64(index)}
		if object {
			if body[i] != '"' {
				return
			}
			end := scanOpenAIWSJSONString(body, i)
			key = m.parseResult(body[i:end], i+result.Index)
			i = skipOpenAIWSJSONSpace(body, end)
			if i >= len(body) || body[i] != ':' {
				return
			}
			i = skipOpenAIWSJSONSpace(body, i+1)
			if i >= len(body) {
				return
			}
		}
		start := i
		if body[i] == '"' || body[i] == '{' || body[i] == '[' {
			i = skipOpenAIWSJSONValue(body, i)
		} else {
			for i < len(body) && body[i] != ',' && body[i] != ']' && body[i] != '}' {
				i++
			}
		}
		part := bytes.TrimSpace(body[start:i])
		offset := start + result.Index
		if index < len(result.Indexes) {
			offset = result.Indexes[index]
		}
		value := m.parseResult(part, offset)
		if m.err != nil || !visit(key, value) {
			return
		}
		i = skipOpenAIWSJSONSpace(body, i)
		if i >= len(body) || body[i] != ',' {
			return
		}
		i++
	}
}
func (m *wsMaterializer) array(result gjson.Result) []gjson.Result {
	if m == nil || !result.IsArray() {
		return result.Array()
	}
	var items []gjson.Result
	m.forEachArray(result, func(_, item gjson.Result) bool { items = append(items, item); return true })
	return items
}

// reserveText holds the independent generated string until the caller's
// conversion completes. A helper may release a short-lived temporary sooner.
func (m *wsMaterializer) reserveText(n int64) (func(), error) {
	if m.err != nil {
		return nil, m.err
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(m.ctx, n)
	if err != nil {
		m.err = err
		return nil, err
	}
	var once sync.Once
	done := func() { once.Do(owner.Drop) }
	m.decoders = append(m.decoders, done)
	return done, nil
}

func (m *wsMaterializer) string(body []byte) string {
	if m == nil {
		return string(body)
	}
	if m.err != nil {
		return ""
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(m.ctx, int64(len(body)))
	if err != nil {
		m.err = err
		return ""
	}
	value := string(body)
	// String bodies retained only within this transformation scope.
	m.decoders = append(m.decoders, owner.Drop)
	return value
}

func (m *wsMaterializer) join(values []string, sep string) string {
	if m == nil || len(values) < 2 {
		return strings.Join(values, sep)
	}
	if m.err != nil {
		return ""
	}
	var size int64
	for i, value := range values {
		var err error
		size, err = wsMemoryAdd(size, int64(len(value)))
		if err == nil && i > 0 {
			size, err = wsMemoryAdd(size, int64(len(sep)))
		}
		if err != nil {
			m.err = err
			return ""
		}
	}
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(m.ctx, size)
	if err != nil {
		m.err = err
		return ""
	}
	value := strings.Join(values, sep)
	m.decoders = append(m.decoders, owner.Drop)
	return value
}
func (m *wsMaterializer) setString(body, path string, value any) (string, error) {
	if m == nil {
		return sjson.Set(body, path, value)
	}
	out, err := m.set(m.rawView(body), path, value)
	if err != nil {
		return "", err
	}
	result := m.string(out)
	return result, m.err
}

func transformWSBody(ctx context.Context, body []byte, transform func(*wsMaterializer) ([]byte, error)) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	memory := newWSMaterializer(ctx)
	defer memory.close()
	next, err := transform(memory)
	if memory != nil && memory.err != nil {
		return nil, nil, memory.err
	}
	if err != nil {
		return nil, nil, err
	}
	if memory == nil {
		return next, nil, nil
	}
	owner := memory.take(next)
	if owner == nil && unsafe.SliceData(body) != unsafe.SliceData(next) {
		return nil, nil, errors.New("unowned WS body transformation result")
	}
	return next, owner, nil
}
