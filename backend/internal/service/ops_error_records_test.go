package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpsErrorRecordMessageRedaction(t *testing.T) {
	const secret = "fixture-credential-not-real-871"
	for _, raw := range []string{
		`{"error":{"message":"503 model-a quota 250","api_key":"` + secret + `"}}`,
		`{"message":"authorization: Bearer ` + secret + `"}`,
		`{"api-key":"` + secret + `"}`,
		`{"nested":[{"private-key":"` + secret + `","access-token":"` + secret + `"}]}`,
		"Authorization: Bearer " + secret,
		"proxy-authorization: Basic " + secret,
		"api_key='" + secret + "' password=" + secret,
		"https://example.test/error?key=" + secret + "&password=" + secret + "&model=model-a",
		`{"message":"api_key=\"` + secret + `\""}`,
		"-----BEGIN PRIVATE KEY-----\n" + secret + "\n-----END PRIVATE KEY-----",
		`{"password":"` + secret,
		"password='" + secret,
	} {
		t.Run(raw[:min(len(raw), 24)], func(t *testing.T) {
			got := sanitizeOpsErrorRecordMessage(raw)
			require.NotContains(t, got, secret)
			require.NotEmpty(t, got)
		})
	}
	got := sanitizeOpsErrorRecordMessage(`{"error":{"code":"quota_exhausted","message":"model-a limit 250"},"max_tokens":4096,"input_tokens":123}`)
	for _, safe := range []string{"quota_exhausted", "model-a", "250", "4096", "123"} {
		require.Contains(t, got, safe)
	}
	require.LessOrEqual(t, len(sanitizeOpsErrorRecordMessage(strings.Repeat("错误", 2000))), 2048)
}

type opsErrorRecordsProjectionRepo struct {
	OpsRepository
	result      *OpsErrorRecordsList
	failure     error
	sawDeadline bool
}

func (r *opsErrorRecordsProjectionRepo) ListErrorRecords(ctx context.Context, _ *OpsErrorRecordsFilter) (*OpsErrorRecordsList, error) {
	deadline, ok := ctx.Deadline()
	r.sawDeadline = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	return r.result, r.failure
}
func (r *opsErrorRecordsProjectionRepo) ListErrorRecordMembers(ctx context.Context, f *OpsErrorRecordsFilter, _ int64) (*OpsErrorRecordsList, error) {
	return r.ListErrorRecords(ctx, f)
}
func TestOpsErrorRecordsServiceProjectionAndFailures(t *testing.T) {
	start, end := time.Now().Add(-time.Hour), time.Now()
	f := &OpsErrorRecordsFilter{OpsErrorLogFilter: OpsErrorLogFilter{StartTime: &start, EndTime: &end}}
	repo := &opsErrorRecordsProjectionRepo{result: &OpsErrorRecordsList{Items: []*OpsErrorRecord{
		{OpsErrorLog: &OpsErrorLog{ID: 1, Message: `{"api_key":"fixture-not-real-secret","max_tokens":4096}`}},
		{Representative: &OpsErrorLog{ID: 2, Message: "Authorization: Bearer fixture-not-real-secret"}},
	}}}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	result, err := svc.GetErrorRecords(context.Background(), f)
	require.NoError(t, err)
	require.True(t, repo.sawDeadline)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fixture-not-real-secret")
	require.Contains(t, string(encoded), "4096")
	repo.failure = fmt.Errorf("private database fixture-not-real-secret")
	_, err = svc.GetErrorRecords(context.Background(), f)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "fixture-not-real-secret")
	repo.failure = context.DeadlineExceeded
	_, err = svc.GetErrorRecords(context.Background(), f)
	require.Error(t, err)
	require.Contains(t, err.Error(), "查询超时")
	repo.failure = sql.ErrNoRows
	_, err = svc.GetErrorRecordMembers(context.Background(), f, 123)
	require.Error(t, err)
	require.Contains(t, err.Error(), "记录已不存在")
}
