// Package model 提供 trpc-agent-go SDK 的 Model 工厂。
//
// 与铁律 F2 / D12 强一致：本包是业务层访问 trpc-agent-go SDK 的少数几个
// 物理隔离点之一（另一个是 internal/llm/agent/）。service/handler/domain/repo
// 层禁止 import trpc-agent-go，通过 model.NewDefaultModel 拿到 SDK Model 实例
// 后传入 agent.Runner，由 agent 包负责所有 SDK 交互。
package model

import (
	"fmt"
	"log/slog"
	"sort"

	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"
	sdkopenai "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// NewDefaultModel 按 RegistryEntry 配置生成 OpenAI 兼容的 SDK Model。
//
// 选择策略（与 spec ai-agent-runtime "default 不可用按字典序 fallback" 对齐）：
//  1. 若 cfg.Default 非空且在 cfg.Providers 中存在且可用 → 直接使用，
//     logger.Info 记录 "selected (default)"。
//  2. 否则按 cfg.Providers map keys 字典序遍历，取第一个可用的，
//     logger.Warn 记录 "fallback by dictionary order"。
//  3. 若全部不可用，返回非 nil error（business code 由调用方决定，
//     一般会让 §9 装配阶段直接退出）。
//
// "可用"判定 = isProviderUsable：Enabled && APIKey != "" && BaseURL != "" && Model != ""。
//
// 关于 Variant：本工厂不显式传 sdkopenai.WithVariant，由 SDK
// inferVariant(baseURL) 自动推断（DeepSeek baseURL → VariantDeepSeek，
// 其它 → VariantOpenAI）。Kimi/GLM/Qwen/Ollama 走 OpenAI 兼容协议，
// 自动推断 VariantOpenAI 即可；如未来某 provider 需要专用 Variant
// （例如 hunyuan），后续在 buildOpenAIModel 内按 baseURL 增加分支。
//
// 返回值：
//   - sdkmodel.Model：实际可调用的 SDK 实例（*sdkopenai.Model 隐式实现该接口）
//   - selected：实际选定的 provider 名（便于上层日志/指标）
//   - error：全不可用时返回，含 default 与 providers 总数信息
func NewDefaultModel(cfg RegistryEntry, logger *slog.Logger) (sdkmodel.Model, string, error) {
	// 1) Default 路径
	if cfg.Default != "" {
		if entry, ok := cfg.Providers[cfg.Default]; ok && isProviderUsable(entry) {
			logger.Info("llm provider selected (default)",
				"provider", cfg.Default, "model", entry.Model)
			return buildOpenAIModel(entry), cfg.Default, nil
		}
	}

	// 2) Fallback：按 providers map keys 字典序遍历
	keys := make([]string, 0, len(cfg.Providers))
	for k := range cfg.Providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := cfg.Providers[k]
		if !isProviderUsable(entry) {
			continue
		}
		logger.Warn("llm default provider unavailable, fallback by dictionary order",
			"default", cfg.Default, "selected", k, "model", entry.Model)
		return buildOpenAIModel(entry), k, nil
	}

	// 3) 全不可用
	return nil, "", fmt.Errorf(
		"no usable llm provider in config (default=%q, providers=%d)",
		cfg.Default, len(cfg.Providers),
	)
}

// isProviderUsable 综合 Enabled / APIKey / BaseURL / Model 四条件判断 entry 是否可构造 SDK Model。
//
// 任一字段空值即视为不可用；调用方据此决定走 default 还是 fallback。
func isProviderUsable(e ProviderEntry) bool {
	return e.Enabled && e.APIKey != "" && e.BaseURL != "" && e.Model != ""
}

// buildOpenAIModel 调 SDK 构造 *sdkopenai.Model。
//
// 第一参数 e.Model 是 SDK 内部 modelName（例如 "deepseek-v4-pro"），
// 不是 provider key（例如 "deepseek"）。SDK Variant 由 inferVariant(BaseURL)
// 自动推断，不显式传 WithVariant。
//
// 不消费 e.Temperature/MaxTokens/TimeoutSec：这些 SDK Option 在 llmagent 层注入。
func buildOpenAIModel(e ProviderEntry) sdkmodel.Model {
	return sdkopenai.New(
		e.Model,
		sdkopenai.WithAPIKey(e.APIKey),
		sdkopenai.WithBaseURL(e.BaseURL),
	)
}
