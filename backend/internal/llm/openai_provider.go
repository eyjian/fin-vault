package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// =====================================================================
// openaiProvider —— 全项目唯一允许 import go-openai 的实现
// =====================================================================

type openaiProvider struct {
	name        string
	model       string
	temperature float32
	maxTokens   int
	client      *openai.Client
}

// NewOpenAIProvider 基于 go-openai 构造一个兼容 Provider。
//
// DeepSeek / GLM / Kimi / 通义千问 / Ollama 等所有 OpenAI 协议兼容厂商都走这一个实现，
// 仅靠改 BaseURL + Model 即可切换，无需另写 Provider。
func NewOpenAIProvider(name string, cfg ProviderConfig) (Provider, error) {
	if cfg.APIKey == "" {
		// Ollama 等本地服务允许空 key，但仍需 BaseURL；其他强制要求。
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("llm provider %q: api_key empty and no base_url", name)
		}
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("llm provider %q: model is required", name)
	}
	oc := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		oc.BaseURL = cfg.BaseURL
	}
	if cfg.TimeoutSec > 0 {
		oc.HTTPClient.Timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	return &openaiProvider{
		name:        name,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		client:      openai.NewClientWithConfig(oc),
	}, nil
}

// Name 返回 Provider 名。
func (p *openaiProvider) Name() string { return p.name }

// Model 返回默认模型名。
func (p *openaiProvider) Model() string { return p.model }

// Chat 一次性补全（不传 tool）。
func (p *openaiProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	resp, err := p.client.CreateChatCompletion(ctx, p.buildRequest(req, nil, false))
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return &ChatResponse{Model: resp.Model, FinishReason: "stop"}, nil
	}
	return p.toResponse(&resp), nil
}

// StreamChat 流式输出。返回的 channel 在 finish 或 ctx 取消时关闭。
func (p *openaiProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	stream, err := p.client.CreateChatCompletionStream(ctx, p.buildRequest(req, nil, true))
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	out := make(chan Chunk, 16)
	go func() {
		defer close(out)
		defer stream.Close()
		// tool_calls 在 streaming 模式下会按 index 累积，需要拼接。
		toolCallAccum := make(map[int]*ToolCall)
		var lastFinish string
		for {
			select {
			case <-ctx.Done():
				out <- Chunk{Done: true, Err: ctx.Err()}
				return
			default:
			}
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				// 收尾：把累积的 tool_calls 转成切片
				if len(toolCallAccum) > 0 {
					tcs := flattenToolCalls(toolCallAccum)
					out <- Chunk{ToolCalls: tcs, FinishReason: lastFinish, Done: true}
				} else {
					out <- Chunk{FinishReason: lastFinish, Done: true}
				}
				return
			}
			if err != nil {
				out <- Chunk{Done: true, Err: err}
				return
			}
			if len(resp.Choices) == 0 {
				continue
			}
			choice := resp.Choices[0]
			lastFinish = string(choice.FinishReason)
			delta := choice.Delta
			if delta.Content != "" {
				out <- Chunk{Content: delta.Content}
			}
			for _, tc := range delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				acc, ok := toolCallAccum[idx]
				if !ok {
					acc = &ToolCall{}
					toolCallAccum[idx] = acc
				}
				if tc.ID != "" {
					acc.ID = tc.ID
				}
				if tc.Function.Name != "" {
					acc.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.Arguments += tc.Function.Arguments
				}
			}
		}
	}()
	return out, nil
}

// ChatWithTools 带工具的对话；返回单轮响应，Service 层判断 finish_reason 后再决定是否回填 tool 结果继续循环。
func (p *openaiProvider) ChatWithTools(ctx context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error) {
	openaiTools := convertTools(tools)
	resp, err := p.client.CreateChatCompletion(ctx, p.buildRequest(req, openaiTools, false))
	if err != nil {
		return nil, fmt.Errorf("openai chat with tools: %w", err)
	}
	if len(resp.Choices) == 0 {
		return &ChatResponse{Model: resp.Model, FinishReason: "stop"}, nil
	}
	return p.toResponse(&resp), nil
}

// =====================================================================
// 内部辅助：消息/工具的协议转换
// =====================================================================

func (p *openaiProvider) buildRequest(req ChatRequest, tools []openai.Tool, stream bool) openai.ChatCompletionRequest {
	model := req.Model
	if model == "" {
		model = p.model
	}
	temp := req.Temperature
	if temp == 0 {
		temp = p.temperature
	}
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = p.maxTokens
	}
	r := openai.ChatCompletionRequest{
		Model:       model,
		Messages:    convertMessages(req.Messages),
		Temperature: temp,
		MaxTokens:   maxTok,
		Stream:      stream,
	}
	if len(tools) > 0 {
		r.Tools = tools
	}
	if req.JSONMode {
		r.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}
	return r
}

func convertMessages(msgs []Message) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(msgs))
	for _, m := range msgs {
		om := openai.ChatCompletionMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]openai.ToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		out = append(out, om)
	}
	return out
}

func convertTools(tools []Tool) []openai.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		// 把 map 转成 json.RawMessage，go-openai 接收 any。
		var params any = t.Parameters
		if t.Parameters == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func (p *openaiProvider) toResponse(resp *openai.ChatCompletionResponse) *ChatResponse {
	choice := resp.Choices[0]
	out := &ChatResponse{
		Content:      choice.Message.Content,
		FinishReason: string(choice.FinishReason),
		Model:        resp.Model,
		Usage: TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
	if len(choice.Message.ToolCalls) > 0 {
		out.ToolCalls = make([]ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}
	return out
}

func flattenToolCalls(m map[int]*ToolCall) []ToolCall {
	if len(m) == 0 {
		return nil
	}
	max := -1
	for k := range m {
		if k > max {
			max = k
		}
	}
	out := make([]ToolCall, 0, max+1)
	for i := 0; i <= max; i++ {
		if v, ok := m[i]; ok {
			out = append(out, *v)
		}
	}
	return out
}

// SafeMarshalArgs 是给 Tool.Handler 写实现时用的辅助：把任意 args 结构体打成 JSON 字符串。
func SafeMarshalArgs(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// SafeUnmarshalArgs 把 LLM 给的 JSON 参数 解到目标结构体。空字符串视为空对象。
func SafeUnmarshalArgs(args string, v any) error {
	args = strings.TrimSpace(args)
	if args == "" {
		args = "{}"
	}
	return json.Unmarshal([]byte(args), v)
}
