//go:build unit

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserTokenVersionNativeMigrationConcurrencyAndRevocation(t *testing.T) {
	// Reuse the existing explicit DSN ownership checks; this never discovers a
	// default database or starts a service. Both old and migrated schemas work.
	db := openPluginCreationNativeDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	migrationSnapshot := func() map[string]string {
		rows, err := db.QueryContext(ctx, `SELECT filename, checksum FROM schema_migrations ORDER BY filename`)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		result := map[string]string{}
		for rows.Next() {
			var name, checksum string
			require.NoError(t, rows.Scan(&name, &checksum))
			result[name] = checksum
		}
		require.NoError(t, rows.Err())
		return result
	}
	before := migrationSnapshot()
	var id int64
	email := fmt.Sprintf("token-version-%d@example.test", time.Now().UnixNano())
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users (email,password_hash,balance,concurrency,status,role,notes)
		VALUES ($1,'native-fixture-hash',12.5,7,'active','user','retain this note') RETURNING id`, email).Scan(&id))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, err := db.ExecContext(cleanupCtx, `DELETE FROM users WHERE id=$1`, id)
		require.NoError(t, err)
	})
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "native-revocation-fixture-key", ExpireHour: 1}}
	oldUser := &service.User{ID: id, Email: email, PasswordHash: "native-fixture-hash", Role: service.RoleUser, Status: service.StatusActive}
	signer := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	oldToken, err := signer.GenerateToken(ctx, oldUser)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, db))
	after := migrationSnapshot()
	for name, checksum := range before {
		require.Equal(t, checksum, after[name], "old migration %s must not change", name)
	}
	if _, existed := before["250_user_token_version.sql"]; existed {
		require.Len(t, after, len(before))
	} else {
		require.Len(t, after, len(before)+1)
	}
	require.NotEmpty(t, after["250_user_token_version.sql"])
	require.NoError(t, ApplyMigrations(ctx, db), "the new migration must be idempotent")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newUserRepositoryWithSQL(client, db)
	users := service.NewUserService(repo, nil, nil, nil)
	loaded, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Zero(t, loaded.TokenVersion)
	claims, err := signer.ValidateToken(oldToken)
	require.NoError(t, err)
	normalized, err := users.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, claims.TokenVersion, normalized.TokenVersion, "migration alone must preserve old tokens")
	stale := *loaded
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			copy := stale
			errs <- repo.Update(ctx, &copy, service.UserUpdateFields{IncrementTokenVersion: true})
		}()
		go func() {
			defer wg.Done()
			_, err := repo.AdjustBalance(ctx, id, 1)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	stale.Username = "profile changed after revocation"
	require.NoError(t, repo.Update(ctx, &stale, service.UserUpdateFields{Username: true}))
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(workers), got.TokenVersion, "all concurrent revocations must persist and survive a stale profile save")
	require.InDelta(t, 12.5+workers, got.Balance, 1e-9)
	require.Equal(t, 7, got.Concurrency)
	require.Equal(t, "retain this note", got.Notes)
	require.Equal(t, email, got.Email)
	auth := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err = auth.RefreshToken(ctx, oldToken)
	require.ErrorIs(t, err, service.ErrTokenRevoked)
	oauth := service.NewMCPOAuthService(NewMCPOAuthRepository(client, db), repo)
	auth.SetMCPGrantRevoker(oauth)
	const resource = "https://native.example.test/api/v1/mcp"
	registered, err := oauth.Register(ctx, service.MCPOAuthRegisterInput{ClientName: "Native regression", RedirectURIs: []string{"https://native.example.test/callback"}})
	require.NoError(t, err)
	verifier := strings.Repeat("n", 43)
	digest := sha256.Sum256([]byte(verifier))
	input := service.MCPOAuthAuthorizeInput{ResponseType: "code", ClientID: registered.ClientID, RedirectURI: registered.RedirectURIs[0],
		Resource: resource, CodeChallenge: base64.RawURLEncoding.EncodeToString(digest[:]), CodeChallengeMethod: "S256"}
	current, err := users.GetByID(ctx, id)
	require.NoError(t, err)
	redirect, err := oauth.Decide(ctx, id, current.TokenVersion, service.MCPOAuthConsentInput{Authorization: input, Approved: true}, resource)
	require.NoError(t, err)
	callback, err := url.Parse(redirect)
	require.NoError(t, err)
	pair, err := oauth.Token(ctx, service.MCPOAuthTokenInput{GrantType: "authorization_code", ClientID: registered.ClientID,
		Code: callback.Query().Get("code"), RedirectURI: input.RedirectURI, CodeVerifier: verifier, Resource: resource}, resource)
	require.NoError(t, err)
	_, err = oauth.Authenticate(ctx, pair.AccessToken)
	require.NoError(t, err)
	require.NoError(t, auth.RevokeAllUserTokens(ctx, id))
	_, err = oauth.Authenticate(ctx, pair.AccessToken)
	require.ErrorIs(t, err, service.ErrMCPOAuthInvalidToken)
	_, err = oauth.Token(ctx, service.MCPOAuthTokenInput{GrantType: "refresh_token", ClientID: registered.ClientID, RefreshToken: pair.RefreshToken, Resource: resource}, resource)
	require.Error(t, err)
	_, err = oauth.Decide(ctx, id, current.TokenVersion, service.MCPOAuthConsentInput{Authorization: input, Approved: true}, resource)
	require.ErrorIs(t, err, service.ErrTokenRevoked)
	newLogin, err := users.GetByID(ctx, id)
	require.NoError(t, err)
	_, err = oauth.Decide(ctx, id, newLogin.TokenVersion, service.MCPOAuthConsentInput{Authorization: input, Approved: true}, resource)
	require.NoError(t, err)
	final, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(workers+1), final.TokenVersion)
	require.InDelta(t, 12.5+workers, final.Balance, 1e-9)
	// Changing credentials must still invalidate tokens while preserving the
	// independent explicit-revocation counter.
	beforePassword, err := auth.GenerateToken(ctx, final)
	require.NoError(t, err)
	final.PasswordHash = "changed-native-fixture-hash"
	require.NoError(t, repo.Update(ctx, final, service.UserUpdateFields{PasswordHash: true}))
	_, err = auth.RefreshToken(ctx, beforePassword)
	require.ErrorIs(t, err, service.ErrTokenRevoked)
	afterPassword, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	beforeEmail, err := auth.GenerateToken(ctx, afterPassword)
	require.NoError(t, err)
	afterPassword.Email = "changed-" + email
	require.NoError(t, repo.Update(ctx, afterPassword, service.UserUpdateFields{Email: true}))
	_, err = auth.RefreshToken(ctx, beforeEmail)
	require.ErrorIs(t, err, service.ErrTokenRevoked)
	afterEmail, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(workers+1), afterEmail.TokenVersion)
}
