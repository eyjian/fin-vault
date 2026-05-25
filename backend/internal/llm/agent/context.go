package agent

import "context"

// =============================================================================
// agent 包 ctx 注入助手（design.md D12 / D13 / D14）
//
// 本文件集中管理 agent 包对外暴露的 ctx 注入 helper。设计要点：
//   - 每个 ctxKey 用独立的 unexported struct 类型（含 D14 新增 assistantMessageIDCtxKey），
//     外包不能伪造 key（与 tools.ctxKeyType 同源策略）。
//   - service 层在调 Runner.Run 之前显式注入；agent 包内部从 ctx 提取，未注入时按场景
//     做合理兜底（user_id 兜底 "anonymous" 仅用作 SDK key 占位；assistant message id
//     未注入时由 runner_trpc.go 现场 uuid.NewString() 兜底，event_handler 落 step 时
//     降级写空串 + warn 不阻塞主流程）。
//
// 注意：
//   - 本包 user_id 是 string 类型（用于 SDK session.Key 拼接），与 tools 包的 uint
//     user_id（用于 repository 查询）职责不同；service 层会做两次注入，互不干扰。
//   - assistantMessageIDFromCtx 第二个返回值 ok=false 表示 service 层未注入（直接调
//     Runner 的非主路径），调用方降级处理而非阻塞。
// =============================================================================

// userIDCtxKey 业务 ctx 中携带 user_id（string）的 key。
type userIDCtxKey struct{}

// assistantMessageIDCtxKey 业务 ctx 中携带本轮 assistant message id 的 key（D14）。
type assistantMessageIDCtxKey struct{}

// WithUserID 在 ctx 中注入用户 ID（service 层在调 Runner.Run 之前调用）。
//
// 该 ID 主要用于 SDK inmemory session.Service 的 key 拼接，单次 Run 后即丢弃。
// 业务侧的用户隔离由 SessionStore + service 层校验保证。
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDCtxKey{}, userID)
}

// userIDFromCtx 从 ctx 取 user_id；未注入时返回 "anonymous"（仅占位）。
func userIDFromCtx(ctx context.Context) string {
	if v := ctx.Value(userIDCtxKey{}); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "anonymous"
}

// WithAssistantMessageID 在 ctx 中注入本轮 assistant message id（D14）。
//
// service 层在调 Runner.Run 之前预生成 uuid 并注入，使 event_handler 落 step 时
// 可以在 step.MessageID 字段填上同一 id，runner_trpc.go 落 assistant message 时
// 也用同一 id 作为 message.ID，从而保证 t_fv_ai_agent_steps.f_message_id 与
// t_fv_ai_messages.f_id 关联可靠（spec ai-agent-runtime 隐含约束 + D14）。
func WithAssistantMessageID(ctx context.Context, msgID string) context.Context {
	return context.WithValue(ctx, assistantMessageIDCtxKey{}, msgID)
}

// assistantMessageIDFromCtx 从 ctx 提取 assistant message id；
// 未注入或注入空串时返回 ("", false)，调用方按场景降级。
func assistantMessageIDFromCtx(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(assistantMessageIDCtxKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
