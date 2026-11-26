//go:build unit

package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type checkinAdminFake struct {
	checkinAdminService
	filter service.CheckinRecordFilter
	page   pagination.PaginationParams
	raw    string
	days   int
}

func (f *checkinAdminFake) SaveConfig(_ context.Context, raw []byte) (service.CheckinConfig, error) {
	f.raw = string(raw)
	return service.CheckinConfig{}, infraerrors.BadRequest("CHECKIN_CONFIG_INVALID", "veteran_min: invalid").WithMetadata(map[string]string{"field": "veteran_min"})
}
func (f *checkinAdminFake) ListAdminRecords(_ context.Context, filter service.CheckinRecordFilter, page pagination.PaginationParams) ([]service.CheckinAdminRecord, int64, error) {
	f.filter = filter
	f.page = page
	return []service.CheckinAdminRecord{}, 41, nil
}
func (f *checkinAdminFake) Overview(_ context.Context, days int) (service.CheckinOverview, error) {
	f.days = days
	return service.CheckinOverview{}, nil
}
func TestCheckinAdminHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := &checkinAdminFake{}
	h := &CheckinHandler{service: f}
	r := gin.New()
	r.PUT("/config", h.SaveConfig)
	r.GET("/records", h.Records)
	r.GET("/overview", h.Overview)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := call("PUT", "/config", `{"veteran_min":0.291}`)
	require.Equal(t, 400, w.Code)
	require.Equal(t, `{"veteran_min":0.291}`, f.raw)
	require.Contains(t, w.Body.String(), "CHECKIN_CONFIG_INVALID")
	require.Contains(t, w.Body.String(), `"field":"veteran_min"`)
	w = call("GET", "/records?date_from=2026-11-01&date_to=2026-11-26&search=fixture&tier=veteran&verification=none&ip=192.0.2.1&page=10&page_size=20", "")
	require.Equal(t, 200, w.Code)
	require.Equal(t, "2026-11-26", f.filter.DateTo)
	require.Equal(t, "192.0.2.1", f.filter.IP)
	require.Equal(t, "veteran", f.filter.Tier)
	require.Equal(t, 10, f.page.Page)
	require.Contains(t, w.Body.String(), `"total":41`)
	require.Equal(t, 400, call("GET", "/records?date_from=2026-02-30", "").Code)
	require.Equal(t, 200, call("GET", "/overview?days=90", "").Code)
	require.Equal(t, 90, f.days)
	require.Equal(t, 400, call("GET", "/overview?days=13", "").Code)
}
