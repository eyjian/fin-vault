package handler

import (
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/llm/model"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// ProviderInfo 暴露给前端的 LLM Provider 元信息。
//
// 字段沿用历史定义（Name / Model / IsDefault）+ 本期 §7.1 新增 Enabled，
// 满足 main 决策 A.II "列出 provider names + enabled 状态"。
type ProviderInfo struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	IsDefault bool   `json:"is_default"`
	Enabled   bool   `json:"enabled"`
}

// AIMetaHandler 暴露 LLM Provider 列表等元信息。
//
// §7.1 之后直接消费 model.RegistryConfig，不再依赖自研 Registry 抽象；
// cfg.Providers 为空时 ListProviders 返回空数组（与原 nil registry 降级路径行为一致）。
type AIMetaHandler struct {
	cfg model.RegistryConfig
}

// NewAIMetaHandler 构造。
func NewAIMetaHandler(cfg model.RegistryConfig) *AIMetaHandler {
	return &AIMetaHandler{cfg: cfg}
}

// Register 挂载到 /api/v1。
func (h *AIMetaHandler) Register(r *gin.RouterGroup) {
	r.GET("/ai/providers", h.ListProviders)
}

// ListProviders GET /api/v1/ai/providers
//
// 排序：按 provider name 字典序（与 model factory 的字典序 fallback 一致）。
// IsDefault：与 cfg.Default 对比；若 Default 为空或不在 Providers 里，
// 输出全 false（与 factory fallback 行为对应——本期不在 handler 复制 fallback 逻辑）。
func (h *AIMetaHandler) ListProviders(c *gin.Context) {
	names := make([]string, 0, len(h.cfg.Providers))
	for n := range h.cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]ProviderInfo, 0, len(names))
	for _, n := range names {
		p := h.cfg.Providers[n]
		out = append(out, ProviderInfo{
			Name:      n,
			Model:     p.Model,
			IsDefault: n == h.cfg.Default,
			Enabled:   p.IsEnabled(),
		})
	}
	response.OK(c, out)
}
