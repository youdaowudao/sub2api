package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/tidwall/gjson"
)

// 读取请求体时上传中断：白名单中间件应返回带中文说明的 400，且不进入 handler。
func TestGroupModelAllowlistTruncatedBodyReturnsChineseReadFailureMessage(t *testing.T) {
	router, calls := newGroupModelAllowlistTestRouter(allowlistAPIKey(true, "claude-sonnet-4.5"), "/v1")

	body := io.MultiReader(strings.NewReader(`{"model":"claude-sonnet-4.5","messages":[`), iotest.ErrReader(io.ErrUnexpectedEOF))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", io.NopCloser(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 8 << 20
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	message := gjson.Get(w.Body.String(), "error.message").String()
	if message != "Failed to read request body: 请求体太大" {
		t.Fatalf("expected Chinese read-failure message, got %q", message)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected handler not to run, got %v", *calls)
	}
}
