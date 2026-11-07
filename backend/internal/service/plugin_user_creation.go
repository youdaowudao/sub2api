package service

import (
	"context"
	"errors"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const userCreationCompletionTimeout = 5 * time.Second

// PluginUserCreation is a completed creation flow, rather than every saved user.
// Email is the current email and is populated only for an authorized read.
type PluginUserCreation struct {
	EventID      string    `json:"event_id"`
	UserID       int64     `json:"user_id"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at"`
	SignupSource string    `json:"signup_source"`
	Email        string    `json:"email,omitempty"`
}

// PluginUserCreationPage bounds one scan round. Once Done is true, a reader
// starts another round from zero so a lower ID completed later is not lost.
type PluginUserCreationPage struct {
	Items         []PluginUserCreation `json:"items"`
	NextUserID    int64                `json:"next_user_id"`
	ThroughUserID int64                `json:"through_user_id"`
	Done          bool                 `json:"done"`
}

// PluginUserCreationStore persists completion independently of the original
// creation transactions. Neither operation invokes a plugin or its KV store.
type PluginUserCreationStore interface {
	RecordCompleted(ctx context.Context, userID int64) error
	List(ctx context.Context, after, through int64, limit int, includeEmail bool) (*PluginUserCreationPage, error)
}

type userCreationCaptureContextKey struct{}

type userCreationCapture struct {
	mu         sync.Mutex
	store      PluginUserCreationStore
	candidates map[int64]bool // true only after RecordCompleted has succeeded.
}

// WithUserCreationCapture initializes request-local creation candidates. The
// middleware must initialize it even if no plugin is installed or enabled.
func WithUserCreationCapture(ctx context.Context, store PluginUserCreationStore) context.Context {
	return context.WithValue(ctx, userCreationCaptureContextKey{}, &userCreationCapture{
		store:      store,
		candidates: make(map[int64]bool),
	})
}

// TrackUserCreation collects an actual new ID after the repository has returned
// it. An outer transaction may still roll back, so this does not record an event.
func TrackUserCreation(ctx context.Context, userID int64) {
	capture := userCreationCaptureFromContext(ctx)
	if capture == nil || userID <= 0 {
		return
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if _, exists := capture.candidates[userID]; !exists {
		capture.candidates[userID] = false
	}
}

// CompleteUserCreation is called only at an explicit success exit, with the
// trusted final user ID, after all original failure and compensation branches.
// A standalone service call without capture is deliberately a no-op.
func CompleteUserCreation(ctx context.Context, userID int64) error {
	capture := userCreationCaptureFromContext(ctx)
	if capture == nil {
		return nil
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	completed, exists := capture.candidates[userID]
	if !exists || completed {
		return nil
	}
	if capture.store == nil {
		return userCreationNotConfirmed(errors.New("user creation completion store is not configured"), false)
	}

	completionCtx, cancel := context.WithTimeout(ctx, userCreationCompletionTimeout)
	defer cancel()
	if err := capture.store.RecordCompleted(completionCtx, userID); err != nil {
		// A failed acknowledgment does not prove that the database rolled back.
		// Do not retry asynchronously or let callers undo the original account.
		return userCreationNotConfirmed(err, true)
	}
	capture.candidates[userID] = true
	return nil
}

func userCreationCaptureFromContext(ctx context.Context) *userCreationCapture {
	if ctx == nil {
		return nil
	}
	capture, _ := ctx.Value(userCreationCaptureContextKey{}).(*userCreationCapture)
	return capture
}

func userCreationNotConfirmed(cause error, outcomeUnknown bool) error {
	err := infraerrors.ServiceUnavailable(
		"USER_CREATION_NOT_CONFIRMED",
		"账号已保存，但创建完成信息未能确认保存。请先尝试登录或在用户列表核对，勿重复创建。",
	).WithCause(cause)
	if outcomeUnknown {
		err = err.WithMetadata(map[string]string{"outcome_unknown": "true"})
	}
	return err
}
