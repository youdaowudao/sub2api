package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userCreationAdminService struct {
	service.AdminService
	created bool
	deleted bool
}

func (s *userCreationAdminService) CreateUser(ctx context.Context, input *service.CreateUserInput) (*service.User, error) {
	s.created = true
	service.TrackUserCreation(ctx, 71)
	return &service.User{ID: 71, Email: input.Email, Role: service.RoleUser, Status: service.StatusActive}, nil
}

func (s *userCreationAdminService) DeleteUser(context.Context, int64) error {
	s.deleted = true
	return nil
}

type adminUserCreationStore struct {
	ids    []int64
	before func()
	err    error
}

func (s *adminUserCreationStore) RecordCompleted(_ context.Context, id int64) error {
	if s.before != nil {
		s.before()
	}
	s.ids = append(s.ids, id)
	return s.err
}

func (*adminUserCreationStore) List(context.Context, int64, int64, int, bool) (*service.PluginUserCreationPage, error) {
	return nil, errors.New("an admin creation must not read plugin events")
}

func TestUserCreationCompletionAdminCreate(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "unconfirmed"
		}
		t.Run(name, func(t *testing.T) {
			creator := &userCreationAdminService{}
			store := &adminUserCreationStore{}
			if fail {
				store.err = errors.New("completion write acknowledgment lost")
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", strings.NewReader(`{"email":"new@example.invalid","password":"test-password"}`)).WithContext(service.WithUserCreationCapture(context.Background(), store))
			ctx.Request.Header.Set("Content-Type", "application/json")
			store.before = func() {
				require.True(t, creator.created)
				require.Zero(t, recorder.Body.Len())
			}
			handler := &UserHandler{adminService: creator}
			handler.Create(ctx)
			require.Equal(t, []int64{71}, store.ids)
			require.False(t, creator.deleted, "completion storage failure must not delete the saved account")
			if fail {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				require.Contains(t, recorder.Body.String(), "USER_CREATION_NOT_CONFIRMED")
			} else {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), "new@example.invalid")
			}
		})
	}
}
