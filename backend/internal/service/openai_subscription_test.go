package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestFetchChatGPTSubscriptionExpiresAt(t *testing.T) {
	const wantExpiresAt = "2026-06-10T02:52:15Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/backend-api/subscriptions", r.URL.Path)
		require.Equal(t, "acc_123", r.URL.Query().Get("account_id"))
		require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan_type":    "plus",
			"active_until": wantExpiresAt,
			"will_renew":   true,
			"id":           "sub_123",
		})
	}))
	defer server.Close()

	oldURL := chatGPTSubscriptionsURL
	chatGPTSubscriptionsURL = server.URL + "/backend-api/subscriptions"
	t.Cleanup(func() { chatGPTSubscriptionsURL = oldURL })

	got := fetchChatGPTSubscriptionExpiresAt(context.Background(), func(proxyURL string) (*req.Client, error) {
		return req.C().SetTimeout(5 * time.Second), nil
	}, "access-token", "", "acc_123")

	require.Equal(t, wantExpiresAt, got)
}

func TestFetchChatGPTAccountInfo_SkipsExpiredWorkspaceCandidate(t *testing.T) {
	expiredAt := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/backend-api/accounts/check/v4-2023-04-27", r.URL.Path)
		require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": map[string]any{
				"org-expired-workspace": map[string]any{
					"account": map[string]any{
						"plan_type":  "self_serve_business_usage_based",
						"is_default": true,
					},
					"entitlement": map[string]any{
						"expires_at": expiredAt,
					},
				},
				"personal-account": map[string]any{
					"account": map[string]any{
						"plan_type": "free",
					},
				},
			},
		})
	}))
	defer server.Close()

	oldURL := chatGPTAccountsCheckURL
	chatGPTAccountsCheckURL = server.URL + "/backend-api/accounts/check/v4-2023-04-27"
	t.Cleanup(func() { chatGPTAccountsCheckURL = oldURL })

	got := fetchChatGPTAccountInfo(context.Background(), func(proxyURL string) (*req.Client, error) {
		return req.C().SetTimeout(5 * time.Second), nil
	}, "access-token", "", "org-expired-workspace", chatGPTPersonalSubscriptionIdentity{})

	require.NotNil(t, got)
	require.Equal(t, "free", got.PlanType)
	require.Empty(t, got.SubscriptionExpiresAt)
}

func TestFetchChatGPTAccountInfo_SkipsDeactivatedWorkspaceCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/backend-api/accounts/check/v4-2023-04-27", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": map[string]any{
				"org-deactivated-workspace": map[string]any{
					"account": map[string]any{
						"plan_type":      "self_serve_business_usage_based",
						"is_default":     true,
						"is_deactivated": true,
					},
				},
				"personal-account": map[string]any{
					"account": map[string]any{
						"plan_type": "pro",
					},
				},
			},
		})
	}))
	defer server.Close()

	oldURL := chatGPTAccountsCheckURL
	chatGPTAccountsCheckURL = server.URL + "/backend-api/accounts/check/v4-2023-04-27"
	t.Cleanup(func() { chatGPTAccountsCheckURL = oldURL })

	got := fetchChatGPTAccountInfo(context.Background(), func(proxyURL string) (*req.Client, error) {
		return req.C().SetTimeout(5 * time.Second), nil
	}, "access-token", "", "org-deactivated-workspace", chatGPTPersonalSubscriptionIdentity{})

	require.NotNil(t, got)
	require.Equal(t, "pro", got.PlanType)
}

func TestShouldApplyChatGPTAccountInfoPlanType(t *testing.T) {
	require.False(t, shouldApplyChatGPTAccountInfoPlanType("pro", "self_serve_business_usage_based"))
	require.False(t, shouldApplyChatGPTAccountInfoPlanType("free", "team"))
	require.False(t, shouldApplyChatGPTAccountInfoPlanType("", ""))
	require.True(t, shouldApplyChatGPTAccountInfoPlanType("", "pro"))
}

func TestChatGPTAccountInfoBelongsToTokenAccount(t *testing.T) {
	require.False(t, chatGPTAccountInfoBelongsToTokenAccount(
		&OpenAITokenInfo{ChatGPTAccountID: "personal-a"}, &ChatGPTAccountInfo{AccountID: "workspace-b"}))
	require.True(t, chatGPTAccountInfoBelongsToTokenAccount(
		&OpenAITokenInfo{ChatGPTAccountID: "personal-a"}, &ChatGPTAccountInfo{AccountID: "PERSONAL-A"}))
	// 任一侧缺 ID 时无法区分，保持既有行为（采用 accounts/check 的值）。
	require.True(t, chatGPTAccountInfoBelongsToTokenAccount(
		&OpenAITokenInfo{}, &ChatGPTAccountInfo{AccountID: "workspace-b"}))
	require.True(t, chatGPTAccountInfoBelongsToTokenAccount(
		&OpenAITokenInfo{ChatGPTAccountID: "personal-a"}, &ChatGPTAccountInfo{}))
}

// accounts 的 map key 可能是 "default" 这类别名，account.account_id 才是账号标识。
func TestFetchChatGPTAccountInfo_ReportsAccountID(t *testing.T) {
	futureAt := time.Now().Add(720 * time.Hour).UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": map[string]any{
				"default": map[string]any{
					"account": map[string]any{
						"account_id": "personal-account-a",
						"plan_type":  "plus",
						"is_default": true,
					},
					"entitlement": map[string]any{"expires_at": futureAt},
				},
			},
		})
	}))
	defer server.Close()

	oldURL := chatGPTAccountsCheckURL
	chatGPTAccountsCheckURL = server.URL + "/backend-api/accounts/check/v4-2023-04-27"
	t.Cleanup(func() { chatGPTAccountsCheckURL = oldURL })

	got := fetchChatGPTAccountInfo(context.Background(), newTestPrivacyClientFactory(), "access-token", "", "", chatGPTPersonalSubscriptionIdentity{})
	require.NotNil(t, got)
	require.Equal(t, "plus", got.PlanType)
	require.Equal(t, "personal-account-a", got.AccountID, "应优先取 account.account_id 而不是 map key")
}

// issue #5459：poid 指向的默认 Personal workspace 与 chatgpt_account_id 是两个不同的
// 标识时，accounts/check 返回的是 workspace 的 entitlement.expires_at。plan_type 那侧
// 已被 shouldApplyChatGPTAccountInfoPlanType 挡住（保留 JWT 里的个人套餐），到期时间
// 这侧原先无条件覆盖，于是显示成「个人 Pro + workspace 到期时间」。
func TestEnrichTokenInfo_WorkspaceEntitlementDoesNotOverridePersonalSubscription(t *testing.T) {
	const (
		personalAccountID   = "personal-account-a"
		workspaceAccountID  = "personal-workspace-b"
		personalActiveUntil = "2027-03-01T00:00:00Z"
	)
	workspaceExpiresAt := time.Now().Add(720 * time.Hour).UTC().Format(time.RFC3339)

	subscriptionCalls := 0
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{
		accountsCheck: map[string]any{
			"accounts": map[string]any{
				workspaceAccountID: map[string]any{
					"account": map[string]any{
						"account_id": workspaceAccountID,
						"plan_type":  "pro",
						"is_default": true,
					},
					"entitlement": map[string]any{"expires_at": workspaceExpiresAt},
				},
			},
		},
		onSubscription: func(accountID string) map[string]any {
			subscriptionCalls++
			require.Equal(t, personalAccountID, accountID,
				"必须用个人 chatgpt_account_id 查订阅，而不是 poid workspace")
			return map[string]any{"plan_type": "pro", "active_until": personalActiveUntil, "will_renew": true}
		},
	})
	defer server.Close()

	tokenInfo := &OpenAITokenInfo{
		AccessToken:      "access-token",
		ChatGPTAccountID: personalAccountID,
		OrganizationID:   workspaceAccountID,
		PlanType:         "pro", // 来自 id_token 的个人套餐
	}
	svc := &OpenAIOAuthService{privacyClientFactory: newTestPrivacyClientFactory()}
	svc.enrichTokenInfo(context.Background(), tokenInfo, "", chatGPTPersonalSubscriptionIdentity{})

	require.Equal(t, "pro", tokenInfo.PlanType)
	require.Equal(t, personalActiveUntil, tokenInfo.SubscriptionExpiresAt,
		"到期时间必须来自个人订阅 active_until，不能是 workspace 的 entitlement.expires_at")
	require.NotEqual(t, workspaceExpiresAt, tokenInfo.SubscriptionExpiresAt)
	require.Equal(t, 1, subscriptionCalls)
}

// 单个人账号（poid == chatgpt_account_id）是绝大多数情况，行为必须保持不变：
// 直接用 accounts/check 的 entitlement，不额外打订阅端点。
func TestEnrichTokenInfo_KeepsEntitlementWhenAccountMatches(t *testing.T) {
	const personalAccountID = "personal-account-a"
	entitlementExpiresAt := time.Now().Add(720 * time.Hour).UTC().Format(time.RFC3339)

	subscriptionCalls := 0
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{
		accountsCheck: map[string]any{
			"accounts": map[string]any{
				personalAccountID: map[string]any{
					"account": map[string]any{
						"account_id": personalAccountID,
						"plan_type":  "plus",
						"is_default": true,
					},
					"entitlement": map[string]any{"expires_at": entitlementExpiresAt},
				},
			},
		},
		onSubscription: func(string) map[string]any {
			subscriptionCalls++
			return map[string]any{}
		},
	})
	defer server.Close()

	tokenInfo := &OpenAITokenInfo{
		AccessToken:      "access-token",
		ChatGPTAccountID: personalAccountID,
		OrganizationID:   personalAccountID,
		PlanType:         "plus",
	}
	svc := &OpenAIOAuthService{privacyClientFactory: newTestPrivacyClientFactory()}
	svc.enrichTokenInfo(context.Background(), tokenInfo, "", chatGPTPersonalSubscriptionIdentity{})

	require.Equal(t, entitlementExpiresAt, tokenInfo.SubscriptionExpiresAt)
	require.Zero(t, subscriptionCalls, "账号一致时不应额外请求订阅端点")
}

// 反向不变式：套餐本身就取自 accounts/check（JWT 没有 plan_type）时，到期时间必须
// 跟着取同一条记录，否则会变成「workspace 套餐 + 个人到期时间」的另一种错配。
func TestEnrichTokenInfo_WorkspacePlanTypeKeepsItsOwnExpiry(t *testing.T) {
	const workspaceAccountID = "workspace-b"
	workspaceExpiresAt := time.Now().Add(720 * time.Hour).UTC().Format(time.RFC3339)

	subscriptionCalls := 0
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{
		accountsCheck: map[string]any{
			"accounts": map[string]any{
				workspaceAccountID: map[string]any{
					"account": map[string]any{
						"account_id": workspaceAccountID,
						"plan_type":  "self_serve_business_usage_based",
						"is_default": true,
					},
					"entitlement": map[string]any{"expires_at": workspaceExpiresAt},
				},
			},
		},
		onSubscription: func(string) map[string]any {
			subscriptionCalls++
			return map[string]any{}
		},
	})
	defer server.Close()

	tokenInfo := &OpenAITokenInfo{
		AccessToken:      "access-token",
		ChatGPTAccountID: "personal-account-a",
		OrganizationID:   workspaceAccountID,
		// id_token 没带 chatgpt_plan_type
	}
	svc := &OpenAIOAuthService{privacyClientFactory: newTestPrivacyClientFactory()}
	svc.enrichTokenInfo(context.Background(), tokenInfo, "", chatGPTPersonalSubscriptionIdentity{})

	require.Equal(t, "self_serve_business_usage_based", tokenInfo.PlanType)
	require.Equal(t, workspaceExpiresAt, tokenInfo.SubscriptionExpiresAt,
		"套餐与到期时间必须来自同一条记录")
	require.Zero(t, subscriptionCalls)
}

type chatGPTBackendTestServerConfig struct {
	accountsCheck  map[string]any
	onSubscription func(accountID string) map[string]any
}

// newChatGPTBackendTestServer 同时接管 accounts/check 与 subscriptions 两个端点，
// 并在 t.Cleanup 里还原包级 URL 变量。
func newChatGPTBackendTestServer(t *testing.T, cfg chatGPTBackendTestServerConfig) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/accounts/check/v4-2023-04-27":
			_ = json.NewEncoder(w).Encode(cfg.accountsCheck)
		case "/backend-api/subscriptions":
			body := map[string]any{}
			if cfg.onSubscription != nil {
				body = cfg.onSubscription(r.URL.Query().Get("account_id"))
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	oldAccounts, oldSubscriptions := chatGPTAccountsCheckURL, chatGPTSubscriptionsURL
	chatGPTAccountsCheckURL = server.URL + "/backend-api/accounts/check/v4-2023-04-27"
	chatGPTSubscriptionsURL = server.URL + "/backend-api/subscriptions"
	t.Cleanup(func() {
		chatGPTAccountsCheckURL, chatGPTSubscriptionsURL = oldAccounts, oldSubscriptions
	})
	return server
}

func newTestPrivacyClientFactory(observers ...func(*req.Request)) PrivacyClientFactory {
	return func(string) (*req.Client, error) {
		return req.C().SetTimeout(time.Second).WrapRoundTripFunc(func(next req.RoundTripper) req.RoundTripFunc {
			return func(request *req.Request) (*req.Response, error) {
				for _, observe := range observers {
					observe(request)
				}
				if request.URL.Hostname() == "127.0.0.1" {
					return next.RoundTrip(request)
				}
				if request.URL.Hostname() == "chatgpt.com" && request.Method == http.MethodPatch {
					return &req.Response{Request: request, Response: &http.Response{StatusCode: http.StatusOK}}, nil
				}
				return nil, errors.New("unexpected endpoint in subscription test")
			}
		}), nil
	}
}

type openAISubscriptionRefreshClient struct {
	openaiOAuthClientRefreshStub
	idToken string
}

func (c openAISubscriptionRefreshClient) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	return &openai.TokenResponse{AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresIn: 3600, IDToken: c.idToken}, nil
}

func TestRefreshTokenWithClientID_ReusesPersonalSubscriptionExpiry(t *testing.T) {
	expiry := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	later := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	fallback := time.Now().Add(96 * time.Hour).UTC().Format(time.RFC3339)
	for _, tc := range []struct {
		name, plan, accountID, accountPlan, entitlementPlan, expiresAt, duplicate string
		wantReuse                                                                 bool
	}{
		{"default_alias", "plus", "personal", "plus", "plus", expiry, "", true},
		{"two_consistent_aliases", "pro", "personal", "pro", "pro", expiry, expiry, true},
		{"different_dates", "plus", "personal", "plus", "plus", expiry, later, false},
		{"case_variant_conflicting_date", "plus", "personal", "plus", "plus", expiry, "", false},
		{"entitlement_mismatch", "plus", "personal", "plus", "pro", expiry, "", false},
		{"entitlement_missing", "plus", "personal", "plus", "", expiry, "", false},
		{"account_plan_mismatch", "plus", "personal", "pro", "plus", expiry, "", false},
		{"missing_id", "plus", "", "plus", "plus", expiry, "", false},
		{"map_key_is_not_the_embedded_id", "plus", "", "plus", "plus", expiry, "", false},
		{"different_id", "plus", "someone-else", "plus", "plus", expiry, "", false},
		{"invalid_date", "plus", "personal", "plus", "plus", "invalid", "", false},
		{"expired_date", "plus", "personal", "plus", "plus", "2020-01-01T00:00:00Z", "", false},
		{"workspace_plan_is_not_personal", "team", "personal", "team", "team", expiry, "", false},
		{"unknown_plan_is_not_personal", "unknown", "personal", "unknown", "unknown", expiry, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := func(accountID, plan, subscriptionPlan, expiresAt string) map[string]any {
				return map[string]any{
					"account":     map[string]any{"account_id": accountID, "plan_type": plan},
					"entitlement": map[string]any{"subscription_plan": subscriptionPlan, "expires_at": expiresAt},
				}
			}
			entryKey := "default"
			if tc.name == "map_key_is_not_the_embedded_id" {
				entryKey = "personal"
			}
			accounts := map[string]any{
				"workspace": entry("workspace", "team", "team", later),
				entryKey:    entry(tc.accountID, tc.accountPlan, tc.entitlementPlan, tc.expiresAt),
			}
			if tc.duplicate != "" {
				accounts["alias"] = entry(tc.accountID, tc.accountPlan, tc.entitlementPlan, tc.duplicate)
			}
			if tc.name == "case_variant_conflicting_date" {
				accounts["case-variant"] = entry("PERSONAL", "plus", "plus", later)
			}
			server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{
				accountsCheck: map[string]any{"accounts": accounts},
				onSubscription: func(string) map[string]any {
					return map[string]any{"active_until": fallback}
				},
			})
			defer server.Close()
			claims, err := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]any{
				"chatgpt_account_id": "personal",
				"chatgpt_plan_type":  tc.plan,
				"organizations":      []map[string]any{{"id": "workspace", "is_default": true}},
			}})
			require.NoError(t, err)
			idToken := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture"
			svc := NewOpenAIOAuthService(nil, &openAISubscriptionRefreshClient{idToken: idToken})
			defer svc.Stop()
			calls := make(map[string]int)
			svc.SetPrivacyClientFactory(newTestPrivacyClientFactory(func(request *req.Request) {
				calls[request.Method+" "+request.URL.Path]++
			}))

			got, err := svc.RefreshTokenWithClientID(context.Background(), "refresh-token", "", "")
			require.NoError(t, err)
			require.Equal(t, "personal", got.ChatGPTAccountID)
			require.Equal(t, "workspace", got.OrganizationID)
			require.Equal(t, 1, calls["GET /backend-api/accounts/check/v4-2023-04-27"])
			require.Equal(t, 1, calls["PATCH /backend-api/settings/account_user_setting"])
			require.Equal(t, PrivacyModeTrainingOff, got.PrivacyMode)
			subscriptionCalls := calls["GET /backend-api/subscriptions"]
			if tc.wantReuse {
				require.Zero(t, subscriptionCalls)
				require.Equal(t, expiry, got.SubscriptionExpiresAt)
			} else {
				require.Equal(t, 1, subscriptionCalls)
				require.Equal(t, fallback, got.SubscriptionExpiresAt)
			}
		})
	}
}
