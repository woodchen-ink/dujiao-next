package channel

import (
	"net/http"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// GetBotConfig GET /api/v1/channel/telegram/config
// 返回 Telegram Bot 配置 + config_version
func (h *Handler) GetBotConfig(c *gin.Context) {
	config, err := h.SettingService.GetTelegramBotConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":            false,
			"error_code":    "internal_error",
			"error_message": "failed to get bot config",
		})
		return
	}

	runtimeStatus, err := h.SettingService.GetTelegramBotRuntimeStatus()
	if err != nil {
		logger.Warnw("channel_get_runtime_status_failed", "error", err)
		runtimeStatus = &service.TelegramBotRuntimeStatusSetting{}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"config": gin.H{
			"bot_display_name":  config.BotDisplayName,
			"bot_description":   config.BotDescription,
			"support_link":      config.SupportLink,
			"avatar_url":        config.AvatarURL,
			"welcome_cover_url": config.WelcomeCoverURL,
			"default_locale":    config.DefaultLocale,
			"welcome_message":   config.WelcomeMessage,
			"announcement":      config.Announcement,
			"announcement_on":   config.AnnouncementOn,
		},
		"config_version": runtimeStatus.ConfigVersion,
	})
}

type reportHeartbeatRequest struct {
	BotVersion    string `json:"bot_version"`
	WebhookStatus string `json:"webhook_status"`
}

// ReportHeartbeat POST /api/v1/channel/telegram/heartbeat
// Bot 上报心跳，更新 runtime_status
func (h *Handler) ReportHeartbeat(c *gin.Context) {
	var req reportHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"ok":            false,
			"error_code":    "bad_request",
			"error_message": "invalid request body",
		})
		return
	}

	// 获取当前运行时状态以保留 config_version 等字段
	current, err := h.SettingService.GetTelegramBotRuntimeStatus()
	if err != nil {
		logger.Warnw("channel_heartbeat_get_status_failed", "error", err)
		current = &service.TelegramBotRuntimeStatusSetting{}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	updated := service.TelegramBotRuntimeStatusSetting{
		Connected:        true,
		LastSeenAt:       now,
		BotVersion:       req.BotVersion,
		WebhookStatus:    req.WebhookStatus,
		ConfigVersion:    current.ConfigVersion,
		LastConfigSyncAt: current.LastConfigSyncAt,
	}

	if err := h.SettingService.UpdateTelegramBotRuntimeStatus(updated); err != nil {
		logger.Errorw("channel_heartbeat_update_failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":            false,
			"error_code":    "internal_error",
			"error_message": "failed to update runtime status",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"config_version": updated.ConfigVersion,
	})
}
