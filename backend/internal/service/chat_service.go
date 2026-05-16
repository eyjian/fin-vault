package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// ChatService —— AI 流式问答（SSE）+ 会话持久化
// =====================================================================

// ChatService AI 流式对话服务。
//
// 设计要点：
//   - SSE 流式由 handler 层用 c.Stream 直接消费 channel，本 service 只产出 chunk channel
//   - 每次会话末尾持久化 user/assistant 两条 AIMessage，并 IncrTokens
//   - 历史上下文最多取最近 N 条（默认 20，由 cfg.AI.HistoryLimit 注入）
type ChatService struct {
	registry  llm.Registry
	convRepo  repository.AIConversationRepository
	toolReg   *tools.Registry
	historyN  int
	maxTokens int
	maxHops   int
}

// ChatConfig ChatService 配置项。
type ChatConfig struct {
	HistoryLimit int // 历史消息加载条数，默认 20
	MaxTokens    int // 单条响应 token 上限，默认 2048
	MaxToolHops  int // Tool Calling 最大循环次数，默认 8
}

// NewChatService 构造。
func NewChatService(
	reg llm.Registry,
	convRepo repository.AIConversationRepository,
	toolReg *tools.Registry,
	cfg ChatConfig,
) *ChatService {
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = 20
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 2048
	}
	if cfg.MaxToolHops <= 0 {
		cfg.MaxToolHops = 8
	}
	return &ChatService{
		registry:  reg,
		convRepo:  convRepo,
		toolReg:   toolReg,
		historyN:  cfg.HistoryLimit,
		maxTokens: cfg.MaxTokens,
		maxHops:   cfg.MaxToolHops,
	}
}

// =====================================================================
// 会话管理
// =====================================================================

// CreateConversationInput 新建会话入参。
type CreateConversationInput struct {
	UserID      uint
	Title       string
	Scene       domain.AIScene
	LLMProvider string
}

// CreateConversation 新建会话。LLMProvider 为空时取默认。
func (s *ChatService) CreateConversation(ctx context.Context, in CreateConversationInput) (*domain.AIConversation, error) {
	if in.UserID == 0 {
		in.UserID = 1
	}
	scene := in.Scene
	if scene == "" {
		scene = domain.AISceneChat
	}
	if !scene.IsValid() {
		return nil, errs.ErrInvalidParam.WithMsg("invalid ai scene: " + string(scene))
	}
	provName := in.LLMProvider
	prov, err := s.registry.Get(provName)
	if err != nil {
		return nil, errs.ErrAIProviderNotFound.WithCause(err)
	}
	conv := &domain.AIConversation{
		UserID:      in.UserID,
		Title:       firstN(in.Title, 30),
		Scene:       scene,
		LLMProvider: prov.Name(),
		LLMModel:    prov.Model(),
		Status:      domain.StatusActive,
	}
	if conv.Title == "" {
		conv.Title = "新会话"
	}
	if err := s.convRepo.CreateConv(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// ListConversations 列出会话。
func (s *ChatService) ListConversations(ctx context.Context, userID uint, opts repository.ListOptions) ([]domain.AIConversation, int64, error) {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	return s.convRepo.ListConversations(ctx, userID, opts)
}

// ListMessages 拉取历史消息。
func (s *ChatService) ListMessages(ctx context.Context, convID uint, limit int) ([]domain.AIMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.convRepo.ListMessages(ctx, convID, limit)
}

// =====================================================================
// 流式发送
// =====================================================================

// StreamRequest SSE 请求。
type StreamRequest struct {
	UserID         uint
	ConversationID uint
	Scene          domain.AIScene
	Content        string
	LLMProvider    string // 可空，覆盖会话级别
}

// StreamEvent SSE 输出事件（handler 层 marshal 后写出）。
type StreamEvent struct {
	Type           string `json:"type"` // chunk / tool_call / tool_result / done / error
	Content        string `json:"content,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolArgs       string `json:"tool_args,omitempty"`
	ToolResult     string `json:"tool_result,omitempty"`
	ConversationID uint   `json:"conversation_id,omitempty"`
	FinishReason   string `json:"finish_reason,omitempty"`
}

// Stream 处理一次流式问答；返回 chunk 通道，handler 负责 c.Stream 消费。
//
// 流程：
//  1. 持久化用户消息
//  2. 加载历史 + 系统 Prompt
//  3. （非 chat 场景）走 Tool Calling 循环；最后再起一次 stream 输出最终回复
//  4. （chat 场景）直接 stream 一次
//  5. 持久化助手回复 + 累加 tokens
func (s *ChatService) Stream(ctx context.Context, req StreamRequest) (<-chan StreamEvent, error) {
	if req.UserID == 0 {
		req.UserID = 1
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, errs.ErrInvalidParam.WithMsg("content required")
	}

	// 1. 找/建会话
	var conv *domain.AIConversation
	if req.ConversationID != 0 {
		c, err := s.convRepo.GetConv(ctx, req.ConversationID)
		if err != nil {
			return nil, err
		}
		conv = c
	} else {
		c, err := s.CreateConversation(ctx, CreateConversationInput{
			UserID:      req.UserID,
			Title:       req.Content,
			Scene:       req.Scene,
			LLMProvider: req.LLMProvider,
		})
		if err != nil {
			return nil, err
		}
		conv = c
	}

	// 2. 决定 Provider
	provName := req.LLMProvider
	if provName == "" {
		provName = conv.LLMProvider
	}
	prov, err := s.registry.Get(provName)
	if err != nil {
		return nil, errs.ErrAIProviderNotFound.WithCause(err)
	}

	// 3. 持久化 user 消息
	userMsg := &domain.AIMessage{
		ConversationID: conv.ID,
		Role:           domain.AIRoleUser,
		Content:        req.Content,
	}
	if err := s.convRepo.AppendMessage(ctx, userMsg); err != nil {
		return nil, err
	}

	// 4. 准备 messages（系统 prompt + 历史 + 当前 user）
	history, _ := s.convRepo.ListMessages(ctx, conv.ID, s.historyN)
	llmMsgs := buildLLMMessages(conv.Scene, history)

	// 5. 选择 tools 子集
	scene := conv.Scene
	if req.Scene != "" {
		scene = req.Scene
	}
	toolList := s.pickToolsForScene(scene)

	// 6. 启动异步处理
	out := make(chan StreamEvent, 32)
	go s.runStream(ctx, prov, conv, llmMsgs, toolList, out)

	return out, nil
}

func (s *ChatService) runStream(
	ctx context.Context,
	prov llm.Provider,
	conv *domain.AIConversation,
	msgs []llm.Message,
	toolList []llm.Tool,
	out chan<- StreamEvent,
) {
	defer close(out)

	// === 步骤 A：Tool Calling 循环（如果有 tools）===
	if len(toolList) > 0 {
		hops := 0
		for hops < s.maxHops {
			resp, err := prov.ChatWithTools(ctx, llm.ChatRequest{
				Messages:  msgs,
				MaxTokens: s.maxTokens,
			}, toolList)
			if err != nil {
				out <- StreamEvent{Type: "error", Content: err.Error()}
				return
			}
			if resp.FinishReason != "tool_calls" {
				// 没有工具调用，直接把 content 当流式 chunk 输出
				if resp.Content != "" {
					out <- StreamEvent{Type: "chunk", Content: resp.Content}
				}
				out <- StreamEvent{
					Type:           "done",
					ConversationID: conv.ID,
					FinishReason:   resp.FinishReason,
				}
				s.persistAssistant(ctx, conv, resp.Content, resp.Usage)
				return
			}
			// finish_reason == tool_calls：执行工具并回填
			msgs = append(msgs, llm.Message{
				Role:      domain.AIRoleAssistant,
				ToolCalls: resp.ToolCalls,
			})
			for _, tc := range resp.ToolCalls {
				out <- StreamEvent{
					Type:     "tool_call",
					ToolName: tc.Name,
					ToolArgs: tc.Arguments,
				}
				tool, ok := s.toolReg.Get(tc.Name)
				if !ok {
					out <- StreamEvent{Type: "error", Content: "unknown tool: " + tc.Name}
					return
				}
				result, err := tool.Handler(ctx, tc.Arguments)
				if err != nil {
					result = fmt.Sprintf(`{"error":true,"message":%q}`, err.Error())
				}
				out <- StreamEvent{
					Type:       "tool_result",
					ToolName:   tc.Name,
					ToolResult: result,
				}
				// 持久化 tool 调用消息
				_ = s.convRepo.AppendMessage(ctx, &domain.AIMessage{
					ConversationID: conv.ID,
					Role:           domain.AIRoleTool,
					Content:        result,
					ToolName:       tc.Name,
					ToolArgs:       tc.Arguments,
					ToolResult:     result,
					ToolCallID:     tc.ID,
				})
				msgs = append(msgs, llm.Message{
					Role:       domain.AIRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			hops++
		}
		out <- StreamEvent{Type: "error", Content: "tool calling exceeded max hops"}
		return
	}

	// === 步骤 B：纯流式（chat 场景）===
	chunks, err := prov.StreamChat(ctx, llm.ChatRequest{
		Messages:  msgs,
		MaxTokens: s.maxTokens,
	})
	if err != nil {
		out <- StreamEvent{Type: "error", Content: err.Error()}
		return
	}
	var fullContent strings.Builder
	for ch := range chunks {
		if ch.Err != nil {
			out <- StreamEvent{Type: "error", Content: ch.Err.Error()}
			return
		}
		if ch.Content != "" {
			fullContent.WriteString(ch.Content)
			out <- StreamEvent{Type: "chunk", Content: ch.Content}
		}
		if ch.Done {
			out <- StreamEvent{
				Type:           "done",
				ConversationID: conv.ID,
				FinishReason:   ch.FinishReason,
			}
			s.persistAssistant(ctx, conv, fullContent.String(), llm.TokenUsage{})
			return
		}
	}
}

func (s *ChatService) persistAssistant(ctx context.Context, conv *domain.AIConversation, content string, usage llm.TokenUsage) {
	_ = s.convRepo.AppendMessage(ctx, &domain.AIMessage{
		ConversationID: conv.ID,
		Role:           domain.AIRoleAssistant,
		Content:        content,
		TokenCount:     usage.CompletionTokens,
	})
	_ = s.convRepo.IncrTokens(ctx, conv.ID, 2 /*user+assistant*/, usage.TotalTokens)
}

// pickToolsForScene 根据场景挑选不同工具。
func (s *ChatService) pickToolsForScene(scene domain.AIScene) []llm.Tool {
	if s.toolReg == nil {
		return nil
	}
	switch scene {
	case domain.AISceneAnalysis:
		return s.toolReg.Pick("holding_query", "history_query", "profit_calc", "market_data")
	case domain.AISceneBuySell:
		return s.toolReg.Pick("holding_query", "market_data", "history_query")
	case domain.AISceneAdvisor:
		return s.toolReg.Pick("holding_query", "platform_summary", "profit_calc", "market_data")
	case domain.AISceneChat, domain.AISceneReport:
		// chat 默认不带 tool（流式纯对话），如果用户希望带，可在 prompt 里说
		return nil
	}
	return nil
}

// =====================================================================
// 辅助
// =====================================================================

// firstN 取字符串前 n 个 rune。
func firstN(s string, n int) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= n {
		return string(rs)
	}
	return string(rs[:n]) + "..."
}

// buildLLMMessages 把 DB 历史消息 + 系统 prompt 拼成 LLM 消息序列。
func buildLLMMessages(scene domain.AIScene, history []domain.AIMessage) []llm.Message {
	out := make([]llm.Message, 0, len(history)+1)
	out = append(out, llm.Message{Role: domain.AIRoleSystem, Content: systemPrompt(scene)})
	for _, m := range history {
		msg := llm.Message{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		// assistant 携带 ToolCalls 的情况：我们只持久化了 result，不重放 tool_calls，避免历史太长
		out = append(out, msg)
	}
	return out
}

// systemPrompt 不同场景的系统提示。
func systemPrompt(scene domain.AIScene) string {
	switch scene {
	case domain.AISceneAnalysis:
		return `你是 FinVault 的盈亏分析助手。请基于 holding_query / profit_calc / history_query / market_data 工具的真实数据回答，不要臆测。回答风格：先结论后理由，金额保留 2 位小数，比例用百分比表示。`
	case domain.AISceneBuySell:
		return `你是 FinVault 的买卖建议助手。先用 holding_query/market_data 看清当前持仓与最新行情，再给出 1-3 条建议；明确风险提示，不构成投资建议。`
	case domain.AISceneAdvisor:
		return `你是 FinVault 的资产配置顾问。请基于 platform_summary/profit_calc/market_data 数据评估当前配置是否均衡，并给出可执行的调整建议。`
	case domain.AISceneReport:
		return `你是 FinVault 报表编辑助手。请按用户给定的时段汇总持仓变化、收益率、关键事件。`
	}
	return `你是 FinVault 个人理财助手，回答需简洁、口语化、保持中性视角，金额永远保留 2 位小数。`
}

// MarshalEvent 把 StreamEvent 序列化为 SSE data 行。供 handler 用。
func MarshalEvent(e StreamEvent) string {
	b, _ := json.Marshal(e)
	return string(b)
}

// HasError 工具：判断 chunk 是否携带错误。
func (e StreamEvent) HasError() bool { return e.Type == "error" }

// SinceUnix 提供单调时间戳，避免 import time 在 handler 测试。
func SinceUnix(t time.Time) int64 { return time.Since(t).Milliseconds() }
