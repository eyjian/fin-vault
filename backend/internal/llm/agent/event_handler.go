package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	sdkevent "trpc.group/trpc-go/trpc-agent-go/event"
	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
)

// AggregateResult 是 AggregateEvents 的输出，包含一次 turn 的全部业务可观测信息。
type AggregateResult struct {
	AssistantMsg string
	ToolCalls    []ToolCall
	Usage        TokenUsage
}

// AggregateEvents 消费 SDK event channel，聚合结果，并实时把 step 事件落库（含敏感字段掩码）。
//
// SDK 设计中 event.Event 是单一类型，靠 Response.Object 字段 + Event helper 方法分类。
// 本函数把 SDK 事件映射到 spec ai-agent-runtime 的 4 类业务 step：
//
//   - "tool_call_started"   ← Object="chat.completion" 且 Choice.Message.ToolCalls 非空
//     （assistant 决定调工具，每个 ToolCall 对应一条 step）
//   - "tool_call_finished"  ← Object="tool.response"（工具实际执行完，含成功/失败）
//   - "token_usage"         ← Response.Usage 非 nil（chunk 末尾或完整响应）
//   - "step_boundary"       ← Object="runner.completion"（runner 收尾事件）
//
// D11 工具失败软处理：
//
//   - tool.response 携带 Response.Error 非 nil → ToolCall.Status="failed" + ErrorMessage，**不**返回错误，
//     让 assistant 有机会基于失败信号继续决策（重试/兜底/询问用户）。
//
// LLM 流硬失败：
//
//   - ev.IsError() 为 true → 立即调 MapResponseError 返回业务错误，停止聚合。
//
// step 落库失败为软依赖：仅记 warn，不打断主流程（spec ai-agent-runtime 描述）。
func AggregateEvents(
	ctx context.Context,
	ch <-chan *sdkevent.Event,
	store session.SessionStore,
	sessionID string,
	logger *slog.Logger,
) (*AggregateResult, error) {
	if logger == nil {
		logger = slog.Default()
	}

	result := &AggregateResult{}
	var assistantBuilder strings.Builder

	// 缓存 tool_call_started 等 tool.response 配对（按 ToolID）
	pendingToolCalls := make(map[string]*ToolCall)

	for ev := range ch {
		if ev == nil || ev.Response == nil {
			continue
		}
		rsp := ev.Response

		// 1) 全局错误事件 → 立即返回业务错误。
		//
		// 注意：我们用 Object == ObjectTypeError 判硬失败，**不**使用 SDK 的 ev.IsError()。
		// 因为 IsError() 同时把"工具响应携带 Response.Error"当作错误事件，但 D11 明确要求
		// 工具失败走"软失败"路径（在 ToolCalls 项标 Status="failed"），不能让 Run 整体失败。
		if rsp.Object == sdkmodel.ObjectTypeError {
			return result, MapResponseError(rsp.Error)
		}

		switch rsp.Object {
		case sdkmodel.ObjectTypeChatCompletionChunk:
			handleChatChunk(ctx, ev, rsp, &assistantBuilder, result, store, sessionID, logger)

		case sdkmodel.ObjectTypeChatCompletion:
			handleChatCompletion(ctx, ev, rsp, &assistantBuilder, result, pendingToolCalls, store, sessionID, logger)

		case sdkmodel.ObjectTypeToolResponse:
			handleToolResponse(ctx, ev, rsp, result, pendingToolCalls, store, sessionID, logger)

		case sdkmodel.ObjectTypeRunnerCompletion:
			handleRunnerCompletion(ctx, ev, store, sessionID, logger)

		default:
			// 其它 Object（preprocessing.* / postprocessing.* / state.update / agent.transfer 等）
			// 暂不落业务 step；如需扩展按 Object 字段加 case 分支即可
		}
	}

	result.AssistantMsg = assistantBuilder.String()
	return result, nil
}

// handleChatChunk 流式 chunk：累积文本 + 累加 usage（出现时落 token_usage step）
func handleChatChunk(
	ctx context.Context,
	ev *sdkevent.Event,
	rsp *sdkmodel.Response,
	builder *strings.Builder,
	result *AggregateResult,
	store session.SessionStore,
	sessionID string,
	logger *slog.Logger,
) {
	for _, c := range rsp.Choices {
		if c.Delta.Content != "" {
			builder.WriteString(c.Delta.Content)
		}
	}
	if rsp.Usage != nil {
		accumulateUsage(&result.Usage, rsp.Usage)
		appendStepSafe(ctx, store, sessionID, "token_usage", "", map[string]any{
			"prompt_tokens":     rsp.Usage.PromptTokens,
			"completion_tokens": rsp.Usage.CompletionTokens,
			"total_tokens":      rsp.Usage.TotalTokens,
		}, logger)
	}
	_ = ev // 保留参数以备未来记录 ev.RequestID 等
}

// handleChatCompletion 完整 chat.completion：拼文本（无 ToolCalls 时）+ 落 tool_call_started step（有 ToolCalls 时）
func handleChatCompletion(
	ctx context.Context,
	ev *sdkevent.Event,
	rsp *sdkmodel.Response,
	builder *strings.Builder,
	result *AggregateResult,
	pending map[string]*ToolCall,
	store session.SessionStore,
	sessionID string,
	logger *slog.Logger,
) {
	for _, c := range rsp.Choices {
		// 当无工具调用时，content 是 assistant 完整回复（非流式场景）
		if c.Message.Content != "" && len(c.Message.ToolCalls) == 0 {
			builder.WriteString(c.Message.Content)
		}
		// 工具调用：每个 ToolCall 落 tool_call_started step + 缓存等待 tool.response 配对
		for _, tc := range c.Message.ToolCalls {
			args, _ := parseArguments(tc.Function.Arguments)
			bizTC := &ToolCall{
				Name:      tc.Function.Name,
				Arguments: args,
				StartedAt: ev.Timestamp,
				Status:    "pending",
			}
			pending[tc.ID] = bizTC
			appendStepSafe(ctx, store, sessionID, "tool_call_started", tc.Function.Name, map[string]any{
				"tool_id":   tc.ID,
				"tool_name": tc.Function.Name,
				"arguments": json.RawMessage(tc.Function.Arguments),
			}, logger)
		}
	}
	if rsp.Usage != nil {
		accumulateUsage(&result.Usage, rsp.Usage)
		appendStepSafe(ctx, store, sessionID, "token_usage", "", map[string]any{
			"prompt_tokens":     rsp.Usage.PromptTokens,
			"completion_tokens": rsp.Usage.CompletionTokens,
			"total_tokens":      rsp.Usage.TotalTokens,
		}, logger)
	}
}

// handleToolResponse 工具响应：与 pending 配对，落 tool_call_finished step，写入 result.ToolCalls
func handleToolResponse(
	ctx context.Context,
	ev *sdkevent.Event,
	rsp *sdkmodel.Response,
	result *AggregateResult,
	pending map[string]*ToolCall,
	store session.SessionStore,
	sessionID string,
	logger *slog.Logger,
) {
	for _, c := range rsp.Choices {
		m := c.Message
		tc, ok := pending[m.ToolID]
		if !ok {
			// 配对不上：仍生成 ToolCall 项，避免吞事件
			tc = &ToolCall{Name: m.ToolName, StartedAt: ev.Timestamp, Status: "pending"}
		}
		tc.FinishedAt = ev.Timestamp
		if rsp.Error != nil {
			// D11 软失败：标 failed + ErrorMessage，但不返回错误
			tc.Status = "failed"
			tc.ErrorMessage = rsp.Error.Message
		} else {
			tc.Status = "success"
		}
		result.ToolCalls = append(result.ToolCalls, *tc)
		errPayload := ""
		if rsp.Error != nil {
			errPayload = rsp.Error.Message
		}
		appendStepSafe(ctx, store, sessionID, "tool_call_finished", m.ToolName, map[string]any{
			"tool_id":   m.ToolID,
			"tool_name": m.ToolName,
			"result":    m.Content,
			"error":     errPayload,
			"status":    tc.Status,
		}, logger)
		delete(pending, m.ToolID)
	}
}

// handleRunnerCompletion 收尾事件 → 落 step_boundary
func handleRunnerCompletion(
	ctx context.Context,
	ev *sdkevent.Event,
	store session.SessionStore,
	sessionID string,
	logger *slog.Logger,
) {
	appendStepSafe(ctx, store, sessionID, "step_boundary", "", map[string]any{
		"request_id":    ev.RequestID,
		"invocation_id": ev.InvocationID,
		"author":        ev.Author,
	}, logger)
}

// accumulateUsage 把 SDK Usage 累加到业务 TokenUsage（一次 Run 内可能有多次 LLM 调用）
func accumulateUsage(dst *TokenUsage, src *sdkmodel.Usage) {
	if src == nil {
		return
	}
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	dst.TotalTokens += src.TotalTokens
}

// parseArguments 把 tool 调用的 arguments（JSON 字节流）解析为业务 ToolCall.Arguments map。
// 解析失败时返回 nil（不阻断 step 落库，原始 JSON 仍会通过 step.Payload.arguments 字段保留）。
func parseArguments(raw []byte) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// appendStepSafe 把 payload 序列化 + 脱敏 + 落库；失败仅 warn 不阻塞主流程（spec 软依赖）。
func appendStepSafe(
	ctx context.Context,
	store session.SessionStore,
	sessionID, eventType, toolName string,
	payload any,
	logger *slog.Logger,
) {
	raw, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("marshal step payload failed", "err", err, "event_type", eventType)
		return
	}
	raw = session.MaskSensitiveJSON(raw)
	step := &domain.AgentStep{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		EventType: eventType,
		ToolName:  toolName,
		Payload:   raw,
	}
	if err := store.AppendStep(ctx, step); err != nil {
		logger.Warn("append step failed", "err", err, "event_type", eventType, "tool_name", toolName)
	}
}
