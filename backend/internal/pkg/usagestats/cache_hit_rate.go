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

type CacheHitRateResult struct {
	StartDate string             `json:"start_date"`
	EndDate   string             `json:"end_date"`
	Items     []CacheHitRateItem `json:"items"`
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
