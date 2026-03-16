package service

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func newSyncSingleSKURepo(t *testing.T) repository.ProductSKURepository {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.ProductSKU{}); err != nil {
		t.Fatalf("auto migrate product sku failed: %v", err)
	}
	return repository.NewProductSKURepository(db)
}

func TestSyncSingleProductSKU_MultipleRowsKeepsSingleActive(t *testing.T) {
	repo := newSyncSingleSKURepo(t)
	productID := uint(2001)

	inactiveDefault := models.ProductSKU{
		ProductID:        productID,
		SKUCode:          models.DefaultSKUCode,
		PriceAmount:      models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		ManualStockTotal: 9,
		IsActive:         false,
		SortOrder:        0,
	}
	firstActive := models.ProductSKU{
		ProductID:        productID,
		SKUCode:          "A",
		PriceAmount:      models.NewMoneyFromDecimal(decimal.NewFromInt(20)),
		ManualStockTotal: 2,
		IsActive:         true,
		SortOrder:        2,
	}
	secondActive := models.ProductSKU{
		ProductID:        productID,
		SKUCode:          "B",
		PriceAmount:      models.NewMoneyFromDecimal(decimal.NewFromInt(30)),
		ManualStockTotal: 4,
		IsActive:         true,
		SortOrder:        1,
	}
	if err := repo.Create(&inactiveDefault); err != nil {
		t.Fatalf("create inactive default sku failed: %v", err)
	}
	inactiveDefault.IsActive = false
	if err := repo.Update(&inactiveDefault); err != nil {
		t.Fatalf("update inactive default sku failed: %v", err)
	}
	if err := repo.Create(&firstActive); err != nil {
		t.Fatalf("create first active sku failed: %v", err)
	}
	if err := repo.Create(&secondActive); err != nil {
		t.Fatalf("create second active sku failed: %v", err)
	}

	targetPrice := decimal.RequireFromString("88.88")
	if err := syncSingleProductSKU(repo, productID, targetPrice, 5, true); err != nil {
		t.Fatalf("sync single sku failed: %v", err)
	}

	skus, err := repo.ListByProduct(productID, false)
	if err != nil {
		t.Fatalf("list sku failed: %v", err)
	}

	activeCount := 0
	for _, sku := range skus {
		if !sku.IsActive {
			continue
		}
		activeCount++
		if sku.ID != firstActive.ID {
			t.Fatalf("expected first active sku id=%d, got id=%d", firstActive.ID, sku.ID)
		}
		if !sku.PriceAmount.Equal(targetPrice) {
			t.Fatalf("expected price %s, got %s", targetPrice.StringFixed(2), sku.PriceAmount.String())
		}
		if sku.ManualStockTotal != 5 {
			t.Fatalf("expected manual stock total 5, got %d", sku.ManualStockTotal)
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active sku, got %d", activeCount)
	}
}

func TestSyncSingleProductSKU_NoActivePrefersDefaultCode(t *testing.T) {
	repo := newSyncSingleSKURepo(t)
	productID := uint(2002)

	inactiveA := models.ProductSKU{
		ProductID:        productID,
		SKUCode:          "A",
		PriceAmount:      models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		ManualStockTotal: 3,
		IsActive:         false,
		SortOrder:        1,
	}
	inactiveDefault := models.ProductSKU{
		ProductID:        productID,
		SKUCode:          models.DefaultSKUCode,
		PriceAmount:      models.NewMoneyFromDecimal(decimal.NewFromInt(20)),
		ManualStockTotal: 8,
		IsActive:         false,
		SortOrder:        0,
	}
	if err := repo.Create(&inactiveA); err != nil {
		t.Fatalf("create inactive sku A failed: %v", err)
	}
	inactiveA.IsActive = false
	if err := repo.Update(&inactiveA); err != nil {
		t.Fatalf("update inactive sku A failed: %v", err)
	}
	if err := repo.Create(&inactiveDefault); err != nil {
		t.Fatalf("create inactive default sku failed: %v", err)
	}
	inactiveDefault.IsActive = false
	if err := repo.Update(&inactiveDefault); err != nil {
		t.Fatalf("update inactive default sku failed: %v", err)
	}

	targetPrice := decimal.RequireFromString("19.90")
	if err := syncSingleProductSKU(repo, productID, targetPrice, 6, true); err != nil {
		t.Fatalf("sync single sku failed: %v", err)
	}

	skus, err := repo.ListByProduct(productID, false)
	if err != nil {
		t.Fatalf("list sku failed: %v", err)
	}

	activeCount := 0
	for _, sku := range skus {
		if !sku.IsActive {
			continue
		}
		activeCount++
		if sku.ID != inactiveDefault.ID {
			t.Fatalf("expected default sku id=%d to be active, got id=%d", inactiveDefault.ID, sku.ID)
		}
		if !sku.PriceAmount.Equal(targetPrice) {
			t.Fatalf("expected price %s, got %s", targetPrice.StringFixed(2), sku.PriceAmount.String())
		}
		if sku.ManualStockTotal != 6 {
			t.Fatalf("expected manual stock total 6, got %d", sku.ManualStockTotal)
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active sku, got %d", activeCount)
	}
}

func TestApplyAutoStockCounts_LegacyStockPrefersDefaultSKU(t *testing.T) {
	svc, db := newAutoStockProductService(t)
	productID := uint(3001)
	defaultSKUID := uint(101)
	otherSKUID := uint(102)

	insertCardSecrets(t, db, productID, 0, models.CardSecretStatusAvailable, 2)
	insertCardSecrets(t, db, productID, 0, models.CardSecretStatusReserved, 1)
	insertCardSecrets(t, db, productID, 0, models.CardSecretStatusUsed, 1)
	insertCardSecrets(t, db, productID, defaultSKUID, models.CardSecretStatusAvailable, 3)
	insertCardSecrets(t, db, productID, otherSKUID, models.CardSecretStatusAvailable, 4)
	counts, err := svc.cardSecretRepo.CountStockByProductIDs([]uint{productID})
	if err != nil {
		t.Fatalf("count stock by product ids failed: %v", err)
	}
	if len(counts) != 5 {
		t.Fatalf("expected 5 grouped stock rows, got %d", len(counts))
	}
	bySKUAndStatus := make(map[uint]map[string]int64)
	for _, row := range counts {
		if bySKUAndStatus[row.SKUID] == nil {
			bySKUAndStatus[row.SKUID] = make(map[string]int64)
		}
		bySKUAndStatus[row.SKUID][row.Status] = row.Total
	}
	if bySKUAndStatus[0][models.CardSecretStatusAvailable] != 2 ||
		bySKUAndStatus[0][models.CardSecretStatusReserved] != 1 ||
		bySKUAndStatus[0][models.CardSecretStatusUsed] != 1 {
		t.Fatalf("unexpected legacy sku(0) rows: %+v", bySKUAndStatus[0])
	}
	if bySKUAndStatus[defaultSKUID][models.CardSecretStatusAvailable] != 3 {
		t.Fatalf("unexpected default sku rows: %+v", bySKUAndStatus[defaultSKUID])
	}
	if bySKUAndStatus[otherSKUID][models.CardSecretStatusAvailable] != 4 {
		t.Fatalf("unexpected other sku rows: %+v", bySKUAndStatus[otherSKUID])
	}

	products := []models.Product{
		{
			ID:              productID,
			FulfillmentType: constants.FulfillmentTypeAuto,
			SKUs: []models.ProductSKU{
				{
					ID:       otherSKUID,
					SKUCode:  "B",
					IsActive: true,
				},
				{
					ID:       defaultSKUID,
					SKUCode:  models.DefaultSKUCode,
					IsActive: true,
				},
			},
		},
	}

	if err := svc.ApplyAutoStockCounts(products); err != nil {
		t.Fatalf("apply auto stock counts failed: %v", err)
	}

	got := products[0]
	if got.AutoStockAvailable != 9 {
		t.Fatalf("expected product auto available=9, got %d", got.AutoStockAvailable)
	}
	if got.AutoStockLocked != 1 {
		t.Fatalf("expected product auto locked=1, got %d", got.AutoStockLocked)
	}
	if got.AutoStockSold != 1 {
		t.Fatalf("expected product auto sold=1, got %d", got.AutoStockSold)
	}
	if got.AutoStockTotal != 10 {
		t.Fatalf("expected product auto total=10, got %d", got.AutoStockTotal)
	}

	if got.SKUs[0].AutoStockAvailable != 4 {
		t.Fatalf("expected other sku auto available=4, got %d", got.SKUs[0].AutoStockAvailable)
	}
	if got.SKUs[0].AutoStockLocked != 0 || got.SKUs[0].AutoStockSold != 0 {
		t.Fatalf("expected other sku locked/sold to remain 0, got locked=%d sold=%d", got.SKUs[0].AutoStockLocked, got.SKUs[0].AutoStockSold)
	}

	if got.SKUs[1].AutoStockAvailable != 5 {
		t.Fatalf("expected default sku auto available=5, got %d", got.SKUs[1].AutoStockAvailable)
	}
	if got.SKUs[1].AutoStockLocked != 1 {
		t.Fatalf("expected default sku auto locked=1, got %d", got.SKUs[1].AutoStockLocked)
	}
	if got.SKUs[1].AutoStockSold != 1 {
		t.Fatalf("expected default sku auto sold=1, got %d", got.SKUs[1].AutoStockSold)
	}
}

func newAutoStockProductService(t *testing.T) (*ProductService, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:product_auto_stock_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.CardSecret{}); err != nil {
		t.Fatalf("auto migrate card secret failed: %v", err)
	}
	secretRepo := repository.NewCardSecretRepository(db)
	return NewProductService(nil, nil, secretRepo, nil), db
}

func insertCardSecrets(t *testing.T, db *gorm.DB, productID, skuID uint, status string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		row := models.CardSecret{
			ProductID: productID,
			SKUID:     skuID,
			Secret:    fmt.Sprintf("secret-%d-%d-%s-%d", productID, skuID, status, i),
			Status:    status,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create card secret failed: %v", err)
		}
	}
}

func TestProductServiceListPublicIncludesChildProductsForParentCategory(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	parent := models.Category{
		Slug:     "games",
		NameJSON: models.JSON{"zh-CN": "games"},
	}
	child := models.Category{
		ParentID: 1,
		Slug:     "steam",
		NameJSON: models.JSON{"zh-CN": "steam"},
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent category failed: %v", err)
	}
	child.ParentID = parent.ID
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child category failed: %v", err)
	}

	parentProduct := models.Product{
		CategoryID:  parent.ID,
		Slug:        "parent-product",
		TitleJSON:   models.JSON{"zh-CN": "parent-product"},
		PriceAmount: models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
	}
	childProduct := models.Product{
		CategoryID:  child.ID,
		Slug:        "child-product",
		TitleJSON:   models.JSON{"zh-CN": "child-product"},
		PriceAmount: models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
	}
	if err := db.Create(&parentProduct).Error; err != nil {
		t.Fatalf("create parent product failed: %v", err)
	}
	if err := db.Create(&childProduct).Error; err != nil {
		t.Fatalf("create child product failed: %v", err)
	}

	products, total, err := svc.ListPublic(strconv.FormatUint(uint64(parent.ID), 10), "", 1, 20)
	if err != nil {
		t.Fatalf("list public products failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}
}

func TestProductServiceCreateRejectsParentCategoryWithChildren(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	parent := models.Category{
		Slug:     "games",
		NameJSON: models.JSON{"zh-CN": "games"},
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent category failed: %v", err)
	}
	child := models.Category{
		ParentID: parent.ID,
		Slug:     "steam",
		NameJSON: models.JSON{"zh-CN": "steam"},
	}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child category failed: %v", err)
	}

	_, err := svc.Create(CreateProductInput{
		CategoryID:      parent.ID,
		Slug:            "invalid-parent-product",
		TitleJSON:       map[string]interface{}{"zh-CN": "invalid-parent-product"},
		PriceAmount:     decimal.NewFromInt(10),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		ManualStockTotal: func() *int {
			value := 1
			return &value
		}(),
	})
	if err != ErrProductCategoryInvalid {
		t.Fatalf("expected ErrProductCategoryInvalid, got %v", err)
	}
}

func newProductServiceForTest(t *testing.T) (*ProductService, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:product_service_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.Category{}, &models.Product{}, &models.ProductSKU{}); err != nil {
		t.Fatalf("auto migrate product service tables failed: %v", err)
	}

	return NewProductService(
		repository.NewProductRepository(db),
		repository.NewProductSKURepository(db),
		nil,
		repository.NewCategoryRepository(db),
	), db
}
