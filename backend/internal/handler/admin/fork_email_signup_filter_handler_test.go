package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type forkFilterHandlerRepo struct {
	*settingHandlerRepoStub
	readErr, writeErr error
	setCalls          int
}

func (r *forkFilterHandlerRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *forkFilterHandlerRepo) Set(_ context.Context, key, value string) error {
	r.setCalls++
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func forkFilterHandlerRequest(t *testing.T, h *ForkEmailSignupFilterHandler, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/filter", h.Get)
	r.PUT("/filter", h.Update)
	req := httptest.NewRequest(method, "/filter", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestForkEmailSignupFilterHandler_Get(t *testing.T) {
	for _, tc := range []struct {
		name, raw        string
		missing, invalid bool
	}{
		{"missing", "", true, false},
		{"corrupt", "{", false, true},
		{"null_disabled", `{"trusted_domains":null}`, false, false},
		{"null_enabled", `{"enabled":true,"trusted_domains":null}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &forkFilterHandlerRepo{settingHandlerRepoStub: &settingHandlerRepoStub{values: map[string]string{}}}
			if !tc.missing {
				repo.values[service.SettingKeyForkEmailSignupFilter] = tc.raw
			}
			h := NewForkEmailSignupFilterHandler(service.NewSettingService(repo, nil))
			w := forkFilterHandlerRequest(t, h, http.MethodGet, "")
			require.Equal(t, http.StatusOK, w.Code)
			var reply struct {
				Data forkEmailSignupFilterResponse `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
			require.Equal(t, []string{}, reply.Data.TrustedDomains)
			require.Equal(t, "09:00", reply.Data.WindowStart)
			require.Equal(t, "18:00", reply.Data.WindowEnd)
			require.Equal(t, timezone.Name(), reply.Data.ServerTimezone)
			require.Equal(t, tc.invalid, reply.Data.ConfigInvalid)
			require.Contains(t, w.Body.String(), `"trusted_domains":[]`)
		})
	}
}

func TestForkEmailSignupFilterHandler_Update(t *testing.T) {
	repo := &forkFilterHandlerRepo{settingHandlerRepoStub: &settingHandlerRepoStub{values: map[string]string{"unrelated": "kept"}}}
	h := NewForkEmailSignupFilterHandler(service.NewSettingService(repo, nil))
	w := forkFilterHandlerRequest(t, h, http.MethodPut, `{"enabled":true,"trusted_domains":[" @QQ.COM ","", "qq.com","Gmail.com"],"time_window_enabled":false,"window_start":"09:00","window_end":"18:00"}`)
	require.Equal(t, http.StatusOK, w.Code)
	var reply struct {
		Data forkEmailSignupFilterResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
	require.Equal(t, []string{"qq.com", "gmail.com"}, reply.Data.TrustedDomains)
	require.True(t, reply.Data.Enabled)
	require.False(t, reply.Data.ConfigInvalid)
	require.Equal(t, timezone.Name(), reply.Data.ServerTimezone)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, "kept", repo.values["unrelated"])
	require.Len(t, repo.values, 2)
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyForkEmailSignupFilter]), &stored))
	require.NotContains(t, stored, "server_timezone")
	require.NotContains(t, stored, "config_invalid")
}

func TestForkEmailSignupFilterHandler_Errors(t *testing.T) {
	valid := `{"enabled":true,"trusted_domains":["qq.com"],"window_start":"09:00","window_end":"18:00"}`
	for _, tc := range []struct {
		name, method, body, reason    string
		status                        int
		readErr, writeErr, nilService bool
	}{
		{"invalid_domain", http.MethodPut, `{"enabled":false,"trusted_domains":["bad@domain.com"],"window_start":"09:00","window_end":"18:00"}`, "FORK_EMAIL_SIGNUP_FILTER_INVALID_DOMAIN", 400, false, false, false},
		{"not_json", http.MethodPut, "{", "", 400, false, false, false},
		{"write_failure", http.MethodPut, valid, "FORK_EMAIL_SIGNUP_FILTER_SAVE_FAILED", 500, false, true, false},
		{"read_failure", http.MethodGet, "", "FORK_EMAIL_SIGNUP_FILTER_LOAD_FAILED", 500, true, false, false},
		{"nil_get", http.MethodGet, "", "FORK_EMAIL_SIGNUP_FILTER_LOAD_FAILED", 500, false, false, true},
		{"nil_update", http.MethodPut, valid, "FORK_EMAIL_SIGNUP_FILTER_SAVE_FAILED", 500, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &forkFilterHandlerRepo{settingHandlerRepoStub: &settingHandlerRepoStub{}}
			if tc.readErr {
				repo.readErr = errors.New("isolated read failure")
			}
			if tc.writeErr {
				repo.writeErr = errors.New("isolated write failure")
			}
			var svc *service.SettingService
			if !tc.nilService {
				svc = service.NewSettingService(repo, nil)
			}
			w := forkFilterHandlerRequest(t, NewForkEmailSignupFilterHandler(svc), tc.method, tc.body)
			require.Equal(t, tc.status, w.Code)
			var reply struct {
				Reason   string            `json:"reason"`
				Metadata map[string]string `json:"metadata"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
			require.Equal(t, tc.reason, reply.Reason)
			if tc.name == "invalid_domain" {
				require.Equal(t, "bad@domain.com", reply.Metadata["domain"])
			}
			if tc.status == 400 {
				require.Zero(t, repo.setCalls)
			}
		})
	}
}
