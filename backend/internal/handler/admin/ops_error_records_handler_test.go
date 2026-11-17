package admin

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseOpsErrorRecordsFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parse := func(query string, members bool) {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/errors"+query, nil)
		f, err := parseOpsErrorRecordsFilter(c, members)
		require.NoError(t, err)
		require.Equal(t, "all", f.Scope)
		require.Equal(t, "hide", f.Finance)
		require.Equal(t, "all", f.View)
		require.Equal(t, 10, f.PageSize)
	}
	parse("", false)
	for _, q := range []string{"?scope=no", "?page=0", "?page_size=101", "?finance=errors", "?view=no", "?user_id=-1", "?sort_by=message", "?error_owner=someone", "?status_codes=600", "?status_codes=503&status_codes_other=true", "?start_time=2026-01-01T00:00:00Z", "?start_time=2026-01-01T00:00:00Z&end_time=2026-01-01T00:00:00Z", "?time_range=custom", "?resolved=1"} {
		t.Run(q, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/errors"+q, nil)
			_, err := parseOpsErrorRecordsFilter(c, false)
			require.Error(t, err)
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/errors?scope=request&view=all&error_owner=platform&user_id=1&api_key_id=2&status_codes=0,503&start_time=2026-01-01T00:00:00Z&end_time=2026-03-01T00:00:00Z", nil)
	f, err := parseOpsErrorRecordsFilter(c, false)
	require.NoError(t, err)
	require.Equal(t, "platform", f.Owner)
	require.EqualValues(t, 1, *f.UserID)
	require.EqualValues(t, 2, *f.APIKeyID)
	require.Equal(t, []int{0, 503}, f.StatusCodes)
	c.Request = httptest.NewRequest("GET", "/errors?display=raw", nil)
	_, err = parseOpsErrorRecordsFilter(c, true)
	require.Error(t, err)
}
