//go:build unit

package service

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayAccountStaysDedicatedWhenDisabled(t *testing.T) {
	settings := map[string]any{"resolutions": []any{"720p", "sd"}, "min_seconds": 5, "max_seconds": 30, "default_seconds": 5}
	account := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://fixture.test", "api_key": "fixture-only",
			"openai_capabilities": []string{"videos"}, OpenAIVideoRelayCredentialsKey: settings,
		},
	}
	require.NoError(t, NormalizeOpenAIVideoRelayCredentials(account.Platform, account.Type, account.Credentials))
	require.True(t, account.IsOpenAIVideoRelayDedicated())
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
	config, ok := account.OpenAIVideoRelaySettings()
	require.True(t, ok)
	require.Equal(t, []string{"480p", "720p"}, config.Resolutions)
	for _, cap := range []OpenAIEndpointCapability{"", OpenAIEndpointCapabilitySeedance, OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityEmbeddings} {
		require.False(t, account.SupportsOpenAIEndpointCapability(cap), cap)
	}
	require.False(t, account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic))
	delete(account.Credentials, "openai_capabilities")
	require.True(t, account.IsOpenAIVideoRelayDedicated())
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
	require.False(t, account.SupportsOpenAIEndpointCapability(""))
}

func TestVideoRelayAccountRejectsInvalidVideoSettings(t *testing.T) {
	for name, settings := range map[string]any{
		"null":                 nil,
		"no resolutions":       map[string]any{"resolutions": []string{}, "min_seconds": 5, "default_seconds": 5, "max_seconds": 30},
		"unknown resolution":   map[string]any{"resolutions": []string{"4k"}, "min_seconds": 5, "default_seconds": 5, "max_seconds": 30},
		"minimum above max":    map[string]any{"resolutions": []string{"480p"}, "min_seconds": 35, "default_seconds": 20, "max_seconds": 30},
		"default out of range": map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "default_seconds": 45, "max_seconds": 30},
		"fractional seconds":   map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "default_seconds": 5.5, "max_seconds": 30},
		"overflow":             map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "default_seconds": 5, "max_seconds": 2147483648},
	} {
		t.Run(name, func(t *testing.T) {
			credentials := map[string]any{
				"base_url": "https://fixture.test", "openai_capabilities": []any{"videos"}, OpenAIVideoRelayCredentialsKey: settings,
			}
			require.Error(t, NormalizeOpenAIVideoRelayCredentials(PlatformOpenAI, AccountTypeAPIKey, credentials))
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: credentials}
			require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
		})
	}
}

type videoRelayAccountAdminRepo struct {
	AccountRepository
	saved  Account
	writes int
}

func (r *videoRelayAccountAdminRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	copy := r.saved
	copy.Credentials = maps.Clone(copy.Credentials)
	return &copy, nil
}

func (r *videoRelayAccountAdminRepo) Update(_ context.Context, account *Account) error {
	r.saved = *account
	r.writes++
	return nil
}

func TestVideoRelayAccountEditPreservesDedicatedSettingsAndRejectsBulkWrites(t *testing.T) {
	ctx := context.Background()
	settings := map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "max_seconds": 30, "default_seconds": 5}
	repo := &videoRelayAccountAdminRepo{saved: Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://fixture.test", "api_key": "fixture-only", "openai_capabilities": []string{"videos"},
			"model_mapping": map[string]string{"old": "old-mapped"}, OpenAIVideoRelayCredentialsKey: settings,
		},
	}}
	admin := &adminServiceImpl{accountRepo: repo}
	for _, credentials := range []map[string]any{nil, {}} {
		updated, err := admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Credentials: credentials})
		require.NoError(t, err)
		require.True(t, updated.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
		require.Contains(t, updated.Credentials, "model_mapping")
	}
	updated, err := admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Credentials: map[string]any{"base_url": "https://fixture.test"}})
	require.NoError(t, err)
	require.NotContains(t, updated.Credentials, "openai_capabilities")
	require.NotContains(t, updated.Credentials, "model_mapping")
	require.True(t, updated.IsOpenAIVideoRelayDedicated())
	require.False(t, updated.SupportsOpenAIEndpointCapability(""))
	kept, valid := updated.OpenAIVideoRelaySettings()
	require.True(t, valid)
	require.Equal(t, 30, kept.MaxSeconds)
	countBeforeReject := repo.writes
	_, err = admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Credentials: map[string]any{
		"base_url": "https://fixture.test", OpenAIVideoRelayCredentialsKey: nil,
	}})
	require.Error(t, err)
	for _, value := range []any{nil, map[string]any{"min_seconds": -1}} {
		_, err = admin.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: map[string]any{OpenAIVideoRelayCredentialsKey: value}})
		require.ErrorContains(t, err, "请在单个账号的编辑页面修改视频设置")
	}
	require.Equal(t, countBeforeReject, repo.writes)
	require.True(t, repo.saved.IsOpenAIVideoRelayDedicated())
}

func TestVideoRelayAccountConnectionTestAndProbeAvoidNonVideoRequests(t *testing.T) {
	for _, capabilities := range [][]string{nil, {"videos"}} {
		account := Account{
			ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials: map[string]any{
				"base_url": "https://fixture.test", "api_key": "fixture-only", "openai_capabilities": capabilities,
				OpenAIVideoRelayCredentialsKey: map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "max_seconds": 30, "default_seconds": 5},
			},
		}
		requests := 0
		svc := &AccountTestService{accountRepo: &videoRelayAccountAdminRepo{saved: account},
			httpUpstream: videoRelayTestTransport{request: func(_ *http.Request) { requests++ }}}
		svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/account-test", nil)
		require.ErrorContains(t, svc.TestAccountConnection(c, account.ID, "", "", ""), "请通过测试分组提交视频进行测试")
		require.Contains(t, w.Body.String(), "请通过测试分组提交视频进行测试")
		require.Zero(t, requests)
	}
}
