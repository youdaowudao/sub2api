package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// StartScheduleTimer closes the existing switch and saves its automatic enable time.
func (h *AccountHandler) StartScheduleTimer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "账号 ID 无效；本次没有更改调度，请刷新后重试")
		return
	}
	var request struct {
		DurationHours int `json:"duration_hours" binding:"required,oneof=6 12 24"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "请选择 6、12 或 24 小时；本次没有更改调度")
		return
	}
	result, err := h.adminService.StartAccountScheduleTimer(c.Request.Context(), id, request.DurationHours)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
