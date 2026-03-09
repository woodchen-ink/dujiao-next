package models

import (
	"time"

	"gorm.io/gorm"
)

// ChannelClient 渠道客户端（Telegram Bot 等外部服务的认证凭证）
type ChannelClient struct {
	ID            uint           `json:"id" gorm:"primaryKey"`
	Name          string         `json:"name" gorm:"size:100;not null"`
	ChannelType   string         `json:"channel_type" gorm:"size:50;not null;index"`
	ChannelKey    string         `json:"channel_key" gorm:"size:64;uniqueIndex;not null"`
	ChannelSecret string         `json:"-" gorm:"size:512;not null"`             // AES-256-GCM encrypted
	Status        int            `json:"status" gorm:"default:1;not null;index"` // 1=active, 0=disabled
	Description   string         `json:"description" gorm:"size:500"`
	LastUsedAt    *time.Time     `json:"last_used_at"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `json:"-" gorm:"index"`
}
