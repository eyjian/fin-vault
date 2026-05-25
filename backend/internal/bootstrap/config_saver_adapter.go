package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/handler"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// configSaverAdapter 实现 handler.ConfigSaver 接口，
// 桥接 handler 和 bootstrap 包（避免循环引用）。
type configSaverAdapter struct {
	cfg           *Config
	sysConfigRepo repository.SysConfigRepository
}

// Verify interface compliance。
var _ handler.ConfigSaver = (*configSaverAdapter)(nil)

// NewConfigSaverAdapter 构造适配器。
func NewConfigSaverAdapter(cfg *Config, sysConfigRepo repository.SysConfigRepository) *configSaverAdapter {
	return &configSaverAdapter{cfg: cfg, sysConfigRepo: sysConfigRepo}
}

// SaveDataProviders 更新 data_providers 配置并持久化（DB + config.yaml）。
func (a *configSaverAdapter) SaveDataProviders(dp *handler.DataProvidersConfig) error {
	a.cfg.DataProviders.Tushare.Enabled = dp.Tushare.Enabled
	if dp.Tushare.Token != "" {
		a.cfg.DataProviders.Tushare.Token = dp.Tushare.Token
	}
	if dp.Tushare.BaseURL != "" {
		a.cfg.DataProviders.Tushare.BaseURL = dp.Tushare.BaseURL
	}

	// 持久化到 DB
	ctx := context.Background()
	if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
		Category: domain.SysConfigCategoryTushare,
		Key:      "enabled",
		Value:    boolToStr(dp.Tushare.Enabled),
	}); err != nil {
		slog.Error("save tushare enabled to db failed", "err", err)
	}
	if dp.Tushare.Token != "" {
		if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
			Category: domain.SysConfigCategoryTushare,
			Key:      "token",
			Value:    dp.Tushare.Token,
		}); err != nil {
			slog.Error("save tushare token to db failed", "err", err)
		}
	}
	if dp.Tushare.BaseURL != "" {
		if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
			Category: domain.SysConfigCategoryTushare,
			Key:      "base_url",
			Value:    dp.Tushare.BaseURL,
		}); err != nil {
			slog.Error("save tushare base_url to db failed", "err", err)
		}
	}

	// 同时持久化到 config.yaml（保留文件与 DB 的双向同步）
	if err := SaveConfig(a.cfg); err != nil {
		slog.Error("save config to yaml failed", "err", err)
		return err
	}
	return nil
}

// GetDataProviders 返回当前 data_providers 配置（优先从 DB 读取）。
func (a *configSaverAdapter) GetDataProviders() *handler.DataProvidersConfig {
	ctx := context.Background()
	dp := &a.cfg.DataProviders

	// 从 DB 覆盖
	if enabledEntry, err := a.sysConfigRepo.Get(ctx, domain.SysConfigCategoryTushare, "enabled"); err == nil && enabledEntry != nil {
		if v, err := strToBool(enabledEntry.Value); err == nil {
			dp.Tushare.Enabled = v
		}
	}
	if tokenEntry, err := a.sysConfigRepo.Get(ctx, domain.SysConfigCategoryTushare, "token"); err == nil && tokenEntry != nil {
		dp.Tushare.Token = tokenEntry.Value
	}
	if baseURL, err := a.sysConfigRepo.Get(ctx, domain.SysConfigCategoryTushare, "base_url"); err == nil && baseURL != nil && baseURL.Value != "" {
		dp.Tushare.BaseURL = baseURL.Value
	}

	return &handler.DataProvidersConfig{
		Tushare: handler.TushareConfig{
			Enabled: dp.Tushare.Enabled,
			Token:   dp.Tushare.Token,
			BaseURL: dp.Tushare.BaseURL,
		},
	}
}

// SaveAIProviders 更新 AI 服务商配置并持久化到 DB。
func (a *configSaverAdapter) SaveAIProviders(providers []handler.AIProviderConfig) error {
	ctx := context.Background()
	for _, p := range providers {
		category := domain.SysConfigCategoryDeepSeek // 默认用 deepseek 分类
		if p.Name != "deepseek" {
			category = domain.SysConfigCategoryLLM
		}

		if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
			Category: category,
			Key:      p.Name + "_enabled",
			Value:    boolToStr(p.Enabled),
		}); err != nil {
			slog.Error("save ai provider enabled to db failed", "provider", p.Name, "err", err)
		}
		if p.APIKey != "" {
			if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
				Category: category,
				Key:      p.Name + "_api_key",
				Value:    p.APIKey,
			}); err != nil {
				slog.Error("save ai provider api_key to db failed", "provider", p.Name, "err", err)
			}
		}
		if p.BaseURL != "" {
			if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
				Category: category,
				Key:      p.Name + "_base_url",
				Value:    p.BaseURL,
			}); err != nil {
				slog.Error("save ai provider base_url to db failed", "provider", p.Name, "err", err)
			}
		}
		if p.Model != "" {
			if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
				Category: category,
				Key:      p.Name + "_model",
				Value:    p.Model,
			}); err != nil {
				slog.Error("save ai provider model to db failed", "provider", p.Name, "err", err)
			}
		}
	}
	return nil
}

// GetAIProviders 从 DB 读取所有 AI 服务商配置。
func (a *configSaverAdapter) GetAIProviders() []handler.AIProviderConfig {
	ctx := context.Background()
	var providers []handler.AIProviderConfig

	// DeepSeek
	if ds, err := a.buildAIProvider(ctx, domain.SysConfigCategoryDeepSeek, "deepseek"); err == nil && ds != nil {
		providers = append(providers, *ds)
	}

	// LLM 分类下的其他 provider（未来扩展）
	if llmList, err := a.sysConfigRepo.GetByCategory(ctx, domain.SysConfigCategoryLLM); err == nil {
		seen := map[string]bool{"deepseek_enabled": true, "deepseek_api_key": true,
			"deepseek_base_url": true, "deepseek_model": true, "default": true}
		extraProviders := extractExtraProviders(llmList, seen)
		providers = append(providers, extraProviders...)
	}

	return providers
}

// SaveLLMDefault 更新 LLM 默认服务商并持久化到 DB。
func (a *configSaverAdapter) SaveLLMDefault(defaultProvider string) error {
	ctx := context.Background()
	if err := a.sysConfigRepo.Upsert(ctx, &repository.SysConfigEntry{
		Category: domain.SysConfigCategoryLLM,
		Key:      "default",
		Value:    defaultProvider,
	}); err != nil {
		slog.Error("save llm default to db failed", "err", err)
		return err
	}
	a.cfg.LLM.Default = defaultProvider
	return nil
}

// GetLLMDefault 从 DB 读取 LLM 默认服务商。
func (a *configSaverAdapter) GetLLMDefault() string {
	ctx := context.Background()
	if entry, err := a.sysConfigRepo.Get(ctx, domain.SysConfigCategoryLLM, "default"); err == nil && entry != nil && entry.Value != "" {
		return entry.Value
	}
	return a.cfg.LLM.Default
}

// buildAIProvider 从 DB 构造单个 AI 服务商配置。
func (a *configSaverAdapter) buildAIProvider(ctx context.Context, category, name string) (*handler.AIProviderConfig, error) {
	enabledEntry, _ := a.sysConfigRepo.Get(ctx, category, name+"_enabled")
	apiKeyEntry, _ := a.sysConfigRepo.Get(ctx, category, name+"_api_key")
	baseURLEntry, _ := a.sysConfigRepo.Get(ctx, category, name+"_base_url")
	modelEntry, _ := a.sysConfigRepo.Get(ctx, category, name+"_model")

	cfg := &handler.AIProviderConfig{Name: name}
	if enabledEntry != nil {
		cfg.Enabled, _ = strToBool(enabledEntry.Value)
	}
	if apiKeyEntry != nil {
		cfg.APIKey = apiKeyEntry.Value
	}
	if baseURLEntry != nil {
		cfg.BaseURL = baseURLEntry.Value
	}
	if modelEntry != nil {
		cfg.Model = modelEntry.Value
	}

	// 如果所有字段都为空，返回 nil 表示不存在
	if cfg.APIKey == "" && cfg.BaseURL == "" && !cfg.Enabled {
		return nil, nil
	}
	return cfg, nil
}

// extractExtraProviders 从 LLM 分类的配置项中提取额外的 AI 服务商。
func extractExtraProviders(entries []repository.SysConfigEntry, seen map[string]bool) []handler.AIProviderConfig {
	// 按服务商名分组
	groups := map[string]*handler.AIProviderConfig{}
	for _, e := range entries {
		if seen[e.Key] {
			continue
		}
		name, field := parseAIProviderKey(e.Key)
		if name == "" {
			continue
		}
		if groups[name] == nil {
			groups[name] = &handler.AIProviderConfig{Name: name}
		}
		switch field {
		case "enabled":
			groups[name].Enabled, _ = strToBool(e.Value)
		case "api_key":
			groups[name].APIKey = e.Value
		case "base_url":
			groups[name].BaseURL = e.Value
		case "model":
			groups[name].Model = e.Value
		}
	}

	var result []handler.AIProviderConfig
	for _, g := range groups {
		result = append(result, *g)
	}
	return result
}

// parseAIProviderKey 解析 "openai_enabled" → ("openai", "enabled")。
func parseAIProviderKey(key string) (name, field string) {
	for _, suffix := range []string{"_enabled", "_api_key", "_base_url", "_model"} {
		if len(key) > len(suffix) && key[len(key)-len(suffix):] == suffix {
			return key[:len(key)-len(suffix)], suffix[1:]
		}
	}
	return "", ""
}

// boolToStr / strToBool 辅助函数。
func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func strToBool(s string) (bool, error) {
	switch s {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no", "":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool string: %q", s)
	}
}
