package llm

import (
	"fmt"
	"sort"
)

// =====================================================================
// staticRegistry —— 启动时注册、运行时只读的多 Provider 路由
// =====================================================================

type staticRegistry struct {
	def       string
	providers map[string]Provider
}

// NewRegistry 根据配置构造 LLM Registry。
//
// 行为：
//   - 跳过 api_key 与 base_url 都为空的 Provider 配置（视为未启用）
//   - 至少要有一个有效 Provider；默认 Provider 不在 providers 里时取首个有效项
//   - Provider 构造失败仅记错跳过（避免 1 个 key 配错让全部 AI 不可用）
func NewRegistry(cfg RegistryConfig) (Registry, error) {
	r := &staticRegistry{
		def:       cfg.Default,
		providers: make(map[string]Provider, len(cfg.Providers)),
	}
	var firstAvail string
	for name, pc := range cfg.Providers {
		// 占位/未配置的 Provider 跳过，便于本地只填 1 个就能跑通
		if pc.APIKey == "" && pc.BaseURL == "" {
			continue
		}
		p, err := NewOpenAIProvider(name, pc)
		if err != nil {
			// 配置错误的 Provider 跳过（slog 由调用方记录）
			continue
		}
		r.providers[name] = p
		if firstAvail == "" {
			firstAvail = name
		}
	}
	if len(r.providers) == 0 {
		return nil, ErrProviderEmpty
	}
	if r.def == "" || r.providers[r.def] == nil {
		r.def = firstAvail
	}
	return r, nil
}

// Get 按名称取 Provider；name 为空走默认。
func (r *staticRegistry) Get(name string) (Provider, error) {
	if name == "" {
		name = r.def
	}
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return p, nil
}

// Default 默认 Provider 名。
func (r *staticRegistry) Default() string { return r.def }

// List 列出所有已注册 Provider，按名字排序，便于前端展示。
func (r *staticRegistry) List() []ProviderInfo {
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ProviderInfo, 0, len(names))
	for _, n := range names {
		p := r.providers[n]
		out = append(out, ProviderInfo{
			Name:      n,
			Model:     p.Model(),
			IsDefault: n == r.def,
		})
	}
	return out
}
