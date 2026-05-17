package model

// ProviderEntry 单个 OpenAI 兼容 LLM Provider 配置（factory 输入版本）。
//
// 与 ProviderConfig（mapstructure 反序列化版本）字段同构，但**独立定义**：
//   - §7.1 自研 llm.Provider/Registry 整包退场后，model 包零受损
//   - bootstrap 装配层通过 RegistryConfig.ToRegistryEntry() 把反序列化版本
//     转成 factory 用的 RegistryEntry，由 §9 实装该装配点
//
// 字段语义：
//   - Enabled：默认 true（bootstrap 转换时把 *bool nil 视为 true）
//   - APIKey/BaseURL/Model：构造 SDK Model 必需，缺一即视为不可用
//   - Temperature/MaxTokens/TimeoutSec：本期 factory 不消费，保留以便后续在
//     llmagent 层注入；不影响选 provider 的判断
type ProviderEntry struct {
	Enabled     bool
	APIKey      string
	BaseURL     string
	Model       string
	Temperature float32
	MaxTokens   int
	TimeoutSec  int
}

// RegistryEntry 顶层 LLM 配置（factory 输入版本，与 cfg.LLM 段语义对齐）。
//
// 字段语义：
//   - Default：首选 provider 名；缺失或不可用时 factory 按字典序 fallback
//   - Providers：name → entry 映射；空 map 时 factory 直接 error
type RegistryEntry struct {
	Default   string
	Providers map[string]ProviderEntry
}

// =====================================================================
// 配置反序列化版本（用于 viper mapstructure 反序列化）
// =====================================================================

// ProviderConfig 单个 OpenAI 兼容 LLM Provider 的反序列化配置（与
// cfg.llm.providers.<name> 段对齐）。
//
// 与 ProviderEntry 的差异：
//   - Enabled 为 *bool 三态：nil/未设置 视为启用（向后兼容旧配置），
//     true=启用，false=显式禁用；通过 IsEnabled() 方法读取
//   - 持有 mapstructure / yaml tag，专供 viper 反序列化使用
//
// 装配阶段会通过 RegistryConfig.ToRegistryEntry() 转成 ProviderEntry，
// 解 *bool → bool 默认语义后喂给 factory.NewDefaultModel。
type ProviderConfig struct {
	Enabled     *bool   `mapstructure:"enabled"      yaml:"enabled"`
	APIKey      string  `mapstructure:"api_key"      yaml:"api_key"`
	BaseURL     string  `mapstructure:"base_url"     yaml:"base_url"`
	Model       string  `mapstructure:"model"        yaml:"model"`
	Temperature float32 `mapstructure:"temperature"  yaml:"temperature"`
	MaxTokens   int     `mapstructure:"max_tokens"   yaml:"max_tokens"`
	TimeoutSec  int     `mapstructure:"timeout_sec"  yaml:"timeout_sec"`
}

// IsEnabled 返回该 Provider 是否启用。enabled 未设置时默认 true（向后兼容旧配置）。
func (c ProviderConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// RegistryConfig LLM 配置反序列化版本（与 cfg.LLM 段对齐）。
//
// bootstrap 加载完成后调用 ToRegistryEntry() 转成 factory 用的 RegistryEntry。
type RegistryConfig struct {
	Default   string                    `mapstructure:"default"   yaml:"default"`
	Providers map[string]ProviderConfig `mapstructure:"providers" yaml:"providers"`
}

// ToRegistryEntry 把反序列化版本转换为 factory 输入版本。
//
// Enabled *bool 通过 IsEnabled() 兼容 nil→true 语义；其余字段直接拷贝。
// 不做"是否有 api_key/base_url"等业务校验——那是 factory 层的责任。
func (c RegistryConfig) ToRegistryEntry() RegistryEntry {
	out := RegistryEntry{
		Default:   c.Default,
		Providers: make(map[string]ProviderEntry, len(c.Providers)),
	}
	for name, p := range c.Providers {
		out.Providers[name] = ProviderEntry{
			Enabled:     p.IsEnabled(),
			APIKey:      p.APIKey,
			BaseURL:     p.BaseURL,
			Model:       p.Model,
			Temperature: p.Temperature,
			MaxTokens:   p.MaxTokens,
			TimeoutSec:  p.TimeoutSec,
		}
	}
	return out
}
