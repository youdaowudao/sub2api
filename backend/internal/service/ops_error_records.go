package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OpsErrorRecordsFilter is a read-only projection; it never changes log ingestion.
type OpsErrorRecordsFilter struct {
	OpsErrorLogFilter
	Scope   string
	Display string
	Finance string
}

type OpsErrorRecordUser struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

type OpsErrorRecordKey struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// A raw item embeds its original log. A grouped item exposes a sample explicitly,
// while identity counts/singletons describe all matching members, never the sample.
type OpsErrorRecord struct {
	*OpsErrorLog
	Representative      *OpsErrorLog        `json:"representative,omitempty"`
	RepresentativeID    int64               `json:"representative_id,omitempty"`
	ClientStatusCode    *int                `json:"client_status_code"`
	IsRequestError      bool                `json:"is_request_error"`
	IsUpstreamError     bool                `json:"is_upstream_error"`
	Count               int64               `json:"count,omitempty"`
	FirstAt             time.Time           `json:"first_at"`
	LastAt              time.Time           `json:"last_at"`
	UserCount           int64               `json:"user_count"`
	APIKeyCount         int64               `json:"api_key_count"`
	HasUnknownUser      bool                `json:"has_unknown_user"`
	HasUnknownAPIKey    bool                `json:"has_unknown_api_key"`
	SingleUser          *OpsErrorRecordUser `json:"single_user,omitempty"`
	SingleAPIKey        *OpsErrorRecordKey  `json:"single_api_key,omitempty"`
	SingleClientIP      *string             `json:"single_client_ip"`
	ClientIPCount       int64               `json:"client_ip_count"`
	HasUnknownClientIP  bool                `json:"has_unknown_client_ip"`
	SingleUserAgent     *string             `json:"single_user_agent"`
	UserAgentCount      int64               `json:"user_agent_count"`
	HasUnknownUserAgent bool                `json:"has_unknown_user_agent"`
}

type OpsErrorRecordsList struct {
	Items        []*OpsErrorRecord `json:"items"`
	Total        int64             `json:"total"`
	TotalRecords int64             `json:"total_records"`
	Page         int               `json:"page"`
	PageSize     int               `json:"page_size"`
	WindowStart  time.Time         `json:"window_start"`
	WindowEnd    time.Time         `json:"window_end"`
}

func (s *OpsService) GetErrorRecords(ctx context.Context, filter *OpsErrorRecordsFilter) (*OpsErrorRecordsList, error) {
	return s.getErrorRecords(ctx, filter, 0)
}

func (s *OpsService) GetErrorRecordMembers(ctx context.Context, filter *OpsErrorRecordsFilter, id int64) (*OpsErrorRecordsList, error) {
	return s.getErrorRecords(ctx, filter, id)
}

func (s *OpsService) getErrorRecords(ctx context.Context, filter *OpsErrorRecordsFilter, id int64) (*OpsErrorRecordsList, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if filter == nil || filter.StartTime == nil || filter.EndTime == nil {
		return nil, infraerrors.BadRequest("OPS_ERROR_RECORDS_INVALID_RANGE", "请选择有效的错误记录时间范围")
	}
	if s.opsRepo == nil {
		return nil, infraerrors.ServiceUnavailable("OPS_ERROR_RECORDS_LOAD_FAILED", "读取错误记录失败，本次未更新结果。请稍后重试。")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *OpsErrorRecordsList
	var err error
	if id > 0 {
		result, err = s.opsRepo.ListErrorRecordMembers(ctx, filter, id)
	} else {
		result, err = s.opsRepo.ListErrorRecords(ctx, filter)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("OPS_ERROR_NOT_FOUND", "该条记录已不存在，可能已被清理。请刷新列表。")
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, infraerrors.New(504, "OPS_ERROR_RECORDS_QUERY_TIMEOUT", "错误记录查询超时，本次未更新结果。请缩小时间范围或筛选账号后重试。")
		}
		return nil, infraerrors.InternalServer("OPS_ERROR_RECORDS_LOAD_FAILED", "读取错误记录失败，本次未更新结果。请稍后重试。")
	}
	for _, item := range result.Items {
		if item.OpsErrorLog != nil {
			item.Message = sanitizeOpsErrorRecordMessage(item.Message)
		}
		if item.Representative != nil {
			item.Representative.Message = sanitizeOpsErrorRecordMessage(item.Representative.Message)
		}
	}
	result.WindowStart = *filter.StartTime
	result.WindowEnd = *filter.EndTime
	return result, nil
}
