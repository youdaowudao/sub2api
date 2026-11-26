package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type registrationIPSettingRepo struct {
	*settingHandlerRepoStub
	reads, writes int
	writeErr      error
}

func (r *registrationIPSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	r.reads++
	if _, ok := r.values[key]; !ok {
		return "", service.ErrSettingNotFound
	}
	return r.settingHandlerRepoStub.GetValue(ctx, key)
}
func (r *registrationIPSettingRepo) Set(_ context.Context, key, value string) error {
	r.writes++
	if r.writeErr != nil {
		return r.writeErr
	}
	r.values[key] = value
	return nil
}

func TestRegistrationIPBlacklistSettings(t *testing.T) {
	const path = "/api/v1/admin/settings/registration-ip-blacklist"
	const key = "registration_ip_blacklist"
	repo := &registrationIPSettingRepo{settingHandlerRepoStub: &settingHandlerRepoStub{values: map[string]string{service.SettingKeyRegistrationEnabled: "true", "unrelated": "preserved"}}}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)
	r := gin.New()
	r.GET(path, h.GetRegistrationIPBlacklist)
	r.PUT(path, h.UpdateRegistrationIPBlacklist)
	rec := serveEmailBlacklistJSON(t, r, http.MethodGet, path, `{}`)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"ips":[]`)
	require.Equal(t, 1, repo.reads)
	for _, body := range []string{`{}`, `{"ips":null}`, `{"ips":3}`, `{"ips":["192.0.2.1",null]}`, `{"ips":["192.0.2.1",5]}`, `{"ips":["192.0.2.1/24"]}`, `{"ips":["example.com"]}`, `{"ips":["192.0.2.1:80"]}`} {
		rec = serveEmailBlacklistJSON(t, r, http.MethodPut, path, body)
		require.Equal(t, 400, rec.Code, body)
		require.Contains(t, rec.Body.String(), "REGISTRATION_IP_BLACKLIST_INVALID")
		require.Zero(t, repo.writes)
	}
	rec = serveEmailBlacklistJSON(t, r, http.MethodPut, path, `{"ips":[" 192.0.2.1 ","::ffff:192.0.2.1","2001:0db8::1",""]}`)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"ips":["192.0.2.1","2001:db8::1"]`)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, 1, repo.reads)
	require.Equal(t, "true", repo.values[service.SettingKeyRegistrationEnabled])
	require.Equal(t, "preserved", repo.values["unrelated"])
	repo.writeErr = errors.New("write failed")
	rec = serveEmailBlacklistJSON(t, r, http.MethodPut, path, `{"ips":[]}`)
	require.Equal(t, 500, rec.Code)
	require.Equal(t, `["192.0.2.1","2001:db8::1"]`, repo.values[key])
	repo.writeErr = nil
	repo.values[key] = `["192.0.2.1",null]`
	rec = serveEmailBlacklistJSON(t, r, http.MethodGet, path, `{}`)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"ips":null`)
	rec = serveEmailBlacklistJSON(t, r, http.MethodPut, path, `{"ips":[]}`)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "[]", repo.values[key])
}

func TestRegistrationIPBlacklistSettingsWholePageDoesNotWriteKey(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		"registration_ip_blacklist": `["192.0.2.1"]`,
		service.SettingKeySiteName:  "Original",
	})
	rec := doUpdateSettings(t, h, map[string]any{"site_name": "Renamed", "registration_ip_blacklist": []string{}}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `["192.0.2.1"]`, repo.values["registration_ip_blacklist"])
	require.NotContains(t, rec.Body.String(), "registration_ip_blacklist")
}
