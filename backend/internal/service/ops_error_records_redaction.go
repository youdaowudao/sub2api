package service

import (
	"encoding/json"
	"regexp"
	"strings"
)

const opsErrorRecordHiddenMessage = "这条错误消息含敏感信息，已隐藏。请结合错误类型、账号和时间排查。"

var (
	opsRecordPair = regexp.MustCompile(`(?i)(^|[\s{,&?;"'])([a-z][a-z0-9_-]{0,63})(["']?\s*[:=]\s*)`)
	opsRecordAuth = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[^\s,"';}\]]+`)
	opsRecordPEM  = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

func sanitizeOpsErrorRecordMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		value = redactSensitiveJSON(value)
		var visit func(any) any
		visit = func(v any) any {
			switch x := v.(type) {
			case map[string]any:
				for key, item := range x {
					if opsErrorRecordSensitiveKey(key) {
						x[key] = "[REDACTED]"
					} else {
						x[key] = visit(item)
					}
				}
				return x
			case []any:
				for i, item := range x {
					x[i] = visit(item)
				}
				return x
			case string:
				return sanitizeOpsErrorRecordText(x)
			default:
				return v
			}
		}
		if encoded, err := json.Marshal(visit(value)); err == nil {
			return truncateString(string(encoded), 2048)
		}
	}
	return truncateString(sanitizeOpsErrorRecordText(raw), 2048)
}

func sanitizeOpsErrorRecordText(raw string) string {
	raw = opsRecordPEM.ReplaceAllString(raw, "[REDACTED PRIVATE KEY]")
	if strings.Contains(strings.ToUpper(raw), "-----BEGIN ") && strings.Contains(strings.ToUpper(raw), "PRIVATE KEY-----") {
		return opsErrorRecordHiddenMessage
	}
	var out strings.Builder
	last := 0
	for _, match := range opsRecordPair.FindAllStringSubmatchIndex(raw, -1) {
		if match[0] < last {
			continue
		}
		rawKey := raw[match[4]:match[5]]
		key := strings.ToLower(strings.ReplaceAll(rawKey, "-", "_"))
		prefix := raw[match[2]:match[3]]
		if !opsErrorRecordSensitiveKey(rawKey) && (key != "key" || !strings.ContainsAny(prefix, "?&")) {
			continue
		}
		start := match[1]
		if start >= len(raw) {
			return opsErrorRecordHiddenMessage
		}
		end := start
		if raw[start] == '"' || raw[start] == '\'' {
			quote := raw[start]
			end++
			for end < len(raw) {
				if raw[end] == '\\' {
					end += 2
					continue
				}
				if raw[end] == quote {
					end++
					break
				}
				end++
			}
			if end > len(raw) || end == 0 || raw[end-1] != quote {
				return opsErrorRecordHiddenMessage
			}
		} else if key == "authorization" || key == "proxy_authorization" {
			for end < len(raw) && !strings.ContainsRune("\r\n,;}", rune(raw[end])) {
				end++
			}
		} else {
			if raw[start] == '\\' {
				return opsErrorRecordHiddenMessage
			}
			for end < len(raw) && !strings.ContainsRune(" \t\r\n&,;}]", rune(raw[end])) {
				end++
			}
			if end == start {
				return opsErrorRecordHiddenMessage
			}
		}
		_, _ = out.WriteString(raw[last:start])
		_, _ = out.WriteString("[REDACTED]")
		last = end
	}
	_, _ = out.WriteString(raw[last:])
	return opsRecordAuth.ReplaceAllString(out.String(), "$1 [REDACTED]")
}

func opsErrorRecordSensitiveKey(key string) bool {
	return isSensitiveKey(key) || isSensitiveKey(strings.ReplaceAll(strings.ToLower(key), "-", "_"))
}
