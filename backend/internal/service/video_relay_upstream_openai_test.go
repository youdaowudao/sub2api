//go:build unit

package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoRelayRequestValidation(t *testing.T) {
	const base = `"model":"demo-video","prompt":"Waves"`
	for name, field := range map[string]string{
		"long model":        `"model":"` + strings.Repeat("浪", 101) + `","prompt":"Waves"`,
		"oversized seconds": base + `,"seconds":"2147483648"`,
		"fraction seconds":  base + `,"seconds":5.1`,
		"null seconds":      base + `,"seconds":null`,
		"bad resolution":    base + `,"resolution":"4k"`,
		"bad ratio":         base + `,"aspect_ratio":"0:9"`,
		"unknown field":     base + `,"unknown":1`,
		"size":              base + `,"size":"864x480"`,
		"duration":          base + `,"duration":5`,
		"multiple videos":   base + `,"n":2`,
		"both inputs":       base + `,"images":["https://fixture.test/image.png"],"input_reference":"https://fixture.test/image.png"`,
		"empty image":       base + `,"images":[""]`,
		"empty path":        base + `,"input_reference":"https://"`,
		"port above range":  base + `,"input_reference":"https://fixture.test:65536/image.png"`,
		"port zero":         base + `,"input_reference":"https://fixture.test:0/image.png"`,
		"two images":        base + `,"images":["https://fixture.test/1.png","https://fixture.test/2.png"]`,
		"non-web image":     base + `,"input_reference":"file:///fixture.png"`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseVideoRelayRequest("application/json", []byte("{"+field+"}"))
			require.NotNil(t, err)
			require.Equal(t, http.StatusBadRequest, err.Status)
		})
	}
	_, err := ParseVideoRelayRequest("multipart/form-data", []byte("not JSON"))
	require.NotNil(t, err)
	require.Equal(t, http.StatusUnsupportedMediaType, err.Status)

	for _, image := range []string{
		`"input_reference":"HTTPS://fixture.test/image.png"`,
		`"images":["HTTPS://fixture.test/image.png"]`,
	} {
		request, err := ParseVideoRelayRequest("application/json; charset=utf-8",
			[]byte("{"+base+`,"seconds":"30","resolution":"480p","aspect_ratio":"16:9","n":1,`+image+"}"))
		require.Nil(t, err)
		require.Equal(t, 30, request.Seconds)
		require.Equal(t, []string{"https://fixture.test/image.png"}, request.Images)
		var moderation struct {
			Images []map[string]string `json:"images"`
		}
		require.NoError(t, json.Unmarshal(request.ModerationBody(), &moderation))
		require.Equal(t, request.Images[0], moderation.Images[0]["image_url"])
	}
}

func TestVideoRelayReplyStatesAndTaskIdentity(t *testing.T) {
	for input, expected := range map[string]string{
		"completed": VideoRelayStatusCompleted, "succeeded": VideoRelayStatusCompleted, "success": VideoRelayStatusCompleted,
		"failed": VideoRelayStatusFailed, "failure": VideoRelayStatusFailed, "cancelled": VideoRelayStatusFailed, "canceled": VideoRelayStatusFailed,
		"queued": VideoRelayStatusQueued, "pending": VideoRelayStatusQueued, "submitted": VideoRelayStatusQueued,
		"in_progress": VideoRelayStatusInProgress, "processing": VideoRelayStatusInProgress, "running": VideoRelayStatusInProgress,
		"unrecognized": "",
	} {
		t.Run(input, func(t *testing.T) {
			reply, err := parseVideoRelayReply([]byte(`{"id":"original","task_id":"original","status":"`+strings.ToUpper(input)+`"}`), "original")
			require.NoError(t, err)
			require.Equal(t, expected, reply.Status)
		})
	}
	reply, err := parseVideoRelayReply([]byte(`{"id":"original","status":"completed","delivery_status":"archiving","progress":99}`), "original")
	require.NoError(t, err)
	require.Equal(t, VideoRelayStatusInProgress, reply.Status)
	require.Equal(t, 99, reply.Progress)
	_, err = parseVideoRelayReply([]byte(`{"id":"other","status":"completed"}`), "original")
	require.Error(t, err)
	_, err = parseVideoRelayReply([]byte(`{"task_id":"other","status":"failed"}`), "original")
	require.Error(t, err)
	_, err = parseVideoRelayReply([]byte(`{"id":"first","task_id":"second","status":"queued"}`), "")
	require.Error(t, err)
	for _, body := range []string{
		`{"success":false,"id":"original","status":"completed"}`,
		`{"code":"denied","message":"fixture rejection","data":null,"id":"original","status":"queued"}`,
	} {
		reply, err := parseVideoRelayReply([]byte(body), "original")
		require.Error(t, err)
		require.Empty(t, reply.TaskID)
	}
}

func TestVideoRelayExplicitRejectionContract(t *testing.T) {
	const withError = `{"error":{"message":"fixture rejection"}}`
	for _, status := range []int{400, 401, 403, 429} {
		require.True(t, explicitVideoRelayRejection(status, "request-id", []byte(withError)))
	}
	for _, status := range []int{302, 408, 409, 425, 499, 500} {
		require.False(t, explicitVideoRelayRejection(status, "request-id", []byte(withError)))
	}
	require.False(t, explicitVideoRelayRejection(400, "", []byte(withError)))
	require.True(t, explicitVideoRelayRejection(400, "request-id", []byte(`{"code":"fail","message":"fixture","data":null}`)))
	for _, body := range []string{
		`{}`, `{"error":{}}`, `{"error":"failure"}`, `{"error":{"message":" "}}`,
		`{"error":`, `<html>failure</html>`, `{"code":"fail","message":"fixture"}`,
		`{"error":{"type":"fixture"},"id":""}`,
		`{"error":{"type":"fixture"},"task_id":null}`,
		`{"error":{"type":"fixture"},"task_id":"../fixture"}`,
		`{"error":{"type":"fixture"},"id":"a","task_id":"b"}`,
		`{"error":{"type":"fixture"},"data":{"task_id":"nested"}}`,
		`{"error":{"type":"fixture"},"data":{"task_id":"first","task_id":"second"}}`,
	} {
		require.False(t, explicitVideoRelayRejection(400, "request-id", []byte(body)), body)
	}
}

func TestVideoRelayLogRawCoverage(t *testing.T) {
	const consume = `{"type":2,"request_id":"request-id","model_name":"upstream-model","other":"{\"task_id\":\"fixture-task\",\"request_path\":\"/tenant/v1/videos\"}"}`
	for _, rowCount := range []int{999, 1000} {
		body := `{"success":true,"data":[` + consume + strings.Repeat(`,{"type":1}`, rowCount-1) + `]}`
		logs, err := parseVideoRelayChargeLogs([]byte(body))
		if rowCount == 999 {
			require.NoError(t, err)
			require.Equal(t, []VideoRelayChargeRecord{{RequestID: "request-id", TaskID: "fixture-task", Model: "upstream-model", Path: "/tenant/v1/videos"}}, logs)
		} else {
			require.Error(t, err)
			require.Empty(t, logs)
		}
	}
	for _, body := range []string{
		`{"success":false,"data":[]}`,
		`{"success":true,"data":null}`,
		`{"success":true,"data":[` + consume + `],"has_more":true}`,
		`{"success":true,"data":[` + consume + `],"total":2}`,
		`{"success":false,"success":true,"data":[` + consume + `]}`,
		`{"success":true,"data":[{"type":2,"request_id":"first","request_id":"second"}]}`,
	} {
		_, err := parseVideoRelayChargeLogs([]byte(body))
		require.Error(t, err)
	}
	conflictingTask := `{"success":true,"data":[{"type":2,"request_id":"request-id","model_name":"upstream-model","other":{"task_id":"first","task_id":"second","request_path":"/v1/videos"}}]}`
	logs, err := parseVideoRelayChargeLogs([]byte(conflictingTask))
	require.NoError(t, err)
	require.Len(t, logs, 1, "keep malformed consumption as evidence against an unnumbered match")
	require.Empty(t, logs[0].TaskID, "a conflicting record cannot supply an upstream task ID")
}
