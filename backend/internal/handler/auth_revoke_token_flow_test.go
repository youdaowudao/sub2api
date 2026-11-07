//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// Store copies so loading/normalizing a user cannot mutate persisted state.
type revokedTokenFlowUsers struct {
	service.UserRepository
	user *service.User
}

func (r *revokedTokenFlowUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	if id != r.user.ID {
		return nil, service.ErrUserNotFound
	}
	copy := *r.user
	return &copy, nil
}

func (r *revokedTokenFlowUsers) Update(_ context.Context, _ *service.User, fields service.UserUpdateFields) error {
	if fields.IsEmpty() {
		return errors.New("revocation must request a persistent update")
	}
	// Only the revocation endpoint writes through this fixture. Real SQL's
	// column mask and atomic increment are covered by repository regressions.
	r.user.TokenVersion++
	return nil
}

func (r *revokedTokenFlowUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *revokedTokenFlowUsers) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

type revokedTokenFlowMCP struct{ handlerMCPRepo }

func (r *revokedTokenFlowMCP) RevokeUserGrants(_ context.Context, _ int64, now time.Time) error {
	if r.grant != nil {
		r.grant.RevokedAt = &now
	}
	return nil
}

func TestRevokeAllSessionsRejectsOldJWTAndMCPAuthorization(t *testing.T) {
	for _, noSID := range []bool{false, true} {
		name := "current token"
		if noSID {
			name = "legacy token without sid"
		}
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			users := &revokedTokenFlowUsers{user: &service.User{
				ID: 7, Email: "revocation@example.test", PasswordHash: "fixture-password-hash",
				Role: service.RoleUser, Status: service.StatusActive, Concurrency: 3,
			}}
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "revocation-test-signing-key", ExpireHour: 1}, MCP: config.MCPConfig{PublicURL: handlerMCPResource}}
			refresh := &userHandlerRefreshTokenCacheStub{}
			auth := service.NewAuthService(nil, users, nil, refresh, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			userService := service.NewUserService(users, nil, nil, nil)
			repo := &revokedTokenFlowMCP{}
			oauth := service.NewMCPOAuthService(repo, users)
			auth.SetMCPGrantRevoker(oauth)
			client, err := oauth.Register(context.Background(), service.MCPOAuthRegisterInput{
				ClientName: "Revocation regression", RedirectURIs: []string{"https://client.example.com/callback?workspace=one"},
			})
			require.NoError(t, err)
			body := handlerMCPBody(t, service.MCPOAuthConsentInput{
				Authorization: handlerMCPAuthorization(client.ClientID), Approved: true, Scope: service.MCPScopeUser,
			})
			router := gin.New()
			group := router.Group("/api/v1", gin.HandlerFunc(middleware.NewJWTAuthMiddleware(auth, userService, nil, nil)))
			group.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })
			group.POST("/auth/revoke-all-sessions", (&AuthHandler{authService: auth}).RevokeAllSessions)
			group.POST("/user/mcp/authorize", NewMCPOAuthHandler(oauth, nil, userService, nil, cfg).Decide)
			request := func(method, path, token, content string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(content))
				req.Header.Set("Authorization", "Bearer "+token)
				if content != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			old, err := auth.GenerateToken(context.Background(), users.user)
			require.NoError(t, err)
			if noSID {
				claims, parseErr := auth.ValidateToken(old)
				require.NoError(t, parseErr)
				claims.SessionID = ""
				old, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWT.Secret))
				require.NoError(t, err)
			}
			require.Equal(t, http.StatusOK, request(http.MethodGet, "/api/v1/protected", old, "").Code)
			require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/v1/user/mcp/authorize", old, body).Code)
			require.Equal(t, 1, repo.writes)
			require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/v1/auth/revoke-all-sessions", old, "").Code)
			require.Equal(t, []int64{7}, refresh.revokedUserIDs)
			require.NotNil(t, repo.grant.RevokedAt)
			t.Run("old JWT cannot access", func(t *testing.T) {
				require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/v1/protected", old, "").Code)
			})
			t.Run("old JWT cannot authorize MCP", func(t *testing.T) {
				require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/api/v1/user/mcp/authorize", old, body).Code)
				require.Equal(t, 1, repo.writes, "revoked JWT must not create a new grant")
			})
			t.Run("legacy refresh cannot restore old JWT", func(t *testing.T) {
				_, refreshErr := auth.RefreshToken(context.Background(), old)
				require.ErrorIs(t, refreshErr, service.ErrTokenRevoked)
			})
			fresh, err := auth.GenerateToken(context.Background(), users.user)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, request(http.MethodGet, "/api/v1/protected", fresh, "").Code)
			require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/v1/user/mcp/authorize", fresh, body).Code)
		})
	}
}
