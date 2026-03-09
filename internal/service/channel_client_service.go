package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/dujiao-next/internal/crypto"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
	"github.com/dujiao-next/internal/upstream"
)

// ChannelClientService 渠道客户端业务服务
type ChannelClientService struct {
	repo      repository.ChannelClientRepository
	encKey    []byte // AES-256 密钥
	secretKey string // 原始密钥（用于派生）
}

// NewChannelClientService 创建渠道客户端服务
func NewChannelClientService(repo repository.ChannelClientRepository, appSecretKey string) *ChannelClientService {
	return &ChannelClientService{
		repo:      repo,
		encKey:    crypto.DeriveKey(appSecretKey),
		secretKey: appSecretKey,
	}
}

// ChannelClientResponse 创建渠道客户端的响应（仅创建时返回明文 secret）
type ChannelClientResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
	ChannelKey  string `json:"channel_key"`
	Secret      string `json:"secret"` // 明文 secret，仅创建时可见
	Description string `json:"description"`
}

// CreateChannelClient 创建渠道客户端
func (s *ChannelClientService) CreateChannelClient(name, channelType, description string) (*ChannelClientResponse, error) {
	// 生成随机 key (32 bytes = 64 hex chars)
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("generate channel key: %w", err)
	}
	channelKey := hex.EncodeToString(keyBytes)

	// 生成随机 secret (32 bytes = 64 hex chars)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, fmt.Errorf("generate channel secret: %w", err)
	}
	plainSecret := hex.EncodeToString(secretBytes)

	// 加密 secret 存储
	encryptedSecret, err := crypto.Encrypt(s.encKey, plainSecret)
	if err != nil {
		return nil, fmt.Errorf("encrypt channel secret: %w", err)
	}

	client := &models.ChannelClient{
		Name:          name,
		ChannelType:   channelType,
		ChannelKey:    channelKey,
		ChannelSecret: encryptedSecret,
		Status:        1,
		Description:   description,
	}

	if err := s.repo.Create(client); err != nil {
		return nil, err
	}

	return &ChannelClientResponse{
		ID:          client.ID,
		Name:        client.Name,
		ChannelType: client.ChannelType,
		ChannelKey:  client.ChannelKey,
		Secret:      plainSecret,
		Description: client.Description,
	}, nil
}

// GetChannelClient 获取渠道客户端
func (s *ChannelClientService) GetChannelClient(id uint) (*models.ChannelClient, error) {
	client, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrChannelClientNotFound
	}
	return client, nil
}

// ListChannelClients 列出所有渠道客户端
func (s *ChannelClientService) ListChannelClients() ([]models.ChannelClient, error) {
	return s.repo.FindAll()
}

// UpdateChannelClientStatus 更新渠道客户端状态
func (s *ChannelClientService) UpdateChannelClientStatus(id uint, status int) error {
	client, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if client == nil {
		return ErrChannelClientNotFound
	}
	client.Status = status
	return s.repo.Update(client)
}

// VerifyChannelSignature 验证渠道签名
// 复用 upstream/signer.go 的 HMAC-SHA256 签名算法
func (s *ChannelClientService) VerifyChannelSignature(key, signature string, timestamp int64, method, path string, body []byte) (*models.ChannelClient, error) {
	// 验证时间戳
	if !upstream.IsTimestampValid(timestamp) {
		return nil, ErrChannelTimestampExpired
	}

	// 查找客户端
	client, err := s.repo.FindByChannelKey(key)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrChannelClientNotFound
	}
	if client.Status != 1 {
		return nil, ErrChannelClientDisabled
	}

	// 解密 secret
	plainSecret, err := crypto.Decrypt(s.encKey, client.ChannelSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt channel secret: %w", err)
	}

	// 验证签名（复用 upstream.Verify）
	if !upstream.Verify(plainSecret, method, path, signature, timestamp, body) {
		return nil, ErrChannelSignatureInvalid
	}

	return client, nil
}
