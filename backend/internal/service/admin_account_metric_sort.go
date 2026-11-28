package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const AccountSortCacheHitRate = "cache_hit_rate"

func IsAccountMetricSort(key string) bool {
	switch key {
	case AccountSortCacheHitRate, "today_requests", "today_tokens", "today_account_cost", "today_user_cost":
		return true
	default:
		return false
	}
}

// AccountMetricSortResult belongs only to the administrative list response.
type AccountMetricSortResult struct {
	Accounts   []Account
	Total      int64
	CacheStats map[int64]usagestats.CacheHitRateItem
	TodayStats map[int64]*usagestats.AccountStats
}

type AccountMetricListService interface {
	ListAccountsByMetric(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string, locateID int64, start, end time.Time) (AccountMetricSortResult, error)
}

type accountMetricListRepository interface {
	ListWithMetricSort(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string, locateID int64, start, end time.Time) (AccountMetricSortResult, error)
}

func (s *adminServiceImpl) ListAccountsByMetric(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string, locateID int64, start, end time.Time) (AccountMetricSortResult, error) {
	if groupID > 0 {
		if err := s.ValidateAccountGroupBindings(ctx, []int64{groupID}); err != nil {
			return AccountMetricSortResult{}, err
		}
	}
	if params.SortBy != AccountSortCacheHitRate {
		start = timezone.Today()
	}
	repo, ok := s.accountRepo.(accountMetricListRepository)
	if !ok {
		return AccountMetricSortResult{}, errors.New("account metric ordering is unavailable")
	}
	return repo.ListWithMetricSort(ctx, params, platform, accountType, status, search, groupID, privacyMode, locateID, start, end)
}

func AccountWindowStatsForList(stats *usagestats.AccountStats) *WindowStats {
	return windowStatsFromAccountStats(stats)
}
