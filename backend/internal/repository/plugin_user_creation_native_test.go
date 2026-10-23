package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// These tests use only the explicitly supplied, project-owned native database.
// They do not start services or use the container-backed integration TestMain.
func openPluginCreationNativeDB(t *testing.T) *sql.DB {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("SUB2API_TEST_PLUGIN_DATABASE_DSN_FILE"))
	if path == "" {
		t.Skip("SUB2API_TEST_PLUGIN_DATABASE_DSN_FILE is not set; skipping native plugin database test")
	}
	cwd, err := os.Getwd()
	require.NoError(t, err)
	projectRoot := cwd
	for {
		if info, statErr := os.Stat(filepath.Join(projectRoot, "backend", "go.mod")); statErr == nil && !info.IsDir() {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			t.Fatal("cannot establish the project root for the native database test")
		}
		projectRoot = parent
	}
	projectRoot, err = filepath.EvalSymlinks(projectRoot)
	require.NoError(t, err)
	path, err = filepath.Abs(path)
	require.NoError(t, err)
	path, err = filepath.EvalSymlinks(path)
	require.NoError(t, err)
	relative, err := filepath.Rel(projectRoot, path)
	require.NoError(t, err)
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		t.Fatal("native plugin database DSN file must resolve inside this project")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read the explicitly supplied native database DSN file")
	}
	if len(data) == 0 || len(data) > 16*1024 {
		t.Fatal("native plugin database DSN file has an invalid size")
	}
	dsn := strings.TrimSpace(string(data))
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		dsn, err = pq.ParseURL(dsn)
		if err != nil {
			t.Fatal("cannot parse the native plugin database DSN (connection details withheld)")
		}
	}
	options, err := pluginCreationNativeDSNOptions(dsn)
	if err != nil {
		// Parse and connection errors may include a DSN; never print them.
		t.Fatal("cannot parse the native plugin database DSN (connection details withheld)")
	}
	if !strings.HasPrefix(options["dbname"], "sub2api_plugin_") || !pluginCreationLoopbackHost(options["host"]) {
		t.Fatal("native plugin database must have a sub2api_plugin_ name and a loopback host")
	}
	// lib/pq accepts a single host; comma-separated/fallback hosts fail the
	// loopback check above. Reject service indirection and alternate addresses.
	if options["service"] != "" || options["servicefile"] != "" || options["hostaddr"] != "" {
		t.Fatal("native plugin database must not use indirect connection hosts")
	}
	connector, err := pq.NewConnector(dsn + " connect_timeout=3")
	if err != nil {
		t.Fatal("cannot configure the native plugin database connection (connection details withheld)")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("cannot connect to the explicitly supplied native plugin database (connection details withheld)")
	}
	var database string
	var address sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT current_database(), host(inet_server_addr())`).Scan(&database, &address); err != nil {
		t.Fatal("cannot verify native plugin database ownership")
	}
	if database != options["dbname"] || !strings.HasPrefix(database, "sub2api_plugin_") || !address.Valid || !pluginCreationLoopbackHost(address.String) {
		t.Fatal("connected native plugin database does not match the required isolated loopback database")
	}
	var ready bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT to_regclass('public.users') IS NOT NULL
		   AND to_regclass('public.sub2api_plugin_user_creations') IS NOT NULL
	`).Scan(&ready))
	require.True(t, ready, "the isolated database must already include the project migrations through 246")
	return db
}

var pluginCreationNativeDSNOptionPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*('(?:\\.|[^'\\])*'|(?:\\.|[^\s\\'])*)(?:\s+|$)`)

// Parse the existing lib/pq keyword form only for ownership checks. The driver
// still parses and uses the original DSN. URI input is normalized by pq.ParseURL.
func pluginCreationNativeDSNOptions(dsn string) (map[string]string, error) {
	options := make(map[string]string)
	for remaining := strings.TrimSpace(dsn); remaining != ""; {
		match := pluginCreationNativeDSNOptionPattern.FindStringSubmatchIndex(remaining)
		if match == nil {
			return nil, fmt.Errorf("invalid native database connection options")
		}
		key, value := remaining[match[2]:match[3]], remaining[match[4]:match[5]]
		if _, exists := options[key]; exists {
			return nil, fmt.Errorf("duplicate native database connection options")
		}
		if strings.HasPrefix(value, "'") {
			value = value[1 : len(value)-1]
		}
		var decoded strings.Builder
		for i := 0; i < len(value); i++ {
			if value[i] == '\\' {
				i++
			}
			decoded.WriteByte(value[i])
		}
		options[key] = decoded.String()
		remaining = remaining[match[1]:]
	}
	return options, nil
}

func pluginCreationLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

type pluginCreationNativeFixture struct {
	db     *sql.DB
	store  service.PluginUserCreationStore
	prefix string
	ids    []int64
}

func newPluginCreationNativeFixture(t *testing.T) *pluginCreationNativeFixture {
	t.Helper()
	db := openPluginCreationNativeDB(t)
	var suffix [8]byte
	_, err := rand.Read(suffix[:])
	require.NoError(t, err)
	fixture := &pluginCreationNativeFixture{
		db:     db,
		store:  NewPluginUserCreationRepository(db),
		prefix: fmt.Sprintf("plugin-creation-native-%x-", suffix),
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, id := range fixture.ids {
			// Match both the exact ID and this test's private email prefix.
			_, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND email LIKE $2`, id, fixture.prefix+"%")
			if err != nil {
				t.Errorf("cleanup of this test's native user failed: %v", err)
			}
		}
	})
	return fixture
}

type pluginCreationNativeRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (f *pluginCreationNativeFixture) createUser(t *testing.T, ctx context.Context, query pluginCreationNativeRowQuerier) int64 {
	t.Helper()
	var id int64
	email := f.prefix + strconv.Itoa(len(f.ids)) + "@example.invalid"
	require.NoError(t, query.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, role, balance, concurrency, status, signup_source)
		VALUES ($1, 'native-plugin-test-no-login', 'user', 0, 1, 'active', 'email')
		RETURNING id
	`, email).Scan(&id))
	f.ids = append(f.ids, id)
	return id
}

func (f *pluginCreationNativeFixture) completionCount(t *testing.T, ctx context.Context, id int64) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sub2api_plugin_user_creations WHERE user_id = $1`, id).Scan(&count))
	return count
}

func TestPluginUserCreationNativeUncommittedAndRolledBackUsers(t *testing.T) {
	f := newPluginCreationNativeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	id := f.createUser(t, ctx, tx)
	require.Error(t, f.store.RecordCompleted(ctx, id), "the independent completion SQL must not see an uncommitted user")
	require.Zero(t, f.completionCount(t, ctx, id))
	require.NoError(t, tx.Rollback())
	require.Error(t, f.store.RecordCompleted(ctx, id), "a rolled-back user must not acquire a completion")
	require.Zero(t, f.completionCount(t, ctx, id))
	var users int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, id).Scan(&users))
	require.Zero(t, users)
}

func TestPluginUserCreationNativeIdempotenceAndEmailAccess(t *testing.T) {
	f := newPluginCreationNativeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := f.createUser(t, ctx, f.db)
	require.NoError(t, f.store.RecordCompleted(ctx, id))
	first, err := f.store.List(ctx, id-1, id, 1, false)
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.Empty(t, first.Items[0].Email)
	require.Equal(t, "user.creation_completed:"+strconv.FormatInt(id, 10), first.Items[0].EventID)
	require.Equal(t, "email", first.Items[0].SignupSource)
	require.False(t, first.Items[0].CompletedAt.IsZero())
	var createdAt time.Time
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id = $1`, id).Scan(&createdAt))
	require.True(t, createdAt.Equal(first.Items[0].CreatedAt))

	currentEmail := f.prefix + "changed@example.invalid"
	_, err = f.db.ExecContext(ctx, `UPDATE users SET email = $2, signup_source = 'oidc' WHERE id = $1`, id, currentEmail)
	require.NoError(t, err)
	require.NoError(t, f.store.RecordCompleted(ctx, id))
	require.Equal(t, 1, f.completionCount(t, ctx, id))
	current, err := f.store.List(ctx, id-1, id, 1, true)
	require.NoError(t, err)
	require.Len(t, current.Items, 1)
	require.Equal(t, currentEmail, current.Items[0].Email)
	require.Equal(t, first.Items[0].SignupSource, current.Items[0].SignupSource)
	require.True(t, first.Items[0].CreatedAt.Equal(current.Items[0].CreatedAt))
	require.True(t, first.Items[0].CompletedAt.Equal(current.Items[0].CompletedAt))
	withoutEmail, err := f.store.List(ctx, id-1, id, 1, false)
	require.NoError(t, err)
	require.Len(t, withoutEmail.Items, 1)
	require.Empty(t, withoutEmail.Items[0].Email)

	_, err = f.db.ExecContext(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, id)
	require.NoError(t, err)
	deleted, err := f.store.List(ctx, id-1, id, 1, true)
	require.NoError(t, err)
	require.Len(t, deleted.Items, 1)
	require.Empty(t, deleted.Items[0].Email)
	_, err = f.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	require.NoError(t, err)
	require.Zero(t, f.completionCount(t, ctx, id), "hard deletion must cascade to the completion fact")
}

func TestPluginUserCreationNativeBoundedRoundsRecoverLateCompletion(t *testing.T) {
	f := newPluginCreationNativeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	low := f.createUser(t, ctx, f.db)
	firstVisible := f.createUser(t, ctx, f.db)
	upper := f.createUser(t, ctx, f.db)
	require.NoError(t, f.store.RecordCompleted(ctx, firstVisible))
	require.NoError(t, f.store.RecordCompleted(ctx, upper))
	first, err := f.store.List(ctx, low-1, 0, 1, false)
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.Equal(t, firstVisible, first.Items[0].UserID)
	require.Equal(t, upper, first.ThroughUserID)
	require.False(t, first.Done)

	newer := f.createUser(t, ctx, f.db)
	require.NoError(t, f.store.RecordCompleted(ctx, newer))
	require.NoError(t, f.store.RecordCompleted(ctx, low))
	last, err := f.store.List(ctx, first.NextUserID, first.ThroughUserID, 1, false)
	require.NoError(t, err)
	require.Len(t, last.Items, 1)
	require.Equal(t, upper, last.Items[0].UserID)
	require.Equal(t, first.ThroughUserID, last.ThroughUserID)
	require.True(t, last.Done, "new higher IDs must not extend an already bounded scan round")

	seen := make(map[int64]bool)
	after, through := int64(0), int64(0)
	for pages := 0; ; pages++ {
		require.Less(t, pages, 1000, "a bounded round must terminate")
		page, err := f.store.List(ctx, after, through, 1000, false)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Items), 100)
		if through == 0 {
			through = page.ThroughUserID
		}
		require.Equal(t, through, page.ThroughUserID)
		for _, item := range page.Items {
			seen[item.UserID] = true
		}
		if page.Done {
			break
		}
		require.Greater(t, page.NextUserID, after)
		after = page.NextUserID
	}
	require.True(t, seen[low], "resetting the next round to zero must recover the lower ID completed late")
	require.True(t, seen[newer], "a later round must also discover records beyond the old upper bound")
}

func TestPluginUserCreationNativeCompletionWriteTimeoutPreservesAccount(t *testing.T) {
	f := newPluginCreationNativeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := f.createUser(t, ctx, f.db)
	locker, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = locker.Rollback() }()
	_, err = locker.ExecContext(ctx, `LOCK TABLE sub2api_plugin_user_creations IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)

	shortCtx, shortCancel := context.WithTimeout(service.WithUserCreationCapture(ctx, f.store), 150*time.Millisecond)
	defer shortCancel()
	service.TrackUserCreation(shortCtx, id)
	started := time.Now()
	err = service.CompleteUserCreation(shortCtx, id)
	require.Error(t, err)
	require.Equal(t, "USER_CREATION_NOT_CONFIRMED", infraerrors.Reason(err))
	require.Less(t, time.Since(started), 2*time.Second)
	require.NoError(t, locker.Rollback())

	var users int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&users))
	require.Equal(t, 1, users, "completion storage failure must not compensate the saved account")
	require.Zero(t, f.completionCount(t, ctx, id))
}
