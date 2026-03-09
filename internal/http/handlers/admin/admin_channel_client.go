package admin

import (
	"errors"
	"strconv"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// ListChannelClients 获取渠道客户端列表
func (h *Handler) ListChannelClients(c *gin.Context) {
	clients, err := h.ChannelClientService.ListChannelClients()
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.channel_clients_fetch_failed", err)
		return
	}
	response.Success(c, clients)
}

type createChannelClientRequest struct {
	Name        string `json:"name" binding:"required"`
	ChannelType string `json:"channel_type" binding:"required"`
	Description string `json:"description"`
}

// CreateChannelClient 创建渠道客户端
func (h *Handler) CreateChannelClient(c *gin.Context) {
	var req createChannelClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	result, err := h.ChannelClientService.CreateChannelClient(req.Name, req.ChannelType, req.Description)
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.channel_client_create_failed", err)
		return
	}

	response.Success(c, result)
}

// GetChannelClient 获取渠道客户端详情
func (h *Handler) GetChannelClient(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	client, err := h.ChannelClientService.GetChannelClient(uint(id))
	if err != nil {
		if errors.Is(err, service.ErrChannelClientNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.channel_client_fetch_failed", err)
		return
	}
	response.Success(c, client)
}

type updateChannelClientStatusRequest struct {
	Status int `json:"status" binding:"oneof=0 1"`
}

// UpdateChannelClientStatus 更新渠道客户端状态
func (h *Handler) UpdateChannelClientStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	var req updateChannelClientStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	if err := h.ChannelClientService.UpdateChannelClientStatus(uint(id), req.Status); err != nil {
		if errors.Is(err, service.ErrChannelClientNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.channel_client_update_failed", err)
		return
	}

	response.Success(c, nil)
}
