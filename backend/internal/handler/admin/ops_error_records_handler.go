package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpsHandler) ListErrorRecords(c *gin.Context)       { h.listErrorRecords(c, false) }
func (h *OpsHandler) ListErrorRecordMembers(c *gin.Context) { h.listErrorRecords(c, true) }

func (h *OpsHandler) listErrorRecords(c *gin.Context, members bool) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "运维服务暂不可用，请稍后重试。")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	filter, err := parseOpsErrorRecordsFilter(c, members)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var result *service.OpsErrorRecordsList
	if members {
		id, parseErr := strconv.ParseInt(c.Param("representative_id"), 10, 64)
		if parseErr != nil || id <= 0 {
			response.BadRequest(c, "错误记录编号无效，请刷新列表后重试。")
			return
		}
		result, err = h.opsService.GetErrorRecordMembers(c.Request.Context(), filter, id)
	} else {
		result, err = h.opsService.GetErrorRecords(c.Request.Context(), filter)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func parseOpsErrorRecordsFilter(c *gin.Context, members bool) (*service.OpsErrorRecordsFilter, error) {
	q := c.Request.URL.Query()
	enum := func(key, fallback string, choices ...string) (string, error) {
		v := strings.ToLower(strings.TrimSpace(q.Get(key)))
		if !q.Has(key) {
			v = fallback
		}
		for _, choice := range choices {
			if v == choice {
				return v, nil
			}
		}
		return "", fmt.Errorf("筛选参数 %s 无效，请重新选择后重试。", key)
	}
	f := &service.OpsErrorRecordsFilter{}
	var err error
	if f.Scope, err = enum("scope", "all", "all", "request", "upstream"); err != nil {
		return nil, err
	}
	if f.Display, err = enum("display", "grouped", "grouped", "raw"); err != nil {
		return nil, err
	}
	if members && q.Has("display") {
		return nil, fmt.Errorf("组内记录不接受显示模式参数，请重新打开该组。")
	}
	if f.Finance, err = enum("finance", "hide", "hide", "include", "only"); err != nil {
		return nil, err
	}
	if f.View, err = enum("view", "all", "all", "errors", "excluded"); err != nil {
		return nil, err
	}
	if f.Phase, err = enum("phase", "", "", "request", "auth", "account_auth", "routing", "upstream", "network", "internal"); err != nil {
		return nil, err
	}
	if f.Owner, err = enum("error_owner", "", "", "provider", "client", "platform"); err != nil {
		return nil, err
	}
	category, err := enum("category", "", "", "auth", "rate_limit", "quota", "invalid_request", "service_unavailable", "upstream", "internal", "cyber")
	if err != nil {
		return nil, err
	}
	f.ErrorPhasesAny, f.ErrorTypesAny = service.CategoryToFilter(category)
	if f.SortBy, err = enum("sort_by", "created_at", "created_at", "model", "status_code"); err != nil {
		return nil, err
	}
	if f.SortOrder, err = enum("sort_order", "desc", "asc", "desc"); err != nil {
		return nil, err
	}
	integer := func(key string, fallback, minValue, maxValue int) (int, error) {
		if !q.Has(key) {
			return fallback, nil
		}
		n, e := strconv.Atoi(q.Get(key))
		if e != nil || n < minValue || (maxValue > 0 && n > maxValue) {
			return 0, fmt.Errorf("分页参数 %s 无效，请重新选择后重试。", key)
		}
		return n, nil
	}
	if f.Page, err = integer("page", 1, 1, 0); err != nil {
		return nil, err
	}
	if f.PageSize, err = integer("page_size", 10, 1, 100); err != nil {
		return nil, err
	}
	if int64(f.Page-1) > int64(^uint64(0)>>1)/int64(f.PageSize) {
		return nil, fmt.Errorf("分页范围过大，请重新选择后重试。")
	}
	for key, target := range map[string]**int64{"group_id": &f.GroupID, "account_id": &f.AccountID, "user_id": &f.UserID, "api_key_id": &f.APIKeyID} {
		if q.Has(key) {
			id, e := strconv.ParseInt(q.Get(key), 10, 64)
			if e != nil || id <= 0 {
				return nil, fmt.Errorf("筛选参数 %s 无效，请清除该条件后重试。", key)
			}
			*target = &id
		}
	}
	f.Platform = strings.TrimSpace(q.Get("platform"))
	f.Model = strings.TrimSpace(q.Get("model"))
	f.Source = strings.TrimSpace(q.Get("error_source"))
	f.Query = strings.TrimSpace(q.Get("q"))
	if len(f.Source) > 64 || utf8.RuneCountInString(f.Query) > 512 {
		return nil, fmt.Errorf("筛选内容过长，请缩短关键词后重试。")
	}
	boolean := func(key string) (bool, error) {
		v := q.Get(key)
		if v != "true" && v != "false" {
			return false, fmt.Errorf("筛选参数 %s 无效，请重新选择后重试。", key)
		}
		return v == "true", nil
	}
	if q.Has("resolved") {
		value, e := boolean("resolved")
		if e != nil {
			return nil, e
		}
		f.Resolved = &value
	}
	if q.Has("status_codes_other") {
		f.StatusCodesOther, err = boolean("status_codes_other")
		if err != nil {
			return nil, err
		}
	}
	if q.Has("status_codes") {
		if q.Has("status_codes_other") {
			return nil, fmt.Errorf("状态码筛选互相冲突，请重新选择。")
		}
		parts := strings.Split(q.Get("status_codes"), ",")
		if len(parts) > 20 {
			return nil, fmt.Errorf("状态码最多选择20项。")
		}
		for _, part := range parts {
			n, e := strconv.Atoi(strings.TrimSpace(part))
			if e != nil || n < 0 || n > 599 {
				return nil, fmt.Errorf("状态码无效，请重新选择。")
			}
			f.StatusCodes = append(f.StatusCodes, n)
		}
	}
	var start, end time.Time
	if q.Has("start_time") || q.Has("end_time") {
		if q.Has("time_range") || !q.Has("start_time") || !q.Has("end_time") {
			return nil, fmt.Errorf("时间范围不完整或互相冲突，请重新选择。")
		}
		start, err = time.Parse(time.RFC3339Nano, q.Get("start_time"))
		if err != nil {
			return nil, fmt.Errorf("开始时间无效，请重新选择。")
		}
		end, err = time.Parse(time.RFC3339Nano, q.Get("end_time"))
		if err != nil || !start.Before(end) {
			return nil, fmt.Errorf("结束时间必须晚于开始时间，请重新选择。")
		}
	} else {
		tr, e := enum("time_range", "1h", "5m", "30m", "1h", "3h", "6h", "24h")
		if e != nil {
			return nil, e
		}
		duration, _ := parseOpsDuration(tr)
		end = time.Now().UTC()
		start = end.Add(-duration)
	}
	f.StartTime = &start
	f.EndTime = &end
	return f, nil
}
