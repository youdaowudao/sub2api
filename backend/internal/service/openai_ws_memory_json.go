package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

func wsMemoryAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, pkghttputil.ErrRequestBodyMemoryBudget
	}
	return a + b, nil
}

// Reserve the decoder's actual growth sequence and the encoded token bounds
// for decoded strings/numbers, including unquoteBytes' temporary storage.
// The original decoder and its UseNumber/trailing-value semantics are kept.
func wsJSONDecodeScope(ctx context.Context, data []byte) (func(), error) {
	return wsJSONDecodeScopeMode(ctx, data, true)
}

func wsJSONDecodeScopeMode(ctx context.Context, data []byte, buffered bool) (func(), error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		return func() {}, nil
	}
	var current, previous int64
	need := int64(0)
	if buffered {
		need = int64(len(data)) + 512
	}
	for current < need {
		if current > (math.MaxInt64-512)/2 {
			return nil, pkghttputil.ErrRequestBodyMemoryBudget
		}
		previous, current = current, current*2+512
	}
	reserved, err := wsMemoryAdd(current, previous)
	if err != nil {
		return nil, err
	}
	var maxUnquote int64
	for i := 0; i < len(data); {
		if data[i] == '"' {
			end := scanOpenAIWSJSONString(data, i)
			if end <= i+1 || end > len(data) {
				return nil, errors.New("invalid JSON string")
			}
			token := data[i+1 : end-1]
			decoded := int64(len(token))
			unquote := false
			for j := 0; j < len(token); {
				if token[j] == '\\' {
					unquote = true
					j += 2
					continue
				}
				if token[j] < utf8.RuneSelf {
					j++
					continue
				}
				r, n := utf8.DecodeRune(token[j:])
				if r == utf8.RuneError && n == 1 {
					unquote = true
					decoded, err = wsMemoryAdd(decoded, 2)
					if err != nil {
						return nil, err
					}
				}
				j += n
			}
			reserved, err = wsMemoryAdd(reserved, decoded)
			if err != nil {
				return nil, err
			}
			if unquote {
				// Go's unquoteBytes starts with len(token)+2*UTFMax and only
				// expands for replacement characters; old/new coexist on growth.
				capNow, capPrev := int64(len(token))+2*utf8.UTFMax, int64(0)
				for capNow-2*utf8.UTFMax < decoded {
					if capNow > (math.MaxInt64/2 - utf8.UTFMax) {
						return nil, pkghttputil.ErrRequestBodyMemoryBudget
					}
					capPrev, capNow = capNow, (capNow+utf8.UTFMax)*2
				}
				peak, err := wsMemoryAdd(capPrev, capNow)
				if err != nil {
					return nil, err
				}
				maxUnquote = max(maxUnquote, peak)
			}
			i = end
			continue
		}
		if data[i] == '-' || (data[i] >= '0' && data[i] <= '9') {
			start := i
			for i < len(data) && bytes.IndexByte([]byte("0123456789.eE+-"), data[i]) >= 0 {
				i++
			}
			reserved, err = wsMemoryAdd(reserved, int64(i-start))
			if err != nil {
				return nil, err
			}
			continue
		}
		i++
	}
	reserved, err = wsMemoryAdd(reserved, maxUnquote)
	if err != nil {
		return nil, err
	}
	scope, release := pkghttputil.WithRequestBodyMemoryScope(ctx)
	if err := pkghttputil.ReserveRequestBodyMemory(scope, reserved); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// For the typed compatibility decoders, RawMessage.UnmarshalJSON copies
// its token in addition to the strings counted by the decoder pass. Walk
// the validated input with the destination's ordinary JSON field types.
func wsJSONRawCopyBound(data []byte, typ reflect.Type) (int64, error) {
	if typ == nil {
		return 0, nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	raw := reflect.TypeFor[json.RawMessage]()
	if typ == raw {
		// stdlib RawMessage appends into an initially empty slice; include
		// allocator page/size-class rounding of that backing allocation.
		return wsMemoryAdd(int64(len(data)), 8192)
	}
	if len(data) == 0 || typ.Kind() == reflect.Interface {
		return 0, nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return 0, nil
	}
	childEnd := func(start int) int {
		if data[start] == '"' || data[start] == '[' || data[start] == '{' {
			return skipOpenAIWSJSONValue(data, start)
		}
		i := start
		for i < len(data) && data[i] != ',' && data[i] != '}' && data[i] != ']' {
			i++
		}
		return i
	}
	var total int64
	add := func(value []byte, t reflect.Type) error {
		n, err := wsJSONRawCopyBound(bytes.TrimSpace(value), t)
		if err != nil {
			return err
		}
		total, err = wsMemoryAdd(total, n)
		return err
	}
	switch typ.Kind() {
	case reflect.Array, reflect.Slice:
		if data[0] != '[' {
			return 0, nil
		}
		for i := 1; ; {
			i = skipOpenAIWSJSONSpace(data, i)
			if i >= len(data) || data[i] == ']' {
				break
			}
			end := childEnd(i)
			if err := add(data[i:end], typ.Elem()); err != nil {
				return 0, err
			}
			i = skipOpenAIWSJSONSpace(data, end)
			if i >= len(data) || data[i] != ',' {
				break
			}
			i++
		}
	case reflect.Map, reflect.Struct:
		if data[0] != '{' {
			return 0, nil
		}
		for i := 1; ; {
			i = skipOpenAIWSJSONSpace(data, i)
			if i >= len(data) || data[i] == '}' {
				break
			}
			if data[i] != '"' {
				break
			}
			keyStart := i
			keyEnd := scanOpenAIWSJSONString(data, i)
			i = skipOpenAIWSJSONSpace(data, keyEnd)
			if i >= len(data) || data[i] != ':' {
				break
			}
			i = skipOpenAIWSJSONSpace(data, i+1)
			if i >= len(data) {
				break
			}
			end := childEnd(i)
			var child reflect.Type
			if typ.Kind() == reflect.Map {
				child = typ.Elem()
			} else {
				// All typed request structs in this conversion use plain field tags.
				// Decode a key only when it could match one of those small field names.
				for n := 0; n < typ.NumField(); n++ {
					field := typ.Field(n)
					if !field.IsExported() {
						continue
					}
					name := strings.Split(field.Tag.Get("json"), ",")[0]
					if name == "-" {
						continue
					}
					if name == "" {
						name = field.Name
					}
					if keyEnd-keyStart > 6*len(name)+2 {
						continue
					}
					var key string
					if json.Unmarshal(data[keyStart:keyEnd], &key) == nil && strings.EqualFold(key, name) {
						child = field.Type
						break
					}
				}
			}
			if child != nil {
				if err := add(data[i:end], child); err != nil {
					return 0, err
				}
			}
			i = skipOpenAIWSJSONSpace(data, end)
			if i >= len(data) || data[i] != ',' {
				break
			}
			i++
		}
	}
	return total, nil
}

func wsJSONStringSize(s string, html bool) int64 {
	n := int64(2)
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch c {
			case '\\', '"', '\n', '\r', '\t', '\b', '\f':
				n += 2
			default:
				if c < 0x20 || (html && (c == '<' || c == '>' || c == '&')) {
					n += 6
				} else {
					n++
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 || r == '\u2028' || r == '\u2029' {
			n += 6
		} else {
			n += int64(size)
		}
		i += size
	}
	return n
}

func wsJSONRawSize(raw json.RawMessage, html bool) int64 {
	if raw == nil {
		return 4
	}
	var n int64
	inString, escaped := false, false
	for i := 0; i < len(raw); {
		c := raw[i]
		if html && (c == '<' || c == '>' || c == '&') {
			n += 6
			i++
			continue
		}
		if html && c == 0xe2 && i+2 < len(raw) && raw[i+1] == 0x80 && raw[i+2]&^1 == 0xa8 {
			n += 6
			i += 3
			continue
		}
		if inString {
			n++
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		switch c {
		case ' ', '\n', '\r', '\t':
		default:
			n++
			if c == '"' {
				inString = true
			}
		}
		i++
	}
	return n
}

// The size pass only accepts the same decoded JSON tree and explicit raw
// values used by the WS transformation path. Unknown marshalers cannot
// silently materialize a new unbounded body.
func wsJSONEncodedSize(v any, html bool) (int64, int64, error) {
	var size, largest int64
	add := func(n int64) error { var err error; size, err = wsMemoryAdd(size, n); return err }
	var visit func(any) error
	visit = func(value any) error {
		switch x := value.(type) {
		case nil:
			return add(4)
		case bool:
			if x {
				return add(4)
			}
			return add(5)
		case string:
			n := wsJSONStringSize(x, html)
			largest = max(largest, n)
			return add(n)
		case json.Number:
			return add(int64(max(len(x), 1)))
		case json.RawMessage:
			n := wsJSONRawSize(x, html)
			largest = max(largest, n)
			return add(n)
		case []json.RawMessage:
			if x == nil {
				return add(4)
			}
			if err := add(2 + int64(max(0, len(x)-1))); err != nil {
				return err
			}
			for _, item := range x {
				if err := visit(item); err != nil {
					return err
				}
			}
		case []any:
			if x == nil {
				return add(4)
			}
			if err := add(2 + int64(max(0, len(x)-1))); err != nil {
				return err
			}
			for _, item := range x {
				if err := visit(item); err != nil {
					return err
				}
			}
		case map[string]any:
			if x == nil {
				return add(4)
			}
			if err := add(2 + int64(max(0, len(x)-1))); err != nil {
				return err
			}
			for key, item := range x {
				n := wsJSONStringSize(key, html)
				largest = max(largest, n)
				if err := add(n + 1); err != nil {
					return err
				}
				if err := visit(item); err != nil {
					return err
				}
			}
		case map[string]string:
			if x == nil {
				return add(4)
			}
			if err := add(2 + int64(max(0, len(x)-1))); err != nil {
				return err
			}
			for key, item := range x {
				n := wsJSONStringSize(key, html)
				largest = max(largest, n)
				if err := add(n + 1); err != nil {
					return err
				}
				if err := visit(item); err != nil {
					return err
				}
			}
		case map[string]json.RawMessage:
			if x == nil {
				return add(4)
			}
			if err := add(2 + int64(max(0, len(x)-1))); err != nil {
				return err
			}
			for key, item := range x {
				n := wsJSONStringSize(key, html)
				largest = max(largest, n)
				if err := add(n + 1); err != nil {
					return err
				}
				if err := visit(item); err != nil {
					return err
				}
			}
		case []string:
			if x == nil {
				return add(4)
			}
			if err := add(2 + int64(max(0, len(x)-1))); err != nil {
				return err
			}
			for _, item := range x {
				if err := visit(item); err != nil {
					return err
				}
			}
		case int:
			return add(int64(len(strconv.Itoa(x))))
		case int64:
			return add(int64(len(strconv.FormatInt(x, 10))))
		case float64:
			// Decoded request bodies use UseNumber. Generated float64 values
			// have a fixed small encoder bound (including exponent syntax).
			return add(32)
		default:
			rv := reflect.ValueOf(value)
			// Generated compatibility types use the ordinary struct encoder.
			// Reject custom marshalers, whose result length is not described
			// by their visible fields.
			if _, custom := value.(json.Marshaler); custom {
				return errors.New("unsupported custom JSON marshaler in WS body size pass")
			}
			switch rv.Kind() {
			case reflect.Pointer:
				if rv.IsNil() {
					return add(4)
				}
				return visit(rv.Elem().Interface())
			case reflect.Slice, reflect.Array:
				if rv.Kind() == reflect.Slice && rv.IsNil() {
					return add(4)
				}
				if rv.Type().Elem().Kind() == reflect.Uint8 {
					return errors.New("unsupported binary JSON value in WS body size pass")
				}
				if err := add(2 + int64(max(0, rv.Len()-1))); err != nil {
					return err
				}
				for i := 0; i < rv.Len(); i++ {
					if err := visit(rv.Index(i).Interface()); err != nil {
						return err
					}
				}
			case reflect.Struct:
				if err := add(2); err != nil {
					return err
				}
				fields := 0
				for i := 0; i < rv.NumField(); i++ {
					field := rv.Type().Field(i)
					if !field.IsExported() {
						continue
					}
					tag := strings.Split(field.Tag.Get("json"), ",")
					if tag[0] == "-" {
						continue
					}
					if field.Anonymous {
						return errors.New("unsupported embedded JSON fields in WS body size pass")
					}
					name := tag[0]
					if name == "" {
						name = field.Name
					}
					for _, option := range tag[1:] {
						if option == "string" {
							return errors.New("unsupported quoted JSON field in WS body size pass")
						}
					}
					// Including omitted fields is a safe upper bound: no
					// synthetic body or duplicate serialization is needed.
					n := wsJSONStringSize(name, html)
					largest = max(largest, n)
					if fields > 0 {
						n++
					}
					if err := add(n + 1); err != nil {
						return err
					}
					if err := visit(rv.Field(i).Interface()); err != nil {
						return err
					}
					fields++
				}
			default:
				return errors.New("unsupported JSON value in WS body size pass")
			}
		}
		return nil
	}
	if err := visit(v); err != nil {
		return 0, 0, err
	}
	return size, largest, nil
}

type wsFixedJSONWriter struct{ b []byte }

func (w *wsFixedJSONWriter) Write(p []byte) (int, error) {
	if len(p) > cap(w.b)-len(w.b) {
		return 0, pkghttputil.ErrRequestBodyMemoryBudget
	}
	w.b = append(w.b, p...)
	return len(p), nil
}

// Keep the stdlib encoder and EscapeHTML setting. Its current-version
// bytes.Buffer.growSlice grows to max(len+n,2*cap), rounding the byte backing
// allocation. A string/RawMessage may first grow an AvailableBuffer append.
// Reserve the old/new work buffers and that token's temporary append before
// the encoder runs; the result is written into an exact pre-reserved buffer.
func reserveWSJSONEncodingWork(ctx context.Context, value any, html bool) (int64, func(), error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		return 0, func() {}, nil
	}
	z, largest, err := wsJSONEncodedSize(value, html)
	if err != nil {
		return 0, nil, err
	}
	z, err = wsMemoryAdd(z, 1)
	if err != nil || z > int64(int(^uint(0)>>1)) {
		return 0, nil, pkghttputil.ErrRequestBodyMemoryBudget
	}
	// rounded capacity upper bound for no-pointer byte allocations in the
	// pinned toolchain; 8KiB covers its allocator size-class/page rounding.
	oldCap := z
	newCap, err := wsMemoryAdd(z, z)
	if err != nil {
		return 0, nil, err
	}
	newCap, err = wsMemoryAdd(newCap, 8192)
	if err != nil {
		return 0, nil, err
	}
	appendCap, err := wsMemoryAdd(largest, largest)
	if err != nil {
		return 0, nil, err
	}
	appendCap, err = wsMemoryAdd(appendCap, 8192)
	if err != nil {
		return 0, nil, err
	}
	work, err := wsMemoryAdd(oldCap, newCap)
	if err != nil {
		return 0, nil, err
	}
	work, err = wsMemoryAdd(work, appendCap)
	if err != nil {
		return 0, nil, err
	}
	// The intermediate append's previous backing can coexist with its grown
	// backing before the encoder copies the token into encodeState.
	work, err = wsMemoryAdd(work, largest)
	if err != nil {
		return 0, nil, err
	}
	scope, release := pkghttputil.WithRequestBodyMemoryScope(ctx)
	if err := pkghttputil.ReserveRequestBodyMemory(scope, work); err != nil {
		release()
		return 0, nil, err
	}
	return z, release, nil
}

func marshalWSJSON(ctx context.Context, value any, html bool) ([]byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if !pkghttputil.HasWSRequestBodyMemory(ctx) {
		if html {
			b, e := json.Marshal(value)
			return b, nil, e
		}
		b, e := marshalOpenAIUpstreamJSON(value)
		return b, nil, e
	}
	z, release, err := reserveWSJSONEncodingWork(ctx, value, html)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	owner, err := pkghttputil.ReserveWSBodyMemoryOwner(ctx, z)
	if err != nil {
		return nil, nil, err
	}
	writer := &wsFixedJSONWriter{b: make([]byte, 0, int(z))}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(html)
	if err := encoder.Encode(value); err != nil {
		owner.Drop()
		return nil, nil, err
	}
	if len(writer.b) == 0 || writer.b[len(writer.b)-1] != '\n' {
		owner.Drop()
		return nil, nil, io.ErrUnexpectedEOF
	}
	return writer.b[:len(writer.b)-1], owner, nil
}
