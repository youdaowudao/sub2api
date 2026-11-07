//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type revokeFailureUsers struct {
	revokedTokenFlowUsers
	err error
}

func (r *revokeFailureUsers) Update(ctx context.Context, user *service.User, fields service.UserUpdateFields) error {
	if r.err != nil {
		return r.err
	}
	return r.revokedTokenFlowUsers.Update(ctx, user, fields)
}

type revokeFailureRefresh struct {
	service.RefreshTokenCache
	err error
}

func (r *revokeFailureRefresh) DeleteUserRefreshTokens(context.Context, int64) error { return r.err }

type revokeFailureMCP struct{ err error }

func (r *revokeFailureMCP) RevokeAllGrants(context.Context, int64) error { return r.err }

func TestAuthHandlerRevokeAllSessionsReportsActualFailureStep(t *testing.T) {
	for _, tt := range []struct {
		name, reason, message string
		dbFail                bool
	}{
		{"database write", "SESSION_REVOCATION_UNCONFIRMED", "会话撤销结果未能确认。请重新登录核对后重试。", true},
		{"record cleanup", "SESSION_CLEANUP_INCOMPLETE", "旧登录凭据已失效，但部分会话清理未完成。请重新登录后重试。", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			users := &revokeFailureUsers{revokedTokenFlowUsers: revokedTokenFlowUsers{user: &service.User{ID: 7}}}
			cache := &revokeFailureRefresh{}
			if tt.dbFail {
				users.err = errors.New("private persistence detail")
			} else {
				cache.err = errors.New("private cache detail")
			}
			auth := service.NewAuthService(nil, users, nil, cache, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil)
			auth.SetMCPGrantRevoker(&revokeFailureMCP{})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/auth/revoke-all-sessions", nil)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
			(&AuthHandler{authService: auth}).RevokeAllSessions(c)
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			var response struct{ Reason, Message string }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, tt.reason, response.Reason)
			require.Equal(t, tt.message, response.Message)
			require.NotContains(t, w.Body.String(), "private")
		})
	}
}

func TestRevokeAllSessionsInvalidatesAdminAndOptionalJWT(t *testing.T) {
	users := &revokedTokenFlowUsers{user: &service.User{ID: 7, Role: service.RoleAdmin, Status: service.StatusActive, Email: "admin@example.test", PasswordHash: "fixture"}}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "revocation-path-test-key", ExpireHour: 1}}
	auth := service.NewAuthService(nil, users, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(users, nil, nil, nil)
	router := gin.New()
	protected := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.GET("/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, userService, nil, nil)), protected)
	router.GET("/optional", gin.HandlerFunc(middleware.NewOptionalJWTAuthMiddleware(auth, userService, nil, nil)), protected)
	request := func(path, token string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	old, err := auth.GenerateToken(context.Background(), users.user)
	require.NoError(t, err)
	for _, path := range []string{"/admin", "/optional"} {
		require.Equal(t, http.StatusOK, request(path, old))
	}
	require.NoError(t, auth.RevokeAllUserTokens(context.Background(), 7))
	for _, path := range []string{"/admin", "/optional"} {
		require.Equal(t, http.StatusUnauthorized, request(path, old))
	}
	fresh, err := auth.GenerateToken(context.Background(), users.user)
	require.NoError(t, err)
	for _, path := range []string{"/admin", "/optional"} {
		require.Equal(t, http.StatusOK, request(path, fresh))
	}
	require.Equal(t, http.StatusOK, request("/optional", ""), "anonymous optional route remains available")
}

func TestMCPOAuthHandlerRejectsMissingAuthenticatedVersion(t *testing.T) {
	h, repo, _, _ := newHandlerMCPFixture(t)
	input := service.MCPOAuthConsentInput{Authorization: handlerMCPAuthorization(repo.clientID), Approved: true}
	c, w := handlerMCPContext(t, http.MethodPost, "/user/mcp/authorize", handlerMCPBody(t, input), "application/json")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	c.Set(middleware.ContextKeySessionID, "insufficient-without-authenticated-version")
	h.Decide(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Zero(t, repo.writes)
}
