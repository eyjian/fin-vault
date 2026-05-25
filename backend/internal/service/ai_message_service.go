package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/agent"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
)

// =====================================================================
// AIMessageService —— AI 单轮对话（spec ai-agent-runtime / ai-tools）
// =====================================================================
//
// 设计要点（与 design.md D12 / D13 / D14 + spec 对齐）：
//   - sessionID 归属校验先于 Runner.Run（防越权，spec "用户 A 不能向用户 B 的会话发消息"）。
//   - **不重复落 user / assistant message**：业务 Runner 自管两条消息落库（D12 第 5 步），
//     service 只透传 Runner 返回值。
//   - 双 ctx + D14 第三注入（顺序：tools.WithUserID → agent.WithUserID → agent.WithAssistantMessageID）：
//       * tools.WithUserID(ctx, userID)             —— D13 工具层 user 隔离（uint 版本，repository 查询用）
//       * agent.WithUserID(ctx, fmt.Sprint(userID)) —— D12 SDK session.Key 拼接（string 版本）
//       * agent.WithAssistantMessageID(ctx, uuid.NewString()) —— D14 step ↔ assistant message 关联
//   - 错误透传 Runner 返回（含 50001/50004/50005/50006/50007），不重新映射。
//   - D12 边界：本文件不 import trpc-agent-go SDK；只 import internal/llm/agent（业务接口）
//     + internal/llm/tools（WithUserID）+ internal/domain + google/uuid + fmt。

// SendResult AI 单轮对话的业务返回。
//
// 设计：返回 assistant message 完整结构而非仅文本，便于 handler 直接渲染（含 ID / TokenUsage / CreatedAt）。
type SendResult struct {
	AssistantMessage *domain.Message
	ToolCalls        []agent.ToolCall
	TokenUsage       agent.TokenUsage
}

// AIMessageService AI 消息服务（一次 Send 触发一轮 LLM 对话）。
type AIMessageService struct {
	sessionSvc *AISessionService
	runner     agent.Runner
}

// NewAIMessageService 构造 AI 消息服务。
func NewAIMessageService(sessionSvc *AISessionService, runner agent.Runner) *AIMessageService {
	return &AIMessageService{
		sessionSvc: sessionSvc,
		runner:     runner,
	}
}

// Send 用户发一条消息，返回本轮 assistant 回复 + 工具调用 + token 用量。
//
// 步骤：
//  1. 校验 sessionID 归属（防越权，失败透传 ErrAISessionNotFound）
//  2. 双 ctx + D14 第三注入（tools/agent userID + assistantMessageID）
//  3. 调 Runner.Run（业务 Runner 自管 user / assistant message 落库 + step 落库）
//  4. 包装返回 SendResult
//
// 不在本层重复落 user/assistant message（D12 第 5 步：业务 Runner 主导，service 不双写）。
func (s *AIMessageService) Send(ctx context.Context, userID uint, sessionID, userMsg string) (*SendResult, error) {
	// step 1: 归属校验（含 userID/sessionID 必填校验，错误透传）
	sess, err := s.sessionSvc.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}

	// step 2: 双 ctx + D14 第三注入
	ctx = tools.WithUserID(ctx, userID)
	ctx = agent.WithUserID(ctx, fmt.Sprint(userID))
	assistantMsgID := uuid.NewString()
	ctx = agent.WithAssistantMessageID(ctx, assistantMsgID)

	// step 3: Runner.Run（错误透传，含 50004/50005/50006/50007）
	assistantText, toolCalls, usage, err := s.runner.Run(ctx, sess.ID, userMsg)
	if err != nil {
		return nil, err
	}

	// step 4: 组装返回（assistant message 已被 Runner 落库到 t_fv_ai_messages，
	// 用 service 注入的 assistantMsgID 作为 ID 保证 step.MessageID == message.ID，D14）。
	// 这里返回的 Message 是构造视图（不再二次查库），字段与 Runner 落库时一致。
	result := &SendResult{
		AssistantMessage: &domain.Message{
			ID:        assistantMsgID,
			SessionID: sess.ID,
			Role:      "assistant",
			Content:   assistantText,
		},
		ToolCalls:  toolCalls,
		TokenUsage: usage,
	}
	return result, nil
}
