package repository

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/models"

	"gorm.io/gorm"
)

// MediaRepository 素材数据访问接口
type MediaRepository interface {
	List(filter MediaListFilter) ([]models.Media, int64, error)
	GetByID(id uint) (*models.Media, error)
	GetByPath(path string) (*models.Media, error)
	Create(media *models.Media) error
	Update(media *models.Media) error
	Delete(id uint) error
	// ReplacePathInAllTables 将所有业务表中引用旧路径的字段替换为新路径
	// 返回每个 table.column 的替换情况，便于前端展示
	ReplacePathInAllTables(oldPath, newPath string) ([]PathReplaceResult, error)
}

// PathReplaceResult 单个表字段的替换结果
type PathReplaceResult struct {
	Table    string `json:"table"`
	Column   string `json:"column"`
	Affected int64  `json:"affected"`
	Error    string `json:"error,omitempty"`
}

// GormMediaRepository GORM 实现
type GormMediaRepository struct {
	db *gorm.DB
}

// NewMediaRepository 创建素材仓库
func NewMediaRepository(db *gorm.DB) *GormMediaRepository {
	return &GormMediaRepository{db: db}
}

// List 素材列表
func (r *GormMediaRepository) List(filter MediaListFilter) ([]models.Media, int64, error) {
	var items []models.Media
	query := r.db.Model(&models.Media{})

	if filter.Scene != "" {
		query = query.Where("scene = ?", filter.Scene)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		likeOp := determineLikeOp(r.db)
		query = query.Where("name "+likeOp+" ? OR filename "+likeOp+" ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = applyPagination(query, filter.Page, filter.PageSize)

	if err := query.Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// GetByID 根据 ID 获取素材
func (r *GormMediaRepository) GetByID(id uint) (*models.Media, error) {
	var media models.Media
	if err := r.db.First(&media, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &media, nil
}

// GetByPath 根据路径获取素材
func (r *GormMediaRepository) GetByPath(path string) (*models.Media, error) {
	var media models.Media
	if err := r.db.Where("path = ?", path).First(&media).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &media, nil
}

// Create 创建素材记录
func (r *GormMediaRepository) Create(media *models.Media) error {
	return r.db.Create(media).Error
}

// Update 更新素材记录
func (r *GormMediaRepository) Update(media *models.Media) error {
	return r.db.Save(media).Error
}

// Delete 软删除素材记录
func (r *GormMediaRepository) Delete(id uint) error {
	return r.db.Delete(&models.Media{}, id).Error
}

// ReplacePathInAllTables 将所有业务表中引用旧路径的字段全部替换为新路径
// 覆盖：products.images、products.content、posts.thumbnail、posts.content、
//       banners.image、banners.mobile_image、categories.icon
// JSON 列在 PostgreSQL 下需要先转 text 再 REPLACE，SQLite 可直接操作。
func (r *GormMediaRepository) ReplacePathInAllTables(oldPath, newPath string) ([]PathReplaceResult, error) {
	isPostgres := r.db.Dialector.Name() == "postgres"

	type replaceTarget struct {
		table  string
		column string
		isJSON bool // JSON 类型列在 PG 下需要 cast
	}
	targets := []replaceTarget{
		{"products", "images", true},
		{"products", "content_json", true},
		{"posts", "thumbnail", false},
		{"posts", "content_json", true},
		{"banners", "image", false},
		{"banners", "mobile_image", false},
		{"categories", "icon", false},
	}

	results := make([]PathReplaceResult, 0, len(targets))
	like := "%" + oldPath + "%"
	for _, t := range targets {
		var sql string
		if isPostgres && t.isJSON {
			// PG：cast json->text 做 LIKE 过滤和替换，结果 cast 回 json
			sql = fmt.Sprintf(
				`UPDATE %s SET %s = REPLACE(%s::text, ?, ?)::json WHERE %s::text LIKE ?`,
				t.table, t.column, t.column, t.column,
			)
		} else {
			sql = fmt.Sprintf(
				`UPDATE %s SET %s = REPLACE(%s, ?, ?) WHERE %s LIKE ?`,
				t.table, t.column, t.column, t.column,
			)
		}
		res := PathReplaceResult{Table: t.table, Column: t.column}
		tx := r.db.Exec(sql, oldPath, newPath, like)
		if tx.Error != nil {
			res.Error = tx.Error.Error()
		} else {
			res.Affected = tx.RowsAffected
		}
		results = append(results, res)
	}
	return results, nil
}

// determineLikeOp 根据数据库类型返回 LIKE 操作符
func determineLikeOp(db *gorm.DB) string {
	if db.Dialector.Name() == "postgres" {
		return "ILIKE"
	}
	return "LIKE"
}
