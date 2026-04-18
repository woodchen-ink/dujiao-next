package service

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
)

// MediaService 素材管理服务
type MediaService struct {
	repo         repository.MediaRepository
	imageHosting *CZLImageHostingService // 可选：删除图床图片时使用
}

// NewMediaService 创建素材服务实例
func NewMediaService(repo repository.MediaRepository) *MediaService {
	return &MediaService{repo: repo}
}

// SetImageHostingService 注入图床服务
func (s *MediaService) SetImageHostingService(svc *CZLImageHostingService) {
	s.imageHosting = svc
}

// List 素材列表
func (s *MediaService) List(scene, search string, page, pageSize int) ([]models.Media, int64, error) {
	return s.repo.List(repository.MediaListFilter{
		Page:     page,
		PageSize: pageSize,
		Scene:    scene,
		Search:   search,
	})
}

// RecordMedia 记录上传的素材元数据（上传后自动调用）
func (s *MediaService) RecordMedia(result *UploadResult, scene string) (*models.Media, error) {
	// 检查是否已存在（基于路径去重）
	existing, err := s.repo.GetByPath(result.URL)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// 从原始文件名生成默认素材名称（去掉扩展名）
	name := result.Filename
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}

	media := &models.Media{
		Name:        name,
		Filename:    result.Filename,
		Path:        result.URL,
		ExternalKey: result.ExternalKey,
		MimeType:    result.MimeType,
		Size:        result.Size,
		Scene:       scene,
		Width:       result.Width,
		Height:      result.Height,
	}
	if err := s.repo.Create(media); err != nil {
		return nil, err
	}
	return media, nil
}

// RecordExternalMedia 将图床外链记录到素材库（图床上传成功后调用）
func (s *MediaService) RecordExternalMedia(url, externalKey, scene string) {
	existing, _ := s.repo.GetByPath(url)
	if existing != nil {
		return
	}
	name := filepath.Base(url)
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	media := &models.Media{
		Name:        name,
		Filename:    filepath.Base(url),
		Path:        url,
		ExternalKey: externalKey,
		MimeType:    "image/jpeg", // 上游图片均为图片类型，无需精确 MIME
		Scene:       scene,
	}
	if err := s.repo.Create(media); err != nil {
		logger.Warnw("media_record_external_failed", "url", url, "error", err)
	}
}

// RecordLocalFile 将本地已存在的文件记录到素材库（用于下载的上游图片等）
// localPath 格式如 /uploads/upstream/uuid.jpg，scene 如 "upstream"
func (s *MediaService) RecordLocalFile(localPath, scene string) {
	// 去重
	existing, _ := s.repo.GetByPath(localPath)
	if existing != nil {
		return
	}

	// 本地物理路径：去掉开头的 /
	diskPath := strings.TrimPrefix(localPath, "/")
	fi, err := os.Stat(diskPath)
	if err != nil {
		return
	}

	filename := filepath.Base(localPath)
	name := filename
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}

	// 检测 MIME 类型
	mimeType := "application/octet-stream"
	if f, err := os.Open(diskPath); err == nil {
		buf := make([]byte, 512)
		if n, _ := f.Read(buf); n > 0 {
			mimeType = http.DetectContentType(buf[:n])
		}
		f.Close()
	}

	// 尝试获取图片尺寸
	var width, height int
	if strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml" {
		if f, err := os.Open(diskPath); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				width = cfg.Width
				height = cfg.Height
			}
			f.Close()
		}
	}

	media := &models.Media{
		Name:     name,
		Filename: filename,
		Path:     localPath,
		MimeType: mimeType,
		Size:     fi.Size(),
		Scene:    scene,
		Width:    width,
		Height:   height,
	}
	if err := s.repo.Create(media); err != nil {
		logger.Warnw("media_record_local_file_failed", "path", localPath, "error", err)
	}
}

// MigrateToImageHosting 将本地素材上传到图床并更新 Path/ExternalKey
// 同时替换同表中所有引用了该旧路径的记录（产品/文章中的 URL 替换由调用方处理）
func (s *MediaService) MigrateToImageHosting(id uint) (newURL string, err error) {
	if s.imageHosting == nil || !s.imageHosting.Enabled() {
		return "", fmt.Errorf("图床未启用")
	}

	media, err := s.repo.GetByID(id)
	if err != nil {
		return "", err
	}
	if media == nil {
		return "", ErrMediaNotFound
	}
	if IsCZLURL(media.Path) {
		return media.Path, nil // 已是图床 URL，无需迁移
	}

	diskPath := strings.TrimPrefix(media.Path, "/")
	czlURL, czlKey, err := s.imageHosting.UploadFromPath(diskPath)
	if err != nil {
		return "", fmt.Errorf("上传图床失败: %w", err)
	}

	oldPath := media.Path
	media.Path = czlURL
	media.ExternalKey = czlKey
	if err := s.repo.Update(media); err != nil {
		return "", fmt.Errorf("更新素材记录失败: %w", err)
	}

	// 替换所有业务表中引用旧路径的字段
	if err := s.repo.ReplacePathInAllTables(oldPath, czlURL); err != nil {
		logger.Warnw("media_migrate_replace_refs_failed", "id", id, "old", oldPath, "new", czlURL, "error", err)
	}

	logger.Infow("media_migrated_to_czl", "id", id, "old", oldPath, "new", czlURL)
	return czlURL, nil
}

// Rename 重命名素材
func (s *MediaService) Rename(id uint, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrMediaNameEmpty
	}
	media, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if media == nil {
		return ErrMediaNotFound
	}
	media.Name = name
	return s.repo.Update(media)
}

// Delete 删除素材（软删除记录，并根据路径类型删除图床文件或本地文件）
func (s *MediaService) Delete(id uint) error {
	media, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if media == nil {
		return ErrMediaNotFound
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}

	if IsCZLURL(media.Path) {
		// 图床文件：Path 即为完整 URL，Key 存储在 ExternalKey 字段（若为空则无法删除，仅记录警告）
		if media.ExternalKey != "" && s.imageHosting != nil && s.imageHosting.Enabled() {
			if err := s.imageHosting.Delete(media.ExternalKey); err != nil {
				logger.Warnw("media_delete_czl_image_failed", "id", id, "key", media.ExternalKey, "error", err)
			}
		}
		return nil
	}

	// 本地文件：Path 格式如 /uploads/product/2026/04/uuid.jpg
	diskPath := strings.TrimPrefix(media.Path, "/")
	if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
		logger.Warnw("media_delete_file_failed", "id", id, "path", diskPath, "error", err)
	}
	return nil
}
