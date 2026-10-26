package admin

import (
	"context"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

var usageCacheHitRateCache = newSnapshotCache(30 * time.Second)

func normalizeCacheHitRateAccountIDs(ids []int64) []int64 {
	result := append([]int64{}, ids...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	n := 0
	for _, id := range result {
		if n == 0 || result[n-1] != id {
			result[n] = id
			n++
		}
	}
	return result[:n]
}

func usageCacheHitRateCacheKey(dimension string, filters usagestats.UsageLogFilters, accountIDs []int64) string {
	start, end := "", ""
	if filters.StartTime != nil {
		start = filters.StartTime.UTC().Format(time.RFC3339)
	}
	if filters.EndTime != nil {
		end = filters.EndTime.UTC().Format(time.RFC3339)
	}
	return mustMarshalDashboardCacheKey(struct {
		usageStatsCacheKeyData
		Dimension  string  `json:"dimension"`
		AccountIDs []int64 `json:"account_ids"`
	}{
		usageStatsCacheKeyData: usageStatsCacheKeyData{
			StartTime: start, EndTime: end,
			UserID: filters.UserID, APIKeyID: filters.APIKeyID, AccountID: filters.AccountID, GroupID: filters.GroupID,
			Model: filters.Model, BillingMode: filters.BillingMode,
			RequestType: filters.RequestType, Stream: filters.Stream, NativeCompactionV2: filters.NativeCompactionV2,
			BillingType: filters.BillingType, UpstreamModelMismatch: filters.UpstreamModelMismatch,
		},
		Dimension: dimension, AccountIDs: normalizeCacheHitRateAccountIDs(accountIDs),
	})
}

func (h *UsageHandler) getCacheHitRateCached(ctx context.Context, filters usagestats.UsageLogFilters, dimension string, accountIDs []int64) ([]usagestats.CacheHitRateItem, error) {
	entry, _, err := usageCacheHitRateCache.GetOrLoad(usageCacheHitRateCacheKey(dimension, filters, accountIDs), func() (any, error) {
		return h.usageService.GetCacheHitRateStats(ctx, filters, dimension, accountIDs)
	})
	if err != nil {
		return nil, err
	}
	return snapshotPayloadAs[[]usagestats.CacheHitRateItem](entry.Payload)
}
