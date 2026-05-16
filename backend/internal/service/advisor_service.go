package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// AdvisorService —— 买卖建议 / 持仓建议（一次性，含 Tool Calling 循环）
// =====================================================================

// AdvisorService 顾问服务。
type AdvisorService struct {
	registry llm.Registry
	convRepo repository.AIConversationRepository
	toolReg  *tools.Registry
	maxHops  int
}

// NewAdvisorService 构造。
func NewAdvisorService(reg llm.Registry, convRepo repository.AIConversationRepository, toolReg *tools.Registry, maxHops int) *AdvisorService {
	if maxHops <= 0 {
		maxHops = 8
	}
	return &AdvisorService{registry: reg, convRepo: convRepo, toolReg: toolReg, maxHops: maxHops}
}

// RecommendInput 推荐请求。
type RecommendInput struct {
	UserID      uint
	Target      string // "buy_sell" / "allocation"
	AssetID     uint   // 可空：仅 buy_sell 针对单个资产时使用
	LLMProvider string
}

// RecommendOutput 推荐响应。
type RecommendOutput struct {
	Content        string                  `json:"content"`
	ToolCalls      []ToolCallTrace         `json:"tool_calls,omitempty"`
	ConversationID uint                    `json:"conversation_id,omitempty"`
	Usage          map[string]int          `json:"usage,omitempty"`
}

// ToolCallTrace 工具调用日志（前端可显示推理过程）。
type ToolCallTrace struct {
	Name   string `json:"name"`
	Args   string `json:"args"`
	Result string `json:"result"`
}

// Recommend 一次性建议（非流式）。
func (s *AdvisorService) Recommend(ctx context.Context, in RecommendInput) (*RecommendOutput, error) {
	if in.UserID == 0 {
		in.UserID = 1
	}
	target := in.Target
	if target == "" {
		target = "buy_sell"
	}
	prov, err := s.registry.Get(in.LLMProvider)
	if err != nil {
		return nil, errs.ErrAIProviderNotFound.WithCause(err)
	}

	// 创建会话用于保留过程
	conv := &domain.AIConversation{
		UserID:      in.UserID,
		Title:       "买卖/配置建议-" + target,
		Scene:       advisorScene(target),
		LLMProvider: prov.Name(),
		LLMModel:    prov.Model(),
		Status:      domain.StatusActive,
	}
	if err := s.convRepo.CreateConv(ctx, conv); err != nil {
		return nil, err
	}

	// 选 tools
	var toolList []llm.Tool
	switch target {
	case "buy_sell":
		toolList = s.toolReg.Pick("holding_query", "market_data", "history_query", "profit_calc")
	case "allocation":
		toolList = s.toolReg.Pick("holding_query", "platform_summary", "profit_calc", "market_data")
	default:
		toolList = s.toolReg.All()
	}

	prompt := advisorPrompt(target, in.UserID, in.AssetID)
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
		// 执行 tool calls 并回填
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

func advisorScene(target string) domain.AIScene {
	if target == "allocation" {
		return domain.AISceneAdvisor
	}
	return domain.AISceneBuySell
}

func advisorPrompt(target string, userID, assetID uint) string {
	var b strings.Builder
	switch target {
	case "buy_sell":
		b.WriteString("请帮我分析当前持仓并给出 1-3 条买/卖/持有建议。")
		if assetID > 0 {
			fmt.Fprintf(&b, "重点关注 asset_id=%d 这只资产。", assetID)
		}
	case "allocation":
		b.WriteString("请评估当前各平台/类型的资产配置是否合理，并给出 1-3 条调整建议。")
	default:
		b.WriteString("请综合给出投资建议。")
	}
	fmt.Fprintf(&b, "（user_id=%d）", userID)
	b.WriteString("回答最后请加一句风险提示。")
	return b.String()
}
