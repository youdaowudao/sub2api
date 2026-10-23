package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	pluginUserCreationMaxPageSize = 100
	pluginUserCreationSQLTimeout  = 5 * time.Second
)

type pluginUserCreationRepository struct {
	db *sql.DB
}

func NewPluginUserCreationRepository(db *sql.DB) service.PluginUserCreationStore {
	return &pluginUserCreationRepository{db: db}
}

func (r *pluginUserCreationRepository) RecordCompleted(ctx context.Context, userID int64) error {
	if r == nil || r.db == nil {
		return errors.New("user creation completion database is not configured")
	}
	if userID <= 0 {
		return errors.New("user creation completion requires a positive user ID")
	}
	queryCtx, cancel := context.WithTimeout(ctx, pluginUserCreationSQLTimeout)
	defer cancel()

	// Use the root database, never the transaction from the creation context.
	// The no-value-change conflict update returns the existing fact even when a
	// concurrent insert committed after this statement's snapshot was taken.
	// It leaves the first completion time, creation time, and source untouched.
	var recordedID int64
	err := r.db.QueryRowContext(queryCtx, `
		INSERT INTO sub2api_plugin_user_creations (user_id, created_at, completed_at, signup_source)
		SELECT id, created_at, NOW(), signup_source
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		ON CONFLICT (user_id) DO UPDATE
		SET user_id = EXCLUDED.user_id
		RETURNING user_id
	`, userID).Scan(&recordedID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("record completed user creation: user %d is missing or soft-deleted", userID)
	}
	if err != nil {
		return fmt.Errorf("record completed user creation: %w", err)
	}
	return nil
}

func (r *pluginUserCreationRepository) List(ctx context.Context, after, through int64, limit int, includeEmail bool) (*service.PluginUserCreationPage, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("user creation completion database is not configured")
	}
	if after < 0 || through < 0 {
		return nil, errors.New("user creation scan cursors must not be negative")
	}
	if limit <= 0 || limit > pluginUserCreationMaxPageSize {
		limit = pluginUserCreationMaxPageSize
	}
	queryCtx, cancel := context.WithTimeout(ctx, pluginUserCreationSQLTimeout)
	defer cancel()
	if through == 0 {
		if err := r.db.QueryRowContext(queryCtx, `
			SELECT COALESCE(MAX(user_id), 0) FROM sub2api_plugin_user_creations
		`).Scan(&through); err != nil {
			return nil, fmt.Errorf("fix user creation scan upper bound: %w", err)
		}
	}
	page := &service.PluginUserCreationPage{
		Items:         make([]service.PluginUserCreation, 0, limit),
		NextUserID:    after,
		ThroughUserID: through,
		Done:          true,
	}
	if after >= through {
		return page, nil
	}

	query := `SELECT c.user_id, c.created_at, c.completed_at, c.signup_source`
	if includeEmail {
		query += `, COALESCE(u.email, '')`
	}
	query += ` FROM sub2api_plugin_user_creations c`
	if includeEmail {
		query += ` LEFT JOIN users u ON u.id = c.user_id AND u.deleted_at IS NULL`
	}
	query += ` WHERE c.user_id > $1 AND c.user_id <= $2 ORDER BY c.user_id ASC LIMIT $3`
	rows, err := r.db.QueryContext(queryCtx, query, after, through, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list completed user creations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item service.PluginUserCreation
		values := []any{&item.UserID, &item.CreatedAt, &item.CompletedAt, &item.SignupSource}
		if includeEmail {
			values = append(values, &item.Email)
		}
		if err := rows.Scan(values...); err != nil {
			return nil, fmt.Errorf("read completed user creation: %w", err)
		}
		if len(page.Items) == limit {
			page.Done = false
			break
		}
		item.EventID = "user.creation_completed:" + strconv.FormatInt(item.UserID, 10)
		item.CreatedAt = item.CreatedAt.UTC()
		item.CompletedAt = item.CompletedAt.UTC()
		page.Items = append(page.Items, item)
		page.NextUserID = item.UserID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list completed user creations: %w", err)
	}
	return page, nil
}
