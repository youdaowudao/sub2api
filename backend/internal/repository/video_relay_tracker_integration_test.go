//go:build integration

package repository

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayRepoStageOneUpgrade(t *testing.T) {
	ctx := context.Background()
	admin := videoRelayIndependentDB(t)
	databaseName := "video_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedName := pq.QuoteIdentifier(databaseName)
	_, err := admin.ExecContext(ctx, "CREATE DATABASE "+quotedName)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.ExecContext(ctx, "DROP DATABASE "+quotedName)
		require.NoError(t, err)
	})
	dsn := integrationDSN + " dbname=" + databaseName
	if strings.HasPrefix(integrationDSN, "postgres://") || strings.HasPrefix(integrationDSN, "postgresql://") {
		connectionURL, err := url.Parse(integrationDSN)
		if err != nil {
			t.Fatal("could not parse the isolated PostgreSQL fixture address")
		}
		connectionURL.Path = "/" + databaseName
		dsn = connectionURL.String()
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	oldMigrations := fstest.MapFS{}
	files, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	for _, name := range files {
		if name > "255_video_relay_tasks.sql" {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, name)
		require.NoError(t, err)
		oldMigrations[name] = &fstest.MapFile{Data: data}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, oldMigrations))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO users (email,password_hash,balance,frozen_balance)
		VALUES ('upgrade@example.com','test-only',23,5) RETURNING id`).Scan(&userID))
	type oldRow struct {
		id, state, status, reason, upstreamID string
		eligible, sent                        bool
	}
	rows := []oldRow{
		{id: videoRelayFixtureID(), state: "held", status: "submitting", eligible: true},
		{id: videoRelayFixtureID(), state: "refunded", status: "failed", reason: "unconfirmed_unresolvable", eligible: true, sent: true},
		{id: videoRelayFixtureID(), state: "refunded", status: "failed", reason: "api_key_deleted", upstreamID: "..", eligible: true, sent: true},
		{id: videoRelayFixtureID(), state: "refunded", status: "failed", reason: "user_deleted", upstreamID: "valid-42", sent: true},
		{id: videoRelayFixtureID(), state: "refunded", status: "failed", reason: "upstream_rejected", sent: true},
		{id: videoRelayFixtureID(), state: "charged", status: "completed", upstreamID: "old-42", sent: true},
	}
	for _, row := range rows {
		_, err := db.ExecContext(ctx, `
			INSERT INTO video_relay_tasks (
				task_id,user_id,api_key_id,group_id,account_id,model,upstream_model,
				seconds,resolution,total_cost,hold_amount,rate_multiplier,
				upstream_site_fingerprint,upstream_credential_fingerprint,upstream_route_fingerprint,
				status,billing_state,failure_reason,upstream_task_id,sent_at,settled_at
			) VALUES ($1,$2,101,102,103,'video-public','video-fixture',5,'480p',5,5,1,
				repeat('a',64),repeat('b',64),repeat('c',64),$3,$4,$5,$6,
				CASE WHEN $7 THEN clock_timestamp()-interval '12 minutes' END,
				CASE WHEN $4 <> 'held' THEN clock_timestamp()-interval '10 minutes' END)
		`, row.id, userID, row.status, row.state, row.reason, row.upstreamID, row.sent)
		require.NoError(t, err)
	}

	require.NoError(t, ApplyMigrations(ctx, db))
	checkHistory := func() {
		for _, row := range rows {
			var state, status, reason, upstreamID string
			var scheduled bool
			var hold float64
			require.NoError(t, db.QueryRowContext(ctx, `
				SELECT billing_state,status,failure_reason,upstream_task_id,hold_amount,next_check_at IS NOT NULL
				FROM video_relay_tasks WHERE task_id=$1`, row.id).
				Scan(&state, &status, &reason, &upstreamID, &hold, &scheduled),
				"upgraded orders must expose the scheduler column without dropping old billing data")
			require.Equal(t, row.eligible, scheduled, "unexpected scheduling for %q / %q", row.state, row.reason)
			require.Equal(t, row.state, state)
			require.Equal(t, row.status, status)
			require.Equal(t, row.reason, reason)
			require.Equal(t, row.upstreamID, upstreamID)
			require.Equal(t, 5.0, hold)
		}
		var balance, frozen float64
		require.NoError(t, db.QueryRowContext(ctx, "SELECT balance,frozen_balance FROM users WHERE id=$1", userID).Scan(&balance, &frozen))
		require.Equal(t, 23.0, balance)
		require.Equal(t, 5.0, frozen)
	}
	checkHistory()
	require.NoError(t, ApplyMigrations(ctx, db))
	checkHistory()

	// A terminal row rejected by the provider is ineligible even if it carries
	// an old due time. The migration backfill alone cannot enforce this.
	_, err = db.ExecContext(ctx, "UPDATE video_relay_tasks SET next_check_at=clock_timestamp() WHERE task_id=$1", rows[4].id)
	require.NoError(t, err)
	tracker := NewVideoRelayRepository(db)
	var claimed []string
	for range 3 {
		task, err := tracker.ClaimDue(ctx)
		require.NoError(t, err)
		require.NotNil(t, task)
		claimed = append(claimed, task.TaskID)
	}
	require.ElementsMatch(t, []string{rows[0].id, rows[1].id, rows[2].id}, claimed)
	empty, err := tracker.ClaimDue(ctx)
	require.NoError(t, err)
	require.Nil(t, empty)
}
