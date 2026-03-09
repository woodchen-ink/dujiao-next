package channel

import "github.com/dujiao-next/internal/provider"

// Handler 渠道 API 处理器（Telegram Bot 等外部服务调用的接口）
type Handler struct {
	*provider.Container
}

// New 创建渠道处理器
func New(c *provider.Container) *Handler {
	return &Handler{Container: c}
}
