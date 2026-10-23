package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type completionExitStore struct {
	ids    []int64
	before func()
	err    error
}

func (s *completionExitStore) RecordCompleted(_ context.Context, id int64) error {
	if s.before != nil {
		s.before()
	}
	s.ids = append(s.ids, id)
	return s.err
}

func (*completionExitStore) List(context.Context, int64, int64, int, bool) (*service.PluginUserCreationPage, error) {
	return nil, errors.New("a registration response must not call the plugin read API")
}

func completionExitContext(t *testing.T, store service.PluginUserCreationStore) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil).WithContext(service.WithUserCreationCapture(context.Background(), store))
	service.TrackUserCreation(ctx.Request.Context(), 71)
	return ctx, recorder
}

func completionExitAuthService() *service.AuthService {
	return service.NewAuthService(nil, nil, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "completion-exit-unit-test-key", ExpireHour: 1}}, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestUserCreationCompletionAccessOnlyFallbackPersistsBeforeSuccess(t *testing.T) {
	store := &completionExitStore{}
	ctx, recorder := completionExitContext(t, store)
	store.before = func() { require.Zero(t, recorder.Body.Len(), "completion must precede the success response") }
	// No refresh-token cache: this must retain the existing access-only fallback.
	respondWithTokenPair(ctx, completionExitAuthService(), &service.User{ID: 71, Email: "completion@example.invalid", Role: service.RoleUser, Status: service.StatusActive})
	require.Equal(t, []int64{71}, store.ids)
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestUserCreationCompletionStoreFailureDoesNotSendSuccess(t *testing.T) {
	store := &completionExitStore{err: errors.New("test completion storage failure")}
	ctx, recorder := completionExitContext(t, store)
	respondWithTokenPair(ctx, completionExitAuthService(), &service.User{ID: 71, Email: "completion@example.invalid", Role: service.RoleUser, Status: service.StatusActive})
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, []int64{71}, store.ids)
}

func TestUserCreationCompletionDoesNotFlushUnrelatedCandidate(t *testing.T) {
	store := &completionExitStore{}
	ctx, recorder := completionExitContext(t, store)
	// A prior failed create attempt is unrelated to this existing user's success.
	respondWithTokenPair(ctx, completionExitAuthService(), &service.User{ID: 72, Email: "existing@example.invalid", Role: service.RoleUser, Status: service.StatusActive})
	require.Empty(t, store.ids)
	require.Equal(t, http.StatusOK, recorder.Code)
}
