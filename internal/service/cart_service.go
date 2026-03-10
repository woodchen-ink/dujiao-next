package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
)

// CartItemDetail 购物车项详情（用于响应）
type CartItemDetail struct {
	ProductID       uint               `json:"product_id"`
	SKUID           uint               `json:"sku_id"`
	Quantity        int                `json:"quantity"`
	FulfillmentType string             `json:"fulfillment_type"`
	UnitPrice       models.Money       `json:"unit_price"`
	OriginalPrice   models.Money       `json:"original_price"`
	Currency        string             `json:"currency"`
	Product         *models.Product    `json:"product"`
	SKU             *models.ProductSKU `json:"sku"`
}

// UpsertCartItemInput 购物车更新输入
type UpsertCartItemInput struct {
	UserID          uint
	ProductID       uint
	SKUID           uint
	Quantity        int
	FulfillmentType string
}

// CartService 购物车服务
type CartService struct {
	cartRepo       repository.CartRepository
	productRepo    repository.ProductRepository
	productSKURepo repository.ProductSKURepository
	promotionRepo  repository.PromotionRepository
	settingService *SettingService
}

// NewCartService 创建购物车服务
func NewCartService(cartRepo repository.CartRepository, productRepo repository.ProductRepository, productSKURepo repository.ProductSKURepository, promotionRepo repository.PromotionRepository, settingService *SettingService) *CartService {
	return &CartService{
		cartRepo:       cartRepo,
		productRepo:    productRepo,
		productSKURepo: productSKURepo,
		promotionRepo:  promotionRepo,
		settingService: settingService,
	}
}

// ListByUser 获取用户购物车
func (s *CartService) ListByUser(userID uint) ([]CartItemDetail, error) {
	if userID == 0 {
		return nil, ErrInvalidOrderItem
	}
	items, err := s.cartRepo.ListByUser(userID)
	if err != nil {
		return nil, err
	}
	currency := s.resolveSiteCurrency()
	details := make([]CartItemDetail, 0, len(items))
	promotionService := NewPromotionService(s.promotionRepo)
	for _, item := range items {
		product := item.Product
		if product == nil || product.ID == 0 {
			p, err := s.productRepo.GetByID(strconv.FormatUint(uint64(item.ProductID), 10))
			if err != nil {
				return nil, err
			}
			product = p
		}
		if product == nil || !product.IsActive {
			_ = s.cartRepo.DeleteByUserProductSKU(userID, item.ProductID, item.SKUID)
			continue
		}

		sku := item.SKU
		if sku == nil || sku.ID == 0 {
			resolvedSKU, resolveErr := s.resolveOrderSKU(product, item.SKUID)
			if resolveErr != nil {
				_ = s.cartRepo.DeleteByUserProductSKU(userID, item.ProductID, item.SKUID)
				continue
			}
			sku = resolvedSKU
		}

		if sku == nil || !sku.IsActive {
			_ = s.cartRepo.DeleteByUserProductSKU(userID, item.ProductID, item.SKUID)
			continue
		}
		if strings.TrimSpace(product.FulfillmentType) == constants.FulfillmentTypeManual &&
			shouldEnforceManualSKUStock(product, sku) &&
			manualSKUAvailable(sku) <= 0 {
			_ = s.cartRepo.DeleteByUserProductSKU(userID, item.ProductID, item.SKUID)
			continue
		}

		priceCarrier := *product
		priceCarrier.PriceAmount = sku.PriceAmount
		unitPrice := sku.PriceAmount
		if promotionService != nil {
			_, discounted, err := promotionService.ApplyPromotion(&priceCarrier, item.Quantity)
			if err != nil {
				return nil, err
			}
			unitPrice = discounted
		}

		fulfillmentType := strings.TrimSpace(product.FulfillmentType)
		if fulfillmentType == "" {
			fulfillmentType = constants.FulfillmentTypeManual
		}

		details = append(details, CartItemDetail{
			ProductID:       item.ProductID,
			SKUID:           sku.ID,
			Quantity:        item.Quantity,
			FulfillmentType: fulfillmentType,
			UnitPrice:       unitPrice,
			OriginalPrice:   sku.PriceAmount,
			Currency:        currency,
			Product:         product,
			SKU:             sku,
		})
	}
	return details, nil
}

// UpsertItem 添加或更新购物车项
func (s *CartService) UpsertItem(input UpsertCartItemInput) error {
	if input.UserID == 0 || input.ProductID == 0 || input.Quantity <= 0 {
		return ErrInvalidOrderItem
	}
	product, err := s.productRepo.GetByID(strconv.FormatUint(uint64(input.ProductID), 10))
	if err != nil {
		return err
	}
	if product == nil || !product.IsActive {
		return ErrProductNotAvailable
	}
	if err := validateProductPurchaseQuantity(product, input.Quantity); err != nil {
		return err
	}
	sku, err := s.resolveOrderSKU(product, input.SKUID)
	if err != nil {
		return err
	}

	fulfillmentType := strings.TrimSpace(product.FulfillmentType)
	if fulfillmentType == "" {
		fulfillmentType = constants.FulfillmentTypeManual
	}
	if fulfillmentType != constants.FulfillmentTypeManual && fulfillmentType != constants.FulfillmentTypeAuto {
		return ErrFulfillmentInvalid
	}
	if fulfillmentType == constants.FulfillmentTypeManual &&
		shouldEnforceManualSKUStock(product, sku) &&
		manualSKUAvailable(sku) < input.Quantity {
		return ErrManualStockInsufficient
	}

	now := time.Now()
	item := &models.CartItem{
		UserID:          input.UserID,
		ProductID:       input.ProductID,
		SKUID:           sku.ID,
		Quantity:        input.Quantity,
		FulfillmentType: fulfillmentType,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return s.cartRepo.Upsert(item)
}

// RemoveItem 删除购物车项
func (s *CartService) RemoveItem(userID, productID, skuID uint) error {
	if userID == 0 || productID == 0 {
		return ErrInvalidOrderItem
	}
	return s.cartRepo.DeleteByUserProductSKU(userID, productID, skuID)
}

func (s *CartService) resolveSiteCurrency() string {
	if s == nil || s.settingService == nil {
		return constants.SiteCurrencyDefault
	}
	currency, err := s.settingService.GetSiteCurrency(constants.SiteCurrencyDefault)
	if err != nil {
		return constants.SiteCurrencyDefault
	}
	return normalizeSiteCurrency(currency)
}

func (s *CartService) resolveOrderSKU(product *models.Product, rawSKUID uint) (*models.ProductSKU, error) {
	if product == nil || product.ID == 0 {
		return nil, ErrProductNotAvailable
	}
	if s.productSKURepo == nil {
		return nil, ErrProductSKUInvalid
	}

	if rawSKUID > 0 {
		sku, err := s.productSKURepo.GetByID(rawSKUID)
		if err != nil {
			return nil, err
		}
		if sku == nil || sku.ProductID != product.ID || !sku.IsActive {
			return nil, ErrProductSKUInvalid
		}
		return sku, nil
	}

	// 兼容窗口：仅当商品只有一个启用 SKU 时允许缺省 sku_id 自动回退。
	activeSKUs, err := s.productSKURepo.ListByProduct(product.ID, true)
	if err != nil {
		return nil, err
	}
	if len(activeSKUs) == 1 {
		return &activeSKUs[0], nil
	}
	if len(activeSKUs) == 0 {
		return nil, ErrProductSKUInvalid
	}
	return nil, ErrProductSKURequired
}

func buildOrderItemKey(productID, skuID uint) string {
	return fmt.Sprintf("%d:%d", productID, skuID)
}
