//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerAffiliateRateRepo struct {
	service.AffiliateRepository
	userID int64
	err    error
}

func (r *handlerAffiliateRateRepo) GetUserRebateRate(_ context.Context, id int64) (*float64, error) {
	r.userID = id
	v := 12.5
	return &v, r.err
}

func TestAffiliateRateHandlerUsesAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		authenticated, enabled, fail bool
		status                       int
	}{
		{"unauthenticated", false, true, false, 401},
		{"disabled", true, false, false, 403},
		{"current user only", true, true, false, 200},
		{"sanitized failure", true, true, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &handlerAffiliateRateRepo{}
			if tc.fail {
				repo.err = errors.New("private SQL details must not escape")
			}
			enabled := "false"
			if tc.enabled {
				enabled = "true"
			}
			settings := service.NewSettingService(&toggleSettingRepo{values: map[string]string{service.SettingKeyAffiliateEnabled: enabled}}, nil)
			h := &UserHandler{affiliateService: service.NewAffiliateService(repo, settings, nil, nil)}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api/v1/user/aff/rate?user_id=99", nil)
			if tc.authenticated {
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
			}
			h.GetAffiliateRebateRate(c)
			require.Equal(t, tc.status, w.Code)
			require.NotContains(t, w.Body.String(), "private SQL")
			if tc.status == 200 {
				require.EqualValues(t, 42, repo.userID)
				var body struct {
					Data map[string]float64 `json:"data"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				require.Equal(t, map[string]float64{"effective_rebate_rate_percent": 12.5}, body.Data)
			} else if !tc.fail {
				require.Zero(t, repo.userID)
			}
		})
	}
}
