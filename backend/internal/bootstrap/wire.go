package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/cache"
	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/handler"
	llmmodel "github.com/eyjian/fin-vault/backend/internal/llm/model"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/internal/platformapi"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	gormrepo "github.com/eyjian/fin-vault/backend/internal/repository/gorm"
	"github.com/eyjian/fin-vault/backend/internal/service"
)

// App 是组装好的应用实例，main.go 拿到后挂到 HTTP server 即可。
type App struct {
	Cfg        *Config
	DB         *gorm.DB
	Cache      cache.Provider
	Repos      *repository.Repositories
	Aggregator *platformapi.QuoteAggregator
	Cron       *CronManager
	Handlers   *Handlers
}

// Handlers 集中管理全部 Gin Handler。
type Handlers struct {
	Meta        *handler.MetaHandler
	Asset       *handler.AssetHandler
	Holding     *handler.HoldingHandler
	Transaction *handler.TransactionHandler
	Quote       *handler.QuoteHandler
	Rate        *handler.RateHandler
	Export      *handler.ExportHandler
	AIMeta      *handler.AIMetaHandler

	// AISession / AIMessage 在 §9 装配 SessionStore + Runner 后才非 nil。
	// 当前 §8 阶段两者均为 nil，路由注册侧用 nil-check 兼容（router.go）。
	AISession *handler.AISessionHandler
	AIMessage *handler.AIMessageHandler

	// AIPulseDiagnosis：D16 LLM 不可用时为 nil，router 条件挂载兜底（不注册路由 → 404）。
	AIPulseDiagnosis *handler.PulseDiagnosisHandler

	// Config：后端配置 HTTP 适配（设置页数据源配置等）。
	Config *handler.ConfigHandler
}

// Wire 总装：DB → Repo → Service → Handler。
//
// 任何子组件构造失败均直接返回错误。
// LLM 相关装配（factory.NewDefaultModel / Runner 等）由 §9 实装；
// 当前阶段只做 AIMeta 的元信息暴露，cfg.LLM.Providers 为空时
// /api/v1/ai/providers 返回空数组（降级行为兼容历史）。
func Wire(cfg *Config) (*App, error) {
	// 1. DB
	db, err := NewDB(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}

	// 2. Cache
	cacheProv := NewCache(cfg.Cache)

	// 3. Repository 层
	repos := &repository.Repositories{
		UoW:            gormrepo.NewUnitOfWork(db),
		User:           gormrepo.NewUserRepository(db),
		Platform:       gormrepo.NewPlatformRepository(db),
		Asset:          gormrepo.NewAssetRepository(db),
		Holding:        gormrepo.NewHoldingRepository(db),
		Transaction:    gormrepo.NewTransactionRepository(db),
		CostLot:        gormrepo.NewCostLotRepository(db),
		Portfolio:      gormrepo.NewPortfolioRepository(db),
		Quote:          gormrepo.NewQuoteRepository(db),
		Rate:           gormrepo.NewRateRepository(db),
		PulseDiagnosis: gormrepo.NewPulseDiagnosisRepository(db),
		SysConfig:      gormrepo.NewSysConfigRepository(db),
	}

	// 3.x 从 DB 加载系统配置覆盖 viper 的 DataProviders 和 LLM 配置
	applyDBConfigOverrides(cfg, repos.SysConfig)

	// 4. LLM 装配（§9 实装）
	//
	// providers 启动日志由 wireAI 接管（更精确：含 configured 列表 + default + reason），
	// 此处不再重复打 Warn。LLM 不可用时由 wireAI 走 D16 降级路径，AIMessage 保持 nil。

	// 5. PlatformAPI Aggregator（行情）
	httpTimeout := cfg.Quote.HTTPTimeout
	if httpTimeout <= 0 {
		httpTimeout = 5 * time.Second
	}
	aggregator, err := platformapi.NewAggregator(
		[]platformapi.QuoteFetcher{
			platformapi.NewEastmoneyFetcher(httpTimeout),
			platformapi.NewSinaFetcher(httpTimeout),
			platformapi.NewTencentFetcher(httpTimeout),
		},
		cfg.Quote.SourcePriority,
		cfg.Quote.PoolSize,
	)
	if err != nil {
		slog.Warn("quote aggregator disabled", "err", err)
		aggregator = nil
	}

	// 6. Tools 注册由 §9 装配阶段完成（agent.NewToolsetAgentFactory + Runner 装配）。

	// 6.x Asset Meta Fetcher（资产录入页"按代码自动填充"）
	//
	// 与 QuoteAggregator 解耦：仅供 AssetProbeService 使用，行情刷新链路不依赖。
	// 失败语义：fetcher 永远可构造（resty 客户端 + 默认 baseURL），不会返回 nil。
	metaFetcher := platformapi.NewEastmoneyMetaFetcher(httpTimeout)
	sinaMetaFetcher := platformapi.NewSinaMetaFetcher(httpTimeout)

	// Tushare 基金净值 Fetcher（需配置 token，否则 Supports 返回 false 被跳过）
	tushareToken := cfg.DataProviders.Tushare.Token
	if tushareToken == "" {
		// 尝试从环境变量读取
		tushareToken = os.Getenv("FINVAULT_DATA_PROVIDERS_TUSHARE_TOKEN")
	}
	var tushareMetaFetcher platformapi.AssetMetaFetcher
	if cfg.DataProviders.Tushare.Enabled && tushareToken != "" {
		tushareMetaFetcher = platformapi.NewTushareFundFetcher(httpTimeout, tushareToken)
	} else {
		if cfg.DataProviders.Tushare.Enabled {
			slog.Warn("tushare enabled but no token configured, skipping tushare fetcher")
		}
		tushareMetaFetcher = nil
	}

	// F10 enricher：A 股行业/板块/上市日补全。独立于主 fetcher 链路——
	// 即便 push2.eastmoney.com 被反爬封 IP（此时主源会降级到新浪），
	// datacenter.eastmoney.com 仍可访问，让降级路径也能享受 F10 补全。
	f10Enricher := platformapi.NewEastmoneyF10Enricher(httpTimeout)
	// 基金详情 enricher：基金公司/类型/业绩基准/风险等级/最新净值补全。同理与
	// pingzhongdata 解耦，走 api.fund.eastmoney.com 的 JJJBQK 接口，对部分新基金
	// 或 pingzhongdata 字段不全的情况是必要的补充源。
	fundDetailEnricher := platformapi.NewEastmoneyFundDetailEnricher(httpTimeout)

	// 7. Services
	holdingSvc := service.NewHoldingService(repos.Holding, repos.Asset, repos.Quote, repos.Rate, repos.Platform)
	assetSvc := service.NewAssetService(repos.UoW, repos.Asset, repos.Platform, holdingSvc)

	// 构造 fetcher 链：主源东方财富 → 新浪 → Tushare（仅基金净值）
	var probeFetchers []platformapi.AssetMetaFetcher
	probeFetchers = append(probeFetchers, metaFetcher)
	probeFetchers = append(probeFetchers, sinaMetaFetcher)
	if tushareMetaFetcher != nil {
		probeFetchers = append(probeFetchers, tushareMetaFetcher)
	}
	assetProbeSvc := service.NewAssetProbeService(probeFetchers...).
		WithEnrichers(f10Enricher, fundDetailEnricher)

	txnSvc := service.NewTransactionService(repos.UoW, repos.Transaction, repos.Holding, repos.Asset)
	quoteSvc := service.NewQuoteService(repos.Quote, repos.Asset, cacheProv, aggregator, cfg.Quote.CacheTTL)
	rateSvc := service.NewRateService(repos.Rate)
	exportSvc := service.NewExportService(repos.Holding, repos.Transaction, repos.Asset, repos.Platform, repos.Quote)
	matureSvc := service.NewMatureService(repos.UoW, repos.Holding, repos.Asset, repos.Transaction)

	// 8. Handlers
	handlers := &Handlers{
		Meta:        handler.NewMetaHandler(assetSvc, "v1.0-impl"),
		Asset:       handler.NewAssetHandler(assetSvc, assetProbeSvc),
		Holding:     handler.NewHoldingHandler(holdingSvc),
		Transaction: handler.NewTransactionHandler(txnSvc),
		Quote:       handler.NewQuoteHandler(quoteSvc),
		Rate:        handler.NewRateHandler(rateSvc),
		Export:      handler.NewExportHandler(exportSvc),
		AIMeta:      handler.NewAIMetaHandler(cfg.LLM),
	}

	// 8.x AI Session / AI Message handlers —— §9 装配（含 D16 LLM 不可用降级）。
	//
	// wireAI 内部完成：
	//   - 始终装 AISession（CRUD 不依赖 Runner）
	//   - 装 7 工具 + factory + Runner + AIMessage（正常路径）
	//   - LLM 不可用 → AIMessage 留 nil，router 条件挂载兜底（POST /messages 不注册 → 404）
	// e2e handler 单测自包装 httptest router，不依赖本装配链。
	sessionStore := session.NewSQLiteStore(db, cfg.AI.Session.HistoryWindow)
	var pulseSvc *service.PulseDiagnosisService
	handlers.AISession, handlers.AIMessage, pulseSvc = wireAI(cfg, repos, sessionStore, slog.Default())
	if pulseSvc != nil {
		handlers.AIPulseDiagnosis = handler.NewPulseDiagnosisHandler(pulseSvc, cfg.AI.PulseDiagnosis.Concurrency)
	}

	// 9.1 Config handler（设置页数据源配置读写）
	configSaver := NewConfigSaverAdapter(cfg, repos.SysConfig)
	handlers.Config = handler.NewConfigHandler(configSaver)

	// 9.2 Cron
	cm := NewCronManager(matureSvc, cfg.Cron.Mature)

	app := &App{
		Cfg:        cfg,
		DB:         db,
		Cache:      cacheProv,
		Repos:      repos,
		Aggregator: aggregator,
		Cron:       cm,
		Handlers:   handlers,
	}
	return app, nil
}

// Close 优雅关闭（cache、aggregator、cron）。
func (a *App) Close() {
	if a.Cron != nil {
		a.Cron.Stop()
	}
	if a.Aggregator != nil {
		a.Aggregator.Close()
	}
	if a.Cache != nil {
		_ = a.Cache.Close()
	}
	if a.DB != nil {
		if sqlDB, err := a.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

// applyDBConfigOverrides 从数据库加载系统配置覆盖 viper 已加载的值。
//
// 支持的分类：
//   - tushare：覆盖 cfg.DataProviders.Tushare
//   - deepseek：覆盖 cfg.LLM.Providers["deepseek"]
//   - llm：覆盖 cfg.LLM.Default
//
// DB 中的值优先于 viper（即 DB 中已设置的配置项会覆盖 config.yaml 的值）。
func applyDBConfigOverrides(cfg *Config, sysConfigRepo repository.SysConfigRepository) {
	ctx := context.Background()

	// 1. Tushare 配置覆盖
	tushareConfigs, err := sysConfigRepo.GetByCategory(ctx, domain.SysConfigCategoryTushare)
	if err != nil {
		slog.Warn("load tushare config from db failed", "err", err)
	} else {
		for _, entry := range tushareConfigs {
			switch entry.Key {
			case "enabled":
				if v, err := strconv.ParseBool(entry.Value); err == nil {
					cfg.DataProviders.Tushare.Enabled = v
				}
			case "token":
				if entry.Value != "" {
					cfg.DataProviders.Tushare.Token = entry.Value
				}
			case "base_url":
				if entry.Value != "" {
					cfg.DataProviders.Tushare.BaseURL = entry.Value
				}
			}
		}
	}

	// 2. DeepSeek / LLM 配置覆盖
	llmConfigs, err := sysConfigRepo.GetByCategory(ctx, domain.SysConfigCategoryDeepSeek)
	if err != nil {
		slog.Warn("load deepseek config from db failed", "err", err)
	} else {
		if cfg.LLM.Providers == nil {
			cfg.LLM.Providers = make(map[string]llmmodel.ProviderConfig)
		}
		dp := llmmodel.ProviderConfig{}
		for _, entry := range llmConfigs {
			switch entry.Key {
			case "enabled":
				if v, err := strconv.ParseBool(entry.Value); err == nil {
					dp.Enabled = &v
				}
			case "api_key":
				dp.APIKey = entry.Value
			case "base_url":
				dp.BaseURL = entry.Value
			case "model":
				dp.Model = entry.Value
			}
		}
		if dp.APIKey != "" || dp.BaseURL != "" {
			if dp.IsEnabled() {
				cfg.LLM.Providers["deepseek"] = dp
				if cfg.LLM.Default == "" {
					cfg.LLM.Default = "deepseek"
				}
			}
		}
	}

	// 3. LLM 默认 provider 覆盖
	llmGeneral, err := sysConfigRepo.Get(ctx, domain.SysConfigCategoryLLM, "default")
	if err != nil {
		slog.Warn("load llm default config from db failed", "err", err)
	} else if llmGeneral != nil && llmGeneral.Value != "" {
		cfg.LLM.Default = llmGeneral.Value
	}
}
