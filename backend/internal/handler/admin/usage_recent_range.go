package admin

import (
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const maxAdminRecentRange = 3 * time.Hour

// parseAdminRecentRange handles the new read-only exact range. Existing date
// parsing belongs to its original caller; don't reinterpret those requests.
func parseAdminRecentRange(c *gin.Context, performance bool) (start, end time.Time, exact bool, err error) {
	rawStart, hasStart := c.GetQuery("start_time")
	rawEnd, hasEnd := c.GetQuery("end_time")
	if !hasStart && !hasEnd {
		return start, end, false, nil
	}
	invalid := errors.New("invalid start_time/end_time")
	if !hasStart || !hasEnd || c.Request.URL.Query().Has("start_date") || c.Request.URL.Query().Has("end_date") {
		return start, end, true, invalid
	}
	if !performance {
		start, end, exact, err = parseDashboardRange(c)
		if err != nil || end.Sub(start) > maxAdminRecentRange {
			return start, end, true, invalid
		}
		return start, end, exact, nil
	}
	start, e1 := time.Parse(time.RFC3339Nano, rawStart)
	end, e2 := time.Parse(time.RFC3339Nano, rawEnd)
	now := timezone.NowInUserLocation(c.Query("timezone"))
	if e1 != nil || e2 != nil || !start.Before(end) || start.After(now) {
		return start, end, true, invalid
	}
	loc := now.Location()
	start, end = start.In(loc), end.In(loc)
	if end.Sub(start) > maxAdminRecentRange {
		today, tomorrow, e := service.ResolveCacheHitRateWindow(now.Format("2006-01-02"), now.Format("2006-01-02"), c.Query("timezone"), now)
		if e != nil || !start.Equal(today) || end.After(tomorrow) {
			return start, end, true, invalid
		}
	}
	return start, end, true, nil
}
