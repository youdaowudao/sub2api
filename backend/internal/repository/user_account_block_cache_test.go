//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type userAccountBlockCommandHook struct {
	writeResult int64
	writeError  error
	writes      [][]any
	reads       int
}

func (h *userAccountBlockCommandHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *userAccountBlockCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *userAccountBlockCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "hsetex":
			h.writes = append(h.writes, cmd.Args())
			cmd.(*redis.IntCmd).SetVal(h.writeResult)
			return h.writeError
		case "hgetall":
			h.reads++
		}
		return next(ctx, cmd)
	}
}

func TestUserAccountBlockCache(t *testing.T) {
	ctx := context.Background()
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	hook := &userAccountBlockCommandHook{writeResult: 1}
	client.AddHook(hook)
	cache := NewUserAccountBlockCache(client)
	record := service.UserAccountBlockRecord{UserID: 1, AccountID: 9, CreatedAt: 100, ExpiresAt: 21700}
	const payload = `{"created_at":100,"expires_at":21700}`

	t.Run("atomic_write_and_confirmation", func(t *testing.T) {
		require.NoError(t, cache.SetUserAccountBlock(ctx, record))
		require.Len(t, hook.writes, 1)
		args := hook.writes[0]
		require.Equal(t, []any{"HSETEX", "user_account_blocks", "EXAT", int64(21700), "FIELDS", 1, "1:9"}, args[:7])
		switch got := args[7].(type) {
		case string:
			require.JSONEq(t, payload, got)
		case []byte:
			require.JSONEq(t, payload, string(got))
		default:
			t.Fatalf("unexpected payload type %T", got)
		}
		hook.writeResult = 0
		require.Error(t, cache.SetUserAccountBlock(ctx, record))
		hook.writeError = errors.New("redis response lost")
		require.ErrorIs(t, cache.SetUserAccountBlock(ctx, record), hook.writeError)
		require.Len(t, hook.writes, 3)
	})

	t.Run("list_and_delete", func(t *testing.T) {
		redisServer.HSet("user_account_blocks", "1:9", payload, "2:9", payload,
			"0:9", payload, "1:9:3", payload, "bogus", payload, "2:1", "{",
			"2:2", `{"created_at":100,"expires_at":100}`,
			"2:3", `{"created_at":100}`, "2:4", `{"created_at":0,"expires_at":100}`)
		got, err := cache.ListUserAccountBlocks(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, hook.reads)
		other := record
		other.UserID = 2
		require.ElementsMatch(t, []service.UserAccountBlockRecord{record, other}, got)
		require.NoError(t, cache.DeleteUserAccountBlock(ctx, 1, 9))
		require.NoError(t, cache.DeleteUserAccountBlock(ctx, 1, 9))
		got, err = cache.ListUserAccountBlocks(ctx)
		require.NoError(t, err)
		require.Equal(t, []service.UserAccountBlockRecord{other}, got)
		require.Equal(t, 2, hook.reads)
		redisServer.SetError("ERR read failed")
		_, err = cache.ListUserAccountBlocks(ctx)
		require.Error(t, err)
	})
}

func TestBuildSchedulerMetadataAccount_KeepsPrivacyModeForGroupGate(t *testing.T) {
	for _, tc := range []struct {
		platform string
		mode     string
		want     bool
	}{
		{service.PlatformOpenAI, "training_off", true},
		{service.PlatformAntigravity, "privacy_set", true},
		{service.PlatformOpenAI, "failed", false},
		{service.PlatformAntigravity, "", false},
	} {
		t.Run(tc.platform+"/"+tc.mode, func(t *testing.T) {
			account := service.Account{ID: 31, Platform: tc.platform, Type: service.AccountTypeOAuth}
			if tc.mode != "" {
				account.Extra = map[string]any{"privacy_mode": tc.mode}
			}
			require.Equal(t, tc.want, account.IsPrivacySet())
			encoded, err := json.Marshal(buildSchedulerMetadataAccount(account))
			require.NoError(t, err)
			restored, err := decodeCachedAccount(string(encoded))
			require.NoError(t, err)
			require.Equal(t, tc.want, restored.IsPrivacySet())
		})
	}
}
