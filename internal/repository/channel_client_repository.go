package repository

import (
	"errors"

	"github.com/dujiao-next/internal/models"

	"gorm.io/gorm"
)

// ChannelClientRepository 渠道客户端数据访问接口
type ChannelClientRepository interface {
	Create(client *models.ChannelClient) error
	FindByID(id uint) (*models.ChannelClient, error)
	FindByChannelKey(key string) (*models.ChannelClient, error)
	FindAll() ([]models.ChannelClient, error)
	Update(client *models.ChannelClient) error
}

// GormChannelClientRepository GORM 实现
type GormChannelClientRepository struct {
	db *gorm.DB
}

// NewChannelClientRepository 创建渠道客户端仓库
func NewChannelClientRepository(db *gorm.DB) *GormChannelClientRepository {
	return &GormChannelClientRepository{db: db}
}

// Create 创建渠道客户端
func (r *GormChannelClientRepository) Create(client *models.ChannelClient) error {
	return r.db.Create(client).Error
}

// FindByID 根据 ID 查找
func (r *GormChannelClientRepository) FindByID(id uint) (*models.ChannelClient, error) {
	var client models.ChannelClient
	if err := r.db.First(&client, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &client, nil
}

// FindByChannelKey 根据 channel_key 查找
func (r *GormChannelClientRepository) FindByChannelKey(key string) (*models.ChannelClient, error) {
	var client models.ChannelClient
	if err := r.db.Where("channel_key = ?", key).First(&client).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &client, nil
}

// FindAll 获取所有渠道客户端
func (r *GormChannelClientRepository) FindAll() ([]models.ChannelClient, error) {
	var clients []models.ChannelClient
	if err := r.db.Order("created_at DESC").Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// Update 更新渠道客户端
func (r *GormChannelClientRepository) Update(client *models.ChannelClient) error {
	return r.db.Save(client).Error
}
