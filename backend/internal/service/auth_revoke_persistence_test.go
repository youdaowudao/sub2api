//go:build unit

package service

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type revocationPersistUsers struct {
	UserRepository
	version int64
	err     error
	fields  []UserUpdateFields
}

func (r *revocationPersistUsers) Update(_ context.Context, _ *User, fields UserUpdateFields) error {
	r.fields = append(r.fields, fields)
	if r.err != nil {
		return r.err
	}
	r.version++
	return nil
}

type revocationRefreshCache struct {
	RefreshTokenCache
	err   error
	calls int
	data  *RefreshTokenData
}

func (r *revocationRefreshCache) DeleteUserRefreshTokens(context.Context, int64) error {
	r.calls++
	return r.err
}

func (r *revocationRefreshCache) GetRefreshToken(context.Context, string) (*RefreshTokenData, error) {
	copy := *r.data
	return &copy, nil
}

func (r *revocationRefreshCache) DeleteTokenFamily(context.Context, string) error { return nil }

func TestRevokeAllUserTokensPersistsBeforeBothCleanupAttempts(t *testing.T) {
	for _, tt := range []struct {
		name              string
		dbFail, cacheFail bool
		mcpFail           bool
		wantReason        string
		wantVersion       int64
		wantCleanupCalls  int
	}{
		{name: "success", wantVersion: 13, wantCleanupCalls: 1},
		{name: "persistence fails", dbFail: true, wantVersion: 12, wantReason: "SESSION_REVOCATION_UNCONFIRMED"},
		{name: "refresh cleanup fails", cacheFail: true, wantVersion: 13, wantCleanupCalls: 1, wantReason: "SESSION_CLEANUP_INCOMPLETE"},
		{name: "MCP cleanup fails", mcpFail: true, wantVersion: 13, wantCleanupCalls: 1, wantReason: "SESSION_CLEANUP_INCOMPLETE"},
		{name: "both cleanup attempts fail", cacheFail: true, mcpFail: true, wantVersion: 13, wantCleanupCalls: 1, wantReason: "SESSION_CLEANUP_INCOMPLETE"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dbErr, cacheErr, mcpErr := errors.New("private database detail"), errors.New("private cache detail"), errors.New("private MCP storage detail")
			users := &revocationPersistUsers{version: 12}
			cache := &revocationRefreshCache{}
			revoker := &mcpGrantRevokerStub{}
			if tt.dbFail {
				users.err = dbErr
			}
			if tt.cacheFail {
				cache.err = cacheErr
			}
			if tt.mcpFail {
				revoker.err = mcpErr
			}
			auth := &AuthService{userRepo: users, refreshTokenCache: cache, mcpGrantRevoker: revoker}
			err := auth.RevokeAllUserTokens(context.Background(), 7)
			if tt.wantReason == "" {
				require.NoError(t, err)
			} else {
				require.Equal(t, tt.wantReason, infraerrors.Reason(err))
				require.Equal(t, 503, infraerrors.Code(err))
				require.NotContains(t, infraerrors.Message(err), "private")
			}
			if tt.dbFail {
				require.ErrorIs(t, err, dbErr)
			}
			if tt.cacheFail {
				require.ErrorIs(t, err, cacheErr)
			}
			if tt.mcpFail {
				require.ErrorIs(t, err, mcpErr)
			}
			require.Equal(t, tt.wantVersion, users.version)
			require.Equal(t, []UserUpdateFields{{IncrementTokenVersion: true}}, users.fields)
			require.Equal(t, tt.wantCleanupCalls, cache.calls)
			require.Len(t, revoker.userIDs, tt.wantCleanupCalls)
		})
	}
}

func TestRevokeAllUserTokensInvalidatesCredentialsWhenCleanupFails(t *testing.T) {
	f := newMCPOAuthFixture(t)
	before := resolvedTokenVersion(f.u.user)
	old, err := f.s.Token(context.Background(), f.code(t, MCPScopeUser, nil), mcpTestResource)
	require.NoError(t, err)
	cache := &revocationRefreshCache{err: errors.New("cleanup unavailable"), data: &RefreshTokenData{
		UserID: 7, TokenVersion: before, FamilyID: "fixture-family", ExpiresAt: time.Now().Add(time.Hour),
	}}
	auth := &AuthService{userRepo: f.u, refreshTokenCache: cache, mcpGrantRevoker: &mcpGrantRevokerStub{err: errors.New("MCP cleanup unavailable")}}
	err = auth.RevokeAllUserTokens(context.Background(), 7)
	require.Equal(t, "SESSION_CLEANUP_INCOMPLETE", infraerrors.Reason(err))
	_, err = f.s.Authenticate(context.Background(), old.AccessToken)
	require.ErrorIs(t, err, ErrMCPOAuthInvalidToken)
	_, err = f.s.Token(context.Background(), MCPOAuthTokenInput{GrantType: "refresh_token", ClientID: f.client.ClientID, RefreshToken: old.RefreshToken, Resource: mcpTestResource}, mcpTestResource)
	requireMCPOAuthError(t, err, "invalid_grant")
	_, err = auth.RefreshTokenPair(context.Background(), refreshTokenPrefix+"old-fixture")
	require.ErrorIs(t, err, ErrTokenRevoked)
	_, err = f.s.Token(context.Background(), f.code(t, MCPScopeUser, nil), mcpTestResource)
	require.NoError(t, err, "a newly authenticated version may authorize again")
}

type revocationAuthorizationRepo struct {
	MCPOAuthRepository
	beforeCreate func()
}

func (r *revocationAuthorizationRepo) CreateAuthorization(ctx context.Context, grant *MCPOAuthGrant, code *MCPOAuthCode, limit int) error {
	r.beforeCreate()
	return r.MCPOAuthRepository.CreateAuthorization(ctx, grant, code, limit)
}

func TestMCPOAuthRejectsStaleAuthenticatedVersion(t *testing.T) {
	f := newMCPOAuthFixture(t)
	oldVersion := resolvedTokenVersion(f.u.user)
	f.u.user.TokenVersion++ // Revoke after JWT auth, before the service reloads the user.
	_, err := f.s.Decide(context.Background(), 7, oldVersion, MCPOAuthConsentInput{Authorization: f.authorize, Approved: true}, mcpTestResource)
	require.ErrorIs(t, err, ErrTokenRevoked)
	require.Empty(t, f.r.grants)
}

func TestMCPOAuthRevocationBetweenValidationAndInsertLeavesNoUsableGrant(t *testing.T) {
	f := newMCPOAuthFixture(t)
	version := resolvedTokenVersion(f.u.user)
	f.s = NewMCPOAuthService(&revocationAuthorizationRepo{MCPOAuthRepository: f.r, beforeCreate: func() { f.u.user.TokenVersion++ }}, f.u)
	redirect, err := f.s.Decide(context.Background(), 7, version, MCPOAuthConsentInput{Authorization: f.authorize, Approved: true}, mcpTestResource)
	require.NoError(t, err)
	callback, err := url.Parse(redirect)
	require.NoError(t, err)
	for _, grant := range f.r.grants {
		require.Equal(t, version, grant.TokenVersion)
	}
	_, err = f.s.Token(context.Background(), MCPOAuthTokenInput{
		GrantType: "authorization_code", ClientID: f.client.ClientID, Code: callback.Query().Get("code"),
		RedirectURI: f.authorize.RedirectURI, CodeVerifier: f.verifier, Resource: mcpTestResource,
	}, mcpTestResource)
	requireMCPOAuthError(t, err, "invalid_grant")
}
