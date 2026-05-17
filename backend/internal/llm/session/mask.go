package session

import (
	"bytes"
	"encoding/json"
	"strings"
)

// maskedValue 敏感字段被替换的固定字符串。
const maskedValue = "***"

// sensitiveKeyTokens 字段名（key）case-insensitive 包含以下任一子串即视为敏感。
//
// 来源：spec ai-tools §61（api_key / password / token）+ HTTP 标准头 authorization
// + 无下划线变体 apikey。后续如需扩展，只要追加这个 slice 即可。
var sensitiveKeyTokens = []string{
	"api_key",       // OpenAI / DeepSeek / Qwen 等 LLM 凭证
	"apikey",        // 无下划线变体（AzureOpenAI / 部分 SDK header 用法）
	"password",
	"token",         // 含 access_token / id_token / refresh_token / token 等
	"authorization", // HTTP 标准头
}

// MaskSensitiveJSON 对 JSON 字节流脱敏后返回新的 JSON 字节流。
//
// 解析失败（非合法 JSON）时原样返回（不应阻断业务，由 caller 决定是否记 warn）。
// nil / 空字节直接返回原值。
//
// 字段匹配规则：key 转小写后 strings.Contains 任一 sensitiveKeyTokens 即替换为
// "***"；递归处理 map 与 slice 的全部层级，标量原样返回。
func MaskSensitiveJSON(payload []byte) []byte {
	if len(bytes.TrimSpace(payload)) == 0 {
		return payload
	}
	var v interface{}
	if err := json.Unmarshal(payload, &v); err != nil {
		return payload
	}
	masked := maskValue(v)
	out, err := json.Marshal(masked)
	if err != nil {
		return payload
	}
	return out
}

// maskValue 递归脱敏：map 走 key 检查，slice 递归元素，标量原样返回。
func maskValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, val := range t {
			if isSensitiveKey(k) {
				m[k] = maskedValue
			} else {
				m[k] = maskValue(val)
			}
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = maskValue(e)
		}
		return out
	default:
		return v
	}
}

// isSensitiveKey 判断字段名（不区分大小写）是否包含任一敏感子串。
func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	for _, tok := range sensitiveKeyTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}
