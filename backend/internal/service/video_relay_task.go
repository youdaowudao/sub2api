package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	VideoRelayStatusSubmitting  = "submitting"
	VideoRelayStatusUnconfirmed = "unconfirmed"
	VideoRelayStatusQueued      = "queued"
	VideoRelayStatusInProgress  = "in_progress"
	VideoRelayStatusCompleted   = "completed"
	VideoRelayStatusFailed      = "failed"

	VideoRelayHeld     = "held"
	VideoRelayCharged  = "charged"
	VideoRelayRefunded = "refunded"

	VideoRelayNotSent          = "not_sent"
	VideoRelayUpstreamRejected = "upstream_rejected"
	VideoRelayUpstreamFailed   = "upstream_failed"
	VideoRelayUnconfirmed      = "unconfirmed_unresolvable"
)

var (
	ErrVideoRelayNotFound    = errors.New("video task not found")
	ErrVideoRelaySendBlocked = errors.New("video route is confirming an earlier submission")
	ErrVideoRelayNoSend      = errors.New("task is no longer eligible to submit a video")
)

type VideoRelayTask struct {
	TaskID    string
	UserID    int64
	APIKeyID  int64
	GroupID   int64
	AccountID int64

	Model          string
	UpstreamModel  string
	Seconds        int
	Resolution     string
	TotalCost      float64
	HoldAmount     float64
	RateMultiplier float64

	SiteFingerprint       string
	CredentialFingerprint string
	RouteFingerprint      string
	UpstreamTaskID        string
	UpstreamRequestID     string

	Status        string
	BillingState  string
	Progress      int
	ErrorMessage  string
	FailureReason string

	SentAt       *time.Time
	LastLookupAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	SettledAt    *time.Time
}

const VideoRelayConfirmationWindow = 30 * time.Minute

func (task *VideoRelayTask) UsageRequestID() string {
	return "video_relay:" + task.TaskID
}

func (task *VideoRelayTask) CanDownload() bool {
	return task.Status == VideoRelayStatusCompleted && task.BillingState == VideoRelayCharged &&
		ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID)
}

func (task *VideoRelayTask) CanLookupSubmission() bool {
	return task.SentAt != nil && !ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID) &&
		(task.BillingState == VideoRelayHeld ||
			(task.BillingState == VideoRelayRefunded && task.FailureReason == VideoRelayUnconfirmed))
}

func ValidVideoRelayTaskID(id string) bool {
	const prefix = "video_"
	if len(id) != len(prefix)+32 || !strings.HasPrefix(id, prefix) {
		return false
	}
	for i := len(prefix); i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func IsOpenAIVideoRelayRoute(key *APIKey, method, template string) bool {
	if key == nil || key.Group == nil || key.Group.Platform != PlatformOpenAI {
		return false
	}
	switch template {
	case "/v1/videos", "/videos":
		return method == http.MethodPost
	case "/v1/videos/:request_id", "/videos/:request_id", "/v1/videos/:request_id/content", "/videos/:request_id/content":
		return method == http.MethodGet
	default:
		return false
	}
}

func isVideoRelayRequestPath(path string) bool {
	path = strings.TrimPrefix(path, "/v1/")
	prefix := "videos"
	path = strings.TrimPrefix(path, "/")
	if path == prefix {
		return true
	}
	id, ok := strings.CutPrefix(path, prefix+"/")
	return ok && ValidVideoRelayTaskID(strings.TrimSuffix(id, "/content"))
}

func ValidVideoRelayUpstreamTaskID(id string) bool {
	if len(id) == 0 || len(id) > 128 || strings.Trim(id, ".") == "" {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

type VideoRelayChargeRecord struct {
	RequestID string
	TaskID    string
	Model     string
	Path      string
}

type VideoRelaySettlement struct {
	Completed     bool
	FailureReason string
	ErrorMessage  string
	AccountType   string
	Billing       UsageBillingCommand
}

type VideoRelayRepository interface {
	Reserve(ctx context.Context, task *VideoRelayTask) error
	Get(ctx context.Context, taskID string, userID, apiKeyID int64) (*VideoRelayTask, error)
	AcquireSend(ctx context.Context, task *VideoRelayTask) (*VideoRelayTask, error)
	RecordSubmission(ctx context.Context, sent *VideoRelayTask, requestID, upstreamTaskID string) (*VideoRelayTask, error)
	ClaimLookup(ctx context.Context, taskID string) (task *VideoRelayTask, claimed bool, err error)
	ConfirmLookup(ctx context.Context, ticket *VideoRelayTask, collectionPath string, logs []VideoRelayChargeRecord) (*VideoRelayTask, error)
	UpdateProgress(ctx context.Context, task *VideoRelayTask, status string, progress int) (*VideoRelayTask, error)
	Settle(ctx context.Context, task *VideoRelayTask, change VideoRelaySettlement) (*VideoRelayTask, *UsageBillingApplyResult, error)
}
