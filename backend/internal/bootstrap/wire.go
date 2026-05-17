package bootstrap

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/cache"
	"github.com/eyjian/fin-vault/backend/internal/handler"
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
		UoW:         gormrepo.NewUnitOfWork(db),
		User:        gormrepo.NewUserRepository(db),
		Platform:    gormrepo.NewPlatformRepository(db),
		Asset:       gormrepo.NewAssetRepository(db),
		Holding:     gormrepo.NewHoldingRepository(db),
		Transaction: gormrepo.NewTransactionRepository(db),
		CostLot:     gormrepo.NewCostLotRepository(db),
		Portfolio:   gormrepo.NewPortfolioRepository(db),
		Quote:       gormrepo.NewQuoteRepository(db),
		Rate:        gormrepo.NewRateRepository(db),
	}

	// 4. LLM 装配（§9 实装）
	//
	// TODO §9: factory.NewDefaultModel(cfg.LLM.ToRegistryEntry(), logger) 喂给 Runner
	if len(cfg.LLM.Providers) == 0 {
		slog.Warn("no llm providers configured, AI endpoints will return empty list")
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
		AIMeta:      handler.NewAIMetaHandler(cfg.LLM),
	}

	// 8.x AI Session / AI Message handlers —— §9 条件装配
	//
	// 当前 §8 阶段：SessionStore + Runner 均未在 wire 装配（属 §9 范围），所以
	// AISessionHandler / AIMessageHandler 都保留 nil 占位，router.go 通过 nil-check
	// 跳过注册。e2e handler 单测自包装（直接 httptest + handler.Register 到临时 router），
	// 不依赖 wire.go 装配链，本期 nil 不影响测试。
	//
	// TODO §9: 取消 placeholder，实装：
	//   sessionStore := session.NewSQLiteStore(db, cfg.AI.Session.HistoryWindow)
	//   aiSessionSvc := service.NewAISessionService(sessionStore)
	//   handlers.AISession = handler.NewAISessionHandler(aiSessionSvc)
	//
	//   factory := agent.NewToolsetAgentFactory(cfg.LLM, ...)
	//   runner   := agent.NewTRPCRunner(factory.Build, sessionStore, cfg.AI.Session.HistoryWindow, slog.Default())
	//   aiMsgSvc := service.NewAIMessageService(aiSessionSvc, runner)
	//   handlers.AIMessage = handler.NewAIMessageHandler(aiMsgSvc)
	slog.Warn("AI session/message endpoints disabled until §9 wiring (SessionStore + Runner not yet assembled)")

	// 9. Cron
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

