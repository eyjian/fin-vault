package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// DataProvidersConfig 多 API 服务商配置（与 bootstrap.DataProvidersConfig 对应，
// 但定义在 handler 包中避免循环引用）。
type DataProvidersConfig struct {
	Tushare TushareConfig `json:"tushare"`
}

// TushareConfig Tushare Pro API 配置。
type TushareConfig struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	BaseURL string `json:"base_url"`
}

// AIProviderConfig AI 服务商配置（如 DeepSeek、OpenAI 等）。
type AIProviderConfig struct {
	Name    string `json:"name"`     // 服务商标识：deepseek / openai / ...
	Enabled bool   `json:"enabled"`  // 是否启用
	APIKey  string `json:"api_key"`  // API Key（脱敏显示）
	BaseURL string `json:"base_url"` // 自定义 API 地址（可选）
	Model   string `json:"model"`    // 默认模型名称（可选）
}

// ConfigSaver 持久化配置的回调接口（由 wire 注入 bootstrap.SaveConfig）。
type ConfigSaver interface {
	SaveDataProviders(cfg *DataProvidersConfig) error
	GetDataProviders() *DataProvidersConfig
	// AI 服务商配置
	SaveAIProviders(providers []AIProviderConfig) error
	GetAIProviders() []AIProviderConfig
	// LLM 默认服务商
	SaveLLMDefault(defaultProvider string) error
	GetLLMDefault() string
}

// ConfigHandler 后端配置 HTTP 适配（用于前端设置页读写 API 服务商配置等）。
type ConfigHandler struct {
	saver ConfigSaver
}

// NewConfigHandler 构造。saver 实现 ConfigSaver 接口（通常由 bootstrap 包提供）。
func NewConfigHandler(saver ConfigSaver) *ConfigHandler {
	return &ConfigHandler{saver: saver}
}

// Register 挂载路由（v1 group）。
func (h *ConfigHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/config")
	g.GET("/data_providers", h.getDataProviders)
	g.PUT("/data_providers", h.updateDataProviders)
	// AI 服务商配置
	g.GET("/ai_providers", h.getAIProviders)
	g.PUT("/ai_providers", h.updateAIProviders)
	// LLM 默认服务商
	g.GET("/llm_default", h.getLLMDefault)
	g.PUT("/llm_default", h.updateLLMDefault)
}

// getDataProviders GET /config/data_providers
//
// 返回当前 data_providers 配置（脱敏 token：仅显示前 4 位 + ***）。
func (h *ConfigHandler) getDataProviders(c *gin.Context) {
	dp := h.saver.GetDataProviders()
	// 脱敏：token 只显示前 4 位
	masked := DataProvidersConfig{
		Tushare: TushareConfig{
			Enabled: dp.Tushare.Enabled,
			Token:   maskToken(dp.Tushare.Token),
			BaseURL: dp.Tushare.BaseURL,
		},
	}
	response.OK(c, gin.H{"data_providers": masked})
}

// updateDataProviders PUT /config/data_providers
//
// 更新 data_providers 配置并持久化到 config.yaml。
// 请求体：{"data_providers": {"tushare": {"enabled": true, "token": "xxx", "base_url": "..."}}}
func (h *ConfigHandler) updateDataProviders(c *gin.Context) {
	var req struct {
		DataProviders DataProvidersConfig `json:"data_providers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}

	dp := &DataProvidersConfig{
		Tushare: TushareConfig{
			Enabled: req.DataProviders.Tushare.Enabled,
			Token:   req.DataProviders.Tushare.Token,
			BaseURL: req.DataProviders.Tushare.BaseURL,
		},
	}

	// 持久化
	if err := h.saver.SaveDataProviders(dp); err != nil {
		response.Fail(c, errs.ErrInternal.WithCause(err))
		return
	}

	// 返回脱敏结果
	saved := h.saver.GetDataProviders()
	masked := DataProvidersConfig{
		Tushare: TushareConfig{
			Enabled: saved.Tushare.Enabled,
			Token:   maskToken(saved.Tushare.Token),
			BaseURL: saved.Tushare.BaseURL,
		},
	}
	response.OK(c, gin.H{"data_providers": masked})
}

// maskToken 对 token 做脱敏处理：保留前 4 位，其余用 *** 替代。
func maskToken(token string) string {
	if len(token) <= 4 {
		if token == "" {
			return ""
		}
		return "***"
	}
	return token[:4] + "***"
}

// getAIProviders GET /config/ai_providers
//
// 返回所有 AI 服务商配置（脱敏 api_key：仅显示前 4 位 + ***）。
func (h *ConfigHandler) getAIProviders(c *gin.Context) {
	providers := h.saver.GetAIProviders()
	masked := make([]AIProviderConfig, 0, len(providers))
	for _, p := range providers {
		masked = append(masked, AIProviderConfig{
			Name:    p.Name,
			Enabled: p.Enabled,
			APIKey:  maskToken(p.APIKey),
			BaseURL: p.BaseURL,
			Model:   p.Model,
		})
	}
	response.OK(c, gin.H{"ai_providers": masked})
}

// updateAIProviders PUT /config/ai_providers
//
// 更新 AI 服务商配置并持久化。
// 请求体：{"ai_providers": [{"name": "deepseek", "enabled": true, "api_key": "xxx", ...}]}
func (h *ConfigHandler) updateAIProviders(c *gin.Context) {
	var req struct {
		AIProviders []AIProviderConfig `json:"ai_providers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}

	if err := h.saver.SaveAIProviders(req.AIProviders); err != nil {
		response.Fail(c, errs.ErrInternal.WithCause(err))
		return
	}

	// 返回脱敏结果
	saved := h.saver.GetAIProviders()
	masked := make([]AIProviderConfig, 0, len(saved))
	for _, p := range saved {
		masked = append(masked, AIProviderConfig{
			Name:    p.Name,
			Enabled: p.Enabled,
			APIKey:  maskToken(p.APIKey),
			BaseURL: p.BaseURL,
			Model:   p.Model,
		})
	}
	response.OK(c, gin.H{"ai_providers": masked})
}

// getLLMDefault GET /config/llm_default
//
// 返回 LLM 默认服务商名称。
func (h *ConfigHandler) getLLMDefault(c *gin.Context) {
	defaultProvider := h.saver.GetLLMDefault()
	response.OK(c, gin.H{"llm_default": defaultProvider})
}

// updateLLMDefault PUT /config/llm_default
//
// 更新 LLM 默认服务商并持久化。
// 请求体：{"llm_default": "deepseek"}
func (h *ConfigHandler) updateLLMDefault(c *gin.Context) {
	var req struct {
		LLMDefault string `json:"llm_default"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}

	if req.LLMDefault == "" {
		response.Fail(c, errs.ErrInvalidParam.WithCause(fmt.Errorf("llm_default is empty")))
		return
	}

	if err := h.saver.SaveLLMDefault(req.LLMDefault); err != nil {
		response.Fail(c, errs.ErrInternal.WithCause(err))
		return
	}
	response.OK(c, gin.H{"llm_default": req.LLMDefault})
}
