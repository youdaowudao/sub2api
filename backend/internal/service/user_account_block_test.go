//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type userAccountBlockMemoryCache struct {
	mu           sync.Mutex
	records      map[[2]int64]UserAccountBlockRecord
	readErr      error
	writeErr     error
	deleteErr    error
	readCalls    int
	beforeWrite  func()
	beforeDelete func()
	afterRead    func()
}

func (c *userAccountBlockMemoryCache) SetUserAccountBlock(_ context.Context, record UserAccountBlockRecord) error {
	if c.beforeWrite != nil {
		c.beforeWrite()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return c.writeErr
	}
	if c.records == nil {
		c.records = make(map[[2]int64]UserAccountBlockRecord)
	}
	c.records[[2]int64{record.UserID, record.AccountID}] = record
	return nil
}

func (c *userAccountBlockMemoryCache) DeleteUserAccountBlock(_ context.Context, userID, accountID int64) error {
	if c.beforeDelete != nil {
		c.beforeDelete()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleteErr != nil {
		return c.deleteErr
	}
	delete(c.records, [2]int64{userID, accountID})
	return nil
}

func (c *userAccountBlockMemoryCache) ListUserAccountBlocks(context.Context) ([]UserAccountBlockRecord, error) {
	c.mu.Lock()
	c.readCalls++
	records := make([]UserAccountBlockRecord, 0, len(c.records))
	for _, record := range c.records {
		records = append(records, record)
	}
	err := c.readErr
	c.mu.Unlock()
	if c.afterRead != nil {
		c.afterRead()
	}
	return records, err
}

func TestUserAccountBlockService_Block(t *testing.T) {
	ctx := context.Background()
	repo := &panelRateLimitSettingRepo{values: map[string]string{"user_account_block_settings": `{"duration_hours":6}`}}
	cache := &userAccountBlockMemoryCache{}
	blocks := NewUserAccountBlockService(cache, NewSettingService(repo, &config.Config{}))
	now := time.Now().UTC().Truncate(time.Second)
	blocks.now = func() time.Time { return now }

	t.Run("rejects_invalid_id", func(t *testing.T) {
		_, err := blocks.Block(ctx, 1, 0)
		require.ErrorIs(t, err, ErrUserAccountBlockInvalidID)
		require.Empty(t, cache.records)
	})
	t.Run("publishes_and_renews_confirmed_write", func(t *testing.T) {
		item, err := blocks.Block(ctx, 1, 9)
		require.NoError(t, err)
		require.Equal(t, now.Add(6*time.Hour), item.ExpiresAt)
		require.Equal(t, map[int64]struct{}{9: {}}, blocks.BlockedAccountIDs(1))
		require.Empty(t, blocks.BlockedAccountIDs(2))
		now = now.Add(time.Hour)
		repo.values["user_account_block_settings"] = `{"duration_hours":12}`
		item, err = blocks.Block(ctx, 1, 9)
		require.NoError(t, err)
		require.Equal(t, now.Add(12*time.Hour), item.ExpiresAt)
	})

	t.Run("failed_write_does_not_publish", func(t *testing.T) {
		cache.writeErr = errors.New("write lost")
		_, err := blocks.Block(ctx, 2, 9)
		require.Error(t, err)
		require.Empty(t, blocks.BlockedAccountIDs(2))
	})
}

func TestUserAccountBlockService_ListAndUnblock(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	makeRecord := func(user, account, created, expiry int64) UserAccountBlockRecord {
		return UserAccountBlockRecord{user, account, now + created, now + expiry}
	}
	cache := &userAccountBlockMemoryCache{records: map[[2]int64]UserAccountBlockRecord{
		{1, 9}: makeRecord(1, 9, -2, 20),
		{2, 9}: makeRecord(2, 9, -1, 20),
		{1, 8}: makeRecord(1, 8, -3, 0),
	}}
	blocks := NewUserAccountBlockService(cache, NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{}))
	blocks.now = func() time.Time { return time.Unix(now, 0) }
	items, err := blocks.List(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, int64(2), items[0].UserID)
	require.Equal(t, int64(1), items[1].UserID)

	cache.deleteErr = errors.New("delete lost")
	require.Error(t, blocks.Unblock(ctx, 1, 9))
	require.Contains(t, blocks.BlockedAccountIDs(1), int64(9))
	cache.deleteErr = nil
	require.NoError(t, blocks.Unblock(ctx, 1, 9))
	require.NoError(t, blocks.Unblock(ctx, 1, 9))
	require.Empty(t, blocks.BlockedAccountIDs(1))
	require.Contains(t, blocks.BlockedAccountIDs(2), int64(9))

	cache.records = nil // 迟到的删除已经在 Redis 生效
	cache.readErr = errors.New("read failed")
	_, err = blocks.List(ctx)
	require.Error(t, err)
	require.Contains(t, blocks.BlockedAccountIDs(2), int64(9))
	cache.readErr = nil
	items, err = blocks.List(ctx)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Empty(t, blocks.BlockedAccountIDs(2))
}

func TestUserAccountBlockService_LocalSelection(t *testing.T) {
	var now = time.Now().UTC().Truncate(time.Second)
	record := UserAccountBlockRecord{1, 9, now.Unix(), now.Add(time.Hour).Unix()}
	waitForRecoveryExit := func(t *testing.T, blocks *UserAccountBlockService) {
		t.Helper()
		select {
		case <-blocks.recoveryDone:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("名单恢复成功后，后台恢复没有自行退出")
		}
	}
	t.Run("recovers_active_record_and_expires_locally", func(t *testing.T) {
		cache := &userAccountBlockMemoryCache{records: map[[2]int64]UserAccountBlockRecord{{1, 9}: record}}
		blocks := NewUserAccountBlockService(cache, nil)
		current := now
		blocks.now = func() time.Time { return current }
		blocks.StartRecovery(time.Second)
		t.Cleanup(func() { blocks.StopRecovery(context.Background()) })
		waitForRecoveryExit(t, blocks)
		require.Contains(t, blocks.BlockedAccountIDs(1), int64(9))
		require.Empty(t, blocks.BlockedAccountIDs(2))
		current = now.Add(time.Hour)
		require.Empty(t, blocks.BlockedAccountIDs(1))
		require.Equal(t, 1, cache.readCalls)
	})
	t.Run("recovers_after_initial_failure", func(t *testing.T) {
		cache := &userAccountBlockMemoryCache{records: map[[2]int64]UserAccountBlockRecord{{1, 9}: record}, readErr: errors.New("redis unavailable")}
		blocks := NewUserAccountBlockService(cache, nil)
		var logs bytes.Buffer
		previousLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
		defer slog.SetDefault(previousLogger)
		blocks.StartRecovery(time.Second)
		t.Cleanup(func() { blocks.StopRecovery(context.Background()) })
		require.Empty(t, blocks.BlockedAccountIDs(1))
		cache.mu.Lock()
		cache.readErr = nil
		cache.mu.Unlock()
		require.Eventually(t, func() bool { return len(blocks.BlockedAccountIDs(1)) == 1 }, 3*time.Second, 10*time.Millisecond)
		waitForRecoveryExit(t, blocks)
		require.Equal(t, 2, cache.readCalls)
		require.Empty(t, logs.String())
	})
	t.Run("admin_refresh_ends_background_recovery", func(t *testing.T) {
		cache := &userAccountBlockMemoryCache{records: map[[2]int64]UserAccountBlockRecord{{1, 9}: record}, readErr: errors.New("redis unavailable")}
		blocks := NewUserAccountBlockService(cache, nil)
		blocks.StartRecovery(time.Second)
		t.Cleanup(func() { blocks.StopRecovery(context.Background()) })
		require.Empty(t, blocks.BlockedAccountIDs(1))
		cache.mu.Lock()
		cache.readErr = nil
		cache.mu.Unlock()
		items, err := blocks.List(context.Background())
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Contains(t, blocks.BlockedAccountIDs(1), int64(9))
		waitForRecoveryExit(t, blocks)
		require.Equal(t, 2, cache.readCalls)
	})
}

func TestUserAccountBlockSettings(t *testing.T) {
	repo := &panelRateLimitSettingRepo{values: make(map[string]string)}
	settings := NewSettingService(repo, &config.Config{})
	for _, tc := range []struct {
		raw  string
		want int
	}{{"", 12}, {"<bad>", 12}, {`{"duration_hours":7}`, 12}, {`{"duration_hours":6}`, 6}, {`{"duration_hours":12}`, 12}} {
		t.Run(tc.raw, func(t *testing.T) {
			repo.values["user_account_block_settings"] = tc.raw
			got, err := settings.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.NotNil(t, got.UserAccountBlockDurationHours)
			require.Equal(t, tc.want, *got.UserAccountBlockDurationHours)
		})
	}
}

func userAccountBlockSelectionFixture(t *testing.T, ids ...int64) (*UserAccountBlockService, *userAccountBlockMemoryCache) {
	t.Helper()
	now := time.Now()
	cache := &userAccountBlockMemoryCache{records: make(map[[2]int64]UserAccountBlockRecord)}
	for _, id := range ids {
		cache.records[[2]int64{1, id}] = UserAccountBlockRecord{1, id, now.Unix(), now.Add(time.Hour).Unix()}
	}
	blocks := NewUserAccountBlockService(cache, nil)
	_, err := blocks.List(context.Background())
	require.NoError(t, err)
	return blocks, cache
}

func checkUserAccountBlockSelection(
	t *testing.T,
	accounts []Account,
	selectAccount func(context.Context, []Account, *UserAccountBlockService, map[int64]struct{}) (*AccountSelectionResult, error),
) {
	t.Helper()
	stickyID, backupID := accounts[0].ID, accounts[1].ID
	for _, tc := range []struct {
		name         string
		userID       int64
		initialState string
		onlySticky   bool
		exclude      map[int64]struct{}
		want         int64
	}{
		{"blocked_user", 1, "active", false, nil, backupID},
		{"other_user", 2, "active", false, nil, stickyID},
		{"anonymous", 0, "active", false, nil, stickyID},
		{"no_record", 1, "absent", false, nil, stickyID},
		{"expired", 1, "expired", false, nil, stickyID},
		{"startup_read_failed", 1, "read_error", false, nil, stickyID},
		{"caller_excludes_backup", 1, "active", false, map[int64]struct{}{backupID: {}}, 0},
		{"only_sticky_is_blocked", 1, "active", true, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithUserAccountBlockUserID(context.Background(), tc.userID)
			now := time.Now().UTC().Truncate(time.Second)
			storage := &userAccountBlockMemoryCache{records: make(map[[2]int64]UserAccountBlockRecord)}
			if tc.initialState != "absent" {
				storage.records[[2]int64{1, stickyID}] = UserAccountBlockRecord{1, stickyID, now.Unix(), now.Add(time.Hour).Unix()}
			}
			if tc.initialState == "read_error" {
				storage.readErr = errors.New("redis unavailable at startup")
			}
			blocks := NewUserAccountBlockService(storage, nil)
			blocks.now = func() time.Time { return now }
			blocks.StartRecovery(time.Second)
			blocks.StopRecovery(ctx)
			if tc.initialState == "expired" {
				now = now.Add(time.Hour)
			}
			candidates := accounts
			if tc.onlySticky {
				candidates = accounts[:1]
			}
			require.Equal(t, 1, storage.readCalls)
			selection, err := selectAccount(ctx, candidates, blocks, tc.exclude)
			require.NotContains(t, tc.exclude, stickyID)
			require.Equal(t, 1, storage.readCalls)
			if tc.want == 0 {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, selection.Account.ID)
		})
	}
}

func TestGatewaySelectAccountWithLoadAwareness_UserAccountBlock(t *testing.T) {
	const a, b = int64(88), int64(89)
	accounts := []Account{
		{ID: a, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 1},
		{ID: b, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 2},
	}
	checkUserAccountBlockSelection(t, accounts, func(ctx context.Context, candidates []Account, blocks *UserAccountBlockService, excluded map[int64]struct{}) (*AccountSelectionResult, error) {
		svc := &GatewayService{
			accountRepo: stubOpenAIAccountRepo{accounts: candidates},
			cache:       &stickyGatewayCacheHotpathStub{stickyID: a},
			cfg:         &config.Config{RunMode: config.RunModeStandard},
		}
		svc.SetUserAccountBlockService(blocks)
		ctx = context.WithValue(ctx, ctxkey.ForcePlatform, PlatformAnthropic)
		return svc.SelectAccountWithLoadAwareness(ctx, nil, "shared", "", excluded, "", 0)
	})
}

func TestOpenAISelectAccountWithScheduler_UserAccountBlock(t *testing.T) {
	const a, b = int64(91), int64(92)
	accounts := []Account{
		{ID: a, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 1},
		{ID: b, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 2},
	}
	checkUserAccountBlockSelection(t, accounts, func(ctx context.Context, candidates []Account, blocks *UserAccountBlockService, excluded map[int64]struct{}) (*AccountSelectionResult, error) {
		svc := &OpenAIGatewayService{
			accountRepo: schedulerTestOpenAIAccountRepo{accounts: candidates},
			cache:       &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:shared": a}},
			cfg:         &config.Config{},
		}
		svc.SetUserAccountBlockService(blocks)
		selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "shared", "gpt-5.1", excluded, OpenAIUpstreamTransportAny, false)
		return selection, err
	})
}

type userAccountBlockImageProvider struct {
	*publicBatchImageProvider
	accountType string
}

func (p userAccountBlockImageProvider) SupportsAccount(account *Account) bool {
	return account.Type == p.accountType
}

type userAccountBlockImageRepo struct {
	*publicBatchImageAccountRepo
	calls int
}

func (r *userAccountBlockImageRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	r.calls++
	return r.publicBatchImageAccountRepo.ListSchedulableByPlatform(ctx, platform)
}

func TestBatchImageSelectProviderAndAccount_UserAccountBlock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		user    int64
		blocked []int64
		want    int64
	}{
		{"reuses_candidates_for_blocked_fallback", 1, []int64{101}, 202},
		{"other_user", 2, []int64{101}, 101},
		{"all_blocked", 1, []int64{101, 202}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, gemini, vertex := newTestBatchImagePublicService(true)
			repo := &userAccountBlockImageRepo{publicBatchImageAccountRepo: svc.AccountRepo.(*publicBatchImageAccountRepo)}
			svc.AccountRepo = repo
			svc.ProviderRegistry = NewBatchImageProviderRegistry(
				userAccountBlockImageProvider{gemini, AccountTypeAPIKey},
				userAccountBlockImageProvider{vertex, AccountTypeServiceAccount},
			)
			blocks, storage := userAccountBlockSelectionFixture(t, tc.blocked...)
			svc.SetUserAccountBlockService(blocks)
			owner := BatchImageOwner{UserID: tc.user}
			beforeReads := storage.readCalls
			_, account, err := svc.selectProviderAndAccount(context.Background(), owner, "", "gemini-2.5-flash-image")
			require.Equal(t, beforeReads, storage.readCalls)
			require.Equal(t, 1, repo.calls)
			if tc.want == 0 {
				require.ErrorIs(t, err, ErrBatchImageNoAccountAvailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, account.ID)
		})
	}
}

type userAccountBlockPrivacyRepo struct {
	*mockAccountRepoForPlatform
	globalErrors []int64
}

func (r *userAccountBlockPrivacyRepo) SetError(_ context.Context, id int64, _ string) error {
	r.globalErrors = append(r.globalErrors, id)
	return nil
}

func TestGatewaySelectAccountWithLoadAwareness_UserAccountBlockGroupPrivacy(t *testing.T) {
	const a, b, c = int64(81), int64(82), int64(83)
	groupID, secondGroupID := int64(17), int64(18)
	for _, path := range []struct {
		name, platform, accountPlatform, privacy, model string
		mixed                                           bool
	}{
		{"single", PlatformAntigravity, PlatformAntigravity, "privacy_set", "", false},
		{"mixed", PlatformAnthropic, PlatformAntigravity, "privacy_set", "", true},
		{"routed_single", PlatformOpenAI, PlatformOpenAI, "training_off", "gpt-5.1", false},
		{"routed_mixed", PlatformAnthropic, PlatformAntigravity, "privacy_set", "claude-sonnet-4-5", true},
	} {
		t.Run(path.name, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				sticky      int64
				user        int64
				excludedIDs map[int64]struct{}
				want        int64
			}{
				{"manual_block", a, 1, nil, c},
				{"caller_exclusion", a, 2, map[int64]struct{}{a: {}}, c},
				{"ineligible_sticky", b, 2, nil, c},
				{"eligible_sticky", a, 2, nil, a},
			} {
				t.Run(tc.name, func(t *testing.T) {
					group := &Group{ID: groupID, Platform: path.platform, Status: StatusActive, RequirePrivacySet: true}
					if path.model != "" {
						group.ModelRoutingEnabled = true
						group.ModelRouting = map[string][]int64{path.model: {a, b, c}}
					}
					repo := &userAccountBlockPrivacyRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
						accountsByID: make(map[int64]*Account),
					}}
					for priority, id := range []int64{b, c, a} {
						extra := map[string]any{"mixed_scheduling": path.mixed}
						if id != b {
							extra["privacy_mode"] = path.privacy
						}
						account := Account{ID: id, Platform: path.accountPlatform, Type: AccountTypeOAuth, Status: StatusActive,
							Schedulable: true, Priority: priority, Extra: extra,
							AccountGroups: []AccountGroup{{GroupID: groupID}, {GroupID: secondGroupID}}}
						if path.model != "" {
							account.Credentials = map[string]any{"model_mapping": map[string]any{path.model: path.model}}
						}
						repo.accounts = append(repo.accounts, account)
						repo.accountsByID[id] = &account
					}
					cache := &stickyGatewayCacheHotpathStub{stickyID: tc.sticky}
					svc := &GatewayService{accountRepo: repo, cache: cache, cfg: &config.Config{},
						groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{
							groupID: group, secondGroupID: {ID: secondGroupID, Platform: path.platform, Status: StatusActive},
						}},
					}
					blocks, _ := userAccountBlockSelectionFixture(t, a)
					svc.SetUserAccountBlockService(blocks)
					ctx := WithUserAccountBlockUserID(context.Background(), tc.user)
					result, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "shared", path.model, tc.excludedIDs, "", 0)
					require.NoError(t, err)
					require.Equal(t, tc.want, result.Account.ID)
					require.Empty(t, repo.globalErrors)

					result, err = svc.SelectAccountWithLoadAwareness(ctx, &secondGroupID, "", path.model, nil, "", 0)
					require.NoError(t, err)
					require.Equal(t, b, result.Account.ID)
				})
			}
		})
	}
}

func TestOpenAISelectAccountWithScheduler_UserAccountBlockKeepsQuotaMaintenance(t *testing.T) {
	const a, b, c = int64(931), int64(932), int64(933)
	ctx := context.Background()
	groupID := int64(10103)
	for _, tc := range []struct {
		name   string
		slotA  bool
		blockA bool
		want   int64
	}{
		{"sticky_available", true, false, a},
		{"sticky_busy", false, false, c},
		{"sticky_blocked", true, true, c},
		{"sticky_busy_and_blocked", false, true, c},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notify := &OpenAIQuotaAutoResetService{ctx: ctx, queue: make(chan int64, 8)}
			openAIAutoResetSchedulerNotifiedAt.Delete(b)
			setOpenAIAutoResetNotifier(notify)
			t.Cleanup(func() {
				clearOpenAIAutoResetNotifier(notify)
				openAIAutoResetSchedulerNotifiedAt.Delete(b)
			})
			now := time.Now().UTC()
			accounts := []Account{
				{ID: a, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
				{ID: b, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
					Extra: map[string]any{
						OpenAIAutoResetCreditEnabledExtraKey: true, OpenAIAutoResetCredit5hThresholdExtraKey: 1.0,
						"auto_pause_7d_disabled": true, "codex_5h_used_percent": 100.0,
						"codex_usage_updated_at": now.Format(time.RFC3339), "codex_5h_reset_at": now.Add(time.Hour).Format(time.RFC3339),
					}},
				{ID: c, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 2, GroupIDs: []int64{groupID}},
			}
			cfg := &config.Config{}
			cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
			cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 15000
			cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
			cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
			var acquiredIDs []int64
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
				cache:       &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:shared": a}},
				cfg:         cfg, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
					acquireResults: map[int64]bool{a: tc.slotA, c: true},
					waitCounts:     map[int64]int{a: 999},
					loadMap:        map[int64]*AccountLoadInfo{a: {AccountID: a, LoadRate: 95, WaitingCount: 9}, c: {AccountID: c}},
					acquiredIDs:    &acquiredIDs,
				}),
			}
			if tc.blockA {
				blocks, _ := userAccountBlockSelectionFixture(t, a)
				svc.SetUserAccountBlockService(blocks)
			}
			selection, _, err := svc.SelectAccountWithScheduler(WithUserAccountBlockUserID(ctx, 1), &groupID, "", "shared",
				"gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, tc.want, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if tc.blockA {
				require.NotContains(t, acquiredIDs, a)
			}
			if tc.want == a {
				require.Empty(t, notify.queue)
			} else {
				require.Len(t, notify.queue, 1)
				require.Equal(t, b, <-notify.queue)
			}
		})
	}
}

type userAccountBlockQuotaAccountRepo struct {
	schedulerGroupAwareOpenAIAccountRepo
	quotaAccount Account
}

func (r userAccountBlockQuotaAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if id == r.quotaAccount.ID {
		return &r.quotaAccount, nil
	}
	return r.schedulerGroupAwareOpenAIAccountRepo.GetByID(ctx, id)
}

func TestOpenAISelectAccountWithScheduler_UserAccountBlockQuotaMaintenanceWithoutSession(t *testing.T) {
	const quotaID, availableID, unrelatedID = int64(35121), int64(35122), int64(35123)
	groupID := int64(10121)
	now := time.Now().UTC()
	for _, path := range []string{"ordinary_candidates", "previous_response"} {
		t.Run(path, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				blockedID int64
				excluded  map[int64]struct{}
				notifies  bool
			}{
				{"no_block", 0, nil, true},
				{"other_group_block", unrelatedID, nil, true},
				{"manual_block", quotaID, nil, false},
				{"caller_exclusion", 0, map[int64]struct{}{quotaID: {}}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := WithUserAccountBlockUserID(context.Background(), 1)
					resetOpenAIAdvancedSchedulerSettingCacheForTest()
					notifier := &OpenAIQuotaAutoResetService{ctx: ctx, queue: make(chan int64, 8)}
					openAIAutoResetSchedulerNotifiedAt.Delete(quotaID)
					setOpenAIAutoResetNotifier(notifier)
					t.Cleanup(func() {
						resetOpenAIAdvancedSchedulerSettingCacheForTest()
						clearOpenAIAutoResetNotifier(notifier)
						openAIAutoResetSchedulerNotifiedAt.Delete(quotaID)
					})

					quotaAccount := Account{
						ID: quotaID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
						Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
						Extra: map[string]any{
							OpenAIAutoResetCreditEnabledExtraKey:           true,
							OpenAIAutoResetCredit5hThresholdExtraKey:       1.0,
							"auto_pause_7d_disabled":                       true,
							"codex_5h_used_percent":                        100.0,
							"codex_usage_updated_at":                       now.Format(time.RFC3339),
							"codex_5h_reset_at":                            now.Add(time.Hour).Format(time.RFC3339),
							"openai_oauth_responses_websockets_v2_enabled": true,
						},
					}
					accounts := []Account{
						{ID: availableID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
						{ID: unrelatedID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID + 1}},
					}
					if path == "ordinary_candidates" {
						accounts = append(accounts, quotaAccount)
					}
					repo := userAccountBlockQuotaAccountRepo{
						schedulerGroupAwareOpenAIAccountRepo: schedulerGroupAwareOpenAIAccountRepo{
							schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
						},
						quotaAccount: quotaAccount,
					}
					cfg := &config.Config{}
					cfg.Gateway.OpenAIWS.Enabled = true
					cfg.Gateway.OpenAIWS.OAuthEnabled = true
					cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
					svc := &OpenAIGatewayService{
						accountRepo: repo, cache: &schedulerTestGatewayCache{}, cfg: cfg,
						rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
						concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
					}
					if tc.blockedID != 0 {
						blocks, _ := userAccountBlockSelectionFixture(t, tc.blockedID)
						svc.SetUserAccountBlockService(blocks)
					}
					var previousResponseID string
					if path == "previous_response" {
						previousResponseID = "resp_user_account_block_quota"
						require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, previousResponseID, quotaID, time.Hour))
					}
					selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, previousResponseID, "",
						"gpt-5.1", tc.excluded, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					require.Equal(t, availableID, selection.Account.ID)
					releaseVideoRelaySlot(selection.ReleaseFunc)

					_, cooledDown := openAIAutoResetSchedulerNotifiedAt.Load(quotaID)
					_, pending := notifier.pending.Load(quotaID)
					require.Equal(t, tc.notifies, cooledDown)
					require.Equal(t, tc.notifies, pending)
					if tc.notifies {
						require.Len(t, notifier.queue, 1)
						require.Equal(t, quotaID, <-notifier.queue)
					} else {
						require.Empty(t, notifier.queue)
					}
				})
			}
		})
	}
}

func TestUserAccountBlockService_ConfirmedWritesAndLateDelete(t *testing.T) {
	ctx := context.Background()
	cache := &userAccountBlockMemoryCache{}
	settings := NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{})
	blocks := NewUserAccountBlockService(cache, settings)
	writeStarted, releaseWrite, writeDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWrite) }) }
	t.Cleanup(release)
	cache.beforeWrite = func() {
		close(writeStarted)
		<-releaseWrite
	}
	go func() { _, err := blocks.Block(ctx, 1, 9); writeDone <- err }()
	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("屏蔽写入没有到达仓储")
	}
	require.Empty(t, blocks.BlockedAccountIDs(1))
	release()
	select {
	case err := <-writeDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("屏蔽写入未返回")
	}
	cache.beforeWrite = nil
	require.Contains(t, blocks.BlockedAccountIDs(1), int64(9))

	cache.deleteErr = errors.New("redis response timed out")
	require.Error(t, blocks.Unblock(ctx, 1, 9))
	require.Contains(t, blocks.BlockedAccountIDs(1), int64(9))
	_, err := blocks.Block(ctx, 1, 9)
	require.NoError(t, err)
	// The older delete arrives after the newer confirmed block.
	delete(cache.records, [2]int64{1, 9})
	require.Contains(t, blocks.BlockedAccountIDs(1), int64(9))
	items, err := blocks.List(ctx)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Empty(t, blocks.BlockedAccountIDs(1))
}

func TestUserAccountBlockService_RefreshSerializesWithConfirmedWrites(t *testing.T) {
	for _, tc := range []struct {
		name         string
		recoveryRead bool
		unblock      bool
	}{
		{"admin_list_and_block", false, false},
		{"admin_list_and_unblock", false, true},
		{"recovery_and_block", true, false},
		{"recovery_and_unblock", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkUserAccountBlockConcurrentRefresh(t, tc.recoveryRead, tc.unblock)
		})
	}
}

func checkUserAccountBlockConcurrentRefresh(t *testing.T, recoveryRead, unblock bool) {
	t.Helper()
	ctx := context.Background()
	const userID, accountID = int64(1), int64(9)
	key := [2]int64{userID, accountID}
	cache := &userAccountBlockMemoryCache{records: make(map[[2]int64]UserAccountBlockRecord)}
	settings := NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{})
	blocks := NewUserAccountBlockService(cache, settings)
	if unblock {
		now := time.Now()
		cache.records[key] = UserAccountBlockRecord{userID, accountID, now.Unix(), now.Add(time.Hour).Unix()}
		_, err := blocks.List(ctx)
		require.NoError(t, err)
		require.Contains(t, blocks.BlockedAccountIDs(userID), accountID)
	}

	snapshotTaken, releaseRead := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseRead) })
	t.Cleanup(release)
	cache.afterRead = func() {
		close(snapshotTaken)
		<-releaseRead
	}
	writeStarted := make(chan struct{})
	cache.beforeWrite = func() { close(writeStarted) }
	cache.beforeDelete = cache.beforeWrite
	readDone := make(chan error, 1)
	go func() {
		if recoveryRead {
			readDone <- blocks.recoverFromCache(ctx)
			return
		}
		_, err := blocks.List(ctx)
		readDone <- err
	}()
	select {
	case <-snapshotTaken:
	case <-time.After(time.Second):
		t.Fatal("刷新未读取旧快照")
	}
	writeCalled, writeDone := make(chan struct{}), make(chan struct{})
	var writeErr error
	go func() {
		defer close(writeDone)
		close(writeCalled)
		if unblock {
			writeErr = blocks.Unblock(ctx, userID, accountID)
		} else {
			_, writeErr = blocks.Block(ctx, userID, accountID)
		}
	}()
	<-writeCalled
	select {
	case <-writeStarted:
		t.Error("刷新确认前已经发出后续写入")
		select {
		case <-writeDone:
		case <-time.After(time.Second):
			t.Fatal("后续写入未返回")
		}
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case err := <-readDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("名单刷新未返回")
	}
	select {
	case <-writeDone:
		require.NoError(t, writeErr)
	case <-time.After(time.Second):
		t.Fatal("后续写入未返回")
	}
	_, activeInStorage := cache.records[key]
	_, activeForSelection := blocks.BlockedAccountIDs(userID)[accountID]
	require.Equal(t, !unblock, activeInStorage)
	require.Equal(t, !unblock, activeForSelection)
}

func TestGatewaySelectAccountWithLoadAwareness_UserAccountBlockThresholdOrder(t *testing.T) {
	const a, b, c = int64(941), int64(942), int64(943)
	now := time.Now().UTC()
	for _, tc := range []struct {
		name    string
		sticky  int64
		blocked int64
		high    int64
		want    int64
		writes  int
	}{
		{"normal_fixed_account", a, 0, b, a, 0},
		{"backup_over_real_threshold", a, a, b, c, 1},
		{"backup_below_threshold", a, a, 0, b, 0},
		{"threshold_applies_before_caller_exclusion", 0, 0, a, c, 1},
		{"threshold_applies_before_manual_exclusion", 0, a, a, c, 1},
		{"manually_blocked_below_threshold", 0, a, 0, c, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
			accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})
			t.Cleanup(func() {
				accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})
				accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
			})
			cfg := &config.Config{RunMode: config.RunModeStandard}
			settings := newMockSettingRepo()
			settings.data[SettingKeyAccountSchedulingThresholds] = `{"openai":80}`
			writer := &rateLimitAccountRepoStub{}
			rateLimit := NewRateLimitService(writer, nil, cfg, nil, nil)
			rateLimit.SetSettingService(NewSettingService(settings, cfg))
			accounts := []Account{
				{ID: a, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 1},
				{ID: b, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 2},
				{ID: c, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 3},
			}
			if tc.sticky == 0 {
				accounts = []Account{accounts[0], accounts[2]}
			}
			for i := range accounts {
				usage := 50.0
				if accounts[i].ID == tc.high {
					usage = 91.5
				}
				accounts[i].Extra = map[string]any{"codex_7d_used_percent": usage, "codex_7d_reset_at": now.Add(time.Hour).Format(time.RFC3339)}
			}
			svc := &GatewayService{accountRepo: stubOpenAIAccountRepo{accounts: accounts}, cache: &stickyGatewayCacheHotpathStub{stickyID: tc.sticky},
				cfg: cfg, rateLimitService: rateLimit}
			if tc.blocked > 0 {
				blocks, _ := userAccountBlockSelectionFixture(t, tc.blocked)
				svc.SetUserAccountBlockService(blocks)
			}
			var exclude map[int64]struct{}
			if tc.sticky == 0 && tc.blocked == 0 {
				exclude = map[int64]struct{}{a: {}}
			}
			ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformOpenAI)
			ctx = WithUserAccountBlockUserID(ctx, 1)
			got, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "fixed", "", exclude, "", 0)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Account.ID)
			require.Equal(t, tc.writes, writer.tempCalls)
			if tc.writes > 0 {
				require.Equal(t, tc.high, writer.lastTempID)
			}
		})
	}
}

func TestOpenAISelectAccountWithScheduler_UserAccountBlockPreviousResponse(t *testing.T) {
	const a, b = int64(35101), int64(35102)
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	groupID := int64(10107)
	accounts := []Account{
		{ID: a, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
			Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true}},
		{ID: b, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
	}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:       &schedulerTestGatewayCache{}, cfg: cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	ctx := WithUserAccountBlockUserID(context.Background(), 1)
	cache := &userAccountBlockMemoryCache{}
	settings := NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{})
	blocks := NewUserAccountBlockService(cache, settings)
	now := time.Now().UTC().Truncate(time.Second)
	blocks.now = func() time.Time { return now }
	svc.SetUserAccountBlockService(blocks)
	const responseID = "resp_manual_account_block"
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, responseID, a, time.Hour))
	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, responseID, "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, a, selection.Account.ID)
	require.True(t, decision.StickyPreviousHit)
	releaseVideoRelaySlot(selection.ReleaseFunc)

	item, err := blocks.Block(context.Background(), 1, a)
	require.NoError(t, err)
	selection, decision, err = svc.SelectAccountWithScheduler(ctx, &groupID, responseID, "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, b, selection.Account.ID)
	require.False(t, decision.StickyPreviousHit)
	releaseVideoRelaySlot(selection.ReleaseFunc)

	now = item.ExpiresAt
	selection, decision, err = svc.SelectAccountWithScheduler(ctx, &groupID, responseID, "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, a, selection.Account.ID)
	require.True(t, decision.StickyPreviousHit)
	require.Zero(t, cache.readCalls)
	releaseVideoRelaySlot(selection.ReleaseFunc)
}

func TestOpenAISelectAccountWithScheduler_UserAccountBlockGrok(t *testing.T) {
	const a, b = int64(36041), int64(36042)
	groupID := int64(10113)
	accounts := []Account{
		{ID: a, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 0},
		{ID: b, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 1},
	}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, cfg: &config.Config{},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	blocks, cache := userAccountBlockSelectionFixture(t, a)
	svc.SetUserAccountBlockService(blocks)
	for _, tc := range []struct{ user, want int64 }{{2, a}, {1, b}} {
		ctx := WithUserAccountBlockUserID(context.Background(), tc.user)
		got, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "grok-4.3", nil,
			OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, false, false, PlatformGrok)
		require.NoError(t, err)
		require.Equal(t, tc.want, got.Account.ID)
		releaseVideoRelaySlot(got.ReleaseFunc)
	}
	require.Equal(t, 1, cache.readCalls)
}

type userAccountBlockAlternatingImageRepo struct {
	*publicBatchImageAccountRepo
	calls int
}

func (r *userAccountBlockAlternatingImageRepo) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	r.calls++
	return []Account{testBatchImageAccount(int64(201+r.calls), AccountTypeServiceAccount)}, nil
}

func TestBatchImageSelectProviderAndAccount_UserAccountBlockLeavesUnrelatedFallback(t *testing.T) {
	for _, excluded := range [][]int64{nil, {9999}} {
		svc, _, _, gemini, vertex := newTestBatchImagePublicService(true)
		repo := &userAccountBlockAlternatingImageRepo{publicBatchImageAccountRepo: svc.AccountRepo.(*publicBatchImageAccountRepo)}
		svc.AccountRepo = repo
		svc.ProviderRegistry = NewBatchImageProviderRegistry(
			userAccountBlockImageProvider{gemini, AccountTypeAPIKey},
			userAccountBlockImageProvider{vertex, AccountTypeServiceAccount},
		)
		blocks, cache := userAccountBlockSelectionFixture(t, excluded...)
		svc.SetUserAccountBlockService(blocks)
		_, account, err := svc.selectProviderAndAccount(context.Background(), BatchImageOwner{UserID: 1}, "", "gemini-2.5-flash-image")
		require.NoError(t, err)
		require.Equal(t, int64(203), account.ID)
		require.Equal(t, 2, repo.calls)
		require.Equal(t, 1, cache.readCalls)
	}
}

type userAccountBlockStartupCache struct {
	*userAccountBlockMemoryCache
	started chan struct{}
}

func (c *userAccountBlockStartupCache) ListUserAccountBlocks(ctx context.Context) ([]UserAccountBlockRecord, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestUserAccountBlockService_StartupWaitAndCancellation(t *testing.T) {
	cache := &userAccountBlockStartupCache{userAccountBlockMemoryCache: &userAccountBlockMemoryCache{}, started: make(chan struct{})}
	blocks := NewUserAccountBlockService(cache, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		blocks.StopRecovery(ctx)
	})
	returned := make(chan struct{})
	go func() {
		blocks.StartRecovery(20 * time.Millisecond)
		close(returned)
	}()
	select {
	case <-cache.started:
	case <-time.After(time.Second):
		t.Fatal("恢复任务没有发起首次读取")
	}
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("首次 Redis 读取阻塞了启动")
	}
	require.Empty(t, blocks.BlockedAccountIDs(1))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	blocks.StopRecovery(ctx)
	require.NoError(t, ctx.Err())
	select {
	case <-blocks.recoveryDone:
	default:
		t.Fatal("关闭后恢复任务仍在运行")
	}
}

func TestOpenAISelectAccountWithScheduler_UserAccountBlockVideoRelay(t *testing.T) {
	const model = "fixture-blocked-video"
	const accountID = int64(21)
	price := 0.25
	group := &Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
		{Models: []string{model}, BillingMode: BillingModeVideo, PerRequestPrice: &price},
	}}
	account := Account{ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{group.ID},
		Credentials: map[string]any{
			"base_url": "https://fixture.test", "api_key": "synthetic-video",
			"openai_capabilities": []any{"videos"},
			OpenAIVideoRelayCredentialsKey: OpenAIVideoRelaySettings{
				Resolutions: []string{"480p"}, MinSeconds: 5, DefaultSeconds: 5, MaxSeconds: 30,
			},
		}}
	for _, name := range []string{"legacy", "advanced"} {
		t.Run(name, func(t *testing.T) {
			repo := &videoRelaySchedulingFixture{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
			billing := &BillingService{}
			svc := &VideoRelayService{gateway: &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo,
				billingService: billing, resolver: NewModelPricingResolver(nil, billing)}}
			if name == "advanced" {
				svc.gateway.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
			}
			blocks, storage := userAccountBlockSelectionFixture(t, accountID)
			svc.gateway.SetUserAccountBlockService(blocks)
			req := VideoRelayRequest{Model: model, Seconds: 5, Resolution: "480p"}
			for _, userID := range []int64{1, 2} {
				before := repo.groupReads
				ctx := WithUserAccountBlockUserID(context.Background(), userID)
				_, task, release, failure := svc.selectRoute(ctx, &APIKey{ID: 3, UserID: userID, Group: group, GroupID: &group.ID}, req)
				releaseVideoRelaySlot(release)
				if userID == 1 {
					require.NotNil(t, failure)
					require.Equal(t, http.StatusServiceUnavailable, failure.Status)
				} else {
					require.Nil(t, failure)
					require.Equal(t, accountID, task.AccountID)
				}
				require.Equal(t, before+1, repo.groupReads)
			}
			require.Equal(t, 1, storage.readCalls)
		})
	}
}
