package agent

import (
	"context"
	"log/slog"

	sdkagent "trpc.group/trpc-go/trpc-agent-go/agent"
	sdkllmagent "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"
	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// DefaultMaxToolIterations Agent 单次 Run 最多串联调多少次 tool；
// 与 spec ai-agent-runtime "Tool Calling 多轮限制" Scenario 对齐（默认 10，
// 由装配方传 0 时回退到此默认值）。
const DefaultMaxToolIterations = 10

// NewToolsetAgentFactory 构造一个 AgentFactory，封装 SDK Agent 装配
// （注入 model + 工具集 + instruction），并在构造时一次性打印工具清单日志。
//
// 设计要点（design.md D12 + spec ai-agent-runtime "工具清单启动可见"）：
//   - tools 在 factory 构造时（启动期）就完成清单日志输出，不在每次 Run 时重复
//   - factory 闭包内每次 Run 都现场 sdkllmagent.New(...) 一个 Agent 实例，
//     但 model / tools / instruction 都是同一组共享指针，无状态可言
//   - maxToolIterations <= 0 时回退到 DefaultMaxToolIterations（10）
//
// 用法（§9 装配示例）：
//
//	tools := []sdktool.CallableTool{
//	    toolspkg.NewSearchFundTool(toolspkg.SearchFundDeps{Asset: repos.Asset}),
//	    toolspkg.NewMarketQuoteTool(toolspkg.MarketQuoteDeps{Quote: repos.Quote, Asset: repos.Asset}),
//	    // ... §6.2-6.4 改造后的 holding_query / market_data / profit_calc / history_query / platform_summary
//	}
//	factory := agent.NewToolsetAgentFactory(
//	    agent.DefaultAppName, sdkModel, tools, logger,
//	    "你是 fin-vault 个人理财助手，回答必须基于工具返回的真实数据，不臆测。", 0,
//	)
//	runner := agent.NewTRPCRunner(factory, store, historyWindow, logger)
func NewToolsetAgentFactory(
	appName string,
	model sdkmodel.Model,
	tools []sdktool.CallableTool,
	logger *slog.Logger,
	instruction string,
	maxToolIterations int,
) AgentFactory {
	if logger == nil {
		logger = slog.Default()
	}
	if maxToolIterations <= 0 {
		maxToolIterations = DefaultMaxToolIterations
	}

	// 一次性打印工具清单（启动期日志，非每次 Run）
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if t == nil || t.Declaration() == nil {
			continue
		}
		names = append(names, t.Declaration().Name)
	}
	logger.Info("llm tools registered",
		"tools", names, "count", len(names),
		"max_tool_iterations", maxToolIterations,
		"app_name", appName,
	)

	// SDK llmagent.WithTools 接受 []tool.Tool；CallableTool 嵌入 Tool，
	// 但 Go 接口切片不协变，需要循环装箱
	sdkTools := make([]sdktool.Tool, 0, len(tools))
	for _, t := range tools {
		sdkTools = append(sdkTools, t)
	}

	return func(ctx context.Context, userID, sessionID string) (sdkagent.Agent, error) {
		ag := sdkllmagent.New(appName,
			sdkllmagent.WithModel(model),
			sdkllmagent.WithTools(sdkTools),
			sdkllmagent.WithInstruction(instruction),
			sdkllmagent.WithMaxToolIterations(maxToolIterations),
		)
		return ag, nil
	}
}
