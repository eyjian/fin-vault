package platformapi

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// extractJSONString 从未严格 JSON 化的文本中提取 "key":"value" 中的 value，并解码 \uxxxx 转义。
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
	raw := rest[:end]
	return decodeUnicodeEscapes(raw)
}

// decodeUnicodeEscapes 将字符串中的 \uXXXX 转义序列解码为实际 UTF-8 字符。
//
// 例如 "\u8d35\u5dde" → "贵州"，"\ud83d\ude00" → "😀"。
// 对于无法识别的转义（非 \uXXXX），原样保留。
// 支持 JSON RFC 7159 §7 定义的 UTF-16 代理对：\uD800–\uDBFF 后跟 \uDC00–\uDFFF。
func decodeUnicodeEscapes(s string) string {
	if !strings.Contains(s, `\u`) {
		return s
	}
	// 预分配 builder
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+5 < len(s) && s[i] == '\\' && s[i+1] == 'u' {
			// 解析 4 位十六进制
			hex := s[i+2 : i+6]
			r, err := parseHexRune(hex)
			if err == nil {
				// 检查 UTF-16 高代理（\uD800–\uDBFF），需后跟低代理组成完整字符
				if r >= 0xD800 && r <= 0xDBFF {
					// 高代理：后面必须紧跟 \uDC00–\uDFFF
					if i+11 < len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
						lowHex := s[i+8 : i+12]
						lowR, lowErr := parseHexRune(lowHex)
						if lowErr == nil && lowR >= 0xDC00 && lowR <= 0xDFFF {
							// 组合代理对：公式 (high-0xD800)*0x400 + (low-0xDC00) + 0x10000
							r = (r-0xD800)<<10 + (lowR - 0xDC00) + 0x10000
							b.WriteRune(r)
							i += 12
							continue
						}
					}
					// 孤立的高代理，原样保留
					b.WriteString(s[i : i+6])
					i += 6
					continue
				}
				// 低代理不应单独出现；若出现则原样保留
				if r >= 0xDC00 && r <= 0xDFFF {
					b.WriteString(s[i : i+6])
					i += 6
					continue
				}
				// BMP 字符，直接写入
				b.WriteRune(r)
				i += 6
				continue
			}
			// 无效的 \uXXXX，原样保留
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// parseHexRune 将 4 位十六进制字符串转为 rune。
func parseHexRune(hex string) (rune, error) {
	if len(hex) != 4 {
		return 0, fmt.Errorf("invalid hex length %d in \\uXXXX escape", len(hex))
	}
	var r rune
	for _, c := range hex {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex char %c in \\uXXXX escape", c)
		}
	}
	return r, nil
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

// ensureUTF8 自动判断字符串是否已经是合法 UTF-8；若不是则尝试按 GBK 解码。
//
// 用途：新浪 hq.sinajs.cn 等中文站点返回 GBK 编码的中文，但响应头不声明字符集，
// resty 的 resp.String() 会按 ISO-8859-1 / 默认 UTF-8 解读，导致中文显示为 �。
//
// 实现策略：
//  1. utf8.ValidString 判断当前字符串是否已经是合法 UTF-8；
//  2. 不合法则按 GBK 解码后转为 UTF-8 返回；
//  3. GBK 解码也失败则原样返回（避免吞错；调用方依然会拿到原值，至少不更糟）。
func ensureUTF8(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}
	decoded, err := gbkToUTF8(s)
	if err != nil {
		return s
	}
	return decoded
}

// gbkToUTF8 把 GBK / GB18030 字节流解码为 UTF-8 字符串。
func gbkToUTF8(s string) (string, error) {
	reader := transform.NewReader(strings.NewReader(s), simplifiedchinese.GBK.NewDecoder())
	out, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("gbk decode: %w", err)
	}
	return string(out), nil
}
