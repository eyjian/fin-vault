package handler

import (
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

// ConfigSaver 持久化配置的回调接口（由 wire 注入 bootstrap.SaveConfig）。
type ConfigSaver interface {
	SaveDataProviders(cfg *DataProvidersConfig) error
	GetDataProviders() *DataProvidersConfig
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
