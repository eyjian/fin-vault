package platformapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestExtractJSONString(t *testing.T) {
	tests := []struct {
		name string
		body string
		key  string
		want string
	}{
		{
			name: "direct Chinese characters",
			body: `{"f58":"贵州茅台"}`,
			key:  "f58",
			want: "贵州茅台",
		},
		{
			name: "unicode escape Chinese characters",
			body: `{"f58":"\u8d35\u5dde\u8305\u53f0"}`,
			key:  "f58",
			want: "贵州茅台",
		},
		{
			name: "mixed unicode escape",
			body: `{"f58":"\u6210\u529f\u79d1\u6280"}`,
			key:  "f58",
			want: "成功科技",
		},
		{
			name: "no unicode escape",
			body: `{"f58":"ABC123"}`,
			key:  "f58",
			want: "ABC123",
		},
		{
			name: "key not found",
			body: `{"f58":"test"}`,
			key:  "f99",
			want: "",
		},
		{
			name: "partial unicode escape at end",
			body: `{"f58":"test\u0041"}`,
			key:  "f58",
			want: "testA",
		},
		{
			name: "unicode escape single character",
			body: `{"f58":"\u4e2d"}`,
			key:  "f58",
			want: "中",
		},
		{
			name: "unicode escape uppercase hex",
			body: `{"f58":"\u4E2D"}`,
			key:  "f58",
			want: "中",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONString(tt.body, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDecodeUnicodeEscapes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"no escapes", "hello", "hello"},
		{"simple Chinese", `\u8d35\u5dde`, "贵州"},
		{"mixed text and escape", `A\u0042C`, "ABC"},
		{"emoji surrogate pair", `\ud83d\ude00`, "😀"},
		{"invalid hex", `\uXXXX`, `\uXXXX`},
		{"incomplete escape", `\u00`, `\u00`},
		{"backslash not followed by u", `\\n`, `\\n`},
		{"standalone high surrogate", `\ud83dABC`, `\ud83dABC`},
		{"standalone low surrogate", `\ude00XYZ`, `\ude00XYZ`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeUnicodeEscapes(tt.s)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseHexRune(t *testing.T) {
	tests := []struct {
		hex  string
		want rune
		ok   bool
	}{
		{"0041", 'A', true},
		{"8d35", '贵', true},
		{"00e4", 'ä', true},
		{"00E4", 'ä', true},
		{"00XX", 0, false},
		{"", 0, false},
		{"123", 0, false},   // too short
		{"12345", 0, false}, // too long
	}
	for _, tt := range tests {
		got, err := parseHexRune(tt.hex)
		if tt.ok {
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		} else {
			assert.Error(t, err)
		}
	}
}

// gbkEncode 测试辅助：把 UTF-8 字符串编码为 GBK 字节序列。
func gbkEncode(t *testing.T, s string) string {
	t.Helper()
	encoder := simplifiedchinese.GBK.NewEncoder()
	out, err := encoder.String(s)
	require.NoError(t, err)
	return out
}

func TestEnsureUTF8(t *testing.T) {
	t.Run("already valid UTF-8 returns as-is", func(t *testing.T) {
		assert.Equal(t, "hello", ensureUTF8("hello"))
		assert.Equal(t, "贵州茅台", ensureUTF8("贵州茅台"))
		assert.Equal(t, "大族激光", ensureUTF8("大族激光"))
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		assert.Equal(t, "", ensureUTF8(""))
	})

	t.Run("GBK-encoded Chinese is decoded to UTF-8", func(t *testing.T) {
		gbk := gbkEncode(t, "大族激光")
		assert.Equal(t, "大族激光", ensureUTF8(gbk))
	})

	t.Run("GBK-encoded stock name 002190", func(t *testing.T) {
		// 模拟新浪返回 GBK 并被 resp.String() 原样转为 string 的乱码场景
		gbk := gbkEncode(t, "大族激光")
		got := ensureUTF8(gbk)
		assert.Equal(t, "大族激光", got)
	})

	t.Run("plain ASCII unchanged", func(t *testing.T) {
		assert.Equal(t, "AAPL", ensureUTF8("AAPL"))
		assert.Equal(t, "002190", ensureUTF8("002190"))
	})
}

func TestGbkToUTF8(t *testing.T) {
	t.Run("valid GBK Chinese", func(t *testing.T) {
		gbk := gbkEncode(t, "贵州茅台")
		got, err := gbkToUTF8(gbk)
		require.NoError(t, err)
		assert.Equal(t, "贵州茅台", got)
	})

	t.Run("ASCII passthrough", func(t *testing.T) {
		got, err := gbkToUTF8("hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", got)
	})

	t.Run("empty string", func(t *testing.T) {
		got, err := gbkToUTF8("")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})
}
