//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthVideoRelayReadsKeepIdentityAndExpiryChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const id = "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	expired := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name, platform, method, path, status, mode string
		expires                                    *time.Time
		want                                       int
	}{
		{"balance exhausted", service.PlatformOpenAI, "GET", "/v1/videos/" + id, service.StatusAPIKeyQuotaExhausted, config.RunModeStandard, nil, http.StatusOK},
		{"active key without balance", service.PlatformOpenAI, "GET", "/v1/videos/" + id, service.StatusActive, config.RunModeStandard, nil, http.StatusOK},
		{"download exhausted", service.PlatformOpenAI, "GET", "/videos/" + id + "/content", service.StatusAPIKeyQuotaExhausted, config.RunModeStandard, nil, http.StatusOK},
		{"submission exhausted", service.PlatformOpenAI, "POST", "/v1/videos", service.StatusAPIKeyQuotaExhausted, config.RunModeStandard, nil, http.StatusTooManyRequests},
		{"alias keeps billing", service.PlatformOpenAI, "GET", "/v1/videos/generations/" + id, service.StatusAPIKeyQuotaExhausted, config.RunModeStandard, nil, http.StatusTooManyRequests},
		{"Grok keeps billing", service.PlatformGrok, "GET", "/v1/videos/" + id, service.StatusAPIKeyQuotaExhausted, config.RunModeStandard, nil, http.StatusTooManyRequests},
		{"disabled", service.PlatformOpenAI, "GET", "/v1/videos/" + id, service.StatusDisabled, config.RunModeStandard, nil, http.StatusUnauthorized},
		{"expired in simple mode", service.PlatformOpenAI, "GET", "/v1/videos/" + id, service.StatusActive, config.RunModeSimple, &expired, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{ID: 21, Status: service.StatusActive, Platform: tc.platform, Hydrated: true}
			user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
			key := &service.APIKey{ID: 31, UserID: user.ID, Key: "video-auth-fixture", GroupID: &group.ID, Group: group, User: user, Status: tc.status, ExpiresAt: tc.expires}
			touches := 0
			repo := &stubApiKeyRepo{
				getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil },
				updateLastUsed: func(context.Context, int64, time.Time) error {
					touches++
					return nil
				},
			}
			cfg := &config.Config{RunMode: tc.mode}
			keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
			ok := func(c *gin.Context) { c.Status(http.StatusOK) }
			router.GET("/v1/videos/:request_id", ok)
			router.GET("/videos/:request_id/content", ok)
			router.GET("/v1/videos/generations/:request_id", ok)
			router.POST("/v1/videos", ok)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("x-api-key", key.Key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Zero(t, touches, "reading a video task must not add a key activity write")
		})
	}
}

func TestAPIKeyAuthVideoRelayAllowsReadsAfterSwitchToSubscription(t *testing.T) {
	group := &service.Group{ID: 42, Status: service.StatusActive, Platform: service.PlatformOpenAI, Hydrated: true,
		SubscriptionType: service.SubscriptionTypeSubscription}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
	key := &service.APIKey{ID: 31, UserID: user.ID, Key: "video-subscription-fixture", GroupID: &group.ID, Group: group,
		User: user, Status: service.StatusActive}
	keyRepo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	subscriptionReads := 0
	subscriptionRepo := &stubUserSubscriptionRepo{
		getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
			subscriptionReads++
			return nil, service.ErrSubscriptionNotFound
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	keys := service.NewAPIKeyService(keyRepo, nil, nil, nil, nil, nil, cfg)
	subscriptions := service.NewSubscriptionService(nil, subscriptionRepo, nil, nil, cfg)
	t.Cleanup(subscriptions.Stop)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, subscriptions, cfg)))
	router.GET("/v1/videos/:request_id/content", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/v1/videos", func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, test := range []struct {
		method string
		path   string
		want   int
		reads  int
	}{
		{http.MethodGet, "/v1/videos/video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/content", http.StatusOK, 0},
		{http.MethodPost, "/v1/videos", http.StatusForbidden, 1},
	} {
		req := httptest.NewRequest(test.method, test.path, nil)
		req.Header.Set("x-api-key", key.Key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, test.want, w.Code, w.Body.String())
		require.Equal(t, test.reads, subscriptionReads)
		if test.method == http.MethodPost {
			require.Contains(t, w.Body.String(), "SUBSCRIPTION_NOT_FOUND")
		}
	}
}
