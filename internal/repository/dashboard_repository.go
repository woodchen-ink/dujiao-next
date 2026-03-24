package repository

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"

	"gorm.io/gorm"
)

// DashboardRepository 仪表盘聚合查询接口
// 说明：仅聚合统计数据，不承载业务规则。
type DashboardRepository interface {
	GetOverview(startAt, endAt time.Time) (DashboardOverviewRow, error)
	GetOrderTrends(startAt, endAt time.Time) ([]DashboardOrderTrendRow, error)
	GetPaymentTrends(startAt, endAt time.Time) ([]DashboardPaymentTrendRow, error)
	GetStockStats(lowStockThreshold int64) (DashboardStockStatsRow, error)
	GetInventoryAlertItems(lowStockThreshold int64) ([]DashboardInventoryAlertRow, error)
	GetTopProducts(startAt, endAt time.Time, limit int) ([]DashboardProductRankingRow, error)
	GetTopChannels(startAt, endAt time.Time, limit int) ([]DashboardChannelRankingRow, error)
}

// DashboardOverviewRow 仪表盘总览原始统计结果
type DashboardOverviewRow struct {
	OrdersTotal          int64
	PaidOrders           int64
	CompletedOrders      int64
	PendingPaymentOrders int64
	ProcessingOrders     int64
	GMVPaid              float64
	PaymentsTotal        int64
	PaymentsSuccess      int64
	PaymentsFailed       int64
	NewUsers             int64
	ActiveProducts       int64
	Currency             string
}

// DashboardOrderTrendRow 订单趋势统计
type DashboardOrderTrendRow struct {
	Day         string
	OrdersTotal int64
	OrdersPaid  int64
}

// DashboardPaymentTrendRow 支付趋势统计
type DashboardPaymentTrendRow struct {
	Day             string
	PaymentsSuccess int64
	PaymentsFailed  int64
	GMVPaid         float64
}

// DashboardStockStatsRow 库存统计
type DashboardStockStatsRow struct {
	OutOfStockProducts   int64
	LowStockProducts     int64
	AutoAvailableSecrets int64
	ManualAvailableUnits int64
}

// DashboardInventoryAlertRow 库存异常明细行
type DashboardInventoryAlertRow struct {
	ProductID         uint
	SKUID             uint
	ProductTitleJSON  models.JSON
	SKUCode           string
	SKUSpecValuesJSON models.JSON
	FulfillmentType   string
	AlertType         string
	AvailableStock    int64
}

// DashboardProductRankingRow 商品排行原始行
type DashboardProductRankingRow struct {
	ProductID  uint
	Title      string
	PaidOrders int64
	Quantity   int64
	PaidAmount float64
}

// DashboardChannelRankingRow 渠道排行原始行
type DashboardChannelRankingRow struct {
	ChannelID     uint
	ChannelName   string
	ProviderType  string
	ChannelType   string
	SuccessCount  int64
	FailedCount   int64
	SuccessAmount float64
}

// GormDashboardRepository GORM 仪表盘聚合实现
type GormDashboardRepository struct {
	db *gorm.DB
}

// NewDashboardRepository 创建仪表盘仓库
func NewDashboardRepository(db *gorm.DB) *GormDashboardRepository {
	return &GormDashboardRepository{db: db}
}

func paidOrderStatuses() []string {
	return []string{
		constants.OrderStatusPaid,
		constants.OrderStatusFulfilling,
		constants.OrderStatusPartiallyDelivered,
		constants.OrderStatusDelivered,
		constants.OrderStatusCompleted,
	}
}

func onlinePaymentBase(db *gorm.DB, startAt, endAt time.Time) *gorm.DB {
	return db.Model(&models.Payment{}).
		Where("created_at >= ? AND created_at < ? AND provider_type <> ?", startAt, endAt, constants.PaymentProviderWallet)
}

func dashboardDayKey(value time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	return value.In(location).Format("2006-01-02")
}

func resolveDashboardManualAvailableStock(product models.Product) (int64, bool) {
	activeSKUs := activeProductSKUs(product.SKUs)
	if len(activeSKUs) == 0 {
		if product.ManualStockTotal == constants.ManualStockUnlimited {
			return 0, true
		}
		available := int64(product.ManualStockTotal)
		if available < 0 {
			available = 0
		}
		return available, false
	}

	total := int64(0)
	for _, sku := range activeSKUs {
		if sku.ManualStockTotal == constants.ManualStockUnlimited {
			return 0, true
		}
		available := int64(sku.ManualStockTotal)
		if available < 0 {
			available = 0
		}
		total += available
	}
	return total, false
}

// GetOverview 获取总览统计
func (r *GormDashboardRepository) GetOverview(startAt, endAt time.Time) (DashboardOverviewRow, error) {
	result := DashboardOverviewRow{}

	orderBase := func() *gorm.DB {
		return r.db.Model(&models.Order{}).
			Where("parent_id IS NULL AND created_at >= ? AND created_at < ?", startAt, endAt)
	}

	if err := orderBase().Count(&result.OrdersTotal).Error; err != nil {
		return result, err
	}

	paidStatuses := paidOrderStatuses()
	if err := orderBase().Where("status IN ?", paidStatuses).Count(&result.PaidOrders).Error; err != nil {
		return result, err
	}
	if err := orderBase().Where("status = ?", constants.OrderStatusCompleted).Count(&result.CompletedOrders).Error; err != nil {
		return result, err
	}
	if err := orderBase().Where("status = ?", constants.OrderStatusPendingPayment).Count(&result.PendingPaymentOrders).Error; err != nil {
		return result, err
	}
	processingStatuses := []string{
		constants.OrderStatusPaid,
		constants.OrderStatusFulfilling,
		constants.OrderStatusPartiallyDelivered,
		constants.OrderStatusDelivered,
	}
	if err := orderBase().Where("status IN ?", processingStatuses).Count(&result.ProcessingOrders).Error; err != nil {
		return result, err
	}

	if err := r.db.Model(&models.Order{}).
		Where("parent_id IS NULL AND created_at >= ? AND created_at < ? AND status IN ?", startAt, endAt, paidStatuses).
		Select("COALESCE(SUM(total_amount), 0)").
		Scan(&result.GMVPaid).Error; err != nil {
		return result, err
	}

	paymentBase := func() *gorm.DB {
		return onlinePaymentBase(r.db, startAt, endAt)
	}
	if err := paymentBase().Count(&result.PaymentsTotal).Error; err != nil {
		return result, err
	}
	if err := paymentBase().Where("status = ?", constants.PaymentStatusSuccess).Count(&result.PaymentsSuccess).Error; err != nil {
		return result, err
	}
	if err := paymentBase().Where("status = ?", constants.PaymentStatusFailed).Count(&result.PaymentsFailed).Error; err != nil {
		return result, err
	}

	if err := r.db.Model(&models.User{}).
		Where("created_at >= ? AND created_at < ?", startAt, endAt).
		Count(&result.NewUsers).Error; err != nil {
		return result, err
	}

	if err := r.db.Model(&models.Product{}).
		Where("is_active = ?", true).
		Count(&result.ActiveProducts).Error; err != nil {
		return result, err
	}

	_ = r.db.Model(&models.Order{}).
		Where("parent_id IS NULL AND created_at >= ? AND created_at < ? AND currency <> ''", startAt, endAt).
		Order("id DESC").
		Limit(1).
		Pluck("currency", &result.Currency).Error

	return result, nil
}

// GetOrderTrends 获取订单趋势
func (r *GormDashboardRepository) GetOrderTrends(startAt, endAt time.Time) ([]DashboardOrderTrendRow, error) {
	type orderRow struct {
		CreatedAt time.Time
		Status    string
	}

	rows := make([]orderRow, 0)
	if err := r.db.Model(&models.Order{}).
		Select("created_at, status").
		Where("parent_id IS NULL AND created_at >= ? AND created_at < ?", startAt, endAt).
		Order("created_at asc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	paidStatuses := make(map[string]struct{}, len(paidOrderStatuses()))
	for _, status := range paidOrderStatuses() {
		paidStatuses[status] = struct{}{}
	}

	location := startAt.Location()
	grouped := make(map[string]*DashboardOrderTrendRow, len(rows))
	for _, row := range rows {
		day := dashboardDayKey(row.CreatedAt, location)
		point := grouped[day]
		if point == nil {
			point = &DashboardOrderTrendRow{Day: day}
			grouped[day] = point
		}
		point.OrdersTotal += 1
		if _, ok := paidStatuses[row.Status]; ok {
			point.OrdersPaid += 1
		}
	}

	result := make([]DashboardOrderTrendRow, 0, len(grouped))
	for _, item := range grouped {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Day < result[j].Day
	})
	return result, nil
}

// GetPaymentTrends 获取支付趋势
func (r *GormDashboardRepository) GetPaymentTrends(startAt, endAt time.Time) ([]DashboardPaymentTrendRow, error) {
	type paymentRow struct {
		CreatedAt time.Time
		Status    string
		Amount    float64
	}

	rows := make([]paymentRow, 0)
	if err := onlinePaymentBase(r.db, startAt, endAt).
		Select("created_at, status, amount").
		Order("created_at asc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	location := startAt.Location()
	grouped := make(map[string]*DashboardPaymentTrendRow, len(rows))
	for _, row := range rows {
		day := dashboardDayKey(row.CreatedAt, location)
		point := grouped[day]
		if point == nil {
			point = &DashboardPaymentTrendRow{Day: day}
			grouped[day] = point
		}
		switch row.Status {
		case constants.PaymentStatusSuccess:
			point.PaymentsSuccess += 1
			point.GMVPaid += row.Amount
		case constants.PaymentStatusFailed:
			point.PaymentsFailed += 1
		}
	}

	result := make([]DashboardPaymentTrendRow, 0, len(grouped))
	for _, item := range grouped {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Day < result[j].Day
	})
	return result, nil
}

// GetStockStats 获取库存总览统计
func (r *GormDashboardRepository) GetStockStats(lowStockThreshold int64) (DashboardStockStatsRow, error) {
	result := DashboardStockStatsRow{}

	products := make([]models.Product, 0)
	if err := r.db.
		Preload("SKUs", func(db *gorm.DB) *gorm.DB {
			return db.Where("is_active = ?", true).Order("sort_order DESC, created_at ASC")
		}).
		Where("is_active = ?", true).
		Find(&products).Error; err != nil {
		return result, err
	}

	autoProductIDs := make([]uint, 0)
	for _, product := range products {
		fulfillmentType := strings.TrimSpace(product.FulfillmentType)
		if fulfillmentType == constants.FulfillmentTypeAuto {
			autoProductIDs = append(autoProductIDs, product.ID)
			continue
		}
		if fulfillmentType != constants.FulfillmentTypeManual {
			continue
		}
		available, unlimited := resolveDashboardManualAvailableStock(product)
		if unlimited {
			continue
		}
		result.ManualAvailableUnits += available
		switch classifyInventoryAlertType(available, lowStockThreshold) {
		case constants.NotificationAlertTypeOutOfStockProducts:
			result.OutOfStockProducts += 1
		case constants.NotificationAlertTypeLowStockProducts:
			result.LowStockProducts += 1
		}
	}

	if len(autoProductIDs) == 0 {
		return result, nil
	}

	type countRow struct {
		ProductID uint
		Total     int64
	}
	var rows []countRow
	if err := r.db.Model(&models.CardSecret{}).
		Select("product_id, COUNT(*) as total").
		Where("product_id IN ? AND status = ?", autoProductIDs, models.CardSecretStatusAvailable).
		Group("product_id").
		Scan(&rows).Error; err != nil {
		return result, err
	}

	availableMap := make(map[uint]int64, len(rows))
	for _, item := range rows {
		availableMap[item.ProductID] = item.Total
		result.AutoAvailableSecrets += item.Total
	}

	for _, productID := range autoProductIDs {
		available := availableMap[productID]
		if available <= 0 {
			result.OutOfStockProducts += 1
		} else if available <= lowStockThreshold {
			result.LowStockProducts += 1
		}
	}

	return result, nil
}

// GetInventoryAlertItems 获取库存异常明细
func (r *GormDashboardRepository) GetInventoryAlertItems(lowStockThreshold int64) ([]DashboardInventoryAlertRow, error) {
	products := make([]models.Product, 0)
	if err := r.db.
		Preload("SKUs", func(db *gorm.DB) *gorm.DB {
			return db.Where("is_active = ?", true).Order("sort_order DESC, created_at ASC")
		}).
		Where("is_active = ?", true).
		Order("sort_order DESC, created_at DESC").
		Find(&products).Error; err != nil {
		return nil, err
	}

	autoProductIDs := make([]uint, 0)
	for _, product := range products {
		if strings.TrimSpace(product.FulfillmentType) == constants.FulfillmentTypeAuto {
			autoProductIDs = append(autoProductIDs, product.ID)
		}
	}

	autoAvailableMap := make(map[uint]map[uint]int64)
	if len(autoProductIDs) > 0 {
		type countRow struct {
			ProductID uint
			SKUID     uint
			Total     int64
		}
		rows := make([]countRow, 0)
		if err := r.db.Model(&models.CardSecret{}).
			Select("product_id, sku_id, COUNT(*) as total").
			Where("product_id IN ? AND status = ?", autoProductIDs, models.CardSecretStatusAvailable).
			Group("product_id, sku_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if autoAvailableMap[row.ProductID] == nil {
				autoAvailableMap[row.ProductID] = make(map[uint]int64)
			}
			autoAvailableMap[row.ProductID][row.SKUID] = row.Total
		}
	}

	result := make([]DashboardInventoryAlertRow, 0)
	for _, product := range products {
		switch strings.TrimSpace(product.FulfillmentType) {
		case constants.FulfillmentTypeAuto:
			result = append(result, collectAutoInventoryAlertRows(product, autoAvailableMap[product.ID], lowStockThreshold)...)
		case constants.FulfillmentTypeManual:
			result = append(result, collectManualInventoryAlertRows(product, lowStockThreshold)...)
		}
	}
	return result, nil
}

// GetTopProducts 获取商品排行榜
func (r *GormDashboardRepository) GetTopProducts(startAt, endAt time.Time, limit int) ([]DashboardProductRankingRow, error) {
	if limit <= 0 {
		limit = 5
	}
	rows := make([]DashboardProductRankingRow, 0)
	titleExpr := localizedJSONCoalesceExpr(r.db, "order_items.title_json")
	if err := r.db.Model(&models.OrderItem{}).
		Select(fmt.Sprintf(`
			order_items.product_id as product_id,
			%s as title,
			COUNT(DISTINCT order_items.order_id) as paid_orders,
			COALESCE(SUM(order_items.quantity), 0) as quantity,
			COALESCE(SUM(order_items.total_price - order_items.coupon_discount - order_items.promotion_discount), 0) as paid_amount
		`, titleExpr)).
		Joins("JOIN orders ON orders.id = order_items.order_id").
		Where("orders.created_at >= ? AND orders.created_at < ? AND orders.status IN ?", startAt, endAt, paidOrderStatuses()).
		Group("order_items.product_id, title").
		Order("paid_amount DESC, quantity DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func collectManualInventoryAlertRows(product models.Product, lowStockThreshold int64) []DashboardInventoryAlertRow {
	result := make([]DashboardInventoryAlertRow, 0)
	activeSKUs := activeProductSKUs(product.SKUs)
	if len(activeSKUs) == 0 {
		if product.ManualStockTotal == constants.ManualStockUnlimited {
			return result
		}
		available := int64(product.ManualStockTotal)
		if available < 0 {
			available = 0
		}
		if alertType := classifyInventoryAlertType(available, lowStockThreshold); alertType != "" {
			result = append(result, DashboardInventoryAlertRow{
				ProductID:        product.ID,
				ProductTitleJSON: product.TitleJSON,
				FulfillmentType:  constants.FulfillmentTypeManual,
				AlertType:        alertType,
				AvailableStock:   available,
			})
		}
		return result
	}

	for _, sku := range activeSKUs {
		if sku.ManualStockTotal == constants.ManualStockUnlimited {
			continue
		}
		available := int64(sku.ManualStockTotal)
		if available < 0 {
			available = 0
		}
		if alertType := classifyInventoryAlertType(available, lowStockThreshold); alertType != "" {
			result = append(result, DashboardInventoryAlertRow{
				ProductID:         product.ID,
				SKUID:             sku.ID,
				ProductTitleJSON:  product.TitleJSON,
				SKUCode:           strings.TrimSpace(sku.SKUCode),
				SKUSpecValuesJSON: sku.SpecValuesJSON,
				FulfillmentType:   constants.FulfillmentTypeManual,
				AlertType:         alertType,
				AvailableStock:    available,
			})
		}
	}
	return result
}

func collectAutoInventoryAlertRows(product models.Product, availableMap map[uint]int64, lowStockThreshold int64) []DashboardInventoryAlertRow {
	result := make([]DashboardInventoryAlertRow, 0)
	activeSKUs := activeProductSKUs(product.SKUs)
	totalAvailable := int64(0)
	activeSKUSet := make(map[uint]struct{}, len(activeSKUs))
	for _, sku := range activeSKUs {
		activeSKUSet[sku.ID] = struct{}{}
	}
	legacyInactiveAvailable := int64(0)
	for skuID, total := range availableMap {
		totalAvailable += total
		if skuID == 0 {
			continue
		}
		if _, ok := activeSKUSet[skuID]; ok {
			continue
		}
		legacyInactiveAvailable += total
	}
	if len(activeSKUs) == 0 {
		if alertType := classifyInventoryAlertType(totalAvailable, lowStockThreshold); alertType != "" {
			result = append(result, DashboardInventoryAlertRow{
				ProductID:        product.ID,
				ProductTitleJSON: product.TitleJSON,
				FulfillmentType:  constants.FulfillmentTypeAuto,
				AlertType:        alertType,
				AvailableStock:   totalAvailable,
			})
		}
		return result
	}

	legacyTargetIdx := resolveDashboardLegacyStockTargetSKUIndex(activeSKUs)
	hasPositiveActive := false
	for idx, sku := range activeSKUs {
		available := availableMap[sku.ID]
		if idx == legacyTargetIdx {
			available += availableMap[0]
		}
		if len(activeSKUs) == 1 {
			available += legacyInactiveAvailable
		}
		if available > 0 {
			hasPositiveActive = true
		}
		if alertType := classifyInventoryAlertType(available, lowStockThreshold); alertType != "" {
			result = append(result, DashboardInventoryAlertRow{
				ProductID:         product.ID,
				SKUID:             sku.ID,
				ProductTitleJSON:  product.TitleJSON,
				SKUCode:           strings.TrimSpace(sku.SKUCode),
				SKUSpecValuesJSON: sku.SpecValuesJSON,
				FulfillmentType:   constants.FulfillmentTypeAuto,
				AlertType:         alertType,
				AvailableStock:    available,
			})
		}
	}
	if hasPositiveActive || legacyInactiveAvailable <= 0 {
		return result
	}
	if fallbackType := classifyInventoryAlertType(totalAvailable, lowStockThreshold); fallbackType != "" {
		return []DashboardInventoryAlertRow{
			{
				ProductID:        product.ID,
				ProductTitleJSON: product.TitleJSON,
				FulfillmentType:  constants.FulfillmentTypeAuto,
				AlertType:        fallbackType,
				AvailableStock:   totalAvailable,
			},
		}
	}
	return result
}

func activeProductSKUs(items []models.ProductSKU) []models.ProductSKU {
	result := make([]models.ProductSKU, 0, len(items))
	for _, item := range items {
		if !item.IsActive {
			continue
		}
		result = append(result, item)
	}
	return result
}

func classifyInventoryAlertType(available int64, lowStockThreshold int64) string {
	switch {
	case available <= 0:
		return constants.NotificationAlertTypeOutOfStockProducts
	case available <= lowStockThreshold:
		return constants.NotificationAlertTypeLowStockProducts
	default:
		return ""
	}
}

func resolveDashboardLegacyStockTargetSKUIndex(skus []models.ProductSKU) int {
	if len(skus) == 0 {
		return -1
	}
	defaultCode := strings.ToUpper(strings.TrimSpace(models.DefaultSKUCode))
	firstActiveIdx := -1
	for idx := range skus {
		if !skus[idx].IsActive {
			continue
		}
		if firstActiveIdx < 0 {
			firstActiveIdx = idx
		}
		if strings.ToUpper(strings.TrimSpace(skus[idx].SKUCode)) == defaultCode {
			return idx
		}
	}
	return firstActiveIdx
}

// GetTopChannels 获取支付渠道排行榜
func (r *GormDashboardRepository) GetTopChannels(startAt, endAt time.Time, limit int) ([]DashboardChannelRankingRow, error) {
	if limit <= 0 {
		limit = 5
	}
	rows := make([]DashboardChannelRankingRow, 0)
	if err := r.db.Model(&models.Payment{}).
		Select(`
			payments.channel_id as channel_id,
			COALESCE(payment_channels.name, '') as channel_name,
			payments.provider_type as provider_type,
			payments.channel_type as channel_type,
			SUM(CASE WHEN payments.status = 'success' THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN payments.status = 'failed' THEN 1 ELSE 0 END) as failed_count,
			COALESCE(SUM(CASE WHEN payments.status = 'success' THEN payments.amount ELSE 0 END), 0) as success_amount
		`).
		Joins("LEFT JOIN payment_channels ON payment_channels.id = payments.channel_id").
		Where("payments.created_at >= ? AND payments.created_at < ? AND payments.provider_type <> ?", startAt, endAt, constants.PaymentProviderWallet).
		Group("payments.channel_id, payment_channels.name, payments.provider_type, payments.channel_type").
		Order("success_amount DESC, success_count DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
