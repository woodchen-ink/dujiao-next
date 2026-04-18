package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
)

// czlImageResponse 图床统一响应结构
type czlImageResponse struct {
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// czlUploadData 上传成功返回的 data 字段
type czlUploadData struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Links struct {
		URL         string `json:"url"`
		HTML        string `json:"html"`
		BBCode      string `json:"bbcode"`
		Markdown    string `json:"markdown"`
		MarkdownSSL string `json:"markdown_with_link"`
		ThumbnailURL string `json:"thumbnail_url"`
	} `json:"links"`
	Size   int    `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Mime   string `json:"mime"`
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

// UploadResult 图床上传结果
type CZLUploadResult struct {
	Key      string // 图片唯一标识，删除时使用
	URL      string // 可访问的外链 URL
	Mime     string
	Size     int
	Width    int
	Height   int
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

	var result czlImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析图床响应失败: %w", err)
	}
	if !result.Status {
		return nil, fmt.Errorf("图床上传失败: %s", result.Message)
	}

	var data czlUploadData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return nil, fmt.Errorf("解析图床上传数据失败: %w", err)
	}

	return &CZLUploadResult{
		Key:    data.Key,
		URL:    data.Links.URL,
		Mime:   data.Mime,
		Size:   data.Size,
		Width:  data.Width,
		Height: data.Height,
	}, nil
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
