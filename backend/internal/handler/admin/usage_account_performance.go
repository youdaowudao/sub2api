package admin

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/gin-gonic/gin"
)

func (h *UsageHandler) accountPerformance(c *gin.Context, filters usagestats.UsageLogFilters, ids []int64, platform string, exact bool) {
	// The opt-in returns account and provider-error information. Do not expand
	// the restricted management-key contract of the existing usage endpoint.
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.Forbidden(c, "account performance requires unrestricted administrator access")
		return
	}
	state := "available"
	if h.opsService == nil || !h.opsService.IsMonitoringEnabled(c.Request.Context()) {
		state = "monitoring_disabled"
	} else if filters.RequestType != nil || filters.Stream != nil || filters.NativeCompactionV2 != nil ||
		filters.BillingType != nil || filters.BillingMode != "" || filters.UpstreamModelMismatch != nil {
		state = "unsupported_filters"
	}
	load := func(ctx context.Context) (usagestats.AccountPerformanceResult, error) {
		items, err := h.usageService.GetAccountPerformanceStats(ctx, filters, ids, platform, state == "available")
		if err != nil {
			return usagestats.AccountPerformanceResult{}, err
		}
		if items == nil {
			items = []usagestats.AccountPerformanceItem{}
		}
		start, end := *filters.StartTime, *filters.EndTime
		return usagestats.AccountPerformanceResult{
			StartDate: start.Format("2006-01-02"), EndDate: end.Add(-time.Nanosecond).Format("2006-01-02"),
			WindowStart: start.Format(time.RFC3339Nano), WindowEnd: end.Format(time.RFC3339Nano),
			GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), ErrorState: state, Items: items,
		}, nil
	}
	var result usagestats.AccountPerformanceResult
	var err error
	ctx := c.Request.Context()
	if exact || parseBoolQueryWithDefault(c.Query("nocache"), false) {
		result, err = load(ctx)
	} else {
		key := mustMarshalDashboardCacheKey(struct {
			UsageKey    string
			Platform    string
			State       string
			Performance bool
		}{usageCacheHitRateCacheKey("account", filters, ids), platform, state, true})
		entry, _, loadErr := usageCacheHitRateCache.GetOrLoad(key, func() (any, error) { return load(ctx) })
		err = loadErr
		if err == nil {
			result, err = snapshotPayloadAs[usagestats.AccountPerformanceResult](entry.Payload)
		}
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
