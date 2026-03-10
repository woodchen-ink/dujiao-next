package service

import (
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
)

// LocalizedText 多语言文本 {"zh-CN": "...", "zh-TW": "...", "en-US": "..."}
type LocalizedText map[string]string

// TelegramBotConfigSetting Telegram Bot 配置实体（嵌套分组）
type TelegramBotConfigSetting struct {
	Enabled       bool                     `json:"enabled"`
	DefaultLocale string                   `json:"default_locale"`
	ConfigVersion int                      `json:"config_version"`
	Basic         TelegramBotBasicConfig   `json:"basic"`
	Welcome       TelegramBotWelcomeConfig `json:"welcome"`
	Menu          TelegramBotMenuConfig    `json:"menu"`
}

// TelegramBotBasicConfig 基本信息分组
type TelegramBotBasicConfig struct {
	DisplayName string        `json:"display_name"`
	Description LocalizedText `json:"description"`
	SupportURL  string        `json:"support_url"`
	CoverURL    string        `json:"cover_url"`
}

// TelegramBotWelcomeConfig 欢迎设置分组
type TelegramBotWelcomeConfig struct {
	Enabled bool          `json:"enabled"`
	Message LocalizedText `json:"message"`
}

// TelegramBotMenuConfig 菜单配置分组
type TelegramBotMenuConfig struct {
	Items []TelegramBotMenuItem `json:"items"`
}

// TelegramBotMenuItem 单个菜单项
type TelegramBotMenuItem struct {
	Key     string                `json:"key"`
	Enabled bool                  `json:"enabled"`
	Order   int                   `json:"order"`
	Label   LocalizedText         `json:"label"`
	Action  TelegramBotMenuAction `json:"action"`
}

// TelegramBotMenuAction 菜单项动作
type TelegramBotMenuAction struct {
	Type  string `json:"type"`
	Value string `json:"value"`
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
		Enabled:       false,
		DefaultLocale: "zh-CN",
		ConfigVersion: 0,
		Basic: TelegramBotBasicConfig{
			Description: make(LocalizedText),
		},
		Welcome: TelegramBotWelcomeConfig{
			Enabled: false,
			Message: make(LocalizedText),
		},
		Menu: TelegramBotMenuConfig{
			Items: []TelegramBotMenuItem{
				{
					Key: "shop_home", Enabled: true, Order: 1,
					Label:  LocalizedText{"zh-CN": "🛍️ 开始购物", "zh-TW": "🛍️ 開始購物", "en-US": "🛍️ Shop Now"},
					Action: TelegramBotMenuAction{Type: "builtin", Value: ""},
				},
				{
					Key: "my_orders", Enabled: true, Order: 2,
					Label:  LocalizedText{"zh-CN": "📦 我的订单", "zh-TW": "📦 我的訂單", "en-US": "📦 My Orders"},
					Action: TelegramBotMenuAction{Type: "builtin", Value: ""},
				},
				{
					Key: "help", Enabled: true, Order: 3,
					Label:  LocalizedText{"zh-CN": "❓ 帮助", "zh-TW": "❓ 幫助", "en-US": "❓ Help"},
					Action: TelegramBotMenuAction{Type: "builtin", Value: ""},
				},
			},
		},
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
		"enabled":        setting.Enabled,
		"default_locale": strings.TrimSpace(setting.DefaultLocale),
		"config_version": setting.ConfigVersion,
		"basic": map[string]interface{}{
			"display_name": strings.TrimSpace(setting.Basic.DisplayName),
			"description":  localizedTextToMap(setting.Basic.Description),
			"support_url":  strings.TrimSpace(setting.Basic.SupportURL),
			"cover_url":    strings.TrimSpace(setting.Basic.CoverURL),
		},
		"welcome": map[string]interface{}{
			"enabled": setting.Welcome.Enabled,
			"message": localizedTextToMap(setting.Welcome.Message),
		},
		"menu": map[string]interface{}{
			"items": menuItemsToSlice(setting.Menu.Items),
		},
	}
}

// MaskTelegramBotConfigForAdmin 返回管理端配置
func MaskTelegramBotConfigForAdmin(setting TelegramBotConfigSetting) models.JSON {
	return models.JSON{
		"enabled":        setting.Enabled,
		"default_locale": setting.DefaultLocale,
		"config_version": setting.ConfigVersion,
		"basic": map[string]interface{}{
			"display_name": setting.Basic.DisplayName,
			"description":  localizedTextToMap(setting.Basic.Description),
			"support_url":  setting.Basic.SupportURL,
			"cover_url":    setting.Basic.CoverURL,
		},
		"welcome": map[string]interface{}{
			"enabled": setting.Welcome.Enabled,
			"message": localizedTextToMap(setting.Welcome.Message),
		},
		"menu": map[string]interface{}{
			"items": menuItemsToSlice(setting.Menu.Items),
		},
	}
}

// SerializeTelegramBotConfigForChannel 返回 Channel API 配置（bot_token 由调用方注入）
func SerializeTelegramBotConfigForChannel(setting TelegramBotConfigSetting, botToken string) models.JSON {
	return models.JSON{
		"enabled":        setting.Enabled,
		"bot_token":      botToken,
		"default_locale": setting.DefaultLocale,
		"config_version": setting.ConfigVersion,
		"basic": map[string]interface{}{
			"display_name": setting.Basic.DisplayName,
			"description":  localizedTextToMap(setting.Basic.Description),
			"support_url":  setting.Basic.SupportURL,
			"cover_url":    setting.Basic.CoverURL,
		},
		"welcome": map[string]interface{}{
			"enabled": setting.Welcome.Enabled,
			"message": localizedTextToMap(setting.Welcome.Message),
		},
		"menu": map[string]interface{}{
			"items": menuItemsToSlice(setting.Menu.Items),
		},
	}
}

// maskBotToken 脱敏 bot token：显示前 4 位和后 4 位
func maskBotToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 12 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
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

// telegramBotConfigFromJSON 从 JSON 读取嵌套结构，兼容旧扁平格式
func telegramBotConfigFromJSON(raw models.JSON, fallback TelegramBotConfigSetting) TelegramBotConfigSetting {
	next := fallback
	if raw == nil {
		return next
	}

	// 兼容旧扁平格式：检测 bot_display_name 字段自动迁移
	if _, hasOldField := raw["bot_display_name"]; hasOldField {
		return migrateOldTelegramBotConfig(raw, fallback)
	}

	next.Enabled = readBool(raw, "enabled", next.Enabled)
	next.DefaultLocale = readString(raw, "default_locale", next.DefaultLocale)
	next.ConfigVersion = readInt(raw, "config_version", next.ConfigVersion)

	if basicRaw, ok := raw["basic"].(map[string]interface{}); ok {
		next.Basic.DisplayName = readString(basicRaw, "display_name", next.Basic.DisplayName)
		next.Basic.Description = readLocalizedText(basicRaw, "description", next.Basic.Description)
		next.Basic.SupportURL = readString(basicRaw, "support_url", next.Basic.SupportURL)
		next.Basic.CoverURL = readString(basicRaw, "cover_url", next.Basic.CoverURL)
	}

	if welcomeRaw, ok := raw["welcome"].(map[string]interface{}); ok {
		next.Welcome.Enabled = readBool(welcomeRaw, "enabled", next.Welcome.Enabled)
		next.Welcome.Message = readLocalizedText(welcomeRaw, "message", next.Welcome.Message)
	}

	if menuRaw, ok := raw["menu"].(map[string]interface{}); ok {
		next.Menu.Items = readMenuItems(menuRaw["items"])
	}

	return next
}

// migrateOldTelegramBotConfig 将旧扁平格式迁移为嵌套结构
func migrateOldTelegramBotConfig(raw models.JSON, fallback TelegramBotConfigSetting) TelegramBotConfigSetting {
	next := fallback
	defaultLocale := readString(raw, "default_locale", "zh-CN")
	next.DefaultLocale = defaultLocale

	next.Basic.DisplayName = readString(raw, "bot_display_name", "")
	// 旧格式的单语言字段迁移到 default_locale
	oldDescription := readString(raw, "bot_description", "")
	if oldDescription != "" {
		next.Basic.Description = LocalizedText{defaultLocale: oldDescription}
	}
	next.Basic.SupportURL = readString(raw, "support_link", "")
	next.Basic.CoverURL = readString(raw, "welcome_cover_url", "")

	oldWelcomeMessage := readString(raw, "welcome_message", "")
	if oldWelcomeMessage != "" {
		next.Welcome.Enabled = true
		next.Welcome.Message = LocalizedText{defaultLocale: oldWelcomeMessage}
	}

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

// normalizeTelegramBotConfig 归一化多语言字段 + trim
func normalizeTelegramBotConfig(raw models.JSON) map[string]interface{} {
	setting := telegramBotConfigFromJSON(raw, TelegramBotConfigDefault())
	// 归一化多语言字段：确保所有支持的语言键都存在
	setting.Basic.Description = normalizeLocalizedText(setting.Basic.Description)
	setting.Welcome.Message = normalizeLocalizedText(setting.Welcome.Message)
	setting.Menu.Items = normalizeMenuItems(setting.Menu.Items)
	return TelegramBotConfigToMap(setting)
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

// UpdateTelegramBotConfig 整对象覆盖更新 Telegram Bot 配置，自动递增 config_version
func (s *SettingService) UpdateTelegramBotConfig(cfg TelegramBotConfigSetting) (*TelegramBotConfigSetting, error) {
	current, err := s.GetTelegramBotConfig()
	if err != nil {
		return nil, err
	}

	// config_version 自动递增
	cfg.ConfigVersion = current.ConfigVersion + 1

	// 归一化多语言字段
	cfg.Basic.Description = normalizeLocalizedText(cfg.Basic.Description)
	cfg.Welcome.Message = normalizeLocalizedText(cfg.Welcome.Message)
	cfg.Menu.Items = normalizeMenuItems(cfg.Menu.Items)

	if _, err := s.Update(constants.SettingKeyTelegramBotConfig, TelegramBotConfigToMap(cfg)); err != nil {
		return nil, err
	}

	// 同步更新运行时状态中的 config_version
	runtimeStatus, _ := s.GetTelegramBotRuntimeStatus()
	if runtimeStatus != nil {
		runtimeStatus.ConfigVersion = cfg.ConfigVersion
		_ = s.UpdateTelegramBotRuntimeStatus(*runtimeStatus)
	}

	return &cfg, nil
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

// validMenuActionTypes 菜单项 action type 白名单
var validMenuActionTypes = map[string]bool{
	"builtin": true,
	"url":     true,
	"command": true,
}

const menuItemsMaxCount = 20

// readMenuItems 从 JSON 解析菜单项数组
func readMenuItems(raw interface{}) []TelegramBotMenuItem {
	arr, ok := raw.([]interface{})
	if !ok || len(arr) == 0 {
		return []TelegramBotMenuItem{}
	}
	items := make([]TelegramBotMenuItem, 0, len(arr))
	for _, v := range arr {
		m, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		item := TelegramBotMenuItem{
			Key:     readString(m, "key", ""),
			Enabled: readBool(m, "enabled", true),
			Order:   readInt(m, "order", 0),
			Label:   readLocalizedText(m, "label", make(LocalizedText)),
		}
		if actionRaw, ok := m["action"].(map[string]interface{}); ok {
			item.Action.Type = readString(actionRaw, "type", "builtin")
			item.Action.Value = readString(actionRaw, "value", "")
		}
		items = append(items, item)
	}
	return items
}

// menuItemsToSlice 序列化菜单项为存储格式
func menuItemsToSlice(items []TelegramBotMenuItem) []interface{} {
	result := make([]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"key":     strings.TrimSpace(item.Key),
			"enabled": item.Enabled,
			"order":   item.Order,
			"label":   localizedTextToMap(item.Label),
			"action": map[string]interface{}{
				"type":  strings.TrimSpace(item.Action.Type),
				"value": strings.TrimSpace(item.Action.Value),
			},
		})
	}
	return result
}

// normalizeMenuItems 归一化菜单项：trim、归一化 label、验证 action type、上限 20 项
func normalizeMenuItems(items []TelegramBotMenuItem) []TelegramBotMenuItem {
	if len(items) > menuItemsMaxCount {
		items = items[:menuItemsMaxCount]
	}
	result := make([]TelegramBotMenuItem, 0, len(items))
	for _, item := range items {
		item.Key = strings.TrimSpace(item.Key)
		item.Label = normalizeLocalizedText(item.Label)
		item.Action.Type = strings.TrimSpace(item.Action.Type)
		item.Action.Value = strings.TrimSpace(item.Action.Value)
		if !validMenuActionTypes[item.Action.Type] {
			item.Action.Type = "builtin"
		}
		result = append(result, item)
	}
	return result
}

// readLocalizedText 从 JSON map 读取 LocalizedText 字段
func readLocalizedText(source map[string]interface{}, key string, fallback LocalizedText) LocalizedText {
	raw, ok := source[key]
	if !ok {
		return fallback
	}
	mapRaw, ok := raw.(map[string]interface{})
	if !ok {
		return fallback
	}
	result := make(LocalizedText, len(mapRaw))
	for k, v := range mapRaw {
		if s, ok := v.(string); ok {
			result[k] = strings.TrimSpace(s)
		}
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}

// localizedTextToMap 将 LocalizedText 转换为 map[string]interface{}
func localizedTextToMap(lt LocalizedText) map[string]interface{} {
	result := make(map[string]interface{}, len(lt))
	for k, v := range lt {
		result[k] = v
	}
	return result
}

// normalizeLocalizedText 确保所有支持的语言键都存在并 trim
func normalizeLocalizedText(lt LocalizedText) LocalizedText {
	result := make(LocalizedText, len(constants.SupportedLocales))
	for _, lang := range constants.SupportedLocales {
		result[lang] = ""
	}
	for k, v := range lt {
		result[k] = strings.TrimSpace(v)
	}
	return result
}
