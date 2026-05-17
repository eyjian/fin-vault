package model

// ProviderEntry 单个 OpenAI 兼容 LLM Provider 配置。
//
// 字段与 internal/llm/config.go 的 ProviderConfig 同构，但**独立定义**：
//   - §7.1 自研 llm.Provider/Registry 整包退场后，model 包零受损
//   - bootstrap 装配层负责把 cfg.LLM (llm.RegistryConfig) 转成 model.RegistryEntry，
//     由 §9 实现该转换函数
//
// 字段语义：
//   - Enabled：默认 true（bootstrap 转换时可把 *bool nil 视为 true）
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

// RegistryEntry 顶层 LLM 配置（与 cfg.LLM 段对齐）。
//
// 字段语义：
//   - Default：首选 provider 名；缺失或不可用时 factory 按字典序 fallback
//   - Providers：name → entry 映射；空 map 时 factory 直接 error
type RegistryEntry struct {
	Default   string
	Providers map[string]ProviderEntry
}
