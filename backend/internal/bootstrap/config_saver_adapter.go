package bootstrap

import (
	"log/slog"

	"github.com/eyjian/fin-vault/backend/internal/handler"
)

// configSaverAdapter 实现 handler.ConfigSaver 接口，
// 桥接 handler 和 bootstrap 包（避免循环引用）。
type configSaverAdapter struct {
	cfg *Config
}

// Verify interface compliance。
var _ handler.ConfigSaver = (*configSaverAdapter)(nil)

// NewConfigSaverAdapter 构造适配器。
func NewConfigSaverAdapter(cfg *Config) *configSaverAdapter {
	return &configSaverAdapter{cfg: cfg}
}

// SaveDataProviders 更新 data_providers 配置并持久化。
func (a *configSaverAdapter) SaveDataProviders(dp *handler.DataProvidersConfig) error {
	a.cfg.DataProviders.Tushare.Enabled = dp.Tushare.Enabled
	if dp.Tushare.Token != "" {
		a.cfg.DataProviders.Tushare.Token = dp.Tushare.Token
	}
	if dp.Tushare.BaseURL != "" {
		a.cfg.DataProviders.Tushare.BaseURL = dp.Tushare.BaseURL
	}
	if err := SaveConfig(a.cfg); err != nil {
		slog.Error("save config failed", "err", err)
		return err
	}
	return nil
}

// GetDataProviders 返回当前 data_providers 配置。
func (a *configSaverAdapter) GetDataProviders() *handler.DataProvidersConfig {
	dp := &a.cfg.DataProviders
	return &handler.DataProvidersConfig{
		Tushare: handler.TushareConfig{
			Enabled: dp.Tushare.Enabled,
			Token:   dp.Tushare.Token,
			BaseURL: dp.Tushare.BaseURL,
		},
	}
}
