package llm

import (
	"context"
	"errors"
	"fmt"
)

// =====================================================================
// FakeProvider —— 测试专用 LLM Provider
// =====================================================================
//
// 用法（service 单测）：
//
//	fp := llm.NewFakeProvider("fake", "fake-model")
//	fp.QueueChat(&llm.ChatResponse{
//	    Content:      "",
//	    FinishReason: "tool_calls",
//	    ToolCalls:    []llm.ToolCall{{ID: "c1", Name: "holding_query", Arguments: `{"user_id":1}`}},
//	})
//	fp.QueueChat(&llm.ChatResponse{
//	    Content:      "你的资产很均衡。",
//	    FinishReason: "stop",
//	    Usage:        llm.TokenUsage{TotalTokens: 42},
//	})
//	reg, _ := llm.NewFakeRegistry(fp)  // 或者 service 接受 Provider 直接注入也行
//
// 行为：
//   - Chat / ChatWithTools 按 FIFO 顺序返回 Queue 里的 ChatResponse；队列空时返回错误
//   - StreamChat 把 Content 切成多个 chunk 推送，最后发 FinishReason+Done
//   - 记录所有调用用于断言（Provided.Calls / Provided.LastTools 等）
//
// 不是为生产准备的，也不实现真正的 token 预算逻辑。

// FakeProvider 内存版 Provider 实现，仅用于测试。
type FakeProvider struct {
	name    string
	model   string
	queue   []*ChatResponse
	errs    []error // 与 queue 对齐：第 i 次调用如果 errs[i]!=nil 则返回 err 而非 ChatResponse
	calls   int
	history []FakeCall
}

// FakeCall 记录一次调用现场，便于断言。
type FakeCall struct {
	Method   string // "Chat" / "ChatWithTools" / "StreamChat"
	Messages []Message
	Tools    []Tool
}

// NewFakeProvider 构造一个 FakeProvider。
func NewFakeProvider(name, model string) *FakeProvider {
	return &FakeProvider{name: name, model: model}
}

// QueueChat 入队一个返回值（ChatWithTools 与 Chat 共用同一队列）。
func (f *FakeProvider) QueueChat(resp *ChatResponse) *FakeProvider {
	f.queue = append(f.queue, resp)
	f.errs = append(f.errs, nil)
	return f
}

// QueueErr 入队一个错误（下一次 Chat/ChatWithTools 返回此 err）。
func (f *FakeProvider) QueueErr(err error) *FakeProvider {
	f.queue = append(f.queue, nil)
	f.errs = append(f.errs, err)
	return f
}

// Calls 已发生的调用次数。
func (f *FakeProvider) Calls() int { return f.calls }

// History 返回调用历史（拷贝，避免外部修改）。
func (f *FakeProvider) History() []FakeCall {
	out := make([]FakeCall, len(f.history))
	copy(out, f.history)
	return out
}

// QueueLen 剩余队列长度。
func (f *FakeProvider) QueueLen() int { return len(f.queue) - f.calls }

// Reset 清空队列和历史。
func (f *FakeProvider) Reset() {
	f.queue = nil
	f.errs = nil
	f.calls = 0
	f.history = nil
}

// Name 实现 Provider 接口。
func (f *FakeProvider) Name() string { return f.name }

// Model 实现 Provider 接口。
func (f *FakeProvider) Model() string { return f.model }

// Chat 取队首响应。
func (f *FakeProvider) Chat(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	f.history = append(f.history, FakeCall{Method: "Chat", Messages: copyMessages(req.Messages)})
	resp, err := f.popResponse()
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ChatWithTools 取队首响应。
func (f *FakeProvider) ChatWithTools(_ context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error) {
	f.history = append(f.history, FakeCall{
		Method:   "ChatWithTools",
		Messages: copyMessages(req.Messages),
		Tools:    copyTools(tools),
	})
	resp, err := f.popResponse()
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// StreamChat 把 Content 切片成多个 chunk 推送，结束时发 Done。
//
// 如果队首是 err，会把 err 包装成 chunk.Err 单 chunk 推送。
func (f *FakeProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	f.history = append(f.history, FakeCall{Method: "StreamChat", Messages: copyMessages(req.Messages)})
	out := make(chan Chunk, 16)

	resp, err := f.popResponse()
	if err != nil {
		go func() {
			defer close(out)
			out <- Chunk{Done: true, Err: err}
		}()
		return out, nil
	}

	go func() {
		defer close(out)
		// 按 8 字节切片模拟 token 流
		content := resp.Content
		const chunkSize = 8
		for i := 0; i < len(content); i += chunkSize {
			select {
			case <-ctx.Done():
				out <- Chunk{Done: true, Err: ctx.Err()}
				return
			default:
			}
			end := i + chunkSize
			if end > len(content) {
				end = len(content)
			}
			out <- Chunk{Content: content[i:end]}
		}
		finish := resp.FinishReason
		if finish == "" {
			finish = "stop"
		}
		out <- Chunk{
			ToolCalls:    resp.ToolCalls,
			FinishReason: finish,
			Done:         true,
		}
	}()
	return out, nil
}

func (f *FakeProvider) popResponse() (*ChatResponse, error) {
	if f.calls >= len(f.queue) {
		return nil, fmt.Errorf("fake provider: no more queued responses (called %d times)", f.calls+1)
	}
	resp := f.queue[f.calls]
	err := f.errs[f.calls]
	f.calls++
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("fake provider: nil response queued")
	}
	return resp, nil
}

// =====================================================================
// FakeRegistry —— 内存版 Registry，可注册任意多个 FakeProvider
// =====================================================================

// FakeRegistry 测试专用 Registry。
type FakeRegistry struct {
	def       string
	providers map[string]Provider
}

// NewFakeRegistry 创建并注册一组 Provider；第一个 Provider 为默认。
func NewFakeRegistry(providers ...Provider) *FakeRegistry {
	r := &FakeRegistry{providers: make(map[string]Provider, len(providers))}
	for i, p := range providers {
		if p == nil {
			continue
		}
		r.providers[p.Name()] = p
		if i == 0 {
			r.def = p.Name()
		}
	}
	return r
}

// SetDefault 设置默认 Provider 名称。
func (r *FakeRegistry) SetDefault(name string) { r.def = name }

// Get 实现 Registry 接口。
func (r *FakeRegistry) Get(name string) (Provider, error) {
	if name == "" {
		name = r.def
	}
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return p, nil
}

// Default 返回默认 Provider 名。
func (r *FakeRegistry) Default() string { return r.def }

// List 实现 Registry 接口。
func (r *FakeRegistry) List() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(r.providers))
	for n, p := range r.providers {
		out = append(out, ProviderInfo{Name: n, Model: p.Model(), IsDefault: n == r.def})
	}
	return out
}

// =====================================================================
// 内部辅助（拷贝切片，避免测试断言被后续修改影响）
// =====================================================================

func copyMessages(in []Message) []Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]Message, len(in))
	copy(out, in)
	return out
}

func copyTools(in []Tool) []Tool {
	if len(in) == 0 {
		return nil
	}
	out := make([]Tool, len(in))
	copy(out, in)
	return out
}
