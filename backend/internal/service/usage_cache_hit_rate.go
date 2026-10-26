package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// ResolveCacheHitRateWindow bounds cache-rate queries to at most two local dates.
// The returned end is exclusive. Calendar arithmetic also handles DST days.
func ResolveCacheHitRateWindow(startDate, endDate, userTZ string, now time.Time) (time.Time, time.Time, error) {
	start, startErr := timezone.ParseInUserLocation("2006-01-02", startDate, userTZ)
	end, endErr := timezone.ParseInUserLocation("2006-01-02", endDate, userTZ)
	if startErr != nil || endErr != nil || start.Format("2006-01-02") != startDate || end.Format("2006-01-02") != endDate {
		return time.Time{}, time.Time{}, errors.New("start_date and end_date are required, format YYYY-MM-DD")
	}
	today := timezone.StartOfDayInUserLocation(now, userTZ)
	yesterday := today.AddDate(0, 0, -1)
	tomorrow := today.AddDate(0, 0, 1)
	switch {
	case start.Equal(today) && end.Equal(today):
		return today, tomorrow, nil
	case start.Equal(yesterday) && end.Equal(yesterday):
		return yesterday, today, nil
	default:
		return yesterday, tomorrow, nil
	}
}

type usageCacheHitRateReader interface {
	GetCacheHitRateStats(context.Context, usagestats.UsageLogFilters, string, []int64) ([]usagestats.CacheHitRateItem, error)
}

func (s *UsageService) GetCacheHitRateStats(ctx context.Context, filters usagestats.UsageLogFilters, dimension string, accountIDs []int64) ([]usagestats.CacheHitRateItem, error) {
	reader, ok := s.usageRepo.(usageCacheHitRateReader)
	if !ok {
		return nil, errors.New("cache rate statistics reader is unavailable")
	}
	return reader.GetCacheHitRateStats(ctx, filters, dimension, accountIDs)
}

// ParseCacheHitRateDefaultRange makes missing and legacy invalid settings safe.
func ParseCacheHitRateDefaultRange(value string) string {
	switch value {
	case "today", "yesterday", "yesterdayAndToday":
		return value
	default:
		return "today"
	}
}
