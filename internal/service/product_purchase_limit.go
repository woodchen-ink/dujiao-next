package service

import "github.com/dujiao-next/internal/models"

// normalizeMaxPurchaseQuantity 归一化商品单次购买数量上限。
func normalizeMaxPurchaseQuantity(value int) int {
	if value <= 0 {
		return 0
	}
	return value
}

// productMaxPurchaseQuantity 返回商品当前有效的单次购买上限。
func productMaxPurchaseQuantity(product *models.Product) int {
	if product == nil {
		return 0
	}
	return normalizeMaxPurchaseQuantity(product.MaxPurchaseQuantity)
}

// validateProductPurchaseQuantity 校验单次购买数量是否超出商品限制。
func validateProductPurchaseQuantity(product *models.Product, quantity int) error {
	if quantity <= 0 {
		return ErrInvalidOrderItem
	}
	limit := productMaxPurchaseQuantity(product)
	if limit > 0 && quantity > limit {
		return ErrProductMaxPurchaseExceeded
	}
	return nil
}
