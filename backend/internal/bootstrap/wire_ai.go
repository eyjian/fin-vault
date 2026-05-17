// wire_ai.go —— §9 AI 装配层（与 §8 nil placeholder 对接）。
//
// 设计要点（与 design.md D12 / D16 / D17 + spec ai-agent-runtime 对齐）：
//   - bootstrap 是装配层，允许 import trpc-agent-go SDK 子包（agent / model / tools / session
//     已物理依赖 SDK），属铁律 F2 物理隔离的"内"侧；service / handler 仍是 0 SDK 命中。
//   - D16 LLM 不可用降级：NewDefaultModel error → 仅装 AISession（CRUD 不依赖 Runner），
//     AIMessage 保持 nil 由 router 条件挂载兜底（POST /messages 路由不注册 → 404）。
//   - D17 §9.3 in-flight step flush 评估：appendStepSafe 同步落库无缓冲，本期不实装
//     Runner.Flush；HTTP server 10s graceful shutdown (main.go) 已能让正在执行的
//     Run() 完成最后一次同步 AppendStep；如未来 Runner 引入异步缓冲再补 flush 接口
//     → 留 §10 follow-up。
//   - 启动日志 5 条契约（spec ai-agent-runtime "工具清单启动可见" + 装配可观测性）：
//       1. "ai providers loaded"（含 configured / default）
//       2. "llm provider selected (...)"（由 NewDefaultModel 内部打 default 或 fallback）
//       3. "llm tools registered"（由 NewToolsetAgentFactory 内部打）
//       4. "ai session config"（含 history_window / max_steps_size_mb）
//       5. "ai endpoints status"（含 session_enabled / message_enabled，降级路径附 reason）
//     降级路径下 #2 #3 自然不打（NewDefaultModel error 提前返回），但 #4 #5 仍输出，
//     方便运维一眼看出降级状态。

package bootstrap

import (
	"log/slog"
	"sort"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/eyjian/fin-vault/backend/internal/handler"
	"github.com/eyjian/fin-vault/backend/internal/llm/agent"
	"github.com/eyjian/fin-vault/backend/internal/llm/model"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/internal/service"
)

// defaultAIInstruction 是注入 SDK Agent 的系统提示词（spec ai-agent-runtime 隐含约束：
// 限定模型基于工具数据回答，避免凭空臆测）。如未来允许用户自定义 system prompt，
// 在此前置一层注入即可。
const defaultAIInstruction = "你是 fin-vault 个人理财助手，回答必须基于工具返回的真实数据，不臆测。"

// buildAITools 构造本期 7 个工具（spec ai-tools 议题首发 6 个 + history_query 历史交易回溯）。
//
// 顺序：search_fund / market_quote / market_data / holding_query / profit_calc /
// platform_summary / history_query —— 与 NewToolsetAgentFactory 启动日志 "llm tools
// registered" 输出顺序保持一致（factory 内部按传入顺序提取 Declaration().Name）。
//
// Deps 仅依赖 repository 接口，无 SDK 依赖；启动期一次性构造后被 factory 持有 +
// 每次 Run 共享同一组工具实例（无状态）。
func buildAITools(repos *repository.Repositories) []sdktool.CallableTool {
	return []sdktool.CallableTool{
		tools.NewSearchFundTool(tools.SearchFundDeps{Asset: repos.Asset}),
		tools.NewMarketQuoteTool(tools.MarketQuoteDeps{Quote: repos.Quote, Asset: repos.Asset}),
		tools.NewMarketDataTool(tools.MarketDataDeps{Quote: repos.Quote, Asset: repos.Asset}),
		tools.NewHoldingQueryTool(tools.HoldingQueryDeps{Holding: repos.Holding, Asset: repos.Asset}),
		tools.NewProfitCalcTool(tools.ProfitCalcDeps{Holding: repos.Holding, Quote: repos.Quote}),
		tools.NewPlatformSummaryTool(tools.PlatformSummaryDeps{
			Holding:  repos.Holding,
			Platform: repos.Platform,
			Quote:    repos.Quote,
		}),
		tools.NewHistoryQueryTool(tools.HistoryQueryDeps{Transaction: repos.Transaction}),
	}
}

// wireAI 装配 AISession + AIMessage 两 handler。
//
// 返回值说明：
//   - sessionH：始终非 nil（CRUD 不依赖 Runner）。
//   - messageH：D16 降级路径下为 nil（NewDefaultModel error），router 条件挂载会
//     跳过 POST /ai/sessions/:id/messages 注册（404）。
//
// 错误语义：本函数刻意不返 error —— LLM 不可用归入 messageH=nil 的降级路径，
// 让进程仍可正常启动（spec "无配置 fallback 空数组"扩展到运行时降级）。如未来
// SessionStore 创建等真硬错误也走这里再考虑改签名。
func wireAI(
	cfg *Config,
	repos *repository.Repositories,
	sessionStore session.SessionStore,
	logger *slog.Logger,
) (*handler.AISessionHandler, *handler.AIMessageHandler) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1) AISession 始终装：CRUD 路由不依赖 Runner，LLM 是否可用都应能列表/创建/删除会话。
	aiSessionSvc := service.NewAISessionService(sessionStore)
	sessionH := handler.NewAISessionHandler(aiSessionSvc)

	// 2) 启动日志 #1：providers loaded。
	providers := make([]string, 0, len(cfg.LLM.Providers))
	for name := range cfg.LLM.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	logger.Info("ai providers loaded",
		"configured", providers,
		"default", cfg.LLM.Default,
	)

	// 3) D16 LLM 不可用降级：构造 SDK Model 失败 → 仅装 AISession。
	sdkModel, _, err := model.NewDefaultModel(cfg.LLM.ToRegistryEntry(), logger)
	if err != nil {
		logger.Warn("AI message endpoint disabled (D16 degrade)",
			"reason", err.Error(),
			"session_enabled", true,
			"message_enabled", false,
		)
		// 启动日志 #4 + #5（降级路径仍打，便于运维监控）
		logger.Info("ai session config",
			"history_window", cfg.AI.Session.HistoryWindow,
			"max_steps_size_mb", cfg.AI.Session.MaxStepsSizeMB,
		)
		logger.Info("ai endpoints status",
			"session_enabled", true,
			"message_enabled", false,
			"reason", err.Error(),
		)
		return sessionH, nil
	}

	// 4) 正常路径：装 7 工具 → factory（内部打日志 #3）→ Runner → AIMessage。
	aiTools := buildAITools(repos)
	factory := agent.NewToolsetAgentFactory(
		agent.DefaultAppName,
		sdkModel,
		aiTools,
		logger,
		defaultAIInstruction,
		0, // 0 → DefaultMaxToolIterations=10（spec ai-agent-runtime "Tool Calling 多轮限制"）
	)
	runner := agent.NewTRPCRunner(factory, sessionStore, cfg.AI.Session.HistoryWindow, logger)
	aiMessageSvc := service.NewAIMessageService(aiSessionSvc, runner)
	messageH := handler.NewAIMessageHandler(aiMessageSvc)

	// 5) 启动日志 #4 + #5（正常路径）。
	logger.Info("ai session config",
		"history_window", cfg.AI.Session.HistoryWindow,
		"max_steps_size_mb", cfg.AI.Session.MaxStepsSizeMB,
	)
	logger.Info("ai endpoints status",
		"session_enabled", true,
		"message_enabled", true,
	)

	return sessionH, messageH
}
