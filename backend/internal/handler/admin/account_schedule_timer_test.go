package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scheduleTimerAdminStub struct {
	*stubAdminService
	service.ScopedAdminAccounts
	calls  int
	id     int64
	hours  int
	result *service.AccountScheduleTimerResult
	err    error
}

func (s *scheduleTimerAdminStub) StartAccountScheduleTimer(_ context.Context, id int64, hours int) (*service.AccountScheduleTimerResult, error) {
	s.calls++
	s.id, s.hours = id, hours
	return s.result, s.err
}

func requestScheduleTimer(t *testing.T, stub *scheduleTimerAdminStub, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Request = httptest.NewRequest(http.MethodPost, "/accounts/"+id+"/scheduling-timer", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	(&AccountHandler{adminService: stub}).StartScheduleTimer(c)
	return w
}

func TestStartScheduleTimerHandler(t *testing.T) {
	until := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	stub := &scheduleTimerAdminStub{result: &service.AccountScheduleTimerResult{
		ID: 17, Schedulable: false, ScheduleResumeAt: &until,
	}}
	w := requestScheduleTimer(t, stub, "17", `{"duration_hours":6}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, stub.calls)
	require.Equal(t, int64(17), stub.id)
	require.Equal(t, 6, stub.hours)
	var response struct {
		Data service.AccountScheduleTimerResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.False(t, response.Data.Schedulable)
	require.Equal(t, until, *response.Data.ScheduleResumeAt)
}

func TestStartScheduleTimerHandlerRejectsInvalidInput(t *testing.T) {
	for _, input := range []struct{ id, body string }{
		{"0", `{"duration_hours":6}`}, {"bad", `{"duration_hours":6}`},
		{"17", `{}`}, {"17", `{"duration_hours":30}`},
		{"17", `{"duration_hours":6.5}`}, {"17", `{"duration_hours":"6"}`},
	} {
		stub := &scheduleTimerAdminStub{}
		w := requestScheduleTimer(t, stub, input.id, input.body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, stub.calls, "invalid requests must not mutate scheduling")
	}
}

func TestStartScheduleTimerHandlerPreservesKnownFailure(t *testing.T) {
	stub := &scheduleTimerAdminStub{err: service.ErrScheduleTimerDisabled}
	w := requestScheduleTimer(t, stub, "17", `{"duration_hours":12}`)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "账号已关闭调度")
	require.NotContains(t, w.Body.String(), `"schedulable":false`)
}

func TestStartScheduleTimerHandlerDistinguishesUnsavedAndUnknown(t *testing.T) {
	known := infraerrors.ServiceUnavailable("ACCOUNT_SCHEDULE_TIMER_NOT_SAVED",
		"数据库操作失败，未能保存自动开启时间；本次没有更改调度或计时，请稍后重试").WithCause(errors.New("database diagnostic"))
	for _, test := range []struct {
		err    error
		status int
		reason string
	}{
		{known, http.StatusServiceUnavailable, "ACCOUNT_SCHEDULE_TIMER_NOT_SAVED"},
		{errors.New("commit result unknown"), http.StatusInternalServerError, ""},
	} {
		w := requestScheduleTimer(t, &scheduleTimerAdminStub{err: test.err}, "17", `{"duration_hours":6}`)
		require.Equal(t, test.status, w.Code)
		var body struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, test.reason, body.Reason)
		require.NotContains(t, body.Message, "diagnostic")
		require.NotContains(t, body.Message, "commit result")
		if test.reason != "" {
			require.Contains(t, body.Message, "本次没有更改调度或计时")
			require.Contains(t, body.Message, "重试")
		}
	}
}

func TestScheduleTimerRetainsRestrictedAdminRouteBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &scheduleTimerAdminStub{}
	h := &AccountHandler{adminService: stub}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{}))
	})
	router.Use(h.ScopedAdminKeyAccounts)
	called := false
	router.POST("/api/v1/admin/accounts/:id/scheduling-timer", func(c *gin.Context) { called = true })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/17/scheduling-timer", strings.NewReader(`{"duration_hours":6}`)))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, called)
	require.Zero(t, stub.calls)
}
