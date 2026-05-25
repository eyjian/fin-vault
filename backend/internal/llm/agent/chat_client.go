// Package agent —— 单轮 LLM 对话客户端（spec ai-pulse-diagnosis 把脉调用基础）。
//
// 设计要点（与 design.md D11 + 铁律 F2 对齐）：
//   - 业务层（service）通过 ChatClient 接口调用 LLM，不直接 import trpc-agent-go SDK
//   - 实现 sdkChatClient 封装 sdkmodel.Model.GenerateContent 流式聚合 + 错误归一
//   - 与 agent.Runner 同地位：Runner 用于多轮 + tool calling 的会话场景；
//     ChatClient 用于单轮、无工具的轻量调用（如把脉、摘要生成、风险提示重写）
//   - bootstrap 在 wireAI 中构造 SDK Model 后，顺便构造 ChatClient 注入业务 service
//
// D16 降级：LLM 不可用时，bootstrap 不构造 ChatClient（保持 nil），上层 service
// 应感知 nil 并返回业务错误（与 AIMessageService 的 Runner=nil 同策略）。
package agent

import (
	"context"
	"errors"
	"strings"

	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// ChatClient 单轮 LLM 对话客户端。
//
// 业务层（如 PulseDiagnosisService）通过本接口完成"给一段 prompt → 拿一段文本"的
// 轻量交互，不卷入 SDK Model / Request / Response 等具体类型。
type ChatClient interface {
	// Chat 发送一次单轮对话，返回模型完整响应文本（已聚合所有流式 chunk）。
	//
	// 参数：
	//   ctx          ：取消信号 + 超时控制
	//   systemPrompt ：系统提示（角色设定与输出格式约束），允许空串
	//   userPrompt   ：用户消息，必填
	//
	// 返回：
	//   content ：模型完整回复文本
	//   err     ：业务错误（errs.ErrAIRequestFailed / errs.ErrAIProviderRateLimited 等）
	Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// sdkChatClient 基于 trpc-agent-go SDK Model 的 ChatClient 实现。
type sdkChatClient struct {
	model sdkmodel.Model
}

// NewSDKChatClient 用 SDK Model 构造 ChatClient。
//
// model 必须非 nil；nil 应在 bootstrap 层 D16 降级路径中早跳过，不构造 ChatClient。
func NewSDKChatClient(model sdkmodel.Model) ChatClient {
	return &sdkChatClient{model: model}
}

// Chat 实现 ChatClient.Chat。
//
// 实现要点：
//   - 构造 sdkmodel.Request：system + user 两条 Message
//   - GenerateContent 返回 chan，按 Choices[0].Delta.Content（流式）或 Message.Content（非流式）
//     聚合所有 chunk
//   - 任一 chunk 的 Response.Error 非空 → 立即返业务错误（不等流结束）
//   - 通道关闭后返回聚合内容
//
// 业务错误映射（与 AIMessageService Runner.Run 错误同口径）：
//   - SDK 函数级 error → ErrAIRequestFailed
//   - Response.Error.Message 含 "rate limit" / "429" → ErrAIProviderRateLimited
//   - 其它 Response.Error → ErrAIRequestFailed（附 cause）
func (c *sdkChatClient) Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.model == nil {
		return "", errs.ErrAIRequestFailed.WithMsg("llm model not configured")
	}
	if strings.TrimSpace(userPrompt) == "" {
		return "", errs.ErrInvalidParam.WithMsg("user prompt required")
	}

	msgs := make([]sdkmodel.Message, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, sdkmodel.Message{
			Role:    sdkmodel.RoleSystem,
			Content: systemPrompt,
		})
	}
	msgs = append(msgs, sdkmodel.Message{
		Role:    sdkmodel.RoleUser,
		Content: userPrompt,
	})

	req := &sdkmodel.Request{Messages: msgs}
	respCh, err := c.model.GenerateContent(ctx, req)
	if err != nil {
		return "", errs.ErrAIRequestFailed.WithCause(err)
	}

	var sb strings.Builder
	for resp := range respCh {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			msg := resp.Error.Message
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "rate limit") || strings.Contains(lower, "429") {
				return "", errs.ErrAIProviderRateLimited.WithCause(errors.New(msg))
			}
			return "", errs.ErrAIRequestFailed.WithCause(errors.New(msg))
		}
		if len(resp.Choices) == 0 {
			continue
		}
		ch := resp.Choices[0]
		// 流式 chunk：累加 Delta.Content；非流式：用 Message.Content（最后一帧）
		if ch.Delta.Content != "" {
			sb.WriteString(ch.Delta.Content)
		} else if ch.Message.Content != "" && resp.Done {
			// 非流式或最终聚合帧，直接覆盖（避免与 Delta 重复）
			if sb.Len() == 0 {
				sb.WriteString(ch.Message.Content)
			}
		}
	}

	out := sb.String()
	if out == "" {
		return "", errs.ErrAIRequestFailed.WithMsg("empty response from llm")
	}
	return out, nil
}
