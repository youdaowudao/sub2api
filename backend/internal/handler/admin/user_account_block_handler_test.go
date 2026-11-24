//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userAccountBlockHandlerSettingsRepo struct {
	settingHandlerRepoStub
	durationReads int
	durationError error
}

func (r *userAccountBlockHandlerSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == "user_account_block_settings" {
		r.durationReads++
		if r.durationError != nil {
			return "", r.durationError
		}
	}
	return r.settingHandlerRepoStub.GetValue(ctx, key)
}

type userAccountBlockHandlerCache struct {
	records map[[2]int64]service.UserAccountBlockRecord
}

func (c *userAccountBlockHandlerCache) SetUserAccountBlock(_ context.Context, record service.UserAccountBlockRecord) error {
	c.records[[2]int64{record.UserID, record.AccountID}] = record
	return nil
}

func (c *userAccountBlockHandlerCache) DeleteUserAccountBlock(_ context.Context, userID, accountID int64) error {
	delete(c.records, [2]int64{userID, accountID})
	return nil
}

func (c *userAccountBlockHandlerCache) ListUserAccountBlocks(context.Context) ([]service.UserAccountBlockRecord, error) {
	items := make([]service.UserAccountBlockRecord, 0, len(c.records))
	for _, val := range c.records {
		items = append(items, val)
	}
	return items, nil
}

func TestUserAccountBlockHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userAccountBlockHandlerSettingsRepo{settingHandlerRepoStub: settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyPromoCodeEnabled: "true",
		"user_account_block_settings":      `{"duration_hours":12}`,
	}}}
	settings := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	handler := NewSettingHandler(settings, nil, nil, nil, nil, nil, nil)
	cache := &userAccountBlockHandlerCache{records: make(map[[2]int64]service.UserAccountBlockRecord)}
	handler.SetUserAccountBlockService(service.NewUserAccountBlockService(cache, settings))
	router := gin.New()
	router.PUT("/settings", handler.UpdateSettings)
	router.GET("/blocks", handler.ListUserAccountBlocks)
	router.POST("/blocks", handler.CreateUserAccountBlock)
	router.DELETE("/blocks/:user_id/:account_id", handler.DeleteUserAccountBlock)

	request := func(method, path, body string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(res, req)
		return res
	}

	t.Run("invalid_ids", func(t *testing.T) {
		res := request(http.MethodPost, "/blocks", `{"user_id":0,"account_id":9}`)
		require.Equal(t, http.StatusBadRequest, res.Code)
		require.Contains(t, res.Body.String(), "USER_ACCOUNT_BLOCK_INVALID_ID")
		res = request(http.MethodDelete, "/blocks/oops/9", "")
		require.Equal(t, http.StatusBadRequest, res.Code)
		require.Contains(t, res.Body.String(), "USER_ACCOUNT_BLOCK_INVALID_ID")
	})

	t.Run("does_not_write_when_duration_read_fails", func(t *testing.T) {
		repo.durationError = errors.New("read failed")
		t.Cleanup(func() { repo.durationError = nil })
		res := request(http.MethodPost, "/blocks", `{"user_id":1,"account_id":9}`)
		require.Equal(t, http.StatusInternalServerError, res.Code)
		require.Contains(t, res.Body.String(), "USER_ACCOUNT_BLOCK_DURATION_READ_FAILED")
		require.Empty(t, cache.records)
	})

	t.Run("create_list_remove", func(t *testing.T) {
		res := request(http.MethodPost, "/blocks", `{"user_id":1,"account_id":9}`)
		require.Equal(t, http.StatusOK, res.Code)
		require.Contains(t, res.Body.String(), `"expires_at":`)
		res = request(http.MethodGet, "/blocks", "")
		require.Contains(t, res.Body.String(), `"account_id":9`)
		res = request(http.MethodDelete, "/blocks/1/9", "")
		require.Equal(t, http.StatusOK, res.Code)
		res = request(http.MethodGet, "/blocks", "")
		require.Contains(t, res.Body.String(), `"items":[]`)
	})

	t.Run("settings_write_uses_exact_key_only", func(t *testing.T) {
		reads := repo.durationReads
		res := request(http.MethodPut, "/settings", `{"user_account_block_duration_hours":6,"USER_ACCOUNT_BLOCK_DURATION_HOURS":7}`)
		require.Equal(t, http.StatusOK, res.Code)
		require.Equal(t, `{"duration_hours":6}`, repo.lastUpdates["user_account_block_settings"])
		require.Contains(t, res.Body.String(), `"user_account_block_duration_hours":6`)
		repo.values["user_account_block_settings"] = `{"duration_hours":12}`
		res = request(http.MethodPut, "/settings", `{"USER_ACCOUNT_BLOCK_DURATION_HOURS":6}`)
		require.Equal(t, http.StatusOK, res.Code)
		require.NotContains(t, repo.lastUpdates, "user_account_block_settings")
		require.Equal(t, `{"duration_hours":12}`, repo.values["user_account_block_settings"])
		for _, raw := range []string{"7", "null"} {
			res = request(http.MethodPut, "/settings", `{"user_account_block_duration_hours":`+raw+`}`)
			require.Equal(t, http.StatusBadRequest, res.Code)
			require.Contains(t, res.Body.String(), "USER_ACCOUNT_BLOCK_INVALID_DURATION")
		}
		require.Equal(t, reads, repo.durationReads)
	})
}
