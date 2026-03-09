package service

import (
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
)

// TelegramBotConfigSetting Telegram Bot 配置实体
type TelegramBotConfigSetting struct {
	BotDisplayName  string `json:"bot_display_name"`
	BotDescription  string `json:"bot_description"`
	SupportLink     string `json:"support_link"`
	AvatarURL       string `json:"avatar_url"`
	WelcomeCoverURL string `json:"welcome_cover_url"`
	DefaultLocale   string `json:"default_locale"`
	WelcomeMessage  string `json:"welcome_message"`
	Announcement    string `json:"announcement"`
	AnnouncementOn  bool   `json:"announcement_on"`
}

// TelegramBotConfigSettingPatch Telegram Bot 配置补丁（全部指针字段）
type TelegramBotConfigSettingPatch struct {
	BotDisplayName  *string `json:"bot_display_name"`
	BotDescription  *string `json:"bot_description"`
	SupportLink     *string `json:"support_link"`
	AvatarURL       *string `json:"avatar_url"`
	WelcomeCoverURL *string `json:"welcome_cover_url"`
	DefaultLocale   *string `json:"default_locale"`
	WelcomeMessage  *string `json:"welcome_message"`
	Announcement    *string `json:"announcement"`
	AnnouncementOn  *bool   `json:"announcement_on"`
}

// TelegramBotRuntimeStatusSetting Telegram Bot 运行时状态
type TelegramBotRuntimeStatusSetting struct {
	Connected        bool   `json:"connected"`
	LastSeenAt       string `json:"last_seen_at"`
	BotVersion       string `json:"bot_version"`
	WebhookStatus    string `json:"webhook_status"`
	ConfigVersion    int    `json:"config_version"`
	LastConfigSyncAt string `json:"last_config_sync_at"`
}

// TelegramBotConfigDefault 默认 Bot 配置
func TelegramBotConfigDefault() TelegramBotConfigSetting {
	return TelegramBotConfigSetting{
		DefaultLocale:  "zh-CN",
		AnnouncementOn: false,
	}
}

// TelegramBotRuntimeStatusDefault 默认运行时状态
func TelegramBotRuntimeStatusDefault() TelegramBotRuntimeStatusSetting {
	return TelegramBotRuntimeStatusSetting{
		Connected:     false,
		ConfigVersion: 0,
	}
}

// TelegramBotConfigToMap 转换为 settings 存储结构
func TelegramBotConfigToMap(setting TelegramBotConfigSetting) map[string]interface{} {
	return map[string]interface{}{
		"bot_display_name":  strings.TrimSpace(setting.BotDisplayName),
		"bot_description":   strings.TrimSpace(setting.BotDescription),
		"support_link":      strings.TrimSpace(setting.SupportLink),
		"avatar_url":        strings.TrimSpace(setting.AvatarURL),
		"welcome_cover_url": strings.TrimSpace(setting.WelcomeCoverURL),
		"default_locale":    strings.TrimSpace(setting.DefaultLocale),
		"welcome_message":   strings.TrimSpace(setting.WelcomeMessage),
		"announcement":      strings.TrimSpace(setting.Announcement),
		"announcement_on":   setting.AnnouncementOn,
	}
}

// MaskTelegramBotConfigForAdmin 返回管理端配置（Bot 配置无需脱敏，原样返回）
func MaskTelegramBotConfigForAdmin(setting TelegramBotConfigSetting) models.JSON {
	return models.JSON{
		"bot_display_name":  setting.BotDisplayName,
		"bot_description":   setting.BotDescription,
		"support_link":      setting.SupportLink,
		"avatar_url":        setting.AvatarURL,
		"welcome_cover_url": setting.WelcomeCoverURL,
		"default_locale":    setting.DefaultLocale,
		"welcome_message":   setting.WelcomeMessage,
		"announcement":      setting.Announcement,
		"announcement_on":   setting.AnnouncementOn,
	}
}

// TelegramBotRuntimeStatusToMap 转换运行时状态为存储结构
func TelegramBotRuntimeStatusToMap(status TelegramBotRuntimeStatusSetting) map[string]interface{} {
	return map[string]interface{}{
		"connected":           status.Connected,
		"last_seen_at":        status.LastSeenAt,
		"bot_version":         status.BotVersion,
		"webhook_status":      status.WebhookStatus,
		"config_version":      status.ConfigVersion,
		"last_config_sync_at": status.LastConfigSyncAt,
	}
}

func telegramBotConfigFromJSON(raw models.JSON, fallback TelegramBotConfigSetting) TelegramBotConfigSetting {
	next := fallback
	if raw == nil {
		return next
	}
	next.BotDisplayName = readString(raw, "bot_display_name", next.BotDisplayName)
	next.BotDescription = readString(raw, "bot_description", next.BotDescription)
	next.SupportLink = readString(raw, "support_link", next.SupportLink)
	next.AvatarURL = readString(raw, "avatar_url", next.AvatarURL)
	next.WelcomeCoverURL = readString(raw, "welcome_cover_url", next.WelcomeCoverURL)
	next.DefaultLocale = readString(raw, "default_locale", next.DefaultLocale)
	next.WelcomeMessage = readString(raw, "welcome_message", next.WelcomeMessage)
	next.Announcement = readString(raw, "announcement", next.Announcement)
	next.AnnouncementOn = readBool(raw, "announcement_on", next.AnnouncementOn)
	return next
}

func telegramBotRuntimeStatusFromJSON(raw models.JSON, fallback TelegramBotRuntimeStatusSetting) TelegramBotRuntimeStatusSetting {
	next := fallback
	if raw == nil {
		return next
	}
	next.Connected = readBool(raw, "connected", next.Connected)
	next.LastSeenAt = readString(raw, "last_seen_at", next.LastSeenAt)
	next.BotVersion = readString(raw, "bot_version", next.BotVersion)
	next.WebhookStatus = readString(raw, "webhook_status", next.WebhookStatus)
	next.ConfigVersion = readInt(raw, "config_version", next.ConfigVersion)
	next.LastConfigSyncAt = readString(raw, "last_config_sync_at", next.LastConfigSyncAt)
	return next
}

// GetTelegramBotConfig 获取 Telegram Bot 配置
func (s *SettingService) GetTelegramBotConfig() (*TelegramBotConfigSetting, error) {
	fallback := TelegramBotConfigDefault()
	value, err := s.GetByKey(constants.SettingKeyTelegramBotConfig)
	if err != nil {
		return &fallback, err
	}
	if value == nil {
		return &fallback, nil
	}
	parsed := telegramBotConfigFromJSON(value, fallback)
	return &parsed, nil
}

// PatchTelegramBotConfig 基于补丁更新 Telegram Bot 配置
func (s *SettingService) PatchTelegramBotConfig(patch TelegramBotConfigSettingPatch) (*TelegramBotConfigSetting, error) {
	current, err := s.GetTelegramBotConfig()
	if err != nil {
		return nil, err
	}

	next := *current
	if patch.BotDisplayName != nil {
		next.BotDisplayName = strings.TrimSpace(*patch.BotDisplayName)
	}
	if patch.BotDescription != nil {
		next.BotDescription = strings.TrimSpace(*patch.BotDescription)
	}
	if patch.SupportLink != nil {
		next.SupportLink = strings.TrimSpace(*patch.SupportLink)
	}
	if patch.AvatarURL != nil {
		next.AvatarURL = strings.TrimSpace(*patch.AvatarURL)
	}
	if patch.WelcomeCoverURL != nil {
		next.WelcomeCoverURL = strings.TrimSpace(*patch.WelcomeCoverURL)
	}
	if patch.DefaultLocale != nil {
		next.DefaultLocale = strings.TrimSpace(*patch.DefaultLocale)
	}
	if patch.WelcomeMessage != nil {
		next.WelcomeMessage = strings.TrimSpace(*patch.WelcomeMessage)
	}
	if patch.Announcement != nil {
		next.Announcement = strings.TrimSpace(*patch.Announcement)
	}
	if patch.AnnouncementOn != nil {
		next.AnnouncementOn = *patch.AnnouncementOn
	}

	if _, err := s.Update(constants.SettingKeyTelegramBotConfig, TelegramBotConfigToMap(next)); err != nil {
		return nil, err
	}
	return &next, nil
}

// GetTelegramBotRuntimeStatus 获取 Telegram Bot 运行时状态
func (s *SettingService) GetTelegramBotRuntimeStatus() (*TelegramBotRuntimeStatusSetting, error) {
	fallback := TelegramBotRuntimeStatusDefault()
	value, err := s.GetByKey(constants.SettingKeyTelegramBotRuntimeStatus)
	if err != nil {
		return &fallback, err
	}
	if value == nil {
		return &fallback, nil
	}
	parsed := telegramBotRuntimeStatusFromJSON(value, fallback)
	return &parsed, nil
}

// UpdateTelegramBotRuntimeStatus 更新 Telegram Bot 运行时状态
func (s *SettingService) UpdateTelegramBotRuntimeStatus(status TelegramBotRuntimeStatusSetting) error {
	_, err := s.Update(constants.SettingKeyTelegramBotRuntimeStatus, TelegramBotRuntimeStatusToMap(status))
	return err
}
