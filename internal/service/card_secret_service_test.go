package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func setupCardSecretServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:card_secret_service_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Product{},
		&models.ProductSKU{},
		&models.CardSecretBatch{},
		&models.CardSecret{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	models.DB = db
	return db
}

func TestCreateCardSecretBatchFallbackToDefaultSKU(t *testing.T) {
	db := setupCardSecretServiceTestDB(t)

	product := &models.Product{
		CategoryID:      1,
		Slug:            "card-secret-product-default",
		TitleJSON:       models.JSON{"zh-CN": "卡密商品"},
		PriceAmount:     models.NewMoneyFromDecimal(decimal.NewFromInt(20)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	defaultSKU := &models.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     models.DefaultSKUCode,
		PriceAmount: models.NewMoneyFromDecimal(decimal.NewFromInt(20)),
		IsActive:    true,
	}
	if err := db.Create(defaultSKU).Error; err != nil {
		t.Fatalf("create default sku failed: %v", err)
	}
	otherSKU := &models.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     "PRO",
		PriceAmount: models.NewMoneyFromDecimal(decimal.NewFromInt(30)),
		IsActive:    true,
	}
	if err := db.Create(otherSKU).Error; err != nil {
		t.Fatalf("create other sku failed: %v", err)
	}

	svc := NewCardSecretService(
		repository.NewCardSecretRepository(db),
		repository.NewCardSecretBatchRepository(db),
		repository.NewProductRepository(db),
		repository.NewProductSKURepository(db),
	)

	batch, created, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: product.ID,
		Secrets:   []string{"AAA-001", "AAA-002"},
		Source:    constants.CardSecretSourceManual,
		AdminID:   1,
	})
	if err != nil {
		t.Fatalf("create card secret batch failed: %v", err)
	}
	if created != 2 {
		t.Fatalf("created count want 2 got %d", created)
	}
	if batch.SKUID != defaultSKU.ID {
		t.Fatalf("batch sku_id want default %d got %d", defaultSKU.ID, batch.SKUID)
	}

	var secretRows []models.CardSecret
	if err := db.Where("batch_id = ?", batch.ID).Find(&secretRows).Error; err != nil {
		t.Fatalf("query card secrets failed: %v", err)
	}
	if len(secretRows) != 2 {
		t.Fatalf("secret rows want 2 got %d", len(secretRows))
	}
	for _, row := range secretRows {
		if row.SKUID != defaultSKU.ID {
			t.Fatalf("secret sku_id want default %d got %d", defaultSKU.ID, row.SKUID)
		}
	}
}

func TestCardSecretServiceSupportsBatchTargetOperations(t *testing.T) {
	db := setupCardSecretServiceTestDB(t)

	product := &models.Product{
		CategoryID:      1,
		Slug:            "card-secret-batch-ops",
		TitleJSON:       models.JSON{"zh-CN": "卡密批次商品"},
		PriceAmount:     models.NewMoneyFromDecimal(decimal.NewFromInt(50)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	defaultSKU := &models.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     models.DefaultSKUCode,
		PriceAmount: models.NewMoneyFromDecimal(decimal.NewFromInt(50)),
		IsActive:    true,
	}
	if err := db.Create(defaultSKU).Error; err != nil {
		t.Fatalf("create default sku failed: %v", err)
	}

	secretRepo := repository.NewCardSecretRepository(db)
	svc := NewCardSecretService(
		secretRepo,
		repository.NewCardSecretBatchRepository(db),
		repository.NewProductRepository(db),
		repository.NewProductSKURepository(db),
	)

	batchA, created, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: product.ID,
		Secrets:   []string{"BATCH-A-001", "BATCH-A-002"},
		Source:    constants.CardSecretSourceManual,
	})
	if err != nil {
		t.Fatalf("create batch A failed: %v", err)
	}
	if created != 2 {
		t.Fatalf("batch A created want 2 got %d", created)
	}

	batchB, created, err := svc.CreateCardSecretBatch(CreateCardSecretBatchInput{
		ProductID: product.ID,
		Secrets:   []string{"BATCH-B-001"},
		Source:    constants.CardSecretSourceManual,
	})
	if err != nil {
		t.Fatalf("create batch B failed: %v", err)
	}
	if created != 1 {
		t.Fatalf("batch B created want 1 got %d", created)
	}

	rows, total, err := svc.ListCardSecrets(ListCardSecretInput{
		ProductID: product.ID,
		BatchID:   batchA.ID,
		Page:      1,
		PageSize:  20,
	})
	if err != nil {
		t.Fatalf("list card secrets by batch failed: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("list by batch A want total=2 len=2 got total=%d len=%d", total, len(rows))
	}
	for _, row := range rows {
		if row.BatchID == nil || *row.BatchID != batchA.ID {
			t.Fatalf("expected batch A id %d got %+v", batchA.ID, row.BatchID)
		}
	}

	affected, err := svc.BatchUpdateCardSecretStatus(nil, batchA.ID, models.CardSecretStatusUsed)
	if err != nil {
		t.Fatalf("batch update status by batch id failed: %v", err)
	}
	if affected != 2 {
		t.Fatalf("batch update affected want 2 got %d", affected)
	}

	batchAIDs, err := secretRepo.ListIDsByBatchID(batchA.ID)
	if err != nil {
		t.Fatalf("list batch A ids failed: %v", err)
	}
	batchASecrets, err := secretRepo.ListByIDs(batchAIDs)
	if err != nil {
		t.Fatalf("list batch A secrets failed: %v", err)
	}
	for _, row := range batchASecrets {
		if row.Status != models.CardSecretStatusUsed {
			t.Fatalf("batch A status want used got %s", row.Status)
		}
	}

	batchBIDs, err := secretRepo.ListIDsByBatchID(batchB.ID)
	if err != nil {
		t.Fatalf("list batch B ids failed: %v", err)
	}
	batchBSecrets, err := secretRepo.ListByIDs(batchBIDs)
	if err != nil {
		t.Fatalf("list batch B secrets failed: %v", err)
	}
	if len(batchBSecrets) != 1 || batchBSecrets[0].Status != models.CardSecretStatusAvailable {
		t.Fatalf("batch B status should remain available, got %+v", batchBSecrets)
	}

	content, contentType, err := svc.ExportCardSecrets(nil, batchA.ID, constants.ExportFormatTXT)
	if err != nil {
		t.Fatalf("export batch A secrets failed: %v", err)
	}
	if contentType != "text/plain; charset=utf-8" {
		t.Fatalf("export content type mismatch: %s", contentType)
	}
	exported := string(content)
	if !strings.Contains(exported, "BATCH-A-001") || !strings.Contains(exported, "BATCH-A-002") {
		t.Fatalf("exported content missing batch A secrets: %s", exported)
	}
	if strings.Contains(exported, "BATCH-B-001") {
		t.Fatalf("exported content should not contain batch B secret: %s", exported)
	}

	deleted, err := svc.BatchDeleteCardSecrets(nil, batchB.ID)
	if err != nil {
		t.Fatalf("delete batch B secrets failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("delete batch B affected want 1 got %d", deleted)
	}

	batchBIDs, err = secretRepo.ListIDsByBatchID(batchB.ID)
	if err != nil {
		t.Fatalf("reload batch B ids failed: %v", err)
	}
	if len(batchBIDs) != 0 {
		t.Fatalf("batch B ids want empty got %v", batchBIDs)
	}
}
