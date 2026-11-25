package service

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenAIRouteCapability400(t *testing.T) {
	const code = "unsupported_parameter"
	const message = "The available upstream route does not support a requested input or tool capability."
	const contextLimitMessage = message + " maximum context length exceeded"
	const wrappedContextLimitMessage = `{"error":{"message":"` + message + ` maximum context\u0020length exceeded"}}`
	errorBody := func(code, message string) []byte {
		return []byte(fmt.Sprintf(`{"error":{"code":%q,"message":%q}}`, code, message))
	}

	for _, tc := range []struct {
		name    string
		code    string
		message string
		want    bool
	}{
		{"route capability", code, message, true},
		{"uppercase message", code, strings.ToUpper(message), true},
		{"ordinary unsupported parameter", code, "Unsupported parameter: 'temperature'", false},
		{"different code", "invalid_request_error", message, false},
		{"uppercase code", strings.ToUpper(code), message, false},
		{"context window error", code, contextLimitMessage, false},
		{"wrapped context window error", code, wrappedContextLimitMessage, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsOpenAIRouteCapabilityUnsupported400(errorBody(tc.code, tc.message)))
		})
	}

	managed := &OpenAIGatewayService{accountRepo: &modelNotFoundManagedAccountRepo{}}
	unmanaged := &OpenAIGatewayService{}
	body := errorBody(code, message)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	t.Run("failover eligibility", func(t *testing.T) {
		assert.True(t, managed.shouldFailoverOpenAIRouteCapability400(http.StatusBadRequest, message, body))
		assert.False(t, managed.shouldFailoverOpenAIRouteCapability400(http.StatusBadGateway, message, body))
		assert.False(t, managed.shouldFailoverOpenAIRouteCapability400(http.StatusBadRequest, contextLimitMessage, errorBody(code, contextLimitMessage)))
		assert.False(t, unmanaged.shouldFailoverOpenAIRouteCapability400(http.StatusBadRequest, message, body))
		assert.False(t, managed.shouldFailoverOpenAIUpstreamResponse(account, http.StatusBadRequest, message, body))
	})
}
