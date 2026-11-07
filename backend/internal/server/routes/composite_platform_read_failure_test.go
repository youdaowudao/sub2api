package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 组合分组读取请求体时上传中断：应返回带中文说明的 400，且不进入 handler。
func TestCompositeTargetPlatformMiddlewareTruncatedBodyReturnsChineseReadFailureMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(nil))
	handlerCalled := false
	router.POST("/v1/responses", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})

	body := io.MultiReader(strings.NewReader(`{"model":"gpt-5","input":"`), iotest.ErrReader(io.ErrUnexpectedEOF))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", io.NopCloser(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 8 << 20
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "Failed to read request body: 请求体太大", gjson.Get(w.Body.String(), "error.message").String())
	require.False(t, handlerCalled)
}
