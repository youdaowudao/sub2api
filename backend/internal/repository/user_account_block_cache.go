package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type userAccountBlockCache struct {
	rdb *redis.Client
}

func NewUserAccountBlockCache(rdb *redis.Client) service.UserAccountBlockCache {
	return &userAccountBlockCache{rdb: rdb}
}

const userAccountBlockHash = "user_account_blocks"

type userAccountBlockValue struct {
	CreatedAt int64 `json:"created_at"`
	ExpiresAt int64 `json:"expires_at"`
}

func userAccountBlockField(userID, accountID int64) string {
	return fmt.Sprintf("%d:%d", userID, accountID)
}

func (c *userAccountBlockCache) SetUserAccountBlock(ctx context.Context, record service.UserAccountBlockRecord) error {
	payload, err := json.Marshal(userAccountBlockValue{CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt})
	if err != nil {
		return err
	}
	result, err := c.rdb.HSetEXWithArgs(ctx, userAccountBlockHash, &redis.HSetEXOptions{
		ExpirationType: redis.HSetEXExpirationEXAT, ExpirationVal: record.ExpiresAt,
	}, userAccountBlockField(record.UserID, record.AccountID), string(payload)).Result()
	if err != nil {
		return err
	}
	if result != 1 {
		return fmt.Errorf("Redis did not confirm user account block write: %d", result)
	}
	return nil
}

func (c *userAccountBlockCache) DeleteUserAccountBlock(ctx context.Context, userID, accountID int64) error {
	return c.rdb.HDel(ctx, userAccountBlockHash, userAccountBlockField(userID, accountID)).Err()
}

func (c *userAccountBlockCache) ListUserAccountBlocks(ctx context.Context) ([]service.UserAccountBlockRecord, error) {
	fields, err := c.rdb.HGetAll(ctx, userAccountBlockHash).Result()
	if err != nil {
		return nil, err
	}
	records := make([]service.UserAccountBlockRecord, 0, len(fields))
	for field, payload := range fields {
		user, account, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}
		userID, userErr := strconv.ParseInt(user, 10, 64)
		accountID, accountErr := strconv.ParseInt(account, 10, 64)
		if userErr != nil || accountErr != nil || userID <= 0 || accountID <= 0 {
			continue
		}
		var value userAccountBlockValue
		if err := json.Unmarshal([]byte(payload), &value); err != nil || value.CreatedAt <= 0 || value.ExpiresAt <= value.CreatedAt {
			continue
		}
		records = append(records, service.UserAccountBlockRecord{
			UserID: userID, AccountID: accountID, CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt,
		})
	}
	return records, nil
}
