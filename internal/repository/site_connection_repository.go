package repository

import (
	"errors"

	"github.com/dujiao-next/internal/models"

	"gorm.io/gorm"
)

// SiteConnectionRepository 对接连接数据访问接口
type SiteConnectionRepository interface {
	GetByID(id uint) (*models.SiteConnection, error)
	GetByApiKey(apiKey string) (*models.SiteConnection, error)
	Create(conn *models.SiteConnection) error
	Update(conn *models.SiteConnection) error
	Delete(id uint) error
	List(filter SiteConnectionListFilter) ([]models.SiteConnection, int64, error)
	ListActive() ([]models.SiteConnection, error)
}

// SiteConnectionListFilter 连接列表筛选
type SiteConnectionListFilter struct {
	Status string
	Pagination
}

// GormSiteConnectionRepository GORM 实现
type GormSiteConnectionRepository struct {
	db *gorm.DB
}

// NewSiteConnectionRepository 创建连接仓库
func NewSiteConnectionRepository(db *gorm.DB) *GormSiteConnectionRepository {
	return &GormSiteConnectionRepository{db: db}
}

// GetByID 根据 ID 获取
func (r *GormSiteConnectionRepository) GetByID(id uint) (*models.SiteConnection, error) {
	var conn models.SiteConnection
	if err := r.db.First(&conn, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &conn, nil
}

// GetByApiKey 根据 ApiKey 获取连接
func (r *GormSiteConnectionRepository) GetByApiKey(apiKey string) (*models.SiteConnection, error) {
	var conn models.SiteConnection
	if err := r.db.Where("api_key = ?", apiKey).First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &conn, nil
}

// Create 创建连接
func (r *GormSiteConnectionRepository) Create(conn *models.SiteConnection) error {
	return r.db.Create(conn).Error
}

// Update 更新连接
func (r *GormSiteConnectionRepository) Update(conn *models.SiteConnection) error {
	return r.db.Save(conn).Error
}

// Delete 软删除连接
func (r *GormSiteConnectionRepository) Delete(id uint) error {
	return r.db.Delete(&models.SiteConnection{}, id).Error
}

// List 列表查询
func (r *GormSiteConnectionRepository) List(filter SiteConnectionListFilter) ([]models.SiteConnection, int64, error) {
	var conns []models.SiteConnection
	var total int64

	q := r.db.Model(&models.SiteConnection{})
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	q = q.Order("created_at DESC")
	if filter.Page > 0 && filter.PageSize > 0 {
		q = q.Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize)
	}

	if err := q.Find(&conns).Error; err != nil {
		return nil, 0, err
	}

	return conns, total, nil
}

// ListActive 获取所有启用的连接
func (r *GormSiteConnectionRepository) ListActive() ([]models.SiteConnection, error) {
	var conns []models.SiteConnection
	if err := r.db.Where("status = ?", "active").Find(&conns).Error; err != nil {
		return nil, err
	}
	return conns, nil
}
