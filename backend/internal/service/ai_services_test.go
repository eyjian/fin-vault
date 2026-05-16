package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// 测试夹具：构造一个 toolReg 含 holding_query；handler 直接返回固定 JSON
// =====================================================================

// fakeTool 构造一个名字可控、handler 行为可注入的 tool。
func fakeTool(name string, handler func(ctx context.Context, args string) (string, error)) llm.Tool {
	return llm.Tool{
		Name:        name,
		Description: "fake tool for testing: " + name,
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     handler,
	}
}

// stubToolRegistry 构造一个含若干指定工具的 Registry。
func stubToolRegistry(t *testing.T, toolNames ...string) *tools.Registry {
	t.Helper()
	tr := tools.NewRegistry()
	for _, n := range toolNames {
		name := n
		tr.Register(fakeTool(name, func(_ context.Context, args string) (string, error) {
			// 默认行为：回 echo JSON
			return `{"tool":"` + name + `","args":` + safeQuote(args) + `,"items":[]}`, nil
		}))
	}
	return tr
}

func safeQuote(s string) string {
	if s == "" {
		return `"{}"`
	}
	// 裸 JSON 直接附进去；如果原 args 是合法 JSON，这里直接拼接，便于断言比对
	return s
}

// =====================================================================
// AdvisorService.Recommend —— 主路径 + Tool Calling 循环
// =====================================================================

func TestAdvisorService_Recommend_HappyPath_WithToolCalling(t *testing.T) {
	fp := llm.NewFakeProvider("fake-deepseek", "deepseek-chat")
	// 第 1 轮：模型决定调 holding_query
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "holding_query", Arguments: `{"user_id":1}`},
		},
		Usage: llm.TokenUsage{TotalTokens: 50, CompletionTokens: 10, PromptTokens: 40},
	})
	// 第 2 轮：拿到 tool result 后给最终回复
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "stop",
		Content:      "建议：当前持仓分散度良好，可继续持有。风险提示：投资有风险。",
		Usage:        llm.TokenUsage{TotalTokens: 70, CompletionTokens: 30, PromptTokens: 40},
	})

	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "market_data", "history_query", "profit_calc")

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	out, err := svc.Recommend(context.Background(), RecommendInput{
		UserID: 1, Target: "buy_sell",
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	// 1) 内容回包正确
	assert.Contains(t, out.Content, "持有")
	assert.Contains(t, out.Content, "风险提示")

	// 2) Provider 被调了 2 次（含 tool calling 循环）
	assert.Equal(t, 2, fp.Calls())

	// 3) 第 1 次调用工具列表是 buy_sell scene 的 4 个
	hist := fp.History()
	require.Len(t, hist, 2)
	assert.Equal(t, "ChatWithTools", hist[0].Method)
	toolNames := make([]string, 0, len(hist[0].Tools))
	for _, tl := range hist[0].Tools {
		toolNames = append(toolNames, tl.Name)
	}
	assert.ElementsMatch(t, []string{"holding_query", "market_data", "history_query", "profit_calc"}, toolNames)

	// 4) tool call trace 记录到 output
	require.Len(t, out.ToolCalls, 1)
	assert.Equal(t, "holding_query", out.ToolCalls[0].Name)
	assert.Equal(t, `{"user_id":1}`, out.ToolCalls[0].Args)
	assert.Contains(t, out.ToolCalls[0].Result, "holding_query")

	// 5) 持久化的消息：1 user + 1 tool + 1 assistant = 3
	require.Greater(t, out.ConversationID, uint(0))
	allMsgs, _ := convRepo.ListMessages(context.Background(), out.ConversationID, 100)
	assert.Len(t, allMsgs, 3)
	assert.Len(t, convRepo.MessagesByRole(out.ConversationID, domain.AIRoleUser), 1)
	assert.Len(t, convRepo.MessagesByRole(out.ConversationID, domain.AIRoleTool), 1)
	assert.Len(t, convRepo.MessagesByRole(out.ConversationID, domain.AIRoleAssistant), 1)

	// 6) 最终的 IncrTokens 调用记录
	require.Len(t, convRepo.IncrTokensLog, 1)
	assert.Equal(t, 120, convRepo.IncrTokensLog[0].DeltaTokens) // 50 + 70
	// deltaMessages = 2 + len(traces) = 3
	assert.Equal(t, 3, convRepo.IncrTokensLog[0].DeltaMessages)

	// 7) Usage 字段
	assert.Equal(t, 120, out.Usage["total_tokens"])
}

// 直接 stop（无工具调用）也能正常返回。
func TestAdvisorService_Recommend_NoToolCallingDirect(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "stop",
		Content:      "建议：持有。",
		Usage:        llm.TokenUsage{TotalTokens: 30},
	})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "market_data", "history_query", "profit_calc")

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	out, err := svc.Recommend(context.Background(), RecommendInput{UserID: 1, Target: "buy_sell"})
	require.NoError(t, err)
	assert.Equal(t, 1, fp.Calls())
	assert.Empty(t, out.ToolCalls)
	assert.Contains(t, out.Content, "持有")
}

// allocation 场景挑选不同工具子集
func TestAdvisorService_Recommend_AllocationSceneToolSet(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueChat(&llm.ChatResponse{FinishReason: "stop", Content: "ok"})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	// 注册全部工具，让 svc 自己 Pick
	toolReg := stubToolRegistry(t, "holding_query", "platform_summary", "profit_calc", "market_data", "history_query")

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	_, err := svc.Recommend(context.Background(), RecommendInput{UserID: 1, Target: "allocation"})
	require.NoError(t, err)

	hist := fp.History()
	require.Len(t, hist, 1)
	got := make([]string, 0, len(hist[0].Tools))
	for _, tl := range hist[0].Tools {
		got = append(got, tl.Name)
	}
	// allocation: holding_query / platform_summary / profit_calc / market_data
	assert.ElementsMatch(t, []string{"holding_query", "platform_summary", "profit_calc", "market_data"}, got)
}

// maxHops 防死循环 → 50002
func TestAdvisorService_Recommend_MaxHopsExceeded_Returns50002(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	// 连续 9 次返回 tool_calls，maxHops=8 时第 9 次循环判断超限
	for i := 0; i < 10; i++ {
		fp.QueueChat(&llm.ChatResponse{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "x", Name: "holding_query", Arguments: "{}"},
			},
		})
	}
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "market_data", "history_query", "profit_calc")

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	_, err := svc.Recommend(context.Background(), RecommendInput{UserID: 1, Target: "buy_sell"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, 50002, be.Code)
}

// Provider 不存在 → 50002（ErrAIProviderNotFound）
func TestAdvisorService_Recommend_ProviderNotFound(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query")

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	_, err := svc.Recommend(context.Background(), RecommendInput{
		UserID: 1, Target: "buy_sell", LLMProvider: "not-exist",
	})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAIProviderNotFound.Code, be.Code)
	assert.Equal(t, 0, fp.Calls(), "Provider 取不到时不应调用 LLM")
}

// LLM Chat 报错 → ErrAIRequestFailed (50004)
func TestAdvisorService_Recommend_LLMReturnsError(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueErr(errors.New("upstream 502"))
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query")

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	_, err := svc.Recommend(context.Background(), RecommendInput{UserID: 1, Target: "buy_sell"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAIRequestFailed.Code, be.Code)
}

// 未知工具调用 → ErrAIToolCallFailed (50005)
func TestAdvisorService_Recommend_UnknownToolName_Returns50005(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{
			{ID: "x", Name: "totally_unknown_tool", Arguments: "{}"},
		},
	})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query") // 没有 totally_unknown_tool

	svc := NewAdvisorService(reg, convRepo, toolReg, 8)
	_, err := svc.Recommend(context.Background(), RecommendInput{UserID: 1, Target: "buy_sell"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAIToolCallFailed.Code, be.Code)
	assert.Contains(t, be.Message, "totally_unknown_tool")
}

// Tool handler 报错时仍应回填 error JSON 给 LLM 继续，不应中断对话。
func TestAdvisorService_Recommend_ToolHandlerError_RecoversAndContinues(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{
			{ID: "x", Name: "holding_query", Arguments: `{"user_id":1}`},
		},
	})
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "stop",
		Content:      "我无法获取持仓数据，请稍后重试。风险提示：投资有风险。",
	})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()

	// holding_query 故意抛错
	tr := tools.NewRegistry()
	tr.Register(fakeTool("holding_query", func(_ context.Context, _ string) (string, error) {
		return "", errors.New("db down")
	}))
	tr.Register(fakeTool("market_data", func(_ context.Context, _ string) (string, error) { return "{}", nil }))
	tr.Register(fakeTool("history_query", func(_ context.Context, _ string) (string, error) { return "{}", nil }))
	tr.Register(fakeTool("profit_calc", func(_ context.Context, _ string) (string, error) { return "{}", nil }))

	svc := NewAdvisorService(reg, convRepo, tr, 8)
	out, err := svc.Recommend(context.Background(), RecommendInput{UserID: 1, Target: "buy_sell"})
	require.NoError(t, err, "tool handler 报错应被 service 包成 error JSON 继续，不应中断")
	require.Len(t, out.ToolCalls, 1)
	// trace.Result 应是 error JSON，包含 "db down"
	assert.Contains(t, out.ToolCalls[0].Result, "error")
	assert.Contains(t, out.ToolCalls[0].Result, "db down")
}

// =====================================================================
// AnalysisService.AnalyzeProfit
// =====================================================================

func TestAnalysisService_AnalyzeProfit_HappyPath(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "holding_query", Arguments: `{"user_id":1}`},
			{ID: "c2", Name: "profit_calc", Arguments: `{"period":"2026-Q1"}`},
		},
		Usage: llm.TokenUsage{TotalTokens: 80},
	})
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "stop",
		Content:      "Q1 盈亏：总盈亏 +1234.56 元，收益率 5.67%。表现最好的是 xxx ...",
		Usage:        llm.TokenUsage{TotalTokens: 100, CompletionTokens: 50, PromptTokens: 50},
	})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "profit_calc", "history_query", "market_data")

	svc := NewAnalysisService(reg, convRepo, toolReg, 8)
	out, err := svc.AnalyzeProfit(context.Background(), ProfitInput{
		UserID: 1, Period: "2026-Q1", DisplayCurrency: "CNY",
	})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Q1")
	assert.Equal(t, 2, fp.Calls())
	require.Len(t, out.ToolCalls, 2)
	assert.Equal(t, "holding_query", out.ToolCalls[0].Name)
	assert.Equal(t, "profit_calc", out.ToolCalls[1].Name)

	// 第一次调用工具列表 = analysis 场景的 4 工具
	hist := fp.History()
	got := make([]string, 0, len(hist[0].Tools))
	for _, tl := range hist[0].Tools {
		got = append(got, tl.Name)
	}
	assert.ElementsMatch(t, []string{"holding_query", "profit_calc", "history_query", "market_data"}, got)

	// totalTokens = 80 + 100 = 180
	require.Len(t, convRepo.IncrTokensLog, 1)
	assert.Equal(t, 180, convRepo.IncrTokensLog[0].DeltaTokens)
	// deltaMessages = 2 + len(traces) = 4
	assert.Equal(t, 4, convRepo.IncrTokensLog[0].DeltaMessages)
}

func TestAnalysisService_AnalyzeProfit_DefaultsCurrencyAndUserID(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	fp.QueueChat(&llm.ChatResponse{FinishReason: "stop", Content: "ok"})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "profit_calc", "history_query", "market_data")

	svc := NewAnalysisService(reg, convRepo, toolReg, 0) // maxHops=0 应被默认成 8
	out, err := svc.AnalyzeProfit(context.Background(), ProfitInput{Period: "2026"}) // UserID=0, Currency=""
	require.NoError(t, err)
	require.Greater(t, out.ConversationID, uint(0))

	// 会话 Scene 应 = analysis
	conv, _ := convRepo.GetConv(context.Background(), out.ConversationID)
	assert.Equal(t, string(domain.AISceneAnalysis), string(conv.Scene))
	// UserID 默认 1
	assert.Equal(t, uint(1), conv.UserID)
}

// =====================================================================
// ChatService.Stream —— SSE 流式
// =====================================================================

func TestChatService_Stream_PureChatScene_StreamsContent(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "fake-model")
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "stop",
		Content:      "hello world from FinVault",
	})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()

	svc := NewChatService(reg, convRepo, tools.NewRegistry(), ChatConfig{
		HistoryLimit: 5, MaxTokens: 256, MaxToolHops: 8,
	})
	ch, err := svc.Stream(context.Background(), StreamRequest{
		UserID: 1, Scene: domain.AISceneChat, Content: "你好",
	})
	require.NoError(t, err)

	var fullText string
	var sawDone bool
	var convID uint
	for ev := range ch {
		switch ev.Type {
		case "chunk":
			fullText += ev.Content
		case "done":
			sawDone = true
			convID = ev.ConversationID
		case "error":
			t.Fatalf("unexpected error event: %s", ev.Content)
		}
	}
	assert.Equal(t, "hello world from FinVault", fullText)
	assert.True(t, sawDone)
	require.Greater(t, convID, uint(0))

	// chat 场景 = StreamChat 一次（无 ChatWithTools 调用）
	hist := fp.History()
	require.Len(t, hist, 1)
	assert.Equal(t, "StreamChat", hist[0].Method)

	// 持久化：user + assistant
	allMsgs, _ := convRepo.ListMessages(context.Background(), convID, 100)
	assert.Len(t, allMsgs, 2)
	assert.Equal(t, domain.AIRoleUser, allMsgs[0].Role)
	assert.Equal(t, domain.AIRoleAssistant, allMsgs[1].Role)
	assert.Equal(t, "hello world from FinVault", allMsgs[1].Content)
}

func TestChatService_Stream_AnalysisScene_RunsToolCallingThenStreams(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	// 第 1 次 ChatWithTools：tool_calls
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "holding_query", Arguments: `{"user_id":1}`},
		},
	})
	// 第 2 次 ChatWithTools：finish stop，直接当 chunk 流出（service 实现里在 tool calling 路径下不再二次 stream）
	fp.QueueChat(&llm.ChatResponse{
		FinishReason: "stop",
		Content:      "盈亏分析结果：+12.34%。",
	})
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "history_query", "profit_calc", "market_data")

	svc := NewChatService(reg, convRepo, toolReg, ChatConfig{HistoryLimit: 20, MaxTokens: 1024, MaxToolHops: 8})
	ch, err := svc.Stream(context.Background(), StreamRequest{
		UserID: 1, Scene: domain.AISceneAnalysis, Content: "Q1 盈亏？",
	})
	require.NoError(t, err)

	var (
		gotToolCall   bool
		gotToolResult bool
		fullChunk     string
		sawDone       bool
		errEvents     []string
	)
	for ev := range ch {
		switch ev.Type {
		case "tool_call":
			gotToolCall = true
			assert.Equal(t, "holding_query", ev.ToolName)
		case "tool_result":
			gotToolResult = true
			assert.Equal(t, "holding_query", ev.ToolName)
			assert.NotEmpty(t, ev.ToolResult)
		case "chunk":
			fullChunk += ev.Content
		case "done":
			sawDone = true
		case "error":
			errEvents = append(errEvents, ev.Content)
		}
	}
	assert.True(t, gotToolCall, "should emit tool_call event")
	assert.True(t, gotToolResult, "should emit tool_result event")
	assert.Contains(t, fullChunk, "盈亏")
	assert.True(t, sawDone)
	assert.Empty(t, errEvents)

	// LLM 调用 2 次（都用 ChatWithTools）
	hist := fp.History()
	require.Len(t, hist, 2)
	assert.Equal(t, "ChatWithTools", hist[0].Method)
	assert.Equal(t, "ChatWithTools", hist[1].Method)
}

// MaxHops 防死循环 → SSE 错误事件
func TestChatService_Stream_MaxHopsExceeded_EmitsError(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	for i := 0; i < 5; i++ {
		fp.QueueChat(&llm.ChatResponse{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{ID: "x", Name: "holding_query", Arguments: "{}"}},
		})
	}
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	toolReg := stubToolRegistry(t, "holding_query", "history_query", "profit_calc", "market_data")

	svc := NewChatService(reg, convRepo, toolReg, ChatConfig{HistoryLimit: 5, MaxTokens: 256, MaxToolHops: 3})
	ch, err := svc.Stream(context.Background(), StreamRequest{
		UserID: 1, Scene: domain.AISceneAnalysis, Content: "?",
	})
	require.NoError(t, err)

	var errMsgs []string
	for ev := range ch {
		if ev.Type == "error" {
			errMsgs = append(errMsgs, ev.Content)
		}
	}
	require.Len(t, errMsgs, 1)
	assert.Contains(t, errMsgs[0], "max hops")
}

// 空 content → ErrInvalidParam
func TestChatService_Stream_EmptyContent_ReturnsInvalidParam(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	svc := NewChatService(reg, convRepo, tools.NewRegistry(), ChatConfig{})

	_, err := svc.Stream(context.Background(), StreamRequest{
		UserID: 1, Content: "   ",
	})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)
}

// =====================================================================
// pickToolsForScene —— 4 个场景路由
// =====================================================================

func TestChatService_PickToolsForScene(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	tr := stubToolRegistry(t, "holding_query", "history_query", "profit_calc", "market_data", "platform_summary")

	svc := NewChatService(reg, convRepo, tr, ChatConfig{})

	cases := []struct {
		scene domain.AIScene
		want  []string
	}{
		{domain.AISceneAnalysis, []string{"holding_query", "history_query", "profit_calc", "market_data"}},
		{domain.AISceneBuySell, []string{"holding_query", "market_data", "history_query"}},
		{domain.AISceneAdvisor, []string{"holding_query", "platform_summary", "profit_calc", "market_data"}},
		{domain.AISceneChat, nil}, // 不带 tools
		{domain.AISceneReport, nil},
	}
	for _, c := range cases {
		got := svc.pickToolsForScene(c.scene)
		names := make([]string, 0, len(got))
		for _, tl := range got {
			names = append(names, tl.Name)
		}
		if c.want == nil {
			assert.Emptyf(t, names, "scene %s should have no tools", c.scene)
		} else {
			assert.ElementsMatchf(t, c.want, names, "scene %s wrong tools, got %v", c.scene, names)
		}
	}
}

// =====================================================================
// ChatService.CreateConversation —— 校验 + Provider 解析
// =====================================================================

func TestChatService_CreateConversation_DefaultsAndScene(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	svc := NewChatService(reg, convRepo, tools.NewRegistry(), ChatConfig{})

	conv, err := svc.CreateConversation(context.Background(), CreateConversationInput{
		// UserID=0 → 默认 1； Scene="" → AISceneChat；Title="" → "新会话"
	})
	require.NoError(t, err)
	assert.Equal(t, uint(1), conv.UserID)
	assert.Equal(t, string(domain.AISceneChat), string(conv.Scene))
	assert.Equal(t, "新会话", conv.Title)
	assert.Equal(t, "fake", conv.LLMProvider)
}

func TestChatService_CreateConversation_InvalidScene(t *testing.T) {
	fp := llm.NewFakeProvider("fake", "m")
	reg := llm.NewFakeRegistry(fp)
	convRepo := testutil.NewMockAIConvRepo()
	svc := NewChatService(reg, convRepo, tools.NewRegistry(), ChatConfig{})

	_, err := svc.CreateConversation(context.Background(), CreateConversationInput{
		UserID: 1, Scene: domain.AIScene("bad-scene"),
	})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)
}

// firstN：长标题截断
func TestFirstN_TruncatesLongString(t *testing.T) {
	short := firstN("hello", 10)
	assert.Equal(t, "hello", short)

	long := firstN("一二三四五六七八九十十一十二", 5)
	assert.Equal(t, "一二三四五...", long)
}
