# dujiao-next 后端

Go + Gin 电商平台后端 API。

## 技术栈

- Go 1.25+ / Gin 框架
- GORM + SQLite（开发）/ PostgreSQL（生产）
- Redis + Asynq 异步任务队列
- JWT 认证（管理端 + 用户端独立配置）
- Casbin RBAC 权限控制

## 目录结构

```
internal/
  config/           # 配置结构与加载（Viper）
  http/handlers/
    admin/          # 管理端 Handler，每类资源一个文件
    shared/         # 公共错误响应工具
  models/           # GORM 模型
  service/          # 业务逻辑层
  repository/       # 数据访问层
  router/           # 路由注册（router.go）
  provider/         # 依赖注入容器
```

## 配置说明

配置文件：`config.yml`（项目根目录）。支持同名环境变量覆盖（`.` 替换为 `_`）。

### OpenAI 配置（新增）

```yaml
openai:
  api_key: "sk-xxx"          # OpenAI API Key（必填）
  base_url: "https://api.openai.com/v1"  # 可自定义代理地址
  model: "gpt-4o-mini"       # 默认模型
```

## AI 辅助生成接口

路由：`POST /api/v1/admin/ai/generate`（需要管理员认证）

Handler 文件：`internal/http/handlers/admin/admin_ai.go`

### 支持的 action

| action | 说明 | 必传 data 字段 |
|--------|------|---------------|
| `category_slug` | 根据分类名称生成 slug | `name` |
| `category_translate` | 翻译分类名称（繁体+英文） | `zh_cn` |
| `product_title_format` | 规整商品名称为「[分类] 名称」格式 | `category_name`, `current_title` |
| `product_slug` | 根据分类和名称生成商品 slug | `category_name`, `title` |
| `product_keywords` | 生成 SEO meta keywords | `category_name`, `title` |
| `product_seo_description` | 生成 SEO meta description | `category_name`, `title`, `description` |
| `product_description` | 生成商品简介 | `category_name`, `title`, `content` |
| `product_content_polish` | 优化商品详情富文本 | `content` |
| `product_translate` | 翻译商品字段（繁体+英文） | `field`, `zh_cn` |

### 响应结构

```json
{ "status_code": 0, "msg": "success", "data": { "result": "生成内容或翻译对象" } }
```

翻译类 action 的 `result` 为：`{ "zh_tw": "...", "en_us": "..." }`

## 编码约定

- 新增 Handler 直接在 `internal/http/handlers/admin/` 下创建文件
- 路由统一在 `router/router.go` 的 `authorized` 组内注册
- 错误响应使用 `shared.RespondError` 或 `shared.RespondErrorWithMsg`
- 成功响应使用 `response.Success(c, data)`
