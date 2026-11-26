//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type checkinHandlerFake struct {
	checkinUserService
	id                       int64
	month, locale, emailCode string
	proof                    service.CheckinProof
	err                      error
}

func (f *checkinHandlerFake) GetState(_ context.Context, id int64, month string) (service.CheckinStateResponse, error) {
	f.id = id
	f.month = month
	return service.CheckinStateResponse{Enabled: false}, f.err
}
func (f *checkinHandlerFake) ListMonth(_ context.Context, id int64, month string) (service.CheckinMonthResponse, error) {
	f.id = id
	f.month = month
	return service.CheckinMonthResponse{Month: month, Records: []service.CheckinMonthRecord{}}, f.err
}
func (f *checkinHandlerFake) Checkin(_ context.Context, id int64, _ string, proof service.CheckinProof, email string) (service.CheckinResult, error) {
	f.id = id
	f.proof = proof
	f.emailCode = email
	return service.CheckinResult{Amount: 0.23, Balance: 12.34}, f.err
}
func (f *checkinHandlerFake) SendEmailCode(_ context.Context, id int64, locale string) (service.CheckinSendCodeResult, error) {
	f.id = id
	f.locale = locale
	return service.CheckinSendCodeResult{MaskedEmail: "u***@example.test", CooldownSeconds: 60}, f.err
}
func TestCheckinUserHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &checkinHandlerFake{}
	h := &CheckinHandler{service: fake}
	r := gin.New()
	authed := r.Group("/user")
	authed.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 77})
		c.Next()
	})
	authed.GET("/checkin", h.GetState)
	authed.GET("/checkin/records", h.ListMonth)
	authed.POST("/checkin", h.Submit)
	authed.POST("/checkin/email-code", h.SendEmailCode)
	r.POST("/unauth", h.Submit)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "zh-CN")
		r.ServeHTTP(w, req)
		return w
	}
	w := call(http.MethodGet, "/user/checkin?month=2026-11", "")
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(77), fake.id)
	require.Equal(t, "2026-11", fake.month)
	require.Contains(t, w.Body.String(), `"enabled":false`)
	w = call(http.MethodGet, "/user/checkin/records?month=2026-10", "")
	require.Equal(t, 200, w.Code)
	require.Equal(t, "2026-10", fake.month)
	w = call(http.MethodPost, "/user/checkin", `{"captcha_token":"fixture","captcha_randstr":"fixture-rand","email_code":"123456"}`)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "fixture", fake.proof.Token)
	require.Equal(t, "fixture-rand", fake.proof.Randstr)
	require.Equal(t, "123456", fake.emailCode)
	require.Contains(t, w.Body.String(), `"balance":12.34`)
	w = call(http.MethodPost, "/user/checkin/email-code", "")
	require.Equal(t, 200, w.Code)
	require.Equal(t, "zh-CN", fake.locale)
	w = call(http.MethodPost, "/unauth", `{}`)
	require.Equal(t, 401, w.Code)
	fake.err = service.ErrCheckinDisabled
	w = call(http.MethodPost, "/user/checkin", `{}`)
	require.Equal(t, 403, w.Code)
	require.Contains(t, w.Body.String(), "CHECKIN_DISABLED")
}
