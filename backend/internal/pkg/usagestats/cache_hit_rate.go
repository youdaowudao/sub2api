package usagestats

import "time"

// CacheHitRateItem contains raw prompt-token totals for one usage dimension.
type CacheHitRateItem struct {
	ID                  int64    `json:"id"`
	Name                string   `json:"name"`
	Platform            string   `json:"platform"`
	Requests            int64    `json:"requests"`
	InputTokens         int64    `json:"input_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheHitRate        *float64 `json:"cache_hit_rate"`
}

type AccountPerformanceItem struct {
	CacheHitRateItem
	AverageDurationMs  *float64 `json:"average_duration_ms"`
	UpstreamErrorCount *int64   `json:"upstream_error_count"`
	RateLimitCount     *int64   `json:"rate_limit_count"`
	Schedulable        *bool    `json:"schedulable"`
	AccountExists      bool     `json:"account_exists"`
}

type AccountPerformanceResult struct {
	StartDate   string                   `json:"start_date"`
	EndDate     string                   `json:"end_date"`
	WindowStart string                   `json:"window_start"`
	WindowEnd   string                   `json:"window_end"`
	GeneratedAt string                   `json:"generated_at"`
	ErrorState  string                   `json:"error_state"`
	Items       []AccountPerformanceItem `json:"items"`
}

type CacheHitRateResult struct {
	StartDate   string             `json:"start_date"`
	EndDate     string             `json:"end_date"`
	Items       []CacheHitRateItem `json:"items"`
	WindowStart string             `json:"window_start,omitempty"`
	WindowEnd   string             `json:"window_end,omitempty"`
}

// CalculateCacheHitRate preserves the recorded totals, including negative values.
func CalculateCacheHitRate(input, creation, read int64) *float64 {
	total := float64(input) + float64(creation) + float64(read)
	if total <= 0 {
		return nil
	}
	rate := float64(read) / total * 100
	return &rate
}

// NewCacheHitRateResult formats the actual inclusive dates of an exclusive window.
func NewCacheHitRateResult(start, end time.Time, items []CacheHitRateItem) CacheHitRateResult {
	return CacheHitRateResult{
		StartDate: start.Format("2006-01-02"),
		EndDate:   end.Add(-time.Nanosecond).Format("2006-01-02"),
		Items:     items,
	}
}
