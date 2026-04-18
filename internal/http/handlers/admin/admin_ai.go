package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/gin-gonic/gin"
)

// AIGenerateRequest AI 生成请求基础结构
type AIGenerateRequest struct {
	Action string                 `json:"action" binding:"required"` // 生成动作类型
	Data   map[string]interface{} `json:"data"`                      // 上下文数据
}

// AIGenerateResponse AI 生成结果
type AIGenerateResponse struct {
	Result interface{} `json:"result"` // 生成内容（字符串或多语言对象）
}

// openAIMessage OpenAI chat message
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIRequest OpenAI chat completion request
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
}

// openAIResponse OpenAI chat completion response
type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// callOpenAI 调用 OpenAI Chat Completion 接口
func (h *Handler) callOpenAI(systemPrompt, userPrompt string) (string, error) {
	cfg := h.Config.OpenAI
	if cfg.APIKey == "" {
		return "", fmt.Errorf("OpenAI API Key 未配置")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	reqBody := openAIRequest{
		Model: model,
		Messages: []openAIMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: 16000,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBytes, &openAIResp); err != nil {
		return "", err
	}

	if openAIResp.Error != nil {
		return "", fmt.Errorf("OpenAI 错误: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI 返回空结果")
	}

	return strings.TrimSpace(openAIResp.Choices[0].Message.Content), nil
}

// toSlugFriendly 将字符串转换为 URL 友好的 slug（仅保留字母、数字、连字符）
func toSlugFriendly(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if r == '-' || r == '_' || unicode.IsSpace(r) {
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// AIGenerate 统一 AI 生成接口
func (h *Handler) AIGenerate(c *gin.Context) {
	var req AIGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	var result interface{}
	var err error

	switch req.Action {
	case "category_slug":
		result, err = h.aiCategorySlug(req.Data)
	case "category_translate":
		result, err = h.aiCategoryTranslate(req.Data)
	case "product_title_format":
		result, err = h.aiProductTitleFormat(req.Data)
	case "product_slug":
		result, err = h.aiProductSlug(req.Data)
	case "product_keywords":
		result, err = h.aiProductKeywords(req.Data)
	case "product_seo_description":
		result, err = h.aiProductSeoDescription(req.Data)
	case "product_description":
		result, err = h.aiProductDescription(req.Data)
	case "product_content_polish":
		result, err = h.aiProductContentPolish(req.Data)
	case "product_translate":
		result, err = h.aiProductTranslate(req.Data)
	default:
		shared.RespondErrorWithMsg(c, response.CodeBadRequest, "未知的 action 类型", nil)
		return
	}

	if err != nil {
		shared.RespondErrorWithMsg(c, response.CodeInternal, err.Error(), err)
		return
	}

	response.Success(c, AIGenerateResponse{Result: result})
}

// getString 从 data map 中安全读取字符串
func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// aiCategorySlug 根据分类名称生成 slug
func (h *Handler) aiCategorySlug(data map[string]interface{}) (interface{}, error) {
	name := getString(data, "name")
	if name == "" {
		return nil, fmt.Errorf("缺少分类名称")
	}

	system := `你是一个 SEO 专家，负责生成 URL 友好的 slug。只输出 slug，不要输出任何解释或多余文字。
slug 规则：
- 全小写英文单词，单词间用连字符（-）分隔
- 中文内容必须翻译为对应的英文语义单词，禁止拼音
- 不含特殊字符，简洁清晰，2-4 个单词为宜
示例：「iCloud礼品卡」→ icloud-gift-card，「Steam充值卡」→ steam-gift-card`
	user := fmt.Sprintf("请为分类「%s」生成一个 slug。", name)

	raw, err := h.callOpenAI(system, user)
	if err != nil {
		return nil, err
	}
	return toSlugFriendly(raw), nil
}

// aiCategoryTranslate 根据简体中文名称翻译分类繁体和英文
func (h *Handler) aiCategoryTranslate(data map[string]interface{}) (interface{}, error) {
	zhCN := getString(data, "zh_cn")
	if zhCN == "" {
		return nil, fmt.Errorf("缺少简体中文名称")
	}

	system := `你是一个多语言翻译助手。根据简体中文分类名称，输出繁体中文和英文翻译。
严格按照以下 JSON 格式输出，不要输出任何其他内容：
{"zh_tw": "繁体名称", "en_us": "English Name"}`
	user := fmt.Sprintf("简体中文分类名称：%s", zhCN)

	raw, err := h.callOpenAI(system, user)
	if err != nil {
		return nil, err
	}

	// 提取 JSON 部分
	raw = extractJSON(raw)
	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("解析翻译结果失败: %w", err)
	}
	return result, nil
}

// aiProductTitleFormat 根据分类和现有名称规整商品名称格式
func (h *Handler) aiProductTitleFormat(data map[string]interface{}) (interface{}, error) {
	categoryName := getString(data, "category_name")
	currentTitle := getString(data, "current_title")
	if currentTitle == "" {
		return nil, fmt.Errorf("缺少商品名称")
	}

	system := `你是一个电商商品命名专家。商品名称为简洁的核心名称，不含分类前缀，例如：「赛博朋克2077」、「100元充值卡」。
只输出格式化后的商品名称，不要输出任何解释或额外内容。`
	user := fmt.Sprintf("分类：%s\n当前商品名称：%s\n请规整为标准格式（去掉分类前缀，保留核心商品名称）。", categoryName, currentTitle)

	return h.callOpenAI(system, user)
}

// aiProductSlug 根据分类和商品名称生成商品 slug
func (h *Handler) aiProductSlug(data map[string]interface{}) (interface{}, error) {
	categoryName := getString(data, "category_name")
	title := getString(data, "title")
	if title == "" {
		return nil, fmt.Errorf("缺少商品名称")
	}

	system := `你是一个 SEO 专家，负责生成 URL 友好的商品 slug。只输出 slug，不要输出任何解释。
slug 规则：
- 全小写英文单词，单词间用连字符（-）分隔
- 中文内容必须翻译为对应的英文语义单词，禁止拼音
- 不含特殊字符，简洁描述商品核心，2-5 个单词为宜
- 品牌名保留原文（如 iCloud、Steam、Netflix）
示例：「iCloud礼品卡」→ icloud-gift-card，「Steam充值卡50元」→ steam-gift-card-50`
	user := fmt.Sprintf("分类：%s\n商品名称：%s\n请生成 slug。", categoryName, title)

	raw, err := h.callOpenAI(system, user)
	if err != nil {
		return nil, err
	}
	return toSlugFriendly(raw), nil
}

// aiProductKeywords 根据分类和商品名称生成 SEO 关键词
func (h *Handler) aiProductKeywords(data map[string]interface{}) (interface{}, error) {
	categoryName := getString(data, "category_name")
	title := getString(data, "title")
	if title == "" {
		return nil, fmt.Errorf("缺少商品名称")
	}

	system := "你是一个 SEO 专家，负责生成商品的 meta keywords。只输出关键词，多个关键词用英文逗号分隔，不要输出解释或其他内容。关键词数量 5-10 个，包含商品名、分类名、相关词汇。"
	user := fmt.Sprintf("分类：%s\n商品名称：%s\n请生成 SEO meta keywords。", categoryName, title)

	return h.callOpenAI(system, user)
}

// aiProductSeoDescription 生成 SEO meta description
func (h *Handler) aiProductSeoDescription(data map[string]interface{}) (interface{}, error) {
	categoryName := getString(data, "category_name")
	title := getString(data, "title")
	description := getString(data, "description")
	if title == "" {
		return nil, fmt.Errorf("缺少商品名称")
	}

	system := "你是一个 SEO 专家，负责生成商品的 meta description。只输出描述文字，不超过 160 字符，简洁吸引人，包含核心关键词，不要输出任何解释。"
	user := fmt.Sprintf("分类：%s\n商品名称：%s\n商品简介：%s\n请生成 SEO meta description。", categoryName, title, description)

	return h.callOpenAI(system, user)
}

// aiProductDescription 根据分类、商品名称和详情生成商品简介
func (h *Handler) aiProductDescription(data map[string]interface{}) (interface{}, error) {
	categoryName := getString(data, "category_name")
	title := getString(data, "title")
	content := getString(data, "content")
	if title == "" {
		return nil, fmt.Errorf("缺少商品名称")
	}

	system := "你是一个电商文案专家，负责撰写商品简介。只输出简介文字，2-4 句话，突出核心卖点，语言简洁有力，不要输出解释或标题。"
	user := fmt.Sprintf("分类：%s\n商品名称：%s\n商品详情：%s\n请生成商品简介。", categoryName, title, content)

	return h.callOpenAI(system, user)
}

// aiProductContentPolish 优化规整商品详情富文本内容
func (h *Handler) aiProductContentPolish(data map[string]interface{}) (interface{}, error) {
	content := getString(data, "content")
	if content == "" {
		return nil, fmt.Errorf("缺少商品详情内容")
	}

	system := `你是一个电商文案优化专家。对商品详情进行优化规整：
1. 修正错别字和语病
2. 优化段落结构，使内容更清晰
3. 保持原有 HTML/Markdown 格式标签
4. 不添加多余内容，不删除核心信息
只输出优化后的内容，不要输出任何解释。`
	user := fmt.Sprintf("请优化以下商品详情：\n%s", content)

	return h.callOpenAI(system, user)
}

// aiProductTranslate 根据简体中文翻译商品多语言字段
func (h *Handler) aiProductTranslate(data map[string]interface{}) (interface{}, error) {
	field := getString(data, "field") // title/description/content/keywords/seo_description
	zhCN := getString(data, "zh_cn")
	if zhCN == "" {
		return nil, fmt.Errorf("缺少简体中文内容")
	}

	var fieldDesc string
	switch field {
	case "title":
		fieldDesc = "商品名称"
	case "description":
		fieldDesc = "商品简介"
	case "content":
		fieldDesc = "商品详情（保留原始 HTML/Markdown 格式）"
	case "keywords":
		fieldDesc = "SEO 关键词"
	case "seo_description":
		fieldDesc = "SEO 描述"
	default:
		fieldDesc = "文本内容"
	}

	system := fmt.Sprintf(`你是一个多语言翻译助手，专注于电商场景。将「%s」翻译为繁体中文和英文。
严格按照以下 JSON 格式输出，不要输出任何其他内容：
{"zh_tw": "繁体翻译", "en_us": "English translation"}`, fieldDesc)
	user := fmt.Sprintf("简体中文内容：\n%s", zhCN)

	raw, err := h.callOpenAI(system, user)
	if err != nil {
		return nil, err
	}

	raw = extractJSON(raw)
	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("解析翻译结果失败: %w", err)
	}
	return result, nil
}

// extractJSON 从可能包含多余文字的 LLM 输出中提取 JSON 对象
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
