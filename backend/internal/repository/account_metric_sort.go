package repository

import (
	"context"
	"fmt"
	"sort"
	"time"

	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ListWithMetricSort replaces the ordinary count with a filtered ID read and
// reuses the selected metric's existing batch aggregation before pagination.
// Only the resulting page receives full account hydration.
func (r *accountRepository) ListWithMetricSort(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string, locateID int64, start, end time.Time) (service.AccountMetricSortResult, error) {
	out := service.AccountMetricSortResult{Accounts: []service.Account{}}
	if !service.IsAccountMetricSort(params.SortBy) {
		return out, fmt.Errorf("unsupported account metric order")
	}
	q := r.accountListFilteredQuery(platform, accountType, status, search, groupID, privacyMode)
	if locateID > 0 {
		q.Where(dbaccount.IDEQ(locateID))
	}
	ids, err := q.Clone().IDs(ctx)
	if err != nil {
		return out, err
	}
	out.Total = int64(len(ids))
	offset := params.Offset()
	if offset >= len(ids) {
		return out, nil
	}
	var cacheByID map[int64]usagestats.CacheHitRateItem
	var todayByID map[int64]*usagestats.AccountStats
	if params.SortBy == service.AccountSortCacheHitRate {
		items, err := readCacheHitRateStats(ctx, r.sql, UsageLogFilters{StartTime: &start, EndTime: &end}, "account", ids)
		if err != nil {
			return out, err
		}
		cacheByID = make(map[int64]usagestats.CacheHitRateItem, len(items))
		for _, item := range items {
			cacheByID[item.ID] = item
		}
	} else {
		todayByID, err = readAccountWindowStatsBatch(ctx, r.sql, ids, start)
		if err != nil {
			return out, err
		}
	}
	sortAccountMetricIDs(ids, params, cacheByID, todayByID)
	endOffset := offset + params.Limit()
	if endOffset > len(ids) {
		endOffset = len(ids)
	}
	pageIDs := ids[offset:endOffset]
	accounts, err := q.Where(dbaccount.IDIn(pageIDs...)).All(ctx)
	if err != nil {
		return out, err
	}
	hydrated, err := r.accountsToService(ctx, accounts)
	if err != nil {
		return out, err
	}
	byID := make(map[int64]service.Account, len(hydrated))
	for _, account := range hydrated {
		byID[account.ID] = account
	}
	if cacheByID != nil {
		out.CacheStats = make(map[int64]usagestats.CacheHitRateItem, len(pageIDs))
	} else {
		out.TodayStats = make(map[int64]*usagestats.AccountStats, len(pageIDs))
	}
	for _, id := range pageIDs {
		account, ok := byID[id]
		if !ok {
			continue
		}
		out.Accounts = append(out.Accounts, account)
		if item, ok := cacheByID[id]; ok {
			out.CacheStats[id] = item
		}
		if item, ok := todayByID[id]; ok {
			out.TodayStats[id] = item
		}
	}
	return out, nil
}

func sortAccountMetricIDs(ids []int64, params pagination.PaginationParams, cache map[int64]usagestats.CacheHitRateItem, today map[int64]*usagestats.AccountStats) {
	desc := params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderDesc
	sort.Slice(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		cmp := 0
		if params.SortBy == service.AccountSortCacheHitRate {
			av, bv := cache[a].CacheHitRate, cache[b].CacheHitRate
			if av == nil || bv == nil {
				if av == nil && bv == nil {
					return a < b
				}
				return av != nil
			}
			cmp = compareMetric(*av, *bv)
		} else {
			av, bv := today[a], today[b]
			if av == nil {
				av = &usagestats.AccountStats{}
			}
			if bv == nil {
				bv = &usagestats.AccountStats{}
			}
			switch params.SortBy {
			case "today_requests":
				cmp = compareMetric(av.Requests, bv.Requests)
			case "today_tokens":
				cmp = compareMetric(av.Tokens, bv.Tokens)
			case "today_account_cost":
				cmp = compareMetric(av.Cost, bv.Cost)
			case "today_user_cost":
				cmp = compareMetric(av.UserCost, bv.UserCost)
			}
		}
		if cmp == 0 {
			return a < b
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareMetric[T ~int64 | ~float64](a, b T) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
