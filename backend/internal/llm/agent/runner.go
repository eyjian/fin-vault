package agent

import (
	"context"
	"time"

	// 真实 import trpc-agent-go 子包，让占位文件可以在 §3.1.1 删除。
	// 这里只引用类型别名占位，避免在接口签名里直接暴露 SDK 类型（铁律 F2）。
	_ "trpc.group/trpc-go/trpc-agent-go/runner"
)

// ToolCall 描述 Agent 在一次 turn 内调用过的一个工具的可观测信息。
//
// 与 spec ai-tools "工具调用对用户透明可见" Scenario 对齐：
//   - Name        ：工具名（snake_case）
//   - Arguments   ：实际调用参数（已脱敏，按 design D7）
//   - StartedAt   ：调用开始时间（服务端 wall clock）
//   - FinishedAt  ：调用结束时间，失败时仍记录
//   - Status      ：success / failed / timeout
//   - ErrorMessage：失败时的简短错误消息（无堆栈），成功时为空
type ToolCall struct {
	Name         string                 `json:"name"`
	Arguments    map[string]interface{} `json:"arguments"`
	StartedAt    time.Time              `json:"started_at"`
	FinishedAt   time.Time              `json:"finished_at"`
	Status       string                 `json:"status"`
	ErrorMessage string                 `json:"error_message,omitempty"`
}

// TokenUsage 对应 spec ai-agent-runtime "token 用量被记录" Scenario 的载荷字段。
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Runner 业务侧 Agent 运行时接口（spec ai-agent-runtime "单例 Runner" / "框架被
// 替换时不影响业务层" 两个 Scenario 的载体）。
//
// 实现位于本包 runner_trpc.go（§5.2 落地），service / handler 仅依赖此接口。
type Runner interface {
	// Run 在指定会话内追加一条用户消息并产出 assistant 回复。
	//
	// 参数：
	//   ctx        ：取消信号 + 用户身份（user_id 已被 service 层注入 context）
	//   sessionID  ：UUID 字符串，对应 t_fv_ai_sessions.f_id
	//   userMessage：原始用户消息文本
	//
	// 返回：
	//   assistantMessage：模型回复文本
	//   toolCalls       ：本轮 turn 内调用过的全部工具（即使失败也包含）
	//   tokenUsage      ：本轮 LLM 调用的 token 总量
	//   err             ：业务错误码（AI_PROVIDER_* / AI_TOOL_* 等，按 design D7）
	Run(ctx context.Context, sessionID string, userMessage string) (
		assistantMessage string,
		toolCalls []ToolCall,
		tokenUsage TokenUsage,
		err error,
	)
}
