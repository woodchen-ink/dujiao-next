package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/logger"
)

// czlImageResponse 图床统一响应结构
type czlImageResponse struct {
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// czlUploadData 上传成功返回的 data 字段（size 为字符串，单位 KB）
type czlUploadData struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	OriginName string `json:"origin_name"`
	Pathname   string `json:"pathname"`
	Size       string `json:"size"` // 图床返回字符串，如 "8.708984375"（单位 KB）
	Mimetype   string `json:"mimetype"`
	Extension  string `json:"extension"`
	Links      struct {
		URL          string `json:"url"`
		HTML         string `json:"html"`
		BBCode       string `json:"bbcode"`
		Markdown     string `json:"markdown"`
		MarkdownSSL  string `json:"markdown_with_link"`
		ThumbnailURL string `json:"thumbnail_url"`
		DeleteURL    string `json:"delete_url"`
	} `json:"links"`
}

// CZLImageHostingService 封装 CZL 图床 API 调用
type CZLImageHostingService struct {
	cfg    config.CZLImageHostingConfig
	client *http.Client
}

// NewCZLImageHostingService 创建图床服务实例
func NewCZLImageHostingService(cfg config.CZLImageHostingConfig) *CZLImageHostingService {
	return &CZLImageHostingService{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Enabled 是否已启用图床
func (s *CZLImageHostingService) Enabled() bool {
	return s.cfg.Enabled && s.cfg.Token != ""
}

// CZLUploadResult 图床上传结果（宽高/大小由本地校验阶段补充，图床不返回）
type CZLUploadResult struct {
	Key  string // 图片唯一标识，删除时使用
	URL  string // 可访问的外链 URL
	Mime string
}

// Upload 上传文件到 CZL 图床，返回外链信息
func (s *CZLImageHostingService) Upload(file multipart.File, filename string) (*CZLUploadResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("构建上传表单失败: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("写入文件内容失败: %w", err)
	}

	// 可选参数：存储策略和相册
	if s.cfg.StrategyID > 0 {
		_ = writer.WriteField("strategy_id", fmt.Sprintf("%d", s.cfg.StrategyID))
	}
	if s.cfg.AlbumID > 0 {
		_ = writer.WriteField("album_id", fmt.Sprintf("%d", s.cfg.AlbumID))
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, s.baseURL()+"/upload", &body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	s.setHeaders(req, writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("图床上传请求失败: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取图床响应失败: %w", err)
	}
	logger.Infow("czl_image_hosting_upload_raw", "status_code", resp.StatusCode, "body", string(rawBody))

	var result czlImageResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("解析图床响应失败: %w", err)
	}
	if !result.Status {
		return nil, fmt.Errorf("图床上传失败: %s", result.Message)
	}

	var data czlUploadData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return nil, fmt.Errorf("解析图床上传数据失败: %w", err)
	}
	logger.Infow("czl_image_hosting_upload_parsed", "key", data.Key, "url", data.Links.URL, "mime", data.Mimetype, "size", data.Size)

	return &CZLUploadResult{
		Key:  data.Key,
		URL:  data.Links.URL,
		Mime: data.Mimetype,
	}, nil
}

// UploadFromPath 从本地文件路径上传到图床，成功后删除本地文件，返回图床外链 URL 和 key
func (s *CZLImageHostingService) UploadFromPath(filePath string) (url string, key string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", "", fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer f.Close()

	result, err := s.Upload(f, filepath.Base(filePath))
	if err != nil {
		return "", "", err
	}

	// 上传成功后删除本地文件
	_ = os.Remove(filePath)

	return result.URL, result.Key, nil
}

// Delete 从 CZL 图床删除图片（key 来自上传结果）
func (s *CZLImageHostingService) Delete(key string) error {
	url := fmt.Sprintf("%s/images/%s", s.baseURL(), key)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("创建删除请求失败: %w", err)
	}
	s.setHeaders(req, "")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("图床删除请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result czlImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析图床删除响应失败: %w", err)
	}
	if !result.Status {
		return fmt.Errorf("图床删除失败: %s", result.Message)
	}
	return nil
}

// IsCZLURL 判断路径是否为图床外链（而非本地路径）
func IsCZLURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

func (s *CZLImageHostingService) baseURL() string {
	base := strings.TrimRight(s.cfg.BaseURL, "/")
	if base == "" {
		return "https://img.czl.net/api/v1"
	}
	return base
}

// setHeaders 统一设置请求头
func (s *CZLImageHostingService) setHeaders(req *http.Request, contentType string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
}
