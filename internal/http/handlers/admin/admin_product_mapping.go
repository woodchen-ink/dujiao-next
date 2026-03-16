package admin

import (
	"errors"
	"strconv"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/repository"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// GetProductMappings 获取商品映射列表
func (h *Handler) GetProductMappings(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = shared.NormalizePagination(page, pageSize)

	connectionID, _ := shared.ParseQueryUint(c.Query("connection_id"), false)

	mappings, total, err := h.ProductMappingService.List(repository.ProductMappingListFilter{
		ConnectionID: connectionID,
		Pagination: repository.Pagination{
			Page:     page,
			PageSize: pageSize,
		},
	})
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.mapping_fetch_failed", err)
		return
	}

	pagination := response.BuildPagination(page, pageSize, total)
	response.SuccessWithPage(c, mappings, pagination)
}

// GetProductMapping 获取商品映射详情
func (h *Handler) GetProductMapping(c *gin.Context) {
	id, err := shared.ParseParamUint(c, "id")
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}

	mapping, err := h.ProductMappingService.GetByID(id)
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.mapping_fetch_failed", err)
		return
	}
	if mapping == nil {
		shared.RespondError(c, response.CodeNotFound, "error.mapping_not_found", nil)
		return
	}

	// 同时返回 SKU 映射
	skuMappings, err := h.ProductMappingService.GetSKUMappings(mapping.ID)
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.mapping_fetch_failed", err)
		return
	}

	response.Success(c, gin.H{
		"mapping":      mapping,
		"sku_mappings": skuMappings,
	})
}

// ImportUpstreamProductRequest 导入上游商品请求
type ImportUpstreamProductRequest struct {
	ConnectionID      uint   `json:"connection_id" binding:"required"`
	UpstreamProductID uint   `json:"upstream_product_id" binding:"required"`
	CategoryID        uint   `json:"category_id"`
	Slug              string `json:"slug"`
}

// ImportUpstreamProduct 导入上游商品
func (h *Handler) ImportUpstreamProduct(c *gin.Context) {
	var req ImportUpstreamProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	mapping, err := h.ProductMappingService.ImportUpstreamProduct(
		req.ConnectionID,
		req.UpstreamProductID,
		req.CategoryID,
		req.Slug,
	)
	if err != nil {
		if errors.Is(err, service.ErrMappingAlreadyExists) {
			shared.RespondError(c, response.CodeBadRequest, "error.mapping_already_exists", nil)
			return
		}
		if errors.Is(err, service.ErrConnectionNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.connection_not_found", nil)
			return
		}
		if errors.Is(err, service.ErrUpstreamProductNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.upstream_product_not_found", nil)
			return
		}
		if errors.Is(err, service.ErrSlugExists) {
			shared.RespondError(c, response.CodeBadRequest, "error.slug_exists", nil)
			return
		}
		if errors.Is(err, service.ErrProductCategoryInvalid) {
			shared.RespondError(c, response.CodeBadRequest, "error.product_category_invalid", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.mapping_import_failed", err)
		return
	}

	response.Success(c, mapping)
}

// BatchImportUpstreamProductRequest 批量导入上游商品请求
type BatchImportUpstreamProductRequest struct {
	ConnectionID       uint   `json:"connection_id" binding:"required"`
	UpstreamProductIDs []uint `json:"upstream_product_ids" binding:"required,min=1"`
	CategoryID         uint   `json:"category_id"`
}

// BatchImportUpstreamProductResult 单个商品导入结果
type BatchImportUpstreamProductResult struct {
	UpstreamProductID uint   `json:"upstream_product_id"`
	Success           bool   `json:"success"`
	Error             string `json:"error,omitempty"`
}

// BatchImportUpstreamProducts 批量导入上游商品
func (h *Handler) BatchImportUpstreamProducts(c *gin.Context) {
	var req BatchImportUpstreamProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	results := make([]BatchImportUpstreamProductResult, len(req.UpstreamProductIDs))
	successCount := 0

	for i, upstreamProductID := range req.UpstreamProductIDs {
		result := BatchImportUpstreamProductResult{
			UpstreamProductID: upstreamProductID,
		}
		_, err := h.ProductMappingService.ImportUpstreamProduct(
			req.ConnectionID,
			upstreamProductID,
			req.CategoryID,
			"", // auto-generate slug
		)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Success = true
			successCount++
		}
		results[i] = result
	}

	response.Success(c, gin.H{
		"results":       results,
		"total":         len(req.UpstreamProductIDs),
		"success_count": successCount,
	})
}

// SyncProductMapping 同步商品映射
func (h *Handler) SyncProductMapping(c *gin.Context) {
	id, err := shared.ParseParamUint(c, "id")
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}

	if err := h.ProductMappingService.SyncProduct(id); err != nil {
		if errors.Is(err, service.ErrMappingNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.mapping_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.mapping_sync_failed", err)
		return
	}

	response.Success(c, gin.H{"synced": true})
}

// UpdateProductMappingStatusRequest 更新映射状态请求
type UpdateProductMappingStatusRequest struct {
	IsActive bool `json:"is_active"`
}

// UpdateProductMappingStatus 启用/禁用映射
func (h *Handler) UpdateProductMappingStatus(c *gin.Context) {
	id, err := shared.ParseParamUint(c, "id")
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}

	var req UpdateProductMappingStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	if err := h.ProductMappingService.SetActive(id, req.IsActive); err != nil {
		if errors.Is(err, service.ErrMappingNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.mapping_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.mapping_update_failed", err)
		return
	}

	response.Success(c, gin.H{"updated": true})
}

// DeleteProductMapping 删除映射
func (h *Handler) DeleteProductMapping(c *gin.Context) {
	id, err := shared.ParseParamUint(c, "id")
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}

	if err := h.ProductMappingService.Delete(id); err != nil {
		if errors.Is(err, service.ErrMappingNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.mapping_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.mapping_delete_failed", err)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

// ListUpstreamProducts 代理拉取上游商品列表
func (h *Handler) ListUpstreamProducts(c *gin.Context) {
	connectionID, err := shared.ParseQueryUint(c.Query("connection_id"), true)
	if err != nil || connectionID == 0 {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = shared.NormalizePagination(page, pageSize)

	result, err := h.ProductMappingService.ListUpstreamProducts(connectionID, page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrConnectionNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.connection_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.upstream_products_fetch_failed", err)
		return
	}

	response.Success(c, result)
}
