//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelOrderBindingInvalidType(t *testing.T) {
	h := NewChannelHandler(nil, nil, nil)
	r := gin.New()
	r.POST("/channels", h.Create)
	r.PUT("/channels/:id", h.Update)
	for _, path := range []string{"/channels", "/channels/1"} {
		method := http.MethodPost
		if path != "/channels" {
			method = http.MethodPut
		}
		for _, invalid := range []string{`[]`, `"bad"`, `{"openai":1}`, `{"openai":[1]}`} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, path, strings.NewReader(`{"name":"fixture","model_mapping_order":`+invalid+`}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "model_mapping_order")
		}
	}
	var create createChannelRequest
	var update updateChannelRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model_mapping_order":null}`), &create))
	require.NoError(t, json.Unmarshal([]byte(`{"model_mapping_order":null}`), &update))
	require.Nil(t, create.ModelMappingOrder)
	require.Nil(t, update.ModelMappingOrder)
}

func TestGroupOrderBindingInvalidType(t *testing.T) {
	h := NewGroupHandler(nil, nil, nil)
	r := gin.New()
	r.POST("/groups", h.Create)
	r.PUT("/groups/:id", h.Update)
	for _, path := range []string{"/groups", "/groups/1"} {
		method := http.MethodPost
		if path != "/groups" {
			method = http.MethodPut
		}
		for _, invalid := range []string{`{}`, `"bad"`, `[1]`} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, path, strings.NewReader(`{"name":"fixture","model_routing_order":`+invalid+`}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "model_routing_order")
		}
	}
	var create CreateGroupRequest
	var update UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model_routing_order":null}`), &create))
	require.NoError(t, json.Unmarshal([]byte(`{"model_routing_order":null}`), &update))
	require.Nil(t, create.ModelRoutingOrder)
	require.Nil(t, update.ModelRoutingOrder)
}
