package usagestats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateCacheHitRate(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		input, creation, read int64
		want                  *float64
	}{
		{"normal", 100, 20, 880, ratePtr(88)},
		{"no cache", 100, 0, 0, ratePtr(0)},
		{"empty", 0, 0, 0, nil},
		{"raw negative totals", -10, 0, 10, nil},
		{"negative denominator", -20, 0, 10, nil},
		{"do not round", 1, 1, 1, ratePtr(100.0 / 3)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateCacheHitRate(tc.input, tc.creation, tc.read)
			if tc.want == nil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.InDelta(t, *tc.want, *got, 1e-10)
			}
		})
	}
}

func ratePtr(v float64) *float64 { return &v }

func TestNewCacheHitRateResultMidnightGap(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	require.NoError(t, err)
	start := time.Date(2026, 9, 6, 1, 0, 0, 0, loc)
	end := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	items := []CacheHitRateItem{{Requests: 1}}
	result := NewCacheHitRateResult(start, end, items)
	require.Equal(t, "2026-09-06", result.StartDate)
	require.Equal(t, "2026-09-06", result.EndDate)
	require.Equal(t, items, result.Items)
}
