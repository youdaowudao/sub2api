package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestParseCacheHitRateSettings(t *testing.T) {
	s := NewSettingService(nil, &config.Config{})
	defaults := s.parseSettings(map[string]string{})
	require.False(t, defaults.AllowUserViewCacheHitRate)
	require.Equal(t, "today", defaults.CacheHitRateDefaultRange)
	for _, v := range []string{"today", "yesterday", "yesterdayAndToday", "week", ""} {
		settings := s.parseSettings(map[string]string{"allow_user_view_cache_hit_rate": "true", "cache_hit_rate_default_range": v})
		require.True(t, settings.AllowUserViewCacheHitRate)
		want := v
		if v == "week" || v == "" {
			want = "today"
		}
		require.Equal(t, want, settings.CacheHitRateDefaultRange)
	}
}
