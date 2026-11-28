//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAuditRegistrationEmailSearch(t *testing.T) {
	ctx := context.Background()
	repo := NewAuditLogRepository(integrationDB)
	now := time.Now().UTC()
	start := now.Add(-time.Second)
	end := now.Add(time.Second)
	const email = "registration_test%unique@example.com"
	const address = "192.0.2.10"
	add := func(action, method, path, body, actor, ip string, status int) {
		t.Helper()
		require.NoError(t, repo.Insert(ctx, &service.AuditLog{CreatedAt: now, Action: action, Method: method, Path: path, RequestBody: body, ActorEmail: actor, ClientIP: ip, StatusCode: status}))
	}
	register := func(body string, status int) {
		add("auth.register", "POST", "/api/v1/auth/register", body, "", address, status)
	}
	register(service.RedactAuditBody([]byte(`{"email":"registration_test%unique@example.com","password":"fixture-only"}`), "application/json"), 200)
	register(`{"email":"REGISTRATION_TEST%UNIQUE@example.com"}`, 400)
	register(`{"email":"registrationXtestYunique@example.com"}`, 200)
	register(`{"nested":{"email":"registration_test%unique@example.com"}}`, 200)
	register(`{"email":["registration_test%unique@example.com"]}`, 200)
	register(`{"email":null}`, 200)
	register(`{"email":123}`, 200)
	register(`{"email":"registration_test%unique@example.com","other":"\u0000"}`, 200)
	for _, body := range []string{"", "<redacted>", `{"email":"registration_test%unique@example.com"...<truncated>`, `["registration_test%unique@example.com"]`} {
		register(body, 200)
	}
	add("auth.login", "POST", "/api/v1/auth/login", `{"email":"registration_test%unique@example.com"}`, "", address, 200)
	add("auth.login", "POST", "/api/v1/auth/login", "", email, "192.0.2.11", 200)
	list := func(f service.AuditLogFilter) *service.AuditLogList {
		t.Helper()
		f.StartTime = &start
		f.EndTime = &end
		got, err := repo.List(ctx, &f)
		require.NoError(t, err)
		return got
	}
	got := list(service.AuditLogFilter{ActorEmail: email, Page: 1, PageSize: 1})
	require.Equal(t, 3, got.Total)
	require.Len(t, got.Logs, 1)
	firstPageID := got.Logs[0].ID
	got = list(service.AuditLogFilter{ActorEmail: email, Page: 2, PageSize: 1})
	require.Equal(t, 3, got.Total)
	require.Len(t, got.Logs, 1)
	require.NotEqual(t, firstPageID, got.Logs[0].ID)
	got = list(service.AuditLogFilter{Query: email, ClientIP: address, Page: 1, PageSize: 10})
	require.Equal(t, 2, got.Total)
	require.Len(t, got.Logs, 2)
	for _, row := range got.Logs {
		require.Equal(t, address, row.ClientIP)
		require.Empty(t, row.ActorEmail)
	}
	success := true
	got = list(service.AuditLogFilter{ActorEmail: "registration_test%", ClientIP: address, Success: &success})
	require.Equal(t, 1, got.Total)
	require.Equal(t, 200, got.Logs[0].StatusCode)
	detail, err := repo.GetByID(ctx, got.Logs[0].ID)
	require.NoError(t, err)
	require.Contains(t, detail.RequestBody, email)
	require.NotContains(t, detail.RequestBody, "fixture-only")
	got = list(service.AuditLogFilter{ActorEmail: email, Query: "registration_test%", ClientIP: address})
	require.Zero(t, got.Total)
	got = list(service.AuditLogFilter{Query: "registration_test%", ClientIP: address})
	require.Zero(t, got.Total)
	got = list(service.AuditLogFilter{Query: "auth.login"})
	require.Equal(t, 2, got.Total)

	// The user-list shortcut must match exactly without reading detail bodies.
	register(`{"email":"prefix-registration_test%unique@example.com"}`, 200)
	got = list(service.AuditLogFilter{RegistrationEmail: "REGISTRATION_TEST%UNIQUE@example.com"})
	require.Equal(t, 2, got.Total)
	got = list(service.AuditLogFilter{RegistrationEmail: email, Success: &success})
	require.Equal(t, 1, got.Total)
	require.Len(t, got.Logs, 1)
	require.Equal(t, address, got.Logs[0].ClientIP)
	require.Empty(t, got.Logs[0].RequestBody)
	got = list(service.AuditLogFilter{RegistrationEmail: "registration_test%"})
	require.Zero(t, got.Total)
}

// TestAuditRegistrationEmailSearchCost compares the same count+page shape with
// generated audit history, not with production data or a latency threshold.
func TestAuditRegistrationEmailSearchCost(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	marker := "registration-email-cost-fixture"
	// Mostly unrelated operations, with a small share of ordinary registrations.
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO audit_logs(created_at,action,method,path,actor_email,client_ip,request_id,request_body,status_code)
 SELECT $1, CASE WHEN n%1000=0 THEN 'auth.register' ELSE 'auth.login' END,
 'POST', CASE WHEN n%1000=0 THEN '/api/v1/auth/register' ELSE '/api/v1/auth/login' END,
 CASE WHEN n%1000=0 THEN '' ELSE 'other-'||n||'@example.test' END,
 '192.0.2.200',$2, CASE WHEN n%1000=0 THEN '{"email":"cost-fixture@example.test"}' ELSE '{}' END,200
 FROM generate_series(1,20000) n`, now, marker)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM audit_logs WHERE request_id=$1", marker)
		require.NoError(t, err)
	})
	start, end := now.Add(-time.Second), now.Add(time.Second)
	pattern := "%cost-fixture@example.test%"
	oldWhere := " WHERE l.created_at >= $1 AND l.created_at <= $2 AND l.actor_email ILIKE $3"
	oldStart := time.Now()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs l"+oldWhere, start, end, pattern).Scan(&count))
	rows, err := integrationDB.QueryContext(ctx, "SELECT"+auditLogSelectColumns+" FROM audit_logs l"+oldWhere+" ORDER BY l.created_at DESC,l.id DESC LIMIT $4 OFFSET $5", start, end, pattern, 50, 0)
	require.NoError(t, err)
	for rows.Next() {
		_, err = scanAuditLogRow(rows.Scan)
		require.NoError(t, err)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	oldDuration := time.Since(oldStart)
	require.Zero(t, count)
	currentStart := time.Now()
	result, err := NewAuditLogRepository(integrationDB).List(ctx, &service.AuditLogFilter{StartTime: &start, EndTime: &end, ActorEmail: "cost-fixture@example.test", Page: 1, PageSize: 50})
	require.NoError(t, err)
	currentDuration := time.Since(currentStart)
	require.Equal(t, 20, result.Total)
	require.Len(t, result.Logs, 20)
	t.Logf("synthetic %d rows, %d registrations: old count+page=%s new count+page=%s (no production latency inference)", 20000, 20, oldDuration, currentDuration)
}
