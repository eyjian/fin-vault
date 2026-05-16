package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// AIMetaHandler 暴露 LLM Provider 列表等元信息。
type AIMetaHandler struct {
	registry llm.Registry
}

// NewAIMetaHandler 构造。
func NewAIMetaHandler(reg llm.Registry) *AIMetaHandler {
	return &AIMetaHandler{registry: reg}
}

// Register 挂载到 /api/v1。
func (h *AIMetaHandler) Register(r *gin.RouterGroup) {
	r.GET("/ai/providers", h.ListProviders)
}

// ListProviders GET /api/v1/ai/providers
func (h *AIMetaHandler) ListProviders(c *gin.Context) {
	if h.registry == nil {
		response.OK(c, []llm.ProviderInfo{})
		return
	}
	response.OK(c, h.registry.List())
}
