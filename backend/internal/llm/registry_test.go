package llm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// NewRegistry
// =====================================================================

func TestNewRegistry_AllEmpty_ReturnsErrProviderEmpty(t *testing.T) {
	cfg := RegistryConfig{
		Default: "deepseek",
		Providers: map[string]ProviderConfig{
			"deepseek": {APIKey: "", BaseURL: "", Model: "deepseek-chat"},
			"glm":      {APIKey: "", BaseURL: "", Model: "glm-4"},
		},
	}
	r, err := NewRegistry(cfg)
	require.ErrorIs(t, err, ErrProviderEmpty)
	assert.Nil(t, r)
}

func TestNewRegistry_SkipsBadConfigButKeepsValidOnes(t *testing.T) {
	// deepseek: 缺 model（NewOpenAIProvider 会报错） → 跳过
	// glm: 完整 → 保留
	cfg := RegistryConfig{
		Default: "deepseek",
		Providers: map[string]ProviderConfig{
			"deepseek": {APIKey: "sk-x", BaseURL: "https://api.deepseek.com", Model: ""}, // 缺 model
			"glm":      {APIKey: "sk-y", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4"},
		},
	}
	r, err := NewRegistry(cfg)
	require.NoError(t, err)
	require.NotNil(t, r)

	// 默认 provider deepseek 不可用 → fallback 到 glm
	assert.Equal(t, "glm", r.Default())

	_, err = r.Get("deepseek")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderNotFound))

	p, err := r.Get("glm")
	require.NoError(t, err)
	assert.Equal(t, "glm", p.Name())
	assert.Equal(t, "glm-4", p.Model())
}

func TestNewRegistry_DefaultEmpty_PickFirstAvail(t *testing.T) {
	cfg := RegistryConfig{
		Default: "", // 空，需 fallback
		Providers: map[string]ProviderConfig{
			"qwen": {APIKey: "sk-q", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-turbo"},
			"kimi": {APIKey: "sk-k", BaseURL: "https://api.moonshot.cn/v1", Model: "moonshot-v1-8k"},
		},
	}
	r, err := NewRegistry(cfg)
	require.NoError(t, err)

	// firstAvail 取决于 map 遍历顺序——只断言不是空且必在 providers 中
	def := r.Default()
	assert.Contains(t, []string{"qwen", "kimi"}, def)

	// 同时两个 provider 都能 Get 到
	for _, name := range []string{"qwen", "kimi"} {
		_, err := r.Get(name)
		require.NoErrorf(t, err, "Get(%s) failed", name)
	}
}

func TestRegistry_Get_EmptyName_UsesDefault(t *testing.T) {
	cfg := RegistryConfig{
		Default: "deepseek",
		Providers: map[string]ProviderConfig{
			"deepseek": {APIKey: "sk-d", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
		},
	}
	r, err := NewRegistry(cfg)
	require.NoError(t, err)

	p, err := r.Get("")
	require.NoError(t, err)
	assert.Equal(t, "deepseek", p.Name())
}

func TestRegistry_Get_UnknownName_ReturnsErrProviderNotFound(t *testing.T) {
	cfg := RegistryConfig{
		Default: "deepseek",
		Providers: map[string]ProviderConfig{
			"deepseek": {APIKey: "sk-d", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
		},
	}
	r, _ := NewRegistry(cfg)

	_, err := r.Get("not-exist")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderNotFound))
}

func TestRegistry_List_SortedByName(t *testing.T) {
	cfg := RegistryConfig{
		Default: "deepseek",
		Providers: map[string]ProviderConfig{
			"qwen":     {APIKey: "sk", BaseURL: "u", Model: "qwen-turbo"},
			"deepseek": {APIKey: "sk", BaseURL: "u", Model: "deepseek-chat"},
			"kimi":     {APIKey: "sk", BaseURL: "u", Model: "moonshot-v1-8k"},
		},
	}
	r, _ := NewRegistry(cfg)

	list := r.List()
	require.Len(t, list, 3)
	// 按名字升序
	assert.Equal(t, []string{"deepseek", "kimi", "qwen"}, []string{list[0].Name, list[1].Name, list[2].Name})

	// IsDefault 标记仅对 deepseek
	for _, info := range list {
		if info.Name == "deepseek" {
			assert.True(t, info.IsDefault)
		} else {
			assert.False(t, info.IsDefault)
		}
	}
}

// 验证 Provider.Name()/Model() 在 NewOpenAIProvider 后正确返回，覆盖配置默认值。
func TestNewOpenAIProvider_NameAndModel(t *testing.T) {
	p, err := NewOpenAIProvider("kimi", ProviderConfig{
		APIKey:  "sk-x",
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "moonshot-v1-8k",
	})
	require.NoError(t, err)
	assert.Equal(t, "kimi", p.Name())
	assert.Equal(t, "moonshot-v1-8k", p.Model())
}

// 缺 model 必失败。
func TestNewOpenAIProvider_MissingModel_Fails(t *testing.T) {
	_, err := NewOpenAIProvider("deepseek", ProviderConfig{
		APIKey:  "sk-x",
		BaseURL: "https://api.deepseek.com",
		Model:   "",
	})
	require.Error(t, err)
}

// 全空（无 api_key 又无 base_url）必失败。
func TestNewOpenAIProvider_NoKeyNoBaseURL_Fails(t *testing.T) {
	_, err := NewOpenAIProvider("xxx", ProviderConfig{
		APIKey: "", BaseURL: "", Model: "any",
	})
	require.Error(t, err)
}

// SafeMarshalArgs/SafeUnmarshalArgs：异常输入安全处理。
func TestSafeMarshalArgs(t *testing.T) {
	type P struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	got := SafeMarshalArgs(P{A: 1, B: "x"})
	assert.JSONEq(t, `{"a":1,"b":"x"}`, got)

	// 不可序列化值（chan）→ 返回 "{}"
	bad := SafeMarshalArgs(make(chan int))
	assert.Equal(t, "{}", bad)
}

func TestSafeUnmarshalArgs_EmptyOrInvalid(t *testing.T) {
	var v map[string]any
	require.NoError(t, SafeUnmarshalArgs("", &v))
	require.NoError(t, SafeUnmarshalArgs("   ", &v))

	// 非法 JSON → 报错
	require.Error(t, SafeUnmarshalArgs("{bad", &v))
}
