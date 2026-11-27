package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func positiveID(id int64) *int64 {
	if id > 0 {
		return &id
	}
	return nil
}

// GetAccountPerformanceStats uses a single SQL statement, pre-aggregating the
// two log tables independently to avoid multiplying rows across tables.
func (r *usageLogRepository) GetAccountPerformanceStats(ctx context.Context, filters UsageLogFilters, accountIDs []int64, platform string, includeErrors bool) ([]usagestats.AccountPerformanceItem, error) {
	query, args := buildAccountPerformanceQuery(filters, accountIDs, platform, includeErrors)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]usagestats.AccountPerformanceItem, 0)
	for rows.Next() {
		var item usagestats.AccountPerformanceItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &item.Requests, &item.InputTokens, &item.CacheCreationTokens,
			&item.CacheReadTokens, &item.AverageDurationMs, &item.UpstreamErrorCount, &item.RateLimitCount, &item.Schedulable, &item.AccountExists); err != nil {
			return nil, err
		}
		item.CacheHitRate = usagestats.CalculateCacheHitRate(item.InputTokens, item.CacheCreationTokens, item.CacheReadTokens)
		items = append(items, item)
	}
	return items, rows.Err()
}

func buildAccountPerformanceQuery(filters UsageLogFilters, accountIDs []int64, platform string, includeErrors bool) (string, []any) {
	conditions, args := buildUsageLogStatsConditions(filters)
	if len(accountIDs) > 0 {
		args = append(args, pq.Array(accountIDs))
		conditions = append(conditions, fmt.Sprintf("account_id = ANY($%d)", len(args)))
	}
	// Filter with the existing usage predicates before joining group/account
	// names. The platform filter follows the Ops usage-side platform semantics.
	from := fmt.Sprintf("(SELECT account_id, group_id, input_tokens, cache_creation_tokens, cache_read_tokens, duration_ms FROM usage_logs %s) ul", buildWhere(conditions))
	where := ""
	if platform != "" {
		args = append(args, platform)
		from += " LEFT JOIN groups g ON g.id = ul.group_id LEFT JOIN accounts a ON a.id = ul.account_id"
		where = fmt.Sprintf("WHERE COALESCE(NULLIF(g.platform,''), a.platform) = $%d", len(args))
	}
	query := fmt.Sprintf(`WITH u AS (
		SELECT ul.account_id AS id, COUNT(*) AS requests,
		COALESCE(SUM(ul.input_tokens),0) AS input_tokens,
		COALESCE(SUM(ul.cache_creation_tokens),0) AS cache_creation_tokens,
		COALESCE(SUM(ul.cache_read_tokens),0) AS cache_read_tokens,
		AVG(ul.duration_ms) AS average_duration_ms
		FROM %s %s GROUP BY ul.account_id
	)`, from, where)
	idExpr, join, errorCount, rateLimitCount := "u.id", "u", "NULL::bigint", "NULL::bigint"
	if includeErrors {
		filter := &service.OpsErrorRecordsFilter{
			Scope: "upstream", Finance: "include", Display: "raw",
			OpsErrorLogFilter: service.OpsErrorLogFilter{
				StartTime: filters.StartTime, EndTime: filters.EndTime, Platform: platform,
				UserID: positiveID(filters.UserID), APIKeyID: positiveID(filters.APIKeyID),
				AccountID: positiveID(filters.AccountID), GroupID: positiveID(filters.GroupID), Model: filters.Model, View: "all",
			},
		}
		var errorWhere string
		errorWhere, args = buildOpsErrorRecordsWhereWithArgs(filter, args)
		errorWhere += " AND e.account_id > 0"
		if len(accountIDs) > 0 {
			args = append(args, pq.Array(accountIDs))
			errorWhere += fmt.Sprintf(" AND e.account_id = ANY($%d)", len(args))
		}
		query += `, errors_by_account AS (
			SELECT e.account_id AS id, COUNT(*) AS errors,
			COUNT(*) FILTER (WHERE COALESCE(e.upstream_status_code,e.status_code,0)=429) AS rate_limits
			FROM ops_error_logs e ` + errorWhere + ` GROUP BY e.account_id
		)`
		idExpr, join = "COALESCE(u.id, e.id)", "u FULL OUTER JOIN errors_by_account e ON u.id=e.id"
		errorCount, rateLimitCount = "COALESCE(e.errors,0)", "COALESCE(e.rate_limits,0)"
	}
	query += fmt.Sprintf(` SELECT %s, COALESCE(a.name,''), COALESCE(a.platform,''),
		COALESCE(u.requests,0), COALESCE(u.input_tokens,0), COALESCE(u.cache_creation_tokens,0), COALESCE(u.cache_read_tokens,0),
		u.average_duration_ms, %s, %s,
		CASE WHEN a.deleted_at IS NULL THEN a.schedulable ELSE NULL END,
		(a.id IS NOT NULL AND a.deleted_at IS NULL)
		FROM %s LEFT JOIN accounts a ON a.id=%s ORDER BY COALESCE(u.requests,0) DESC, %s ASC`, idExpr, errorCount, rateLimitCount, join, idExpr, idExpr)
	return query, args
}
