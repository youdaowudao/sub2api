package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveCacheHitRateWindow(t *testing.T) {
	for _, tc := range []struct {
		name, now, start, end, wantStart, wantEnd, tz string
		hours                                         int
	}{
		{"today", "2026-10-26", "2026-10-26", "2026-10-26", "2026-10-26", "2026-10-27", "Asia/Shanghai", 24},
		{"yesterday", "2026-10-26", "2026-10-25", "2026-10-25", "2026-10-25", "2026-10-26", "Asia/Shanghai", 24},
		{"both", "2026-10-26", "2026-10-25", "2026-10-26", "2026-10-25", "2026-10-27", "Asia/Shanghai", 48},
		{"week", "2026-10-26", "2026-10-20", "2026-10-26", "2026-10-25", "2026-10-27", "Asia/Shanghai", 48},
		{"old single day", "2026-10-26", "2026-09-01", "2026-09-01", "2026-10-25", "2026-10-27", "Asia/Shanghai", 48},
		{"future end", "2026-10-26", "2026-10-26", "2026-10-27", "2026-10-25", "2026-10-27", "Asia/Shanghai", 48},
		{"reversed", "2026-10-26", "2026-10-26", "2026-10-25", "2026-10-25", "2026-10-27", "Asia/Shanghai", 48},
		{"month first", "2026-11-01", "2026-11-01", "2026-11-01", "2026-11-01", "2026-11-02", "Asia/Shanghai", 24},
		{"month second", "2026-11-02", "2026-11-01", "2026-11-02", "2026-11-01", "2026-11-03", "Asia/Shanghai", 48},
		{"month third", "2026-11-03", "2026-11-01", "2026-11-03", "2026-11-02", "2026-11-04", "Asia/Shanghai", 48},
		{"spring DST today", "2026-03-08", "2026-03-08", "2026-03-08", "2026-03-08", "2026-03-09", "America/New_York", 23},
		{"spring DST yesterday", "2026-03-09", "2026-03-08", "2026-03-08", "2026-03-08", "2026-03-09", "America/New_York", 23},
		{"fall DST both", "2026-11-01", "2026-10-31", "2026-11-01", "2026-10-31", "2026-11-02", "America/New_York", 49},
		{"historical midnight gap is a valid date", "2026-10-26", "2026-09-06", "2026-09-06", "2026-10-25", "2026-10-27", "America/Santiago", 48},
		{"midnight gap today", "2026-09-06", "2026-09-06", "2026-09-06", "2026-09-06", "2026-09-07", "America/Santiago", 23},
		{"midnight gap yesterday", "2026-09-07", "2026-09-06", "2026-09-06", "2026-09-06", "2026-09-07", "America/Santiago", 23},
		{"midnight gap both", "2026-09-06", "2026-09-05", "2026-09-06", "2026-09-05", "2026-09-07", "America/Santiago", 47},
		{"next day starts after midnight gap", "2026-09-05", "2026-09-05", "2026-09-05", "2026-09-05", "2026-09-06", "America/Santiago", 24},
		{"repeated midnight uses earliest occurrence", "2026-11-01", "2026-11-01", "2026-11-01", "2026-11-01", "2026-11-02", "America/Havana", 25},
		{"positive offset repeated midnight", "2021-10-29", "2021-10-29", "2021-10-29", "2021-10-29", "2021-10-30", "Asia/Amman", 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.tz)
			require.NoError(t, err)
			now, err := time.ParseInLocation("2006-01-02", tc.now, loc)
			require.NoError(t, err)
			start, end, err := ResolveCacheHitRateWindow(tc.start, tc.end, tc.tz, now.Add(12*time.Hour))
			require.NoError(t, err)
			require.Equal(t, tc.wantStart, start.Format("2006-01-02"))
			require.Equal(t, tc.wantEnd, end.Format("2006-01-02"))
			require.Equal(t, time.Duration(tc.hours)*time.Hour, end.Sub(start))
		})
	}
	for _, dates := range [][2]string{{"", "2026-10-26"}, {"2026-10-26", ""}, {"bad", "2026-10-26"}, {"2026-02-30", "2026-10-26"}, {"2026-1-1", "2026-10-26"}} {
		_, _, err := ResolveCacheHitRateWindow(dates[0], dates[1], "Asia/Shanghai", time.Now())
		require.ErrorContains(t, err, "start_date and end_date are required, format YYYY-MM-DD")
	}
}
