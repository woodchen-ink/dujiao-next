package public

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// OrderItemRequest 订单项请求
type OrderItemRequest struct {
	ProductID       uint   `json:"product_id" binding:"required"`
	SKUID           uint   `json:"sku_id"`
	Quantity        int    `json:"quantity" binding:"required"`
	FulfillmentType string `json:"fulfillment_type"`
}

// CreateOrderRequest 创建订单请求
type CreateOrderRequest struct {
	Items               []OrderItemRequest     `json:"items" binding:"required"`
	CouponCode          string                 `json:"coupon_code"`
	AffiliateCode       string                 `json:"affiliate_code"`
	AffiliateVisitorKey string                 `json:"affiliate_visitor_key"`
	ManualFormData      map[string]models.JSON `json:"manual_form_data"`
}

// PreviewOrder 订单金额预览
func (h *Handler) PreviewOrder(c *gin.Context) {
	uid, ok := shared.GetUserID(c)
	if !ok {
		return
	}

	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	var items []service.CreateOrderItem
	for _, item := range req.Items {
		items = append(items, service.CreateOrderItem{
			ProductID:       item.ProductID,
			SKUID:           item.SKUID,
			Quantity:        item.Quantity,
			FulfillmentType: item.FulfillmentType,
		})
	}

	preview, err := h.OrderService.PreviewOrder(service.CreateOrderInput{
		UserID:              uid,
		Items:               items,
		CouponCode:          req.CouponCode,
		AffiliateCode:       req.AffiliateCode,
		AffiliateVisitorKey: req.AffiliateVisitorKey,
		ClientIP:            c.ClientIP(),
		ManualFormData:      req.ManualFormData,
	})
	if err != nil {
		respondUserOrderPreviewError(c, err)
		return
	}

	response.Success(c, preview)
}

// CreateOrder 创建订单
func (h *Handler) CreateOrder(c *gin.Context) {
	uid, ok := shared.GetUserID(c)
	if !ok {
		return
	}

	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	var items []service.CreateOrderItem
	for _, item := range req.Items {
		items = append(items, service.CreateOrderItem{
			ProductID:       item.ProductID,
			SKUID:           item.SKUID,
			Quantity:        item.Quantity,
			FulfillmentType: item.FulfillmentType,
		})
	}

	order, err := h.OrderService.CreateOrder(service.CreateOrderInput{
		UserID:              uid,
		Items:               items,
		CouponCode:          req.CouponCode,
		AffiliateCode:       req.AffiliateCode,
		AffiliateVisitorKey: req.AffiliateVisitorKey,
		ClientIP:            c.ClientIP(),
		ManualFormData:      req.ManualFormData,
	})
	if err != nil {
		respondUserOrderCreateError(c, err)
		return
	}

	order.MaskUpstreamFulfillmentType()
	response.Success(c, order)
}

// ListOrders 获取订单列表
func (h *Handler) ListOrders(c *gin.Context) {
	uid, ok := shared.GetUserID(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = shared.NormalizePagination(page, pageSize)

	status := strings.TrimSpace(c.Query("status"))
	orderNo := strings.TrimSpace(c.Query("order_no"))

	orders, total, err := h.OrderService.ListOrdersByUser(repository.OrderListFilter{
		Page:     page,
		PageSize: pageSize,
		UserID:   uid,
		Status:   status,
		OrderNo:  orderNo,
	})
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
		return
	}

	for i := range orders {
		orders[i].MaskUpstreamFulfillmentType()
	}
	pagination := response.BuildPagination(page, pageSize, total)
	response.SuccessWithPage(c, orders, pagination)
}

// GetOrder 获取订单详情
func (h *Handler) GetOrder(c *gin.Context) {
	uid, ok := shared.GetUserID(c)
	if !ok {
		return
	}

	orderID, err := shared.ParseParamUint(c, "id")
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}

	order, err := h.OrderService.GetOrderByUser(orderID, uid)
	if err != nil {
		if errors.Is(err, service.ErrOrderNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.order_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
		return
	}

	order.MaskUpstreamFulfillmentType()
	response.Success(c, order)
}

// GetOrderByOrderNo 按订单号获取订单详情
func (h *Handler) GetOrderByOrderNo(c *gin.Context) {
	uid, ok := shared.GetUserID(c)
	if !ok {
		return
	}

	orderNo := strings.TrimSpace(c.Param("order_no"))
	if orderNo == "" {
		shared.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}

	order, err := h.OrderService.GetOrderByUserOrderNo(orderNo, uid)
	if err != nil {
		if errors.Is(err, service.ErrOrderNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.order_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
		return
	}

	order.MaskUpstreamFulfillmentType()
	response.Success(c, order)
}

// CancelOrder 用户取消订单
func (h *Handler) CancelOrder(c *gin.Context) {
	uid, ok := shared.GetUserID(c)
	if !ok {
		return
	}

	orderID, err := shared.ParseParamUint(c, "id")
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}

	order, err := h.OrderService.CancelOrder(orderID, uid)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOrderNotFound):
			shared.RespondError(c, response.CodeNotFound, "error.order_not_found", nil)
		case errors.Is(err, service.ErrOrderCancelNotAllowed):
			shared.RespondError(c, response.CodeBadRequest, "error.order_cancel_not_allowed", nil)
		default:
			shared.RespondError(c, response.CodeInternal, "error.order_update_failed", err)
		}
		return
	}

	order.MaskUpstreamFulfillmentType()
	response.Success(c, order)
}
