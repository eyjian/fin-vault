package platformapi

import (
	"strings"
)

// extractJSONString 从未严格 JSON 化的文本中提取 "key":"value" 中的 value（不解码 \uxxxx）。
//
// 用于解析 jsonp 包裹的小 payload，避免引入 sjson；适用于已知字段顺序稳定的场景。
func extractJSONString(body, key string) string {
	needle := `"` + key + `":"`
	i := strings.Index(body, needle)
	if i < 0 {
		return ""
	}
	rest := body[i+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// extractJSONNumber 从文本中提取 "key":number 的数字部分。返回原字符串。
func extractJSONNumber(body, key string) string {
	needle := `"` + key + `":`
	i := strings.Index(body, needle)
	if i < 0 {
		return ""
	}
	rest := body[i+len(needle):]
	// 跳过空格
	rest = strings.TrimLeft(rest, " ")
	// 取到逗号 / 大括号 / 中括号 为止
	end := len(rest)
	for j, ch := range rest {
		if ch == ',' || ch == '}' || ch == ']' || ch == '\n' || ch == ' ' {
			end = j
			break
		}
	}
	return strings.TrimSpace(rest[:end])
}

// truncate 安全截断字符串。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
