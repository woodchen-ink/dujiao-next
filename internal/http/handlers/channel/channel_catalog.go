package channel

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"

	"github.com/gin-gonic/gin"
)

// GetCategories GET /api/v1/channel/catalog/categories?locale=zh-CN
func (h *Handler) GetCategories(c *gin.Context) {
	locale := c.DefaultQuery("locale", "zh-CN")
	defaultLocale := "zh-CN"

	categories, err := h.CategoryService.List()
	if err != nil {
		logger.Errorw("channel_catalog_list_categories", "error", err)
		respondChannelError(c, 500, 500, "internal_error", "error.internal_error", err)
		return
	}

	type categoryItem struct {
		ID           uint   `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		Slug         string `json:"slug"`
		ProductCount int64  `json:"product_count"`
	}

	var items []categoryItem
	for _, cat := range categories {
		count, err := h.CategoryRepo.CountActiveProducts(fmt.Sprintf("%d", cat.ID))
		if err != nil {
			logger.Warnw("channel_catalog_count_products", "category_id", cat.ID, "error", err)
			count = 0
		}
		if count == 0 {
			continue // 跳过无上架商品的分类
		}
		items = append(items, categoryItem{
			ID:           cat.ID,
			Name:         resolveLocalizedJSON(cat.NameJSON, locale, defaultLocale),
			Icon:         cat.Icon,
			Slug:         cat.Slug,
			ProductCount: count,
		})
	}

	respondChannelSuccess(c, gin.H{"items": items})
}

// GetProducts GET /api/v1/channel/catalog/products?locale=zh-CN&category_id=1&page=1&page_size=5
func (h *Handler) GetProducts(c *gin.Context) {
	locale := c.DefaultQuery("locale", "zh-CN")
	defaultLocale := "zh-CN"
	categoryID := c.DefaultQuery("category_id", "")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "5"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 20 {
		pageSize = 5
	}

	products, total, err := h.ProductService.ListPublic(categoryID, "", page, pageSize)
	if err != nil {
		logger.Errorw("channel_catalog_list_products", "error", err)
		respondChannelError(c, 500, 500, "internal_error", "error.internal_error", err)
		return
	}

	if err := h.ProductService.ApplyAutoStockCounts(products); err != nil {
		logger.Warnw("channel_catalog_apply_stock", "error", err)
	}

	currency, err := h.SettingService.GetSiteCurrency("CNY")
	if err != nil {
		logger.Warnw("channel_catalog_get_currency", "error", err)
		currency = "CNY"
	}

	type productItem struct {
		ID           uint   `json:"id"`
		Title        string `json:"title"`
		Summary      string `json:"summary"`
		ImageURL     string `json:"image_url"`
		PriceFrom    string `json:"price_from"`
		Currency     string `json:"currency"`
		StockStatus  string `json:"stock_status"`
		StockCount   int64  `json:"stock_count"`
		CategoryName string `json:"category_name"`
	}

	items := make([]productItem, 0, len(products))
	for _, p := range products {
		title := resolveLocalizedJSON(p.TitleJSON, locale, defaultLocale)
		desc := resolveLocalizedJSON(p.DescriptionJSON, locale, defaultLocale)
		summary := truncate(stripHTML(desc), 100)

		var imageURL string
		if len(p.Images) > 0 {
			imageURL = string(p.Images[0])
		}

		items = append(items, productItem{
			ID:           p.ID,
			Title:        title,
			Summary:      summary,
			ImageURL:     imageURL,
			PriceFrom:    p.PriceAmount.String(),
			Currency:     currency,
			StockStatus:  computeStockStatus(p.FulfillmentType, p.AutoStockAvailable, p.ManualStockTotal),
			StockCount:   computeStockCount(p.FulfillmentType, p.AutoStockAvailable, p.ManualStockTotal),
			CategoryName: resolveLocalizedJSON(p.Category.NameJSON, locale, defaultLocale),
		})
	}

	totalPages := int64(math.Ceil(float64(total) / float64(pageSize)))

	respondChannelSuccess(c, gin.H{
		"items":      items,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": totalPages,
	})
}

// GetProductDetail GET /api/v1/channel/catalog/products/:id?locale=zh-CN
func (h *Handler) GetProductDetail(c *gin.Context) {
	locale := c.DefaultQuery("locale", "zh-CN")
	defaultLocale := "zh-CN"
	id := c.Param("id")

	product, err := h.ProductRepo.GetByID(id)
	if err != nil {
		logger.Errorw("channel_catalog_get_product", "id", id, "error", err)
		respondChannelError(c, 500, 500, "internal_error", "error.internal_error", err)
		return
	}
	if product == nil || !product.IsActive {
		respondChannelError(c, 404, 404, "product_not_found", "error.product_not_found", nil)
		return
	}

	// 计算库存（ApplyAutoStockCounts 接受 []models.Product 并修改 slice 元素）
	stockSlice := []models.Product{*product}
	if err := h.ProductService.ApplyAutoStockCounts(stockSlice); err != nil {
		logger.Warnw("channel_catalog_apply_stock_detail", "error", err)
	}
	*product = stockSlice[0]

	currency, err := h.SettingService.GetSiteCurrency("CNY")
	if err != nil {
		logger.Warnw("channel_catalog_get_currency_detail", "error", err)
		currency = "CNY"
	}

	title := resolveLocalizedJSON(product.TitleJSON, locale, defaultLocale)
	description := stripHTML(resolveLocalizedJSON(product.ContentJSON, locale, defaultLocale))

	var imageURL string
	if len(product.Images) > 0 {
		imageURL = string(product.Images[0])
	}

	type skuItem struct {
		ID          uint   `json:"id"`
		SKUCode     string `json:"sku_code"`
		SpecValues  string `json:"spec_values"`
		Price       string `json:"price"`
		StockStatus string `json:"stock_status"`
		StockCount  int64  `json:"stock_count"`
	}

	skus := make([]skuItem, 0, len(product.SKUs))
	for _, sku := range product.SKUs {
		if !sku.IsActive {
			continue
		}
		specValues := resolveLocalizedJSON(sku.SpecValuesJSON, locale, defaultLocale)
		skus = append(skus, skuItem{
			ID:          sku.ID,
			SKUCode:     sku.SKUCode,
			SpecValues:  specValues,
			Price:       sku.PriceAmount.String(),
			StockStatus: computeStockStatus(product.FulfillmentType, sku.AutoStockAvailable, sku.ManualStockTotal),
			StockCount:  computeStockCount(product.FulfillmentType, sku.AutoStockAvailable, sku.ManualStockTotal),
		})
	}

	respondChannelSuccess(c, gin.H{
		"id":                    product.ID,
		"title":                 title,
		"description":           description,
		"image_url":             imageURL,
		"price_from":            product.PriceAmount.String(),
		"currency":              currency,
		"stock_status":          computeStockStatus(product.FulfillmentType, product.AutoStockAvailable, product.ManualStockTotal),
		"stock_count":           computeStockCount(product.FulfillmentType, product.AutoStockAvailable, product.ManualStockTotal),
		"category_name":         resolveLocalizedJSON(product.Category.NameJSON, locale, defaultLocale),
		"fulfillment_type":      product.FulfillmentType,
		"max_purchase_quantity": normalizeChannelMaxPurchaseQuantity(product.MaxPurchaseQuantity),
		"manual_form_schema":    normalizeChannelManualFormSchema(product.ManualFormSchemaJSON, locale, defaultLocale),
		"purchase_note":         "",
		"skus":                  skus,
	})
}

func normalizeChannelManualFormSchema(schema models.JSON, locale, defaultLocale string) gin.H {
	fieldsRaw, ok := schema["fields"]
	if !ok {
		return gin.H{"fields": []gin.H{}}
	}

	fieldList, ok := fieldsRaw.([]interface{})
	if !ok {
		return gin.H{"fields": []gin.H{}}
	}

	fields := make([]gin.H, 0, len(fieldList))
	for _, rawField := range fieldList {
		fieldMap, ok := rawField.(map[string]interface{})
		if !ok {
			continue
		}

		field := gin.H{}
		if key, ok := fieldMap["key"].(string); ok {
			field["key"] = key
		}
		if typeValue, ok := fieldMap["type"].(string); ok {
			field["type"] = typeValue
		}
		if required, ok := fieldMap["required"].(bool); ok {
			field["required"] = required
		}
		if label := localizedFieldText(fieldMap["label"], locale, defaultLocale); label != "" {
			field["label"] = label
		}
		if placeholder := localizedFieldText(fieldMap["placeholder"], locale, defaultLocale); placeholder != "" {
			field["placeholder"] = placeholder
		}
		if regex, ok := fieldMap["regex"].(string); ok && strings.TrimSpace(regex) != "" {
			field["regex"] = regex
		}
		if minValue, ok := fieldMap["min"]; ok {
			field["min"] = minValue
		}
		if maxValue, ok := fieldMap["max"]; ok {
			field["max"] = maxValue
		}
		if maxLen, ok := fieldMap["max_len"]; ok {
			field["max_len"] = maxLen
		}
		if options, ok := fieldMap["options"].([]string); ok {
			field["options"] = options
		} else if optionsRaw, ok := fieldMap["options"].([]interface{}); ok {
			options := make([]string, 0, len(optionsRaw))
			for _, rawOption := range optionsRaw {
				option := strings.TrimSpace(fmt.Sprintf("%v", rawOption))
				if option == "" || option == "<nil>" {
					continue
				}
				options = append(options, option)
			}
			if len(options) > 0 {
				field["options"] = options
			}
		}

		fields = append(fields, field)
	}

	return gin.H{"fields": fields}
}

func localizedFieldText(raw interface{}, locale, defaultLocale string) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case models.JSON:
		return strings.TrimSpace(resolveLocalizedJSON(value, locale, defaultLocale))
	case map[string]interface{}:
		return strings.TrimSpace(resolveLocalizedJSON(models.JSON(value), locale, defaultLocale))
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

// computeStockCount 计算可用库存数量（-1 表示无限库存）
func computeStockCount(fulfillmentType string, autoStockAvailable int64, manualStockTotal int) int64 {
	if fulfillmentType == "auto" {
		return autoStockAvailable
	}
	// manual: -1 表示无限库存
	return int64(manualStockTotal)
}

func normalizeChannelMaxPurchaseQuantity(value int) int {
	if value <= 0 {
		return 0
	}
	return value
}

// computeStockStatus 计算库存状态
func computeStockStatus(fulfillmentType string, autoStockAvailable int64, manualStockTotal int) string {
	if fulfillmentType == "auto" {
		if autoStockAvailable > 0 {
			return "in_stock"
		}
		return "out_of_stock"
	}
	// manual: -1 表示无限库存
	if manualStockTotal < 0 {
		return "in_stock"
	}
	if manualStockTotal > 0 {
		return "in_stock"
	}
	return "out_of_stock"
}
