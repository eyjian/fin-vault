package bootstrap

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/cache"
	"github.com/eyjian/fin-vault/backend/internal/handler"
	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/platformapi"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	gormrepo "github.com/eyjian/fin-vault/backend/internal/repository/gorm"
	"github.com/eyjian/fin-vault/backend/internal/service"
)

// App 是组装好的应用实例，main.go 拿到后挂到 HTTP server 即可。
type App struct {
	Cfg          *Config
	DB           *gorm.DB
	Cache        cache.Provider
	Repos        *repository.Repositories
	LLMRegistry  llm.Registry // 可能为 nil（未配置 LLM 时降级）
	Aggregator   *platformapi.QuoteAggregator
	Cron         *CronManager
	Handlers     *Handlers
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
}

// Wire 总装：DB → Repo → Service → Handler。
//
// 任何子组件构造失败均直接返回错误；LLM 配置缺失只 Warn 不 Fatal（AI 功能降级）。
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
	}

	// 4. LLM Registry（失败降级）
	var llmReg llm.Registry
	if len(cfg.LLM.Providers) > 0 {
		reg, err := llm.NewRegistry(cfg.LLM)
		if err != nil {
			slog.Warn("llm registry disabled", "err", err)
		} else {
			llmReg = reg
		}
	} else {
		slog.Warn("no llm providers configured, AI endpoints will be disabled")
	}

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

	// 7. Services
	assetSvc := service.NewAssetService(repos.UoW, repos.Asset, repos.Platform)
	holdingSvc := service.NewHoldingService(repos.Holding, repos.Asset, repos.Quote, repos.Rate, repos.Platform)
	txnSvc := service.NewTransactionService(repos.UoW, repos.Transaction, repos.Holding, repos.Asset)
	quoteSvc := service.NewQuoteService(repos.Quote, repos.Asset, cacheProv, aggregator, cfg.Quote.CacheTTL)
	rateSvc := service.NewRateService(repos.Rate)
	exportSvc := service.NewExportService(repos.Holding, repos.Transaction, repos.Asset, repos.Platform, repos.Quote)
	matureSvc := service.NewMatureService(repos.UoW, repos.Holding, repos.Asset, repos.Transaction)

	// 8. Handlers
	handlers := &Handlers{
		Meta:        handler.NewMetaHandler(assetSvc, "v1.0-impl"),
		Asset:       handler.NewAssetHandler(assetSvc),
		Holding:     handler.NewHoldingHandler(holdingSvc),
		Transaction: handler.NewTransactionHandler(txnSvc),
		Quote:       handler.NewQuoteHandler(quoteSvc),
		Rate:        handler.NewRateHandler(rateSvc),
		Export:      handler.NewExportHandler(exportSvc),
	}
	if llmReg != nil {
		handlers.AIMeta = handler.NewAIMetaHandler(llmReg)
	}

	// 9. Cron
	cm := NewCronManager(matureSvc, cfg.Cron.Mature)

	app := &App{
		Cfg:         cfg,
		DB:          db,
		Cache:       cacheProv,
		Repos:       repos,
		LLMRegistry: llmReg,
		Aggregator:  aggregator,
		Cron:        cm,
		Handlers:    handlers,
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
