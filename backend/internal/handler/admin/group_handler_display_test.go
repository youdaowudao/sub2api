package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func groupDisplayRouter(svc service.AdminService) *gin.Engine {
	h := NewGroupHandler(svc, nil, nil)
	r := gin.New()
	r.POST("/groups", h.Create)
	r.PUT("/groups/:id", h.Update)
	return r
}

func sendGroupDisplayRequest(t *testing.T, r *gin.Engine, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}

func TestGroupHandlerDisplayRequestFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	icon, category, empty := "gemini", "domestic", ""
	for _, mode := range []string{"standard", "simple"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name                               string
				icon, category                     *string
				wantCreateIcon, wantCreateCategory string
			}{
				{name: "set", icon: &icon, category: &category, wantCreateIcon: "gemini", wantCreateCategory: "domestic"},
				{name: "omit"},
				{name: "clear", icon: &empty, category: &empty},
			} {
				t.Run(tc.name, func(t *testing.T) {
					svc := newStubAdminService()
					var r *gin.Engine
					if mode == "simple" {
						r = newSimpleModeGroupRouter(svc)
					} else {
						r = groupDisplayRouter(svc)
					}
					body := map[string]any{"name": "display group", "platform": "anthropic", "rate_multiplier": 1}
					if tc.icon != nil {
						body["display_icon"] = *tc.icon
					}
					if tc.category != nil {
						body["display_category"] = *tc.category
					}
					resp := sendGroupDisplayRequest(t, r, http.MethodPost, "/groups", body)
					require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
					require.Len(t, svc.createdGroups, 1)
					require.Equal(t, tc.wantCreateIcon, svc.createdGroups[0].DisplayIcon)
					require.Equal(t, tc.wantCreateCategory, svc.createdGroups[0].DisplayCategory)

					resp = sendGroupDisplayRequest(t, r, http.MethodPut, "/groups/1", body)
					require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
					require.Len(t, svc.updatedGroups, 1)
					require.Equal(t, tc.icon, svc.updatedGroups[0].DisplayIcon)
					require.Equal(t, tc.category, svc.updatedGroups[0].DisplayCategory)
				})
			}
		})
	}
}

type groupDisplayCountingRepository struct {
	service.AdminGroupRepository
	creates, updates, reads int
}

func (r *groupDisplayCountingRepository) Create(context.Context, *service.Group) error {
	r.creates++
	return nil
}

func (r *groupDisplayCountingRepository) Update(context.Context, *service.Group) error {
	r.updates++
	return nil
}

func (r *groupDisplayCountingRepository) GetByID(context.Context, int64) (*service.Group, error) {
	r.reads++
	return &service.Group{ID: 1, Name: "display group", Platform: service.PlatformAnthropic, RateMultiplier: 1}, nil
}

func TestGroupHandlerDisplayInvalidValuesReturnBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ field, value, reason string }{
		{"display_icon", "abc", "INVALID_GROUP_DISPLAY_ICON"},
		{"display_icon", "typesafe", "INVALID_GROUP_DISPLAY_ICON"},
		{"display_category", "other", "INVALID_GROUP_DISPLAY_CATEGORY"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			repo := &groupDisplayCountingRepository{}
			svc := service.NewAdminService(nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			r := groupDisplayRouter(svc)
			body := map[string]any{
				"name": "display group", "platform": "anthropic", "rate_multiplier": 1, tc.field: tc.value,
			}
			for _, route := range []struct{ method, path string }{
				{http.MethodPost, "/groups"},
				{http.MethodPut, "/groups/1"},
			} {
				t.Run(route.method, func(t *testing.T) {
					resp := sendGroupDisplayRequest(t, r, route.method, route.path, body)
					require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
					var payload struct {
						Reason string `json:"reason"`
					}
					require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
					require.Equal(t, tc.reason, payload.Reason)
					require.Zero(t, repo.creates)
					require.Zero(t, repo.updates)
					require.Zero(t, repo.reads)
				})
			}
		})
	}
}
