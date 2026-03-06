package upstream

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/provider"
	"github.com/dujiao-next/internal/repository"
	"github.com/dujiao-next/internal/service"
	upstreamadapter "github.com/dujiao-next/internal/upstream"

	"github.com/gin-gonic/gin"
)

// Handler 上游 API 处理器（本站作为 B 站暴露给下游 A 站的接口）
type Handler struct {
	*provider.Container
	downstreamRefRepo repository.DownstreamOrderRefRepository
}

// New 创建上游处理器
func New(c *provider.Container, downstreamRefRepo repository.DownstreamOrderRefRepository) *Handler {
	return &Handler{
		Container:         c,
		downstreamRefRepo: downstreamRefRepo,
	}
}

// ---- context helpers (避免循环引用 router 包) ----

const (
	upstreamUserIDKey       = "upstream_user_id"
	upstreamCredentialIDKey = "upstream_credential_id"
)

func getUpstreamUserID(c *gin.Context) uint {
	v, _ := c.Get(upstreamUserIDKey)
	if id, ok := v.(uint); ok {
		return id
	}
	return 0
}

func getUpstreamCredentialID(c *gin.Context) uint {
	v, _ := c.Get(upstreamCredentialIDKey)
	if id, ok := v.(uint); ok {
		return id
	}
	return 0
}

// ---- response helpers ----

func successResponse(c *gin.Context, data interface{}) {
	if data == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusOK, data)
}

func errorResponse(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"ok":            false,
		"error_code":    code,
		"error_message": message,
	})
}

// ---- Ping ----

// Ping POST /api/v1/upstream/ping
func (h *Handler) Ping(c *gin.Context) {
	userID := getUpstreamUserID(c)
	if userID == 0 {
		errorResponse(c, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	// 站点名称
	siteName := ""
	siteConfig, err := h.SettingService.GetByKey(constants.SettingKeySiteConfig)
	if err == nil && siteConfig != nil {
		if name, ok := siteConfig["site_name"]; ok {
			if s, ok := name.(string); ok {
				siteName = s
			}
		}
	}

	// 用户钱包余额
	balanceStr := "0.00"
	account, err := h.WalletService.GetAccount(userID)
	if err == nil && account != nil {
		balanceStr = account.Balance.StringFixed(2)
	}

	// 币种
	currency, _ := h.SettingService.GetSiteCurrency("CNY")

	successResponse(c, gin.H{
		"ok":               true,
		"site_name":        siteName,
		"protocol_version": "1.0",
		"user_id":          userID,
		"balance":          balanceStr,
		"currency":         currency,
	})
}

// ---- ListProducts ----

// upstreamProduct 上游商品响应格式
type upstreamProduct struct {
	ID              uint               `json:"id"`
	Slug            string             `json:"slug"`
	Title           models.JSON        `json:"title"`
	Description     models.JSON        `json:"description"`
	Images          models.StringArray `json:"images"`
	Tags            models.StringArray `json:"tags"`
	PriceAmount     string             `json:"price_amount"`
	FulfillmentType string             `json:"fulfillment_type"`
	IsActive        bool               `json:"is_active"`
	CategoryID      uint               `json:"category_id"`
	SKUs            []upstreamSKU      `json:"skus"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type upstreamSKU struct {
	ID          uint        `json:"id"`
	SKUCode     string      `json:"sku_code"`
	SpecValues  models.JSON `json:"spec_values"`
	PriceAmount string      `json:"price_amount"`
	StockStatus string      `json:"stock_status"`
	IsActive    bool        `json:"is_active"`
}

// ListProducts GET /api/v1/upstream/products
func (h *Handler) ListProducts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	products, total, err := h.ProductService.ListPublic("", "", page, pageSize)
	if err != nil {
		logger.Errorw("upstream_list_products_failed", "error", err)
		errorResponse(c, http.StatusInternalServerError, "internal_error", "failed to list products")
		return
	}

	// 补充自动发货库存计数
	if err := h.ProductService.ApplyAutoStockCounts(products); err != nil {
		logger.Warnw("upstream_apply_stock_counts_failed", "error", err)
	}

	items := make([]upstreamProduct, 0, len(products))
	for _, p := range products {
		items = append(items, toUpstreamProduct(p))
	}

	successResponse(c, gin.H{
		"ok":        true,
		"products":  items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetProduct GET /api/v1/upstream/products/:id
func (h *Handler) GetProduct(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		errorResponse(c, http.StatusBadRequest, "bad_request", "product id is required")
		return
	}

	product, err := h.ProductService.GetAdminByID(id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			errorResponse(c, http.StatusNotFound, "product_not_found", "product not found")
			return
		}
		logger.Errorw("upstream_get_product_failed", "id", id, "error", err)
		errorResponse(c, http.StatusInternalServerError, "internal_error", "failed to get product")
		return
	}

	if !product.IsActive {
		errorResponse(c, http.StatusNotFound, "product_unavailable", "product is not active")
		return
	}

	// 补充自动发货库存计数
	products := []models.Product{*product}
	if err := h.ProductService.ApplyAutoStockCounts(products); err != nil {
		logger.Warnw("upstream_apply_stock_counts_failed", "error", err)
	}

	successResponse(c, gin.H{
		"ok":      true,
		"product": toUpstreamProduct(products[0]),
	})
}

// ---- CreateOrder ----

type createOrderRequest struct {
	SKUID             uint        `json:"sku_id" binding:"required"`
	Quantity          int         `json:"quantity" binding:"required,min=1"`
	ManualFormData    models.JSON `json:"manual_form_data"`
	DownstreamOrderNo string      `json:"downstream_order_no"`
	TraceID           string      `json:"trace_id"`
	CallbackURL       string      `json:"callback_url"`
}

// CreateOrder POST /api/v1/upstream/orders
func (h *Handler) CreateOrder(c *gin.Context) {
	userID := getUpstreamUserID(c)
	credentialID := getUpstreamCredentialID(c)
	if userID == 0 || credentialID == 0 {
		errorResponse(c, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errorResponse(c, http.StatusBadRequest, "bad_request", "invalid request body: "+err.Error())
		return
	}

	// 查找 SKU 获取所属商品 ID
	sku, err := h.ProductSKURepo.GetByID(req.SKUID)
	if err != nil || sku == nil {
		errorResponse(c, http.StatusBadRequest, "sku_unavailable", "sku not found")
		return
	}
	if !sku.IsActive {
		errorResponse(c, http.StatusBadRequest, "sku_unavailable", "sku is not active")
		return
	}

	// 验证商品是否上架
	product, err := h.ProductRepo.GetByID(fmt.Sprintf("%d", sku.ProductID))
	if err != nil || product == nil || !product.IsActive {
		errorResponse(c, http.StatusBadRequest, "product_unavailable", "product is not available")
		return
	}

	// 构建手动表单数据
	var manualFormData map[string]models.JSON
	if req.ManualFormData != nil && product.FulfillmentType == constants.FulfillmentTypeManual {
		manualFormData = map[string]models.JSON{
			fmt.Sprintf("%d", sku.ProductID): req.ManualFormData,
		}
	}

	// 创建订单
	input := service.CreateOrderInput{
		UserID: userID,
		Items: []service.CreateOrderItem{
			{
				ProductID:       sku.ProductID,
				SKUID:           req.SKUID,
				Quantity:        req.Quantity,
				FulfillmentType: product.FulfillmentType,
			},
		},
		ClientIP:       c.ClientIP(),
		ManualFormData: manualFormData,
	}

	order, err := h.OrderService.CreateOrder(input)
	if err != nil {
		mapOrderErrorToResponse(c, err)
		return
	}

	// 创建下游订单引用记录
	ref := &models.DownstreamOrderRef{
		OrderID:           order.ID,
		ApiCredentialID:   credentialID,
		DownstreamOrderNo: req.DownstreamOrderNo,
		CallbackURL:       req.CallbackURL,
		TraceID:           req.TraceID,
		CallbackStatus:    "pending",
	}
	if createErr := h.downstreamRefRepo.Create(ref); createErr != nil {
		logger.Errorw("upstream_create_downstream_ref_failed",
			"order_id", order.ID,
			"error", createErr,
		)
		// 不回滚订单，仅记录错误
	}

	// 自动使用钱包余额支付（上游 API 订单默认钱包扣款）
	payResult, payErr := h.PaymentService.CreatePayment(service.CreatePaymentInput{
		OrderID:    order.ID,
		UseBalance: true,
		ClientIP:   c.ClientIP(),
	})
	if payErr != nil {
		logger.Errorw("upstream_auto_wallet_pay_failed",
			"order_id", order.ID,
			"error", payErr,
		)
		// 钱包余额不足或支付失败，返回 200 + ok:false 让 A 站正确解析错误码
		c.JSON(http.StatusOK, gin.H{
			"ok":            false,
			"order_id":      order.ID,
			"order_no":      order.OrderNo,
			"status":        order.Status,
			"error_code":    "payment_failed",
			"error_message": fmt.Sprintf("wallet payment failed: %s", payErr.Error()),
		})
		return
	}

	// 刷新订单状态
	finalStatus := order.Status
	if payResult != nil && payResult.OrderPaid {
		finalStatus = constants.OrderStatusPaid
	}

	// 币种
	currency, _ := h.SettingService.GetSiteCurrency("CNY")

	successResponse(c, gin.H{
		"ok":       true,
		"order_id": order.ID,
		"order_no": order.OrderNo,
		"status":   finalStatus,
		"amount":   order.TotalAmount.StringFixed(2),
		"currency": currency,
	})
}

// ---- GetOrder ----

// GetOrder GET /api/v1/upstream/orders/:id
func (h *Handler) GetOrder(c *gin.Context) {
	userID := getUpstreamUserID(c)
	if userID == 0 {
		errorResponse(c, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	orderID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errorResponse(c, http.StatusBadRequest, "bad_request", "invalid order id")
		return
	}

	order, err := h.OrderService.GetOrderByUser(uint(orderID), userID)
	if err != nil {
		if errors.Is(err, service.ErrOrderNotFound) {
			errorResponse(c, http.StatusNotFound, "order_not_found", "order not found")
			return
		}
		logger.Errorw("upstream_get_order_failed", "order_id", orderID, "error", err)
		errorResponse(c, http.StatusInternalServerError, "internal_error", "failed to get order")
		return
	}

	resp := gin.H{
		"ok":       true,
		"order_id": order.ID,
		"order_no": order.OrderNo,
		"status":   order.Status,
		"amount":   order.TotalAmount.StringFixed(2),
		"currency": order.Currency,
	}

	// 若已交付，返回交付信息
	if order.Fulfillment != nil && order.Fulfillment.Status == constants.FulfillmentStatusDelivered {
		resp["fulfillment"] = gin.H{
			"type":         order.Fulfillment.Type,
			"status":       order.Fulfillment.Status,
			"payload":      order.Fulfillment.Payload,
			"delivered_at": order.Fulfillment.DeliveredAt,
		}
	}

	// 订单项信息
	if len(order.Items) > 0 {
		items := make([]gin.H, 0, len(order.Items))
		for _, item := range order.Items {
			items = append(items, gin.H{
				"product_id":       item.ProductID,
				"sku_id":           item.SKUID,
				"title":            item.TitleJSON,
				"quantity":         item.Quantity,
				"unit_price":       item.UnitPrice.StringFixed(2),
				"total_price":      item.TotalPrice.StringFixed(2),
				"fulfillment_type": item.FulfillmentType,
			})
		}
		resp["items"] = items
	}

	successResponse(c, resp)
}

// ---- CancelOrder ----

// CancelOrder POST /api/v1/upstream/orders/:id/cancel
func (h *Handler) CancelOrder(c *gin.Context) {
	userID := getUpstreamUserID(c)
	if userID == 0 {
		errorResponse(c, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	orderID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errorResponse(c, http.StatusBadRequest, "bad_request", "invalid order id")
		return
	}

	order, err := h.OrderService.CancelOrder(uint(orderID), userID)
	if err != nil {
		if errors.Is(err, service.ErrOrderNotFound) {
			errorResponse(c, http.StatusNotFound, "order_not_found", "order not found")
			return
		}
		if errors.Is(err, service.ErrOrderCancelNotAllowed) {
			errorResponse(c, http.StatusConflict, "cancel_not_allowed", "order cannot be canceled in current status")
			return
		}
		logger.Errorw("upstream_cancel_order_failed", "order_id", orderID, "error", err)
		errorResponse(c, http.StatusInternalServerError, "internal_error", "failed to cancel order")
		return
	}

	successResponse(c, gin.H{
		"ok":       true,
		"order_id": order.ID,
		"order_no": order.OrderNo,
		"status":   order.Status,
	})
}

// ---- HandleCallback (A 站接收 B 站回调) ----

type callbackPayload struct {
	Event             string `json:"event"`
	OrderID           uint   `json:"order_id"`
	OrderNo           string `json:"order_no"`
	DownstreamOrderNo string `json:"downstream_order_no"`
	Status            string `json:"status"`
	Fulfillment       *struct {
		Type        string     `json:"type"`
		Status      string     `json:"status"`
		Payload     string     `json:"payload"`
		DeliveredAt *time.Time `json:"delivered_at"`
	} `json:"fulfillment,omitempty"`
	Timestamp int64 `json:"timestamp"`
}

// HandleCallback POST /api/v1/upstream/callback (A 站点接收 B 站回调)
func (h *Handler) HandleCallback(c *gin.Context) {
	// ---- 签名验证 ----
	apiKey := c.GetHeader(upstreamadapter.HeaderApiKey)
	timestampStr := c.GetHeader(upstreamadapter.HeaderTimestamp)
	signature := c.GetHeader(upstreamadapter.HeaderSignature)

	if apiKey == "" || timestampStr == "" || signature == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "missing authentication headers"})
		return
	}

	timestamp, err := upstreamadapter.ParseTimestamp(timestampStr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "invalid timestamp"})
		return
	}

	if !upstreamadapter.IsTimestampValid(timestamp) {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "timestamp expired"})
		return
	}

	// 根据 api_key 查找对应的站点连接
	conn, err := h.SiteConnectionRepo.GetByApiKey(apiKey)
	if err != nil {
		logger.Errorw("upstream_callback_lookup_connection_failed", "api_key", apiKey, "error", err)
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "internal error"})
		return
	}
	if conn == nil || conn.Status != "active" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "invalid api key"})
		return
	}

	// 读取 body 用于签名验证
	var body []byte
	if c.Request.Body != nil {
		body, err = io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "message": "failed to read request body"})
			return
		}
		c.Request.Body = io.NopCloser(&bodyBuf{data: body})
	}

	// 解密 api_secret 并验证签名
	apiSecret := conn.ApiSecret
	if h.SiteConnectionService != nil {
		if decrypted, decErr := h.SiteConnectionService.DecryptSecret(apiSecret); decErr == nil {
			apiSecret = decrypted
		}
	}

	if !upstreamadapter.Verify(apiSecret, "POST", "/api/v1/upstream/callback", signature, timestamp, body) {
		logger.Warnw("upstream_callback_signature_invalid", "api_key", apiKey)
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "signature verification failed"})
		return
	}

	// ---- 解析 payload ----
	var payload callbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "invalid request body"})
		return
	}

	if payload.DownstreamOrderNo == "" || payload.Status == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "missing required fields"})
		return
	}

	if h.ProcurementOrderService == nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "service not available"})
		return
	}

	// 根据 downstream_order_no（即本站的 local_order_no）查找对应的采购单
	procOrder, err := h.ProcurementOrderService.GetByLocalOrderNo(payload.DownstreamOrderNo)
	if err != nil || procOrder == nil {
		logger.Warnw("upstream_callback_procurement_not_found",
			"downstream_order_no", payload.DownstreamOrderNo,
			"upstream_order_id", payload.OrderID,
		)
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "procurement order not found"})
		return
	}

	// 转换状态并处理回调
	var uf *upstreamadapter.UpstreamFulfillment
	if payload.Fulfillment != nil {
		uf = &upstreamadapter.UpstreamFulfillment{
			Type:        payload.Fulfillment.Type,
			Status:      payload.Fulfillment.Status,
			Payload:     payload.Fulfillment.Payload,
			DeliveredAt: payload.Fulfillment.DeliveredAt,
		}
	}

	upstreamStatus := mapCallbackStatus(payload.Status)
	if err := h.ProcurementOrderService.HandleUpstreamCallback(procOrder.ID, upstreamStatus, uf); err != nil {
		logger.Warnw("upstream_callback_handle_failed",
			"procurement_order_id", procOrder.ID,
			"upstream_status", upstreamStatus,
			"error", err,
		)
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "callback processing failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "received"})
}

// bodyBuf 用于重置 body
type bodyBuf struct {
	data   []byte
	offset int
}

func (b *bodyBuf) Read(p []byte) (n int, err error) {
	if b.offset >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

// mapCallbackStatus 将上游订单状态映射为回调处理状态
func mapCallbackStatus(status string) string {
	switch status {
	case "delivered", "completed":
		return "delivered"
	case "canceled":
		return "canceled"
	default:
		return status
	}
}

// ---- helpers ----

func toUpstreamProduct(p models.Product) upstreamProduct {
	skus := make([]upstreamSKU, 0, len(p.SKUs))
	for _, s := range p.SKUs {
		if !s.IsActive {
			continue
		}
		skus = append(skus, upstreamSKU{
			ID:          s.ID,
			SKUCode:     s.SKUCode,
			SpecValues:  s.SpecValuesJSON,
			PriceAmount: s.PriceAmount.StringFixed(2),
			StockStatus: computeSKUStockStatus(p, s),
			IsActive:    s.IsActive,
		})
	}

	return upstreamProduct{
		ID:              p.ID,
		Slug:            p.Slug,
		Title:           p.TitleJSON,
		Description:     p.DescriptionJSON,
		Images:          p.Images,
		Tags:            p.Tags,
		PriceAmount:     p.PriceAmount.StringFixed(2),
		FulfillmentType: p.FulfillmentType,
		IsActive:        p.IsActive,
		CategoryID:      p.CategoryID,
		SKUs:            skus,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// computeSKUStockStatus 计算 SKU 的库存状态（不暴露精确数字）
func computeSKUStockStatus(p models.Product, s models.ProductSKU) string {
	if p.FulfillmentType == constants.FulfillmentTypeManual {
		// 手动交付：根据手动库存判断
		if p.ManualStockTotal < 0 {
			// 无限库存
			return "in_stock"
		}
		available := p.ManualStockTotal - p.ManualStockLocked
		if available <= 0 {
			return "out_of_stock"
		}
		if available <= 20 {
			return "low_stock"
		}
		return "in_stock"
	}

	// 自动发货：根据卡密库存判断
	available := s.AutoStockAvailable
	if available > 100 {
		return "in_stock"
	}
	if available > 0 {
		if available <= 20 {
			return "low_stock"
		}
		return "in_stock"
	}
	return "out_of_stock"
}

// mapOrderErrorToResponse 将订单创建错误映射为上游 API 错误响应
func mapOrderErrorToResponse(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWalletInsufficientBalance):
		errorResponse(c, http.StatusPaymentRequired, "insufficient_balance", "wallet balance is insufficient")
	case errors.Is(err, service.ErrCardSecretInsufficient),
		errors.Is(err, service.ErrManualStockInsufficient):
		errorResponse(c, http.StatusConflict, "insufficient_stock", "product stock is insufficient")
	case errors.Is(err, service.ErrProductNotAvailable),
		errors.Is(err, service.ErrProductNotFound):
		errorResponse(c, http.StatusBadRequest, "product_unavailable", "product is not available")
	case errors.Is(err, service.ErrProductSKUInvalid),
		errors.Is(err, service.ErrProductSKURequired):
		errorResponse(c, http.StatusBadRequest, "sku_unavailable", "sku is invalid or not available")
	case errors.Is(err, service.ErrInvalidOrderItem):
		errorResponse(c, http.StatusBadRequest, "bad_request", "invalid order parameters")
	case errors.Is(err, service.ErrManualFormRequiredMissing),
		errors.Is(err, service.ErrManualFormFieldInvalid),
		errors.Is(err, service.ErrManualFormTypeInvalid),
		errors.Is(err, service.ErrManualFormOptionInvalid):
		errorResponse(c, http.StatusBadRequest, "bad_request", "manual form data is invalid: "+err.Error())
	default:
		logger.Errorw("upstream_create_order_failed", "error", err)
		errorResponse(c, http.StatusInternalServerError, "internal_error", "failed to create order")
	}
}
