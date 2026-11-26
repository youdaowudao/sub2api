//go:build unit

package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegistrationIPBlacklistOAuthOIDCFastPath(t *testing.T) {
	h, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{
		settingValues: map[string]string{"registration_ip_blacklist": `["192.0.2.1"]`},
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/callback", nil)
	ctx := service.WithSessionBinding(req.Context(), &service.SessionBinding{IP: "192.0.2.1"})
	c.Request = req.WithContext(ctx)
	handled := h.tryOIDCVerifiedEmailFastPath(c, "/auth/oidc/callback", "/dashboard", service.PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: "https://issuer.example.com", ProviderSubject: "blocked-fixture"}, "blocked@example.com", "blocked", nil)
	require.True(t, handled)
	require.Equal(t, http.StatusFound, recorder.Code)
	location, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)
	values, err := url.ParseQuery(location.Fragment)
	require.NoError(t, err)
	require.Equal(t, "REGISTRATION_IP_BLOCKED", values.Get("error"))
	require.Empty(t, values.Get("access_token"))
	require.Empty(t, values.Get("refresh_token"))
	count, err := client.User.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRegistrationIPBlacklistOAuthExistingVerifiedEmail(t *testing.T) {
	for _, provider := range []string{"google", "github", "oidc"} {
		t.Run(provider, func(t *testing.T) {
			h, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{settingValues: map[string]string{"registration_ip_blacklist": `["192.0.2.1"]`}})
			ctx := service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: "192.0.2.1"})
			existing, err := client.User.Create().SetEmail("existing@example.com").SetUsername("existing").SetPasswordHash("fixture").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
			require.NoError(t, err)
			tokens, user, err := h.authService.LoginOrRegisterVerifiedEmailOAuth(ctx, service.EmailOAuthIdentityInput{ProviderType: provider, ProviderSubject: "existing-fixture", Email: existing.Email, EmailVerified: true})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			require.Equal(t, existing.ID, user.ID)
			n, err := client.User.Query().Count(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, n)
		})
	}
}

func TestRegistrationIPBlacklistOAuthPendingJSON(t *testing.T) {
	const address = "fixture-new@example.com"
	cache := &oauthPendingFlowEmailCacheStub{verificationCodes: map[string]*service.VerificationCodeData{
		address: {Code: "246810", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
	}}
	h, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{
		settingValues: map[string]string{"registration_ip_blacklist": `["192.0.2.1"]`}, emailVerifyEnabled: true, emailCache: cache,
	})
	ctx := service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: "192.0.2.1"})
	session, err := client.PendingAuthSession.Create().SetSessionToken("registration-ip-fixture").SetIntent("login").SetProviderType("oidc").SetProviderKey("https://issuer.example").SetProviderSubject("registration-ip-blocked").SetBrowserSessionKey("registration-ip-browser").SetUpstreamIdentityClaims(map[string]any{"username": "fixture"}).SetRedirectTo("/profile").SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/oidc/create-account", bytes.NewBufferString(`{"email":"fixture-new@example.com","verify_code":"246810","password":"fixture-password","adopt_display_name":false,"adopt_avatar":false}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	req.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("registration-ip-browser")})
	c.Request = req
	h.CreateOIDCOAuthAccount(c)
	require.Equal(t, http.StatusForbidden, rec.Code)
	payload := decodeJSONBody(t, rec)
	require.Equal(t, "REGISTRATION_IP_BLOCKED", payload["reason"])
	require.NotContains(t, payload, "access_token")
	count, err := client.User.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	stored, err := client.PendingAuthSession.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Nil(t, stored.ConsumedAt)
}
