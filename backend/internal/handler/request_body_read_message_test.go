package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 上传途中连接断开（请求体被截断）时，Codex 使用的 Responses 入口应返回
// 保留英文前缀的中文说明，而不是只有一句英文。
func TestOpenAIResponsesTruncatedBodyReturnsChineseReadFailureMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	body := io.MultiReader(strings.NewReader(`{"model":"gpt-5.2","input":"`), iotest.ErrReader(io.ErrUnexpectedEOF))
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", io.NopCloser(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.ContentLength = 8 << 20

	groupID := int64(10)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      342,
		GroupID: &groupID,
		User:    &service.User{ID: 357, Status: service.StatusActive},
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 357})

	h := &OpenAIGatewayHandler{
		gatewayService:      &service.OpenAIGatewayService{},
		billingCacheService: &service.BillingCacheService{},
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper: &ConcurrencyHelper{
			concurrencyService: &service.ConcurrencyService{},
		},
	}
	h.Responses(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "invalid_request_error", gjson.GetBytes(w.Body.Bytes(), "error.type").String())
	require.Equal(t, "Failed to read request body: 请求体太大", gjson.GetBytes(w.Body.Bytes(), "error.message").String())
}
