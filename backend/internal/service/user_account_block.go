package service

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	UserAccountBlockDurationShortHours   = 6
	UserAccountBlockDurationLongHours    = 12
	DefaultUserAccountBlockDurationHours = 12
)

var (
	ErrUserAccountBlockInvalidID          = infraerrors.BadRequest("USER_ACCOUNT_BLOCK_INVALID_ID", "user_id and account_id must be positive integers")
	ErrUserAccountBlockInvalidDuration    = infraerrors.BadRequest("USER_ACCOUNT_BLOCK_INVALID_DURATION", "duration_hours must be 6 or 12")
	ErrUserAccountBlockDurationReadFailed = infraerrors.InternalServer("USER_ACCOUNT_BLOCK_DURATION_READ_FAILED", "could not read block duration")
)

type UserAccountBlockSettings struct {
	DurationHours int `json:"duration_hours"`
}

type UserAccountBlockRecord struct {
	UserID, AccountID, CreatedAt, ExpiresAt int64
}

type UserAccountBlockCache interface {
	SetUserAccountBlock(context.Context, UserAccountBlockRecord) error
	DeleteUserAccountBlock(context.Context, int64, int64) error
	ListUserAccountBlocks(context.Context) ([]UserAccountBlockRecord, error)
}

type UserAccountBlockItem struct {
	UserID    int64     `json:"user_id"`
	AccountID int64     `json:"account_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type UserAccountBlockService struct {
	cache          UserAccountBlockCache
	settingService *SettingService
	adminOps       sync.Mutex
	blocksMu       sync.RWMutex
	blocksByUser   map[int64]map[int64]UserAccountBlockRecord
	now            func() time.Time

	recoveryMu     sync.Mutex
	recoveryCancel context.CancelFunc
	recoveryDone   chan struct{}
}

func NewUserAccountBlockService(cache UserAccountBlockCache, settingService *SettingService) *UserAccountBlockService {
	return &UserAccountBlockService{
		cache: cache, settingService: settingService, now: time.Now,
		blocksByUser: make(map[int64]map[int64]UserAccountBlockRecord),
	}
}

const userAccountBlockRecoveryWindow = 12 * time.Hour

// StartRecovery waits briefly for the first read. The same worker continues
// through timeouts and stops on the first successful replacement of the list.
func (s *UserAccountBlockService) StartRecovery(startupWait time.Duration) {
	s.recoveryMu.Lock()
	if s.recoveryDone != nil {
		s.recoveryMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), userAccountBlockRecoveryWindow)
	first := make(chan struct{})
	done := make(chan struct{})
	s.recoveryCancel, s.recoveryDone = cancel, done
	s.recoveryMu.Unlock()

	go func() {
		defer close(done)
		defer cancel()
		err := s.recoverFromCache(ctx)
		close(first)
		for delay := time.Second; err != nil; delay = min(2*delay, 30*time.Second) {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				err = s.recoverFromCache(ctx)
			}
		}
	}()
	timer := time.NewTimer(startupWait)
	defer timer.Stop()
	select {
	case <-first:
	case <-timer.C:
	}
}

func (s *UserAccountBlockService) StopRecovery(ctx context.Context) {
	s.recoveryMu.Lock()
	done := s.recoveryDone
	if s.recoveryCancel != nil {
		s.recoveryCancel()
	}
	s.recoveryMu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
}

func (s *UserAccountBlockService) recoverFromCache(ctx context.Context) error {
	s.adminOps.Lock()
	defer s.adminOps.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.reloadLocked(ctx)
	return err
}

func (s *UserAccountBlockService) Block(ctx context.Context, userID, accountID int64) (*UserAccountBlockItem, error) {
	if userID <= 0 || accountID <= 0 {
		return nil, ErrUserAccountBlockInvalidID
	}
	s.adminOps.Lock()
	defer s.adminOps.Unlock()

	settings, err := s.settingService.GetUserAccountBlockSettings(ctx)
	if err != nil {
		return nil, ErrUserAccountBlockDurationReadFailed.WithCause(err)
	}
	now := s.now()
	record := UserAccountBlockRecord{
		UserID: userID, AccountID: accountID, CreatedAt: now.Unix(),
		ExpiresAt: now.Add(time.Duration(settings.DurationHours) * time.Hour).Unix(),
	}
	if err := s.cache.SetUserAccountBlock(ctx, record); err != nil {
		return nil, err
	}

	s.blocksMu.Lock()
	s.pruneExpiredLocked(s.now().Unix())
	if s.blocksByUser[userID] == nil {
		s.blocksByUser[userID] = make(map[int64]UserAccountBlockRecord)
	}
	s.blocksByUser[userID][accountID] = record
	s.blocksMu.Unlock()
	item := userAccountBlockItem(record)
	return &item, nil
}

func (s *UserAccountBlockService) Unblock(ctx context.Context, userID, accountID int64) error {
	if userID <= 0 || accountID <= 0 {
		return ErrUserAccountBlockInvalidID
	}
	s.adminOps.Lock()
	defer s.adminOps.Unlock()

	if err := s.cache.DeleteUserAccountBlock(ctx, userID, accountID); err != nil {
		return err
	}
	s.blocksMu.Lock()
	delete(s.blocksByUser[userID], accountID)
	s.pruneExpiredLocked(s.now().Unix())
	s.blocksMu.Unlock()
	return nil
}

func (s *UserAccountBlockService) List(ctx context.Context) ([]UserAccountBlockItem, error) {
	s.adminOps.Lock()
	defer s.adminOps.Unlock()
	return s.reloadLocked(ctx)
}

func (s *UserAccountBlockService) reloadLocked(ctx context.Context) ([]UserAccountBlockItem, error) {
	records, err := s.cache.ListUserAccountBlocks(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	byUser := make(map[int64]map[int64]UserAccountBlockRecord)
	items := make([]UserAccountBlockItem, 0, len(records))
	for _, record := range records {
		if record.ExpiresAt <= now {
			continue
		}
		if byUser[record.UserID] == nil {
			byUser[record.UserID] = make(map[int64]UserAccountBlockRecord)
		}
		byUser[record.UserID][record.AccountID] = record
		items = append(items, userAccountBlockItem(record))
	}
	slices.SortFunc(items, func(a, b UserAccountBlockItem) int {
		if order := b.CreatedAt.Compare(a.CreatedAt); order != 0 {
			return order
		}
		if order := cmp.Compare(a.UserID, b.UserID); order != 0 {
			return order
		}
		return cmp.Compare(a.AccountID, b.AccountID)
	})

	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.blocksMu.Lock()
	s.blocksByUser = byUser
	s.blocksMu.Unlock()
	if s.recoveryCancel != nil {
		s.recoveryCancel()
	}
	return items, nil
}

func (s *UserAccountBlockService) pruneExpiredLocked(now int64) {
	for userID, accounts := range s.blocksByUser {
		for accountID, record := range accounts {
			if record.ExpiresAt <= now {
				delete(accounts, accountID)
			}
		}
		if len(accounts) == 0 {
			delete(s.blocksByUser, userID)
		}
	}
}

func userAccountBlockItem(record UserAccountBlockRecord) UserAccountBlockItem {
	return UserAccountBlockItem{
		UserID: record.UserID, AccountID: record.AccountID,
		CreatedAt: time.Unix(record.CreatedAt, 0).UTC(),
		ExpiresAt: time.Unix(record.ExpiresAt, 0).UTC(),
	}
}

func (s *UserAccountBlockService) BlockedAccountIDs(userID int64) map[int64]struct{} {
	if s == nil || userID <= 0 {
		return nil
	}
	s.blocksMu.RLock()
	defer s.blocksMu.RUnlock()
	accounts := s.blocksByUser[userID]
	if len(accounts) == 0 {
		return nil
	}
	now := s.now().Unix()
	var ids map[int64]struct{}
	for id, record := range accounts {
		if record.ExpiresAt <= now {
			continue
		}
		if ids == nil {
			ids = make(map[int64]struct{}, len(accounts))
		}
		ids[id] = struct{}{}
	}
	return ids
}

func (s *UserAccountBlockService) withExcludedAccounts(ctx context.Context, excluded map[int64]struct{}) map[int64]struct{} {
	blocked := s.BlockedAccountIDs(UserAccountBlockUserIDFromContext(ctx))
	if len(blocked) == 0 {
		return excluded
	}
	maps.Copy(blocked, excluded)
	return blocked
}

type userAccountBlockUserIDKey struct{}

func WithUserAccountBlockUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userAccountBlockUserIDKey{}, userID)
}

func UserAccountBlockUserIDFromContext(ctx context.Context) int64 {
	userID, _ := ctx.Value(userAccountBlockUserIDKey{}).(int64)
	return userID
}

func (s *GatewayService) SetUserAccountBlockService(blocks *UserAccountBlockService) {
	s.userAccountBlocks = blocks
}

func (s *OpenAIGatewayService) SetUserAccountBlockService(blocks *UserAccountBlockService) {
	s.userAccountBlocks = blocks
}

func (s *BatchImagePublicService) SetUserAccountBlockService(blocks *UserAccountBlockService) {
	s.userAccountBlocks = blocks
}
