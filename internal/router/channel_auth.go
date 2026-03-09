package router

import (
	"io"
	"net/http"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/provider"
	"github.com/dujiao-next/internal/service"
	"github.com/dujiao-next/internal/upstream"

	"github.com/gin-gonic/gin"
)

const (
	channelClientIDKey = "channel_client_id"
	channelKeyCtxKey   = "channel_key"
	channelTypeCtxKey  = "channel_type"
)

// ChannelAPIAuthMiddleware 渠道 API 签名鉴权中间件
func ChannelAPIAuthMiddleware(container *provider.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		channelKey := c.GetHeader("X-Channel-Key")
		timestampStr := c.GetHeader("X-Channel-Timestamp")
		signature := c.GetHeader("X-Channel-Signature")

		if channelKey == "" || timestampStr == "" || signature == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"ok":            false,
				"error_code":    "missing_auth_headers",
				"error_message": "missing channel authentication headers",
			})
			return
		}

		timestamp, err := upstream.ParseTimestamp(timestampStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"ok":            false,
				"error_code":    "invalid_timestamp",
				"error_message": "invalid timestamp format",
			})
			return
		}

		// 读取 body 用于签名验证
		var body []byte
		if c.Request.Body != nil {
			body, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"ok":            false,
					"error_code":    "bad_request",
					"error_message": "failed to read request body",
				})
				return
			}
			// 重置 body 供后续 handler 读取
			c.Request.Body = io.NopCloser(&bodyReader{data: body})
		}

		method := c.Request.Method
		path := c.Request.URL.Path

		client, err := container.ChannelClientService.VerifyChannelSignature(
			channelKey, signature, timestamp, method, path, body,
		)
		if err != nil {
			switch err {
			case service.ErrChannelTimestampExpired:
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"ok":            false,
					"error_code":    "timestamp_expired",
					"error_message": "timestamp expired",
				})
			case service.ErrChannelClientNotFound:
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"ok":            false,
					"error_code":    "invalid_channel_key",
					"error_message": "channel key is invalid",
				})
			case service.ErrChannelClientDisabled:
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"ok":            false,
					"error_code":    "channel_disabled",
					"error_message": "channel client is disabled",
				})
			case service.ErrChannelSignatureInvalid:
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"ok":            false,
					"error_code":    "invalid_signature",
					"error_message": "signature verification failed",
				})
			default:
				logger.Errorw("channel_auth_error", "error", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"ok":            false,
					"error_code":    "internal_error",
					"error_message": "internal error",
				})
			}
			return
		}

		// 异步更新 last_used_at
		now := time.Now()
		go func() {
			client.LastUsedAt = &now
			if updateErr := container.ChannelClientRepo.Update(client); updateErr != nil {
				logger.Warnw("channel_auth_update_last_used_failed", "error", updateErr)
			}
		}()

		// 设置 context keys
		c.Set(channelClientIDKey, client.ID)
		c.Set(channelKeyCtxKey, client.ChannelKey)
		c.Set(channelTypeCtxKey, client.ChannelType)

		c.Next()
	}
}

// KeyByChannelKey 按 channel key 限流
func KeyByChannelKey() RateLimitKeyFunc {
	return func(c *gin.Context) string {
		key := c.GetHeader("X-Channel-Key")
		if key != "" {
			return key
		}
		return c.ClientIP()
	}
}
