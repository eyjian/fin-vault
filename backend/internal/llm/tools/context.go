package tools

import "context"

// ctxKeyType 是 ctx key 的私有类型（unexported），防止外包伪造 key 绕过用户隔离。
//
// 与 design.md D13 规则 3 严格一致：每个包私有定义 ctxKey 类型，禁止跨包共享 key。
// agent 包另有自己的 user_id ctx key（string 类型，用于 SDK session key 拼接），
// 与本包的 uint user_id（用于 repository 查询）职责不同，互不干扰。
type ctxKeyType struct{}

// ctxKeyUserID 是携带 user_id 的 ctx key。
var ctxKeyUserID = ctxKeyType{}

// WithUserID 在 ctx 中注入 user_id，供工具 fn 内强制隔离使用。
//
// service 层在调 agent.Runner.Run 之前调用此函数（与 agent.WithUserID 双注入）；
// 工具 fn 通过 UserIDFromContext 取出后传给 repository 层做用户级隔离过滤。
func WithUserID(ctx context.Context, uid uint) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, uid)
}

// UserIDFromContext 从 ctx 提取 user_id；不存在或为 0 返回 (0, false)。
//
// 涉用户工具（market_quote / holding_query / history_query / profit_calc /
// platform_summary）必须用此函数获取身份，禁止读 args.UserID（涉用户工具
// 入参 schema 不应含该字段，按 D13 规则 1）。
//
// 公共数据工具（search_fund 等）不需调本函数。
func UserIDFromContext(ctx context.Context) (uint, bool) {
	v, ok := ctx.Value(ctxKeyUserID).(uint)
	if !ok || v == 0 {
		return 0, false
	}
	return v, true
}
