package llm

// ProviderConfig 单个 LLM Provider 配置。
//
// 注意：本结构由 bootstrap 层 viper 加载后传入；所有 OpenAI 兼容协议的国内厂商
// （DeepSeek/GLM/Kimi/通义/Ollama 等）都通过修改 BaseURL + Model 即可接入。
type ProviderConfig struct {
	Enabled     *bool   `mapstructure:"enabled"      yaml:"enabled"` // 显式开关；nil/未设置 视为启用（向后兼容）
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

// RegistryConfig LLMRegistry 配置（与 cfg.LLM 段对齐）。
type RegistryConfig struct {
	Default   string                    `mapstructure:"default"   yaml:"default"`
	Providers map[string]ProviderConfig `mapstructure:"providers" yaml:"providers"`
}
