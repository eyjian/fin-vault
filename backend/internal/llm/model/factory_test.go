package model_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/llm/model"
)

// newCapturingLogger 构造一个把日志写入 buf 的 slog.Logger，方便测试断言日志内容。
//
// 用 TextHandler + LevelDebug，确保 Info / Warn / Error 都能落进 buf。
func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, buf
}

// usable 构造一个可用 provider entry（4 条件全满足）。
func usable(name string) model.ProviderEntry {
	return model.ProviderEntry{
		Enabled: true,
		APIKey:  "sk-" + name,
		BaseURL: "https://api." + name + ".com",
		Model:   name + "-flagship",
	}
}

// =====================================================================
// 选 default 路径
// =====================================================================

// TestFactory_DefaultUsable_PicksDefault：default 可用 → 直接选 default，
// logger 含 "selected (default)"，selected 名 == default 名。
func TestFactory_DefaultUsable_PicksDefault(t *testing.T) {
	logger, buf := newCapturingLogger()
	cfg := model.RegistryEntry{
		Default: "deepseek",
		Providers: map[string]model.ProviderEntry{
			"deepseek": usable("deepseek"),
			"glm":      usable("glm"),
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m, "返回非 nil SDK Model")
	assert.Equal(t, "deepseek", selected)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "selected (default)")
	assert.Contains(t, logOutput, "provider=deepseek")
	assert.NotContains(t, logOutput, "fallback by dictionary order",
		"default 路径不应触发 fallback warn")
}

// =====================================================================
// fallback 路径
// =====================================================================

// TestFactory_DefaultMissing_FallbackByDictOrder：default 在 providers map 里没有 →
// fallback 选字典序最小的 deepseek（providers 含 deepseek+glm，"deepseek" < "glm"）。
func TestFactory_DefaultMissing_FallbackByDictOrder(t *testing.T) {
	logger, buf := newCapturingLogger()
	cfg := model.RegistryEntry{
		Default: "kimi", // 不在 providers map 中
		Providers: map[string]model.ProviderEntry{
			"glm":      usable("glm"),
			"deepseek": usable("deepseek"),
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "deepseek", selected, "字典序 deepseek < glm")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "fallback by dictionary order")
	assert.Contains(t, logOutput, "selected=deepseek")
	assert.Contains(t, logOutput, "default=kimi")
}

// TestFactory_DefaultEmptyAPIKey_Fallback：default 存在但 APIKey 空 → fallback。
func TestFactory_DefaultEmptyAPIKey_Fallback(t *testing.T) {
	logger, buf := newCapturingLogger()
	bad := usable("deepseek")
	bad.APIKey = ""
	cfg := model.RegistryEntry{
		Default: "deepseek",
		Providers: map[string]model.ProviderEntry{
			"deepseek": bad,
			"glm":      usable("glm"),
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "glm", selected, "deepseek 不可用，字典序剩 glm")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "fallback by dictionary order")
}

// TestFactory_DefaultEnabledFalse_Fallback：default Enabled=false → fallback。
func TestFactory_DefaultEnabledFalse_Fallback(t *testing.T) {
	logger, buf := newCapturingLogger()
	disabled := usable("deepseek")
	disabled.Enabled = false
	cfg := model.RegistryEntry{
		Default: "deepseek",
		Providers: map[string]model.ProviderEntry{
			"deepseek": disabled,
			"qwen":     usable("qwen"),
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "qwen", selected)
	assert.Contains(t, buf.String(), "fallback by dictionary order")
}

// TestFactory_FallbackSkipsAllUnusableUntilUsable：default 不可用 +
// 字典序前面的 entry 也不可用 → 跳过继续，最终选到第一个可用的。
func TestFactory_FallbackSkipsAllUnusableUntilUsable(t *testing.T) {
	logger, _ := newCapturingLogger()
	bad := usable("deepseek")
	bad.BaseURL = ""
	worseGLM := usable("glm")
	worseGLM.Model = ""

	cfg := model.RegistryEntry{
		Default: "deepseek",
		Providers: map[string]model.ProviderEntry{
			"deepseek": bad,
			"glm":      worseGLM,
			"kimi":     usable("kimi"),
			"qwen":     usable("qwen"),
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)
	// 字典序顺序：deepseek(bad) → glm(bad) → kimi(usable) → qwen(skipped already won)
	assert.Equal(t, "kimi", selected)
}

// =====================================================================
// 错误路径
// =====================================================================

// TestFactory_AllUnusable_ReturnsError：所有 entry 都缺字段 → error。
func TestFactory_AllUnusable_ReturnsError(t *testing.T) {
	logger, _ := newCapturingLogger()
	bad := usable("deepseek")
	bad.APIKey = ""
	cfg := model.RegistryEntry{
		Default: "deepseek",
		Providers: map[string]model.ProviderEntry{
			"deepseek": bad,
			"glm":      {Enabled: true, APIKey: "sk-glm", Model: "glm-flagship" /* BaseURL 缺 */},
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, m)
	assert.Empty(t, selected)
	assert.Contains(t, err.Error(), "no usable llm provider")
	assert.Contains(t, err.Error(), `default="deepseek"`)
	assert.Contains(t, err.Error(), "providers=2")
}

// TestFactory_EmptyProviders：providers map 空 → error。
func TestFactory_EmptyProviders(t *testing.T) {
	logger, _ := newCapturingLogger()
	cfg := model.RegistryEntry{
		Default:   "deepseek",
		Providers: map[string]model.ProviderEntry{},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, m)
	assert.Empty(t, selected)
	assert.Contains(t, err.Error(), "no usable llm provider")
	assert.Contains(t, err.Error(), "providers=0")
}

// TestFactory_NilProvidersMap：providers 字段为 nil（非空 map） → error。
func TestFactory_NilProvidersMap(t *testing.T) {
	logger, _ := newCapturingLogger()
	cfg := model.RegistryEntry{
		Default: "deepseek",
		// Providers 为 nil
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, m)
	assert.Empty(t, selected)
}

// TestFactory_EmptyDefault_FallbackByDictOrder：Default 字段为空字符串
// → 跳过 default 路径直接走 fallback，依然能返回字典序最小可用 entry。
func TestFactory_EmptyDefault_FallbackByDictOrder(t *testing.T) {
	logger, buf := newCapturingLogger()
	cfg := model.RegistryEntry{
		Default: "",
		Providers: map[string]model.ProviderEntry{
			"qwen":     usable("qwen"),
			"deepseek": usable("deepseek"),
		},
	}

	m, selected, err := model.NewDefaultModel(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "deepseek", selected, "字典序 deepseek < qwen")
	// Default 为空时也走 fallback warn 路径
	assert.True(t, strings.Contains(buf.String(), "fallback by dictionary order"))
}

// =====================================================================
// 表驱动：isProviderUsable 4 条件
//
// isProviderUsable 是包内私有函数，无法直接测试；通过 NewDefaultModel 的可观测
// 行为间接覆盖每条 boolean 失败路径。
// =====================================================================

func TestFactory_IsProviderUsable_AllConditions(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*model.ProviderEntry)
		usable  bool
		comment string
	}{
		{"all_set", func(e *model.ProviderEntry) {}, true, "全部字段非空"},
		{"disabled", func(e *model.ProviderEntry) { e.Enabled = false }, false, "Enabled=false"},
		{"empty_api_key", func(e *model.ProviderEntry) { e.APIKey = "" }, false, "APIKey 空"},
		{"empty_base_url", func(e *model.ProviderEntry) { e.BaseURL = "" }, false, "BaseURL 空"},
		{"empty_model", func(e *model.ProviderEntry) { e.Model = "" }, false, "Model 空"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := newCapturingLogger()
			entry := usable("deepseek")
			tc.mutate(&entry)

			cfg := model.RegistryEntry{
				Default: "deepseek",
				Providers: map[string]model.ProviderEntry{
					"deepseek": entry,
				},
			}
			_, selected, err := model.NewDefaultModel(cfg, logger)
			if tc.usable {
				require.NoError(t, err, tc.comment)
				assert.Equal(t, "deepseek", selected, tc.comment)
			} else {
				require.Error(t, err, tc.comment)
				assert.Empty(t, selected, tc.comment)
			}
		})
	}
}
