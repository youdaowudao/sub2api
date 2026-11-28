package apicompat

import (
	"encoding/json"
	"strings"
)

// ResponsesToolMemory is supplied only by budgeted WS conversion. The
// caller owns the lifetime of retained generated strings; nil preserves the
// original HTTP conversion and error semantics.
type ResponsesToolMemory struct {
	Marshal     func(any) ([]byte, error)
	Unmarshal   func([]byte, any) error
	EncodeText  func(any) (string, error)
	Validate    func(any) error
	ReserveText func(int64) (func(), error)
	err         error
}

func firstResponsesToolMemory(values []*ResponsesToolMemory) *ResponsesToolMemory {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}
func (m *ResponsesToolMemory) encodeText(v any) (string, error) {
	if m == nil {
		b, e := json.Marshal(v)
		return string(b), e
	}
	if m.err != nil {
		return "", m.err
	}
	text, err := m.EncodeText(v)
	if err != nil {
		m.err = err
	}
	return text, err
}
func (m *ResponsesToolMemory) validate(v any) error {
	if m == nil {
		_, err := json.Marshal(v)
		return err
	}
	if m.err != nil {
		return m.err
	}
	err := m.Validate(v)
	if err != nil {
		m.err = err
	}
	return err
}
func (m *ResponsesToolMemory) join(parts ...string) string {
	if m == nil {
		return strings.Join(parts, "")
	}
	if m.err != nil {
		return ""
	}
	var total int64
	for _, part := range parts {
		total += int64(len(part))
	}
	_, err := m.ReserveText(total)
	if err != nil {
		m.err = err
		return ""
	}
	return strings.Join(parts, "")
}
func (m *ResponsesToolMemory) flatten(namespace, name string) string {
	if m == nil {
		return flattenNamespaceToolName(namespace, name)
	}
	if m.err != nil {
		return ""
	}
	full := int64(len(namespace)) + 2 + int64(len(name))
	done, err := m.ReserveText(full)
	if err != nil {
		m.err = err
		return ""
	}
	if full > chatToolNameMaxLen {
		// The existing flatten algorithm hashes full and builds a short
		// prefix/suffix and final result. Full is temporary, unlike the result.
		if _, err = m.ReserveText(2 * chatToolNameMaxLen); err != nil {
			done()
			m.err = err
			return ""
		}
		defer done()
	}
	return flattenNamespaceToolName(namespace, name)
}
func (m *ResponsesToolMemory) Err() error {
	if m == nil {
		return nil
	}
	return m.err
}

func (m *ResponsesToolMemory) marshal(v any) ([]byte, error) {
	if m == nil {
		return json.Marshal(v)
	}
	if m.err != nil {
		return nil, m.err
	}
	result, err := m.Marshal(v)
	if err != nil {
		m.err = err
	}
	return result, err
}
func (m *ResponsesToolMemory) unmarshal(data []byte, v any) error {
	if m == nil {
		return json.Unmarshal(data, v)
	}
	if m.err != nil {
		return m.err
	}
	// Ordinary decode type errors must keep existing string/array fallbacks.
	return m.Unmarshal(data, v)
}
