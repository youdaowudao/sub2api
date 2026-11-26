package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type checkinAdminService interface {
	GetConfig(context.Context) (service.CheckinConfigResult, error)
	SaveConfig(context.Context, []byte) (service.CheckinConfig, error)
	Overview(context.Context, int) (service.CheckinOverview, error)
	ListAdminUsers(context.Context, service.CheckinUserFilter, pagination.PaginationParams) ([]service.CheckinAdminUser, int64, error)
	ListAdminRecords(context.Context, service.CheckinRecordFilter, pagination.PaginationParams) ([]service.CheckinAdminRecord, int64, error)
	ListAdminIPs(context.Context, service.CheckinIPFilter, pagination.PaginationParams) ([]service.CheckinAdminIP, int64, error)
}
type CheckinHandler struct{ service checkinAdminService }

func NewCheckinHandler(s *service.CheckinService) *CheckinHandler { return &CheckinHandler{service: s} }
func (h *CheckinHandler) GetConfig(c *gin.Context) {
	result, e := h.service.GetConfig(c.Request.Context())
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *CheckinHandler) SaveConfig(c *gin.Context) {
	raw, e := c.GetRawData()
	if e != nil {
		response.BadRequest(c, "签到配置读取失败")
		return
	}
	result, e := h.service.SaveConfig(c.Request.Context(), raw)
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *CheckinHandler) Overview(c *gin.Context) {
	days := 30
	if raw := c.Query("days"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || (n != 7 && n != 30 && n != 90) {
			response.BadRequest(c, "days 必须是 7、30 或 90")
			return
		}
		days = n
	}
	result, e := h.service.Overview(c.Request.Context(), days)
	if response.ErrorFrom(c, e) {
		return
	}
	response.Success(c, result)
}
func checkinPage(c *gin.Context) pagination.PaginationParams {
	p, ps := response.ParsePagination(c)
	return pagination.PaginationParams{Page: p, PageSize: ps, SortBy: c.Query("sort"), SortOrder: c.Query("order")}
}
func checkinValidTier(tier string) bool {
	return tier == "" || tier == "veteran" || tier == "new_funded" || tier == "new_unfunded"
}
func checkinDateRange(c *gin.Context) (string, string, bool) {
	from, to := c.Query("date_from"), c.Query("date_to")
	for _, date := range []string{from, to} {
		if date != "" {
			parsed, e := time.Parse("2006-01-02", date)
			if e != nil || parsed.Format("2006-01-02") != date {
				response.BadRequest(c, "日期格式无效")
				return "", "", false
			}
		}
	}
	if from != "" && to != "" && from > to {
		response.BadRequest(c, "起始日期不能晚于结束日期")
		return "", "", false
	}
	return from, to, true
}
func (h *CheckinHandler) Users(c *gin.Context) {
	p := checkinPage(c)
	f := service.CheckinUserFilter{Search: strings.TrimSpace(c.Query("search")), Tier: c.Query("tier")}
	if !checkinValidTier(f.Tier) {
		response.BadRequest(c, "用户类型无效")
		return
	}
	result, total, e := h.service.ListAdminUsers(c.Request.Context(), f, p)
	if response.ErrorFrom(c, e) {
		return
	}
	response.Paginated(c, result, total, p.Page, p.PageSize)
}
func (h *CheckinHandler) Records(c *gin.Context) {
	from, to, ok := checkinDateRange(c)
	if !ok {
		return
	}
	p := checkinPage(c)
	f := service.CheckinRecordFilter{DateFrom: from, DateTo: to, Search: strings.TrimSpace(c.Query("search")), IP: strings.TrimSpace(c.Query("ip")), Tier: c.Query("tier"), Verification: c.Query("verification")}
	if !checkinValidTier(f.Tier) {
		response.BadRequest(c, "用户类型无效")
		return
	}
	switch f.Verification {
	case "", "none", "captcha", "email":
	default:
		response.BadRequest(c, "验证方式无效")
		return
	}
	result, total, e := h.service.ListAdminRecords(c.Request.Context(), f, p)
	if response.ErrorFrom(c, e) {
		return
	}
	response.Paginated(c, result, total, p.Page, p.PageSize)
}
func (h *CheckinHandler) IPs(c *gin.Context) {
	from, to, ok := checkinDateRange(c)
	if !ok {
		return
	}
	p := checkinPage(c)
	f := service.CheckinIPFilter{DateFrom: from, DateTo: to, MinUsers: 2}
	if raw := c.Query("min_users"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 {
			response.BadRequest(c, "账号数必须为正整数")
			return
		}
		f.MinUsers = n
	}
	result, total, e := h.service.ListAdminIPs(c.Request.Context(), f, p)
	if response.ErrorFrom(c, e) {
		return
	}
	response.Paginated(c, result, total, p.Page, p.PageSize)
}
