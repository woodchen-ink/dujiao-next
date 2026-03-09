package admin

import (
	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// GetTelegramBotConfig 获取 Telegram Bot 配置
func (h *Handler) GetTelegramBotConfig(c *gin.Context) {
	setting, err := h.SettingService.GetTelegramBotConfig()
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, service.MaskTelegramBotConfigForAdmin(*setting))
}

// UpdateTelegramBotConfig 更新 Telegram Bot 配置
func (h *Handler) UpdateTelegramBotConfig(c *gin.Context) {
	var req service.TelegramBotConfigSettingPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	setting, err := h.SettingService.PatchTelegramBotConfig(req)
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		return
	}

	response.Success(c, service.MaskTelegramBotConfigForAdmin(*setting))
}

// GetTelegramBotRuntimeStatus 获取 Telegram Bot 运行时状态
func (h *Handler) GetTelegramBotRuntimeStatus(c *gin.Context) {
	status, err := h.SettingService.GetTelegramBotRuntimeStatus()
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, status)
}
