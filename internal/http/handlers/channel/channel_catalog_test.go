package channel

import (
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/dujiao-next/internal/models"
)

func TestNormalizeChannelManualFormSchemaUsesLocaleText(t *testing.T) {
	schema := models.JSON{
		"fields": []interface{}{
			map[string]interface{}{
				"key":      "account",
				"type":     "text",
				"required": true,
				"label": map[string]interface{}{
					"zh-CN": "充值账号",
					"en-US": "Account",
				},
				"placeholder": map[string]interface{}{
					"zh-CN": "请输入账号",
					"en-US": "Enter account",
				},
			},
			map[string]interface{}{
				"key":      "server",
				"type":     "radio",
				"required": false,
				"label":    "区服",
				"options":  []interface{}{"亚服", "国际服"},
			},
		},
	}

	got := normalizeChannelManualFormSchema(schema, "zh-CN", "en-US")
	fields, ok := got["fields"].([]gin.H)
	if !ok || len(fields) != 2 {
		t.Fatalf("expected 2 fields, got=%T len=%d", got["fields"], len(fields))
	}
	if fields[0]["label"] != "充值账号" {
		t.Fatalf("expected localized label, got=%v", fields[0]["label"])
	}
	if fields[0]["placeholder"] != "请输入账号" {
		t.Fatalf("expected localized placeholder, got=%v", fields[0]["placeholder"])
	}
	options, ok := fields[1]["options"].([]string)
	if !ok || len(options) != 2 {
		t.Fatalf("expected options list, got=%T %#v", fields[1]["options"], fields[1]["options"])
	}
}
