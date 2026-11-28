package admin

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type registrationEmailAuditRepo struct {
	service.AuditLogRepository
	filter *service.AuditLogFilter
}

func (r *registrationEmailAuditRepo) List(_ context.Context, f *service.AuditLogFilter) (*service.AuditLogList, error) {
	r.filter = f
	return &service.AuditLogList{Logs: []*service.AuditLog{}, Page: f.Page, PageSize: f.PageSize}, nil
}

func TestAuditLogListRegistrationEmailFilter(t *testing.T) {
	repo := &registrationEmailAuditRepo{}
	svc := service.NewAuditLogService(repo, nil)
	t.Cleanup(svc.Stop)
	h := NewAuditLogHandler(svc, nil)
	r := gin.New()
	const path = "/api/v1/admin/audit-logs"
	r.GET(path, h.List)
	query := url.Values{"registration_email": {" exact_test%email@example.com "}, "success": {"true"}, "page_size": {"200"}}
	rec := serveEmailBlacklistJSON(t, r, http.MethodGet, path+"?"+query.Encode(), `{}`)
	require.Equal(t, 200, rec.Code)
	require.NotNil(t, repo.filter)
	require.Equal(t, "exact_test%email@example.com", repo.filter.RegistrationEmail)
	require.Empty(t, repo.filter.ActorEmail)
	require.NotNil(t, repo.filter.Success)
	require.True(t, *repo.filter.Success)
	require.Equal(t, 200, repo.filter.PageSize)
	rec = serveEmailBlacklistJSON(t, r, http.MethodGet, path+"?actor_email=ordinary", `{}`)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "ordinary", repo.filter.ActorEmail)
	require.Empty(t, repo.filter.RegistrationEmail)
}
