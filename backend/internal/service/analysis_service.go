package service

import (
	"context"
	"fmt"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// AnalysisService —— 盈亏分析（Tool Calling 循环）
// =====================================================================

// AnalysisService 盈亏分析服务。
type AnalysisService struct {
	registry llm.Registry
	convRepo repository.AIConversationRepository
	toolReg  *tools.Registry
	maxHops  int
}

// NewAnalysisService 构造。
func NewAnalysisService(reg llm.Registry, convRepo repository.AIConversationRepository, toolReg *tools.Registry, maxHops int) *AnalysisService {
	if maxHops <= 0 {
		maxHops = 8
	}
	return &AnalysisService{registry: reg, convRepo: convRepo, toolReg: toolReg, maxHops: maxHops}
}

// ProfitInput 盈亏分析输入。
type ProfitInput struct {
	UserID          uint
	Period          string // 形如 "2026-Q1" / "2026" / "last_30d"
	DisplayCurrency string // CNY / raw
	LLMProvider     string
}

// AnalyzeProfit 一次性盈亏分析（含 Tool Calling 循环）。
func (s *AnalysisService) AnalyzeProfit(ctx context.Context, in ProfitInput) (*RecommendOutput, error) {
	if in.UserID == 0 {
		in.UserID = 1
	}
	if in.DisplayCurrency == "" {
		in.DisplayCurrency = "CNY"
	}
	prov, err := s.registry.Get(in.LLMProvider)
	if err != nil {
		return nil, errs.ErrAIProviderNotFound.WithCause(err)
	}
	conv := &domain.AIConversation{
		UserID:      in.UserID,
		Title:       "盈亏分析-" + in.Period,
		Scene:       domain.AISceneAnalysis,
		LLMProvider: prov.Name(),
		LLMModel:    prov.Model(),
		Status:      domain.StatusActive,
	}
	if err := s.convRepo.CreateConv(ctx, conv); err != nil {
		return nil, err
	}

	toolList := s.toolReg.Pick("holding_query", "profit_calc", "history_query", "market_data")
	prompt := fmt.Sprintf(`请分析 user_id=%d 在 %s 期间的盈亏情况（展示币种 %s）。
要求：
- 总成本/总市值/已实现盈亏/未实现盈亏/总盈亏/收益率
- 按资产类型与平台分别给出贡献度
- 列出表现最好和最差的 3 只资产并解释原因
- 最后用一句话总结整体表现`,
		in.UserID, in.Period, in.DisplayCurrency)
	msgs := []llm.Message{
		{Role: domain.AIRoleSystem, Content: systemPrompt(conv.Scene)},
		{Role: domain.AIRoleUser, Content: prompt},
	}
	_ = s.convRepo.AppendMessage(ctx, &domain.AIMessage{
		ConversationID: conv.ID,
		Role:           domain.AIRoleUser,
		Content:        prompt,
	})

	traces := []ToolCallTrace{}
	hops := 0
	totalTokens := 0
	for hops < s.maxHops {
		resp, err := prov.ChatWithTools(ctx, llm.ChatRequest{Messages: msgs}, toolList)
		if err != nil {
			return nil, errs.ErrAIRequestFailed.WithCause(err)
		}
		totalTokens += resp.Usage.TotalTokens
		if resp.FinishReason != "tool_calls" {
			_ = s.convRepo.AppendMessage(ctx, &domain.AIMessage{
				ConversationID: conv.ID,
				Role:           domain.AIRoleAssistant,
				Content:        resp.Content,
				TokenCount:     resp.Usage.CompletionTokens,
			})
			_ = s.convRepo.IncrTokens(ctx, conv.ID, 2+len(traces), totalTokens)
			return &RecommendOutput{
				Content:        resp.Content,
				ToolCalls:      traces,
				ConversationID: conv.ID,
				Usage: map[string]int{
					"prompt_tokens":     resp.Usage.PromptTokens,
					"completion_tokens": resp.Usage.CompletionTokens,
					"total_tokens":      totalTokens,
				},
			}, nil
		}
		msgs = append(msgs, llm.Message{Role: domain.AIRoleAssistant, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			tool, ok := s.toolReg.Get(tc.Name)
			if !ok {
				return nil, errs.ErrAIToolCallFailed.WithMsg("unknown tool: " + tc.Name)
			}
			result, err := tool.Handler(ctx, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf(`{"error":true,"message":%q}`, err.Error())
			}
			traces = append(traces, ToolCallTrace{Name: tc.Name, Args: tc.Arguments, Result: result})
			_ = s.convRepo.AppendMessage(ctx, &domain.AIMessage{
				ConversationID: conv.ID,
				Role:           domain.AIRoleTool,
				Content:        result,
				ToolName:       tc.Name,
				ToolArgs:       tc.Arguments,
				ToolResult:     result,
				ToolCallID:     tc.ID,
			})
			msgs = append(msgs, llm.Message{Role: domain.AIRoleTool, Content: result, ToolCallID: tc.ID})
		}
		hops++
	}
	return nil, errs.New(50002, "tool calling exceeded max hops")
}
