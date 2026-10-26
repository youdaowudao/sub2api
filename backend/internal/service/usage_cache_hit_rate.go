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
	// Validate civil dates independently of whether local midnight exists.
	start, startErr := time.Parse("2006-01-02", startDate)
	end, endErr := time.Parse("2006-01-02", endDate)
	if startErr != nil || endErr != nil || start.Format("2006-01-02") != startDate || end.Format("2006-01-02") != endDate {
		return time.Time{}, time.Time{}, errors.New("start_date and end_date are required, format YYYY-MM-DD")
	}
	location := timezone.StartOfDayInUserLocation(now, userTZ).Location()
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	yesterday, tomorrow := today.AddDate(0, 0, -1), today.AddDate(0, 0, 1)
	first, afterLast := yesterday, tomorrow
	switch {
	case start.Equal(today) && end.Equal(today):
		first = today
	case start.Equal(yesterday) && end.Equal(yesterday):
		afterLast = today
	}
	return cacheHitRateDayStart(first, location), cacheHitRateDayStart(afterLast, location), nil
}

// cacheHitRateDayStart resolves each civil day independently. Some zones skip
// midnight or repeat it; use the first instant belonging to that day.
func cacheHitRateDayStart(calendarDate time.Time, location *time.Location) time.Time {
	start := time.Date(calendarDate.Year(), calendarDate.Month(), calendarDate.Day(), 0, 0, 0, 0, location)
	localDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	zoneStart, zoneEnd := start.ZoneBounds()
	if localDate.Before(calendarDate) {
		// Go normalized a nonexistent midnight back into the previous day.
		return zoneEnd
	}
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		// Go normalized forward past a gap; the new zone starts this day.
		return zoneStart
	}
	if !zoneStart.IsZero() {
		// If Go chose the second midnight in a fold, try the previous offset.
		_, previousOffset := zoneStart.Add(-time.Nanosecond).Zone()
		earlier := calendarDate.Add(-time.Duration(previousOffset) * time.Second).In(location)
		if earlier.Before(start) && earlier.Format("2006-01-02 15:04:05") == calendarDate.Format("2006-01-02 15:04:05") {
			return earlier
		}
	}
	return start
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
