//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type registrationIPRouteRepo struct {
	service.SettingRepository
	reads, writes int
	value         string
}

func (r *registrationIPRouteRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != "registration_ip_blacklist" {
		return "", service.ErrSettingNotFound
	}
	r.reads++
	return r.value, nil
}
func (r *registrationIPRouteRepo) Set(_ context.Context, key, value string) error {
	if key != "registration_ip_blacklist" {
		panic("unexpected setting write")
	}
	r.writes++
	r.value = value
	return nil
}

type registrationIPRouteUsers struct{ service.UserRepository }

func (*registrationIPRouteUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	role := service.RoleUser
	if id == 9 {
		role = service.RoleAdmin
	}
	return &service.User{ID: id, Role: role, Status: service.StatusActive}, nil
}
func (*registrationIPRouteUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestRegistrationIPBlacklistAdminRouting(t *testing.T) {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "local-test-only", ExpireHour: 1}}
	repo := &registrationIPRouteRepo{value: `["192.0.2.1"]`}
	settings := service.NewSettingService(repo, cfg)
	settings.SetScopedAdminKeyRepository(&scopedRouteKeys{})
	users := service.NewUserService(&registrationIPRouteUsers{}, nil, nil, nil)
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	scoped, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{Name: "fixture", Enabled: true, Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.read"}, GroupIDs: []int64{1}, AccountTypes: []string{"oauth"}}}}}, 9)
	require.NoError(t, err)
	r := gin.New()
	RegisterAdminRoutes(r.Group("/api/v1"), &handler.Handlers{Admin: &handler.AdminHandlers{Setting: adminhandler.NewSettingHandler(settings, nil, nil, nil, nil, nil, nil)}}, middleware.NewAdminAuthMiddleware(auth, users, settings, nil), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	const path = "/api/v1/admin/settings/registration-ip-blacklist"
	for _, tc := range []struct {
		name   string
		id     int64
		scoped bool
		status int
	}{
		{"admin", 9, false, 200}, {"user", 10, false, 403}, {"restricted", 0, true, 403}, {"anonymous", 0, false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				beforeReads, beforeWrites := repo.reads, repo.writes
				req := httptest.NewRequest(method, path, strings.NewReader(`{"ips":["192.0.2.1"]}`))
				req.Header.Set("Content-Type", "application/json")
				if tc.scoped {
					req.Header.Set("x-api-key", scoped)
				} else if tc.id != 0 {
					user, err := (&registrationIPRouteUsers{}).GetByID(context.Background(), tc.id)
					require.NoError(t, err)
					token, err := auth.GenerateToken(context.Background(), user)
					require.NoError(t, err)
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				require.Equal(t, tc.status, rec.Code)
				if tc.status != 200 {
					require.Equal(t, beforeReads, repo.reads)
					require.Equal(t, beforeWrites, repo.writes)
				}
			}
		})
	}
	require.Equal(t, 1, repo.reads)
	require.Equal(t, 1, repo.writes)
}
