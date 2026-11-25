package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type VideoTaskHandler struct {
	repo service.VideoRelayRepository
}

func NewVideoTaskHandler(repo service.VideoRelayRepository) *VideoTaskHandler {
	return &VideoTaskHandler{repo: repo}
}

func (h *VideoTaskHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	pageSize = min(pageSize, 200)
	filter := service.VideoRelayAdminFilter{
		Status: c.Query("status"),
		TaskID: c.Query("task_id"),
	}
	switch filter.Status {
	case "", "processing", "confirmation", "completed", "failed":
	default:
		response.BadRequest(c, "Invalid status")
		return
	}
	if filter.TaskID != "" && !service.ValidVideoRelayTaskID(filter.TaskID) {
		response.BadRequest(c, "Invalid task_id")
		return
	}
	if raw, present := c.GetQuery("user_id"); present {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		filter.UserID = id
	}
	if raw, present := c.GetQuery("risk_only"); present {
		switch raw {
		case "true":
			filter.RiskOnly = true
		case "false":
		default:
			response.BadRequest(c, "Invalid risk_only")
			return
		}
	}
	tasks, total, err := h.repo.ListAdmin(c.Request.Context(), filter, (page-1)*pageSize, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, tasks, total, page, pageSize)
}
