// Package llm 提供大模型 Provider 抽象与多模型路由。
//
// 全项目只有 openai_provider.go 一个文件允许 import sashabaranov/go-openai。
// 业务 service 层只能 import 本包，禁止直接 import 第三方 LLM SDK。
package llm

import (
	"context"
	"errors"
)

// =====================================================================
// 消息 / 工具 / 请求 / 响应 类型
// =====================================================================

// Message 单条对话消息（兼容 OpenAI 协议）。
type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 模型发起的一次工具调用请求。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// Tool 业务层注册的工具描述。
type Tool struct {
	Name        string                                                       // 工具名（必须英文蛇形）
	Description string                                                       // 给模型看的描述
	Parameters  map[string]any                                               // JSON Schema
	Handler     func(ctx context.Context, args string) (result string, err error) // 实际执行
}

// ChatRequest 一次对话请求。
type ChatRequest struct {
	Provider    string    // 可空，空则走默认 Provider
	Model       string    // 可空，空则走 Provider 默认模型
	Messages    []Message
	Temperature float32
	MaxTokens   int
	JSONMode    bool
}

// ChatResponse 非流式对话响应。
type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string // stop / tool_calls / length / content_filter
	Usage        TokenUsage
	Model        string
}

// Chunk 流式对话单个片段（SSE 推送给前端）。
type Chunk struct {
	Content      string     `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Done         bool       `json:"done"`
	Err          error      `json:"-"` // 流内部错误（前端展示前转 json）
}

// TokenUsage Token 使用统计。
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// =====================================================================
// 接口定义
// =====================================================================

// Provider 统一 LLM 协议接口。所有第三方 SDK 实现都对此接口建模。
type Provider interface {
	// Name 返回 Provider 名称（如 deepseek / glm / kimi / qwen / ollama）。
	Name() string

	// Model 返回 Provider 默认模型名。
	Model() string

	// Chat 一次性返回完整回复（不带工具调用）。
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// StreamChat 流式回复，返回 chunk channel；调用方负责 range 完成或 ctx.Done() 时退出。
	StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, error)

	// ChatWithTools 带工具的对话。Service 层负责接收 tool_calls -> 执行 -> 回填，循环直到 finish_reason!=tool_calls。
	ChatWithTools(ctx context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error)
}

// Registry 多 Provider 注册与路由。
type Registry interface {
	// Get 按名称返回 Provider；name 为空时返回默认。
	Get(name string) (Provider, error)
	// Default 返回默认 Provider 名称。
	Default() string
	// List 已注册的 Provider 名称列表。
	List() []ProviderInfo
}

// ProviderInfo Provider 元信息（供 /ai/providers 接口）。
type ProviderInfo struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	IsDefault bool   `json:"is_default"`
}

// =====================================================================
// 标准错误
// =====================================================================

var (
	// ErrProviderNotFound 未找到指定名称的 Provider。
	ErrProviderNotFound = errors.New("llm provider not found")
	// ErrProviderEmpty Registry 没有任何 Provider。
	ErrProviderEmpty = errors.New("no llm provider configured")
)
