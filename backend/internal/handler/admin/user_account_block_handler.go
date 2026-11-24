package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) SetUserAccountBlockService(blocks *service.UserAccountBlockService) {
	h.userAccountBlocks = blocks
}

func (h *SettingHandler) ListUserAccountBlocks(c *gin.Context) {
	items, err := h.userAccountBlocks.List(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"items": items})
}

func (h *SettingHandler) CreateUserAccountBlock(c *gin.Context) {
	var request struct {
		UserID    int64 `json:"user_id"`
		AccountID int64 `json:"account_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, service.ErrUserAccountBlockInvalidID)
		return
	}
	item, err := h.userAccountBlocks.Block(c.Request.Context(), request.UserID, request.AccountID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, item)
}

func (h *SettingHandler) DeleteUserAccountBlock(c *gin.Context) {
	userID, userErr := strconv.ParseInt(c.Param("user_id"), 10, 64)
	accountID, accountErr := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if userErr != nil || accountErr != nil {
		response.ErrorFrom(c, service.ErrUserAccountBlockInvalidID)
		return
	}
	if response.ErrorFrom(c, h.userAccountBlocks.Unblock(c.Request.Context(), userID, accountID)) {
		return
	}
	response.Success(c, nil)
}
