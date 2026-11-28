package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const videoRelayCollectionPath = "/v1/videos"

type VideoRelayError struct {
	Status         int
	Code           string
	Message        string
	UpstreamStatus int
	ContentRange   string
}

func (e *VideoRelayError) Error() string { return e.Message }

func invalidVideoRelayRequest(message string) *VideoRelayError {
	return &VideoRelayError{Status: http.StatusBadRequest, Code: "invalid_request_error", Message: message}
}

type VideoRelayRequest struct {
	Model       string
	Prompt      string
	Seconds     int
	Resolution  string
	AspectRatio string
	Images      []string
}

func (r VideoRelayRequest) ModerationBody() []byte {
	return (GrokMediaRequestInfo{Prompt: r.Prompt, InputImageURLs: r.Images}).ModerationBody()
}

func ParseVideoRelayRequest(contentType string, body []byte) (VideoRelayRequest, *VideoRelayError) {
	var result VideoRelayRequest
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return result, &VideoRelayError{Status: http.StatusUnsupportedMediaType, Code: "invalid_request_error", Message: "Send the video request as application/json. 请用 JSON 格式提交视频请求。"}
	}
	in, err := decodeVideoRelayObject(body)
	if err != nil {
		return result, invalidVideoRelayRequest("Send one JSON object containing the video settings. 请求体必须是一个 JSON 对象。")
	}
	result.Model, result.Prompt = videoRelayString(in["model"]), videoRelayString(in["prompt"])
	if result.Model == "" || utf8.RuneCountInString(result.Model) > 100 || result.Prompt == "" {
		return result, invalidVideoRelayRequest("Provide a prompt and a model name of at most 100 characters. 请填写提示词和模型名，模型名最多 100 个字符。")
	}
	for field, value := range in {
		switch field {
		case "model", "prompt", "images", "input_reference":
		case "seconds":
			result.Seconds = videoRelayPositiveSeconds(value)
			if result.Seconds == 0 {
				return result, invalidVideoRelayRequest("Provide seconds as a positive whole number. 秒数必须是正整数。")
			}
		case "resolution":
			resolution, ok := LookupVideoBillingResolution(videoRelayString(value))
			if !ok {
				return result, invalidVideoRelayRequest("Choose 480p, 720p, or 1080p for resolution. 分辨率请选择 480p、720p 或 1080p。")
			}
			result.Resolution = resolution
		case "aspect_ratio":
			ratio := videoRelayString(value)
			width, height, ok := strings.Cut(ratio, ":")
			if !ok || !videoRelayPositiveDigits(width) || !videoRelayPositiveDigits(height) {
				return result, invalidVideoRelayRequest("Provide aspect_ratio as two positive whole numbers separated by a colon. 画面比例请写成 16:9 这样的格式。")
			}
			result.AspectRatio = ratio
		case "n":
			if string(bytes.TrimSpace(value)) != "1" {
				return result, invalidVideoRelayRequest("Only one video can be submitted per request. 每次请求只能生成一条视频。")
			}
		default:
			return result, invalidVideoRelayRequest(unsupportedVideoRelayFieldMessage(field))
		}
	}
	image, ok := videoRelayReference(in)
	if !ok {
		return result, invalidVideoRelayRequest("Provide at most one reference image using a valid HTTP or HTTPS URL. 最多提供一张参考图，必须是有效的 HTTP 或 HTTPS 链接。")
	}
	if image != "" {
		result.Images = []string{image}
	}
	return result, nil
}

func unsupportedVideoRelayFieldMessage(field string) string {
	if len(field) > 0 && len(field) <= 64 {
		safe := true
		for _, c := range field {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' {
				continue
			}
			safe = false
			break
		}
		if safe {
			return fmt.Sprintf("The video endpoint does not support the field %q. 视频接口不支持字段「%s」。", field, field)
		}
	}
	return "The request contains a field that the video endpoint does not support. 请求里有视频接口不支持的字段。"
}

func videoRelayReference(in map[string]json.RawMessage) (string, bool) {
	images, hasImages := in["images"]
	reference, hasReference := in["input_reference"]
	if hasImages && hasReference {
		return "", false
	}
	var raw string
	switch {
	case hasImages:
		var values []json.RawMessage
		if err := json.Unmarshal(images, &values); err != nil || len(values) != 1 {
			return "", false
		}
		raw = videoRelayString(values[0])
	case hasReference:
		raw = videoRelayString(reference)
	default:
		return "", true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if port := u.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return "", false
		}
	}
	return u.String(), true
}

func videoRelayPositiveDigits(s string) bool {
	hasNonZero := false
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		hasNonZero = hasNonZero || s[i] != '0'
	}
	return hasNonZero
}

func videoRelayPositiveSeconds(value json.RawMessage) int {
	s := string(bytes.TrimSpace(value))
	if strings.HasPrefix(s, "\"") {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return 0
		}
		s = text
	}
	if !videoRelayPositiveDigits(s) {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0
	}
	return int(n)
}

func videoRelayString(value json.RawMessage) string {
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func decodeVideoRelayObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("expected one JSON object")
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid JSON field")
		}
		if _, duplicate := object[key]; duplicate {
			return nil, errors.New("duplicate JSON field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		object[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after JSON object")
	}
	return object, nil
}

type videoRelayRoute struct {
	account     *Account
	collection  *url.URL
	logURL      string
	bearer      string
	proxyURL    string
	site        string
	credential  string
	fingerprint string
}

func (s *OpenAIGatewayService) videoRelayRoute(account *Account) (*videoRelayRoute, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, errors.New("video route is unavailable")
	}
	base := strings.TrimSpace(account.GetCredential("base_url"))
	// The public validator checks the host and scheme. Video routing preserves
	// the validated configuration's escaped path for both sending and lookup.
	if _, err := s.validateUpstreamBaseURL(base); err != nil {
		return nil, err
	}
	collection, err := canonicalVideoRelayCollectionURL(base)
	if err != nil {
		return nil, err
	}
	rawBearer := account.GetOpenAIProtocolAPIKey()
	bearer := strings.TrimSpace(rawBearer)
	if bearer == "" || strings.ContainsAny(rawBearer, "\r\n") {
		return nil, errors.New("video route credential is unavailable")
	}
	headers := make(http.Header)
	// Bind persisted tasks to configuration, never to a particular user's
	// expanded identity. Keep the original wire casing and ordering so fixed
	// header configurations retain their existing fingerprints.
	for name, value := range account.GetHeaderOverrides() {
		headers[resolveWireCasing(name)] = []string{value}
	}
	deleteHeaderAllForms(headers, "idempotency-key")
	deleteHeaderAllForms(headers, "x-idempotency-key")
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	site, credential := videoRelayFingerprint(collection.String()), videoRelayFingerprint(bearer)
	routeParts := []string{site, credential}
	for _, name := range names {
		routeParts = append(routeParts, strings.ToLower(name), strconv.Itoa(len(headers[name])))
		routeParts = append(routeParts, headers[name]...)
	}
	route := &videoRelayRoute{
		account: account, collection: collection, bearer: bearer,
		logURL: strings.TrimSuffix(collection.String(), videoRelayCollectionPath) + "/api/log/token",
		site:   site, credential: credential, fingerprint: videoRelayFingerprint(routeParts...),
	}
	if account.ProxyID != nil && account.Proxy != nil {
		route.proxyURL = account.Proxy.URL()
	}
	return route, nil
}

func canonicalVideoRelayCollectionURL(base string) (*url.URL, error) {
	u, err := url.Parse(base)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("invalid video base URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	basePath := strings.TrimRight(canonicalVideoRelayEscapedPath(u.EscapedPath()), "/")
	suffix := videoRelayCollectionPath
	if strings.HasSuffix(basePath, "/v1") {
		suffix = "/videos"
	}
	u.RawPath = basePath + suffix
	u.Path, err = url.PathUnescape(u.RawPath)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func canonicalVideoRelayEscapedPath(path string) string {
	if !strings.Contains(path, "%") {
		return path
	}
	var normalized strings.Builder
	normalized.Grow(len(path))
	for i := 0; i < len(path); i++ {
		if path[i] != '%' {
			_ = normalized.WriteByte(path[i])
			continue
		}
		// EscapedPath supplies valid escapes. Decode unreserved bytes while
		// keeping reserved bytes (such as an encoded slash) in the path segment.
		escaped := path[i+1 : i+3]
		value, _ := strconv.ParseUint(escaped, 16, 8)
		b := byte(value)
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
			b >= '0' && b <= '9' || b == '-' || b == '.' || b == '_' || b == '~' {
			_ = normalized.WriteByte(b)
		} else {
			_ = normalized.WriteByte('%')
			_, _ = normalized.WriteString(strings.ToUpper(escaped))
		}
		i += 2
	}
	return normalized.String()
}

func videoRelayFingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = h.Write(length[:])
		_, _ = io.WriteString(h, part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *videoRelayRoute) matches(task *VideoRelayTask) bool {
	return task.SiteFingerprint == r.site && task.CredentialFingerprint == r.credential &&
		task.RouteFingerprint == r.fingerprint
}

func (r *videoRelayRoute) request(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	ctx = WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI)
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), method, target, body)
	if err != nil {
		return nil, err
	}
	r.account.ApplyHeaderOverrides(req.Context(), req.Header)
	deleteHeaderAllForms(req.Header, "idempotency-key")
	deleteHeaderAllForms(req.Header, "x-idempotency-key")
	req.Header.Set("Authorization", "Bearer "+r.bearer)
	req.Header.Set("Accept", "application/json")
	videoRelayForgetReportedHeaders(req.Context(), r.account.ID, "idempotency-key", "x-idempotency-key", "accept")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.GetBody = nil
	}
	return req, nil
}

// These overrides were removed or replaced by the existing video protocol
// rules. They must not be presented as values actually sent to the upstream.
func videoRelayForgetReportedHeaders(ctx context.Context, accountID int64, names ...string) {
	scope := headerOverrideScopeFromContext(ctx)
	if scope == nil {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	for _, name := range names {
		delete(scope.reported[accountID], name)
	}
}

func (r *videoRelayRoute) submitRequest(ctx context.Context, input VideoRelayRequest, task *VideoRelayTask) (*http.Request, error) {
	payload, err := json.Marshal(struct {
		Model       string   `json:"model"`
		Prompt      string   `json:"prompt"`
		Seconds     string   `json:"seconds"`
		Resolution  string   `json:"resolution"`
		AspectRatio string   `json:"aspect_ratio,omitempty"`
		Images      []string `json:"images,omitempty"`
	}{task.UpstreamModel, input.Prompt, strconv.Itoa(task.Seconds), task.Resolution, input.AspectRatio, input.Images})
	if err != nil {
		return nil, err
	}
	return r.request(ctx, http.MethodPost, r.collection.String(), bytes.NewReader(payload))
}

func (r *videoRelayRoute) taskRequest(ctx context.Context, upstreamID string, download bool) (*http.Request, error) {
	if !ValidVideoRelayUpstreamTaskID(upstreamID) {
		return nil, errors.New("invalid video task path")
	}
	target := r.collection.String() + "/" + upstreamID
	if download {
		target += "/content"
	}
	req, err := r.request(ctx, http.MethodGet, target, nil)
	if err == nil && download {
		// Download replaces this header with the caller's validated range.
		videoRelayForgetReportedHeaders(req.Context(), r.account.ID, "range")
	}
	return req, err
}

func readVideoRelayBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("upstream video response is too large")
	}
	return data, nil
}

type videoRelayReply struct {
	TaskID   string
	Status   string
	Progress int
}

func parseVideoRelayReply(body []byte, expectedID string) (videoRelayReply, error) {
	var result videoRelayReply
	object, err := decodeVideoRelayObject(body)
	if err != nil {
		return result, err
	}
	if raw, exists := object["success"]; exists {
		var success bool
		if err := json.Unmarshal(raw, &success); err != nil || !success {
			return result, errors.New("provider returned an unsuccessful video response")
		}
	}
	if hasVideoRelayErrorEnvelope(object) {
		return result, errors.New("provider returned a video error envelope")
	}
	var reportedStatus string
	if raw, exists := object["status"]; exists {
		var status *string
		if err := json.Unmarshal(raw, &status); err != nil || status == nil {
			return result, errors.New("invalid upstream video status")
		}
		reportedStatus = strings.ToLower(strings.TrimSpace(*status))
	}
	failed := reportedStatus == "failed" || reportedStatus == "failure" || reportedStatus == "cancelled" || reportedStatus == "canceled"
	if value := object["error"]; value != nil && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !failed {
		return result, errors.New("provider returned a video error object")
	}
	result.TaskID = expectedID
	for _, name := range []string{"id", "task_id"} {
		if value, exists := object[name]; exists {
			var id string
			if json.Unmarshal(value, &id) != nil || !ValidVideoRelayUpstreamTaskID(id) ||
				result.TaskID != "" && result.TaskID != id {
				return videoRelayReply{}, errors.New("upstream task identity does not match")
			}
			result.TaskID = id
		}
	}
	var progress int
	_ = json.Unmarshal(object["progress"], &progress)
	result.Progress = min(100, max(0, progress))
	switch reportedStatus {
	case "completed", "succeeded", "success":
		delivery := videoRelayString(object["delivery_status"])
		if raw := object["delivery_status"]; raw != nil && string(raw) != "null" {
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return videoRelayReply{}, errors.New("invalid video delivery state")
			}
		}
		switch strings.ToLower(delivery) {
		case "", "ready", "local_ready":
			result.Status = VideoRelayStatusCompleted
		default:
			result.Status = VideoRelayStatusInProgress
		}
	case "failed", "failure", "cancelled", "canceled":
		result.Status = VideoRelayStatusFailed
	case "queued", "pending", "submitted":
		result.Status = VideoRelayStatusQueued
	case "in_progress", "processing", "running":
		result.Status = VideoRelayStatusInProgress
	}
	return result, nil
}

func explicitVideoRelayRejection(status int, savedRequestID string, body []byte) bool {
	if status < 400 || status >= 500 || status == 408 || status == 409 || status == 425 ||
		status == 499 || savedRequestID == "" {
		return false
	}
	object, err := decodeVideoRelayObject(body)
	if err != nil || videoRelayHasTaskField(object) {
		return false
	}
	if raw := bytes.TrimSpace(object["data"]); len(raw) > 0 && raw[0] == '{' {
		data, dataErr := decodeVideoRelayObject(raw)
		if dataErr != nil || videoRelayHasTaskField(data) {
			return false
		}
	}
	if nested, err := decodeVideoRelayObject(object["error"]); err == nil {
		for _, field := range []string{"code", "message", "type"} {
			if videoRelayString(nested[field]) != "" {
				return true
			}
		}
	}
	return hasVideoRelayErrorEnvelope(object)
}

func hasVideoRelayErrorEnvelope(object map[string]json.RawMessage) bool {
	return videoRelayString(object["code"]) != "" && videoRelayString(object["message"]) != "" &&
		bytes.Equal(bytes.TrimSpace(object["data"]), []byte("null"))
}

func videoRelayHasTaskField(object map[string]json.RawMessage) bool {
	return object["id"] != nil || object["task_id"] != nil
}

func parseVideoRelayChargeLogs(body []byte) ([]VideoRelayChargeRecord, error) {
	envelope, err := decodeVideoRelayObject(body)
	var items []json.RawMessage
	if err != nil || !bytes.Equal(bytes.TrimSpace(envelope["success"]), []byte("true")) ||
		json.Unmarshal(envelope["data"], &items) != nil || items == nil {
		return nil, errors.New("upstream video billing records are unavailable")
	}
	var hasMore bool
	var total int
	if raw := envelope["has_more"]; raw != nil && json.Unmarshal(raw, &hasMore) != nil {
		return nil, errors.New("invalid upstream billing record coverage")
	}
	if raw := envelope["total"]; raw != nil && json.Unmarshal(raw, &total) != nil {
		return nil, errors.New("invalid upstream billing record coverage")
	}
	if len(items) >= 1000 || hasMore || total > len(items) {
		return nil, errors.New("upstream video billing records are incomplete")
	}
	records := make([]VideoRelayChargeRecord, 0, len(items))
	for _, item := range items {
		log, err := decodeVideoRelayObject(item)
		var recordType *int
		if err != nil || json.Unmarshal(log["type"], &recordType) != nil || recordType == nil {
			return nil, errors.New("invalid upstream billing record")
		}
		if *recordType != 2 {
			continue
		}
		// Preserve an unparseable consumption entry so the repository can reject
		// an unnumbered match while still checking an unrelated exact request ID.
		var record VideoRelayChargeRecord
		if raw := log["request_id"]; raw != nil && json.Unmarshal(raw, &record.RequestID) != nil {
			return nil, errors.New("invalid upstream billing record request ID")
		}
		if raw := log["model_name"]; raw != nil && json.Unmarshal(raw, &record.Model) != nil {
			return nil, errors.New("invalid upstream billing record model")
		}
		other := log["other"]
		if quoted := videoRelayString(other); quoted != "" {
			other = json.RawMessage(quoted)
		}
		if details, err := decodeVideoRelayObject(other); err == nil {
			_ = json.Unmarshal(details["task_id"], &record.TaskID)
			_ = json.Unmarshal(details["request_path"], &record.Path)
		}
		records = append(records, record)
	}
	return records, nil
}

func (r *videoRelayRoute) queryCharges(ctx context.Context, upstream HTTPUpstream) ([]VideoRelayChargeRecord, error) {
	req, err := r.request(ctx, http.MethodGet, r.logURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := upstream.Do(req, r.proxyURL, r.account.ID, r.account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream video billing log returned HTTP %d", resp.StatusCode)
	}
	body, err := readVideoRelayBody(resp.Body, 8*1024*1024)
	if err != nil {
		return nil, err
	}
	return parseVideoRelayChargeLogs(body)
}
