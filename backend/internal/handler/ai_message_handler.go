// Package handler —— AI 消息发送路由（spec ai-agent-runtime + ai-tools）。
//
// 设计要点（与 design.md D12 / D13 / D14 / D15 + spec 对齐）：
//   - D12 边界：handler 不 import trpc-agent-go SDK；agent 包对外暴露的 ToolCall /
//     TokenUsage 都是纯 Go 类型（§5 定义），handler 直接复用。
//   - D15 强校验：入口用 requireUserIDFromHeader 取 X-User-Id；缺失 / 非法 / 0 →
//     401 ErrUnauthorized（与 ai_session_handler 同策略）。
//   - 跨用户隔离：service.AIMessageService.Send 内部先做 sessionID 归属校验，
//     失败返 ErrAISessionNotFound（404 不暴露存在性，绝不返 403）；handler 透传。
//   - 错误透传：Runner 失败的业务错误码（ErrAIRequestFailed / ErrAIToolCallFailed /
//     ErrAIProviderRateLimited / ErrAIToolNotFound）由 service 层透传，handler 不
//     重新映射，response.Fail 按 errs.Error 自动决定 HTTP 状态码。
//   - 失败工具调用也返回（spec ai-tools §53-57）：SendResp.ToolCalls 包含本轮所有
//     ToolCall（含 Status="failed" + ErrorMessage），让前端能展示失败原因。
package handler

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/llm/agent"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// DTO
// =====================================================================

// SendReq POST /api/v1/ai/sessions/:id/messages 请求体。
type SendReq struct {
	Content string `json:"content" binding:"required"` // 用户消息文本（spec ai-agent-runtime）
}

// ToolCallDTO 单次工具调用的可观测信息（spec ai-tools §53-57：成功 / 失败均返回）。
//
// Arguments 在 service.SendResult 里是 map[string]interface{}（agent.ToolCall.Arguments
// 类型），handler 层 marshal 成 JSON 字符串透传给前端，避免 map 排序不稳定影响 e2e 断言。
type ToolCallDTO struct {
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	StartedAt    string          `json:"started_at"` // RFC3339；time.Time MarshalJSON 后等价
	FinishedAt   string          `json:"finished_at"`
	Status       string          `json:"status"`                  // success / failed
	ErrorMessage string          `json:"error_message,omitempty"` // 失败时简短消息
}

// SendResp POST /api/v1/ai/sessions/:id/messages 响应。
//
// AssistantMessage 复用 ai_session_handler 的 MessageDTO 结构（同包 unexported 类型）。
// 注意：service 层 SendResult.AssistantMessage 当前只填 ID/SessionID/Role/Content（不含
// CreatedAt/TokenUsage —— 这两项在 Runner 落库时已写入；handler 这里 CreatedAt 留零值
// 以避免与 Runner 落库时间不一致，前端可通过 ListMessages 拿到权威值）；TokenUsage
// 在响应顶层 token_usage 字段独立返回，避免 MessageDTO 透传 raw JSON 与本期结构化字段
// 类型不一致。
type SendResp struct {
	AssistantMessage MessageDTO       `json:"assistant_message"`
	ToolCalls        []ToolCallDTO    `json:"tool_calls"`
	TokenUsage       agent.TokenUsage `json:"token_usage"`
}

// =====================================================================
// Handler
// =====================================================================

// AIMessageHandler 暴露 POST /ai/sessions/:id/messages（一次 LLM 对话）。
type AIMessageHandler struct {
	msgSvc *service.AIMessageService
}

// NewAIMessageHandler 构造。
func NewAIMessageHandler(msgSvc *service.AIMessageService) *AIMessageHandler {
	return &AIMessageHandler{msgSvc: msgSvc}
}

// Register 挂载到 /api/v1。
func (h *AIMessageHandler) Register(r *gin.RouterGroup) {
	r.POST("/ai/sessions/:id/messages", h.send)
}

// =====================================================================
// 路由实现
// =====================================================================

// send POST /api/v1/ai/sessions/:id/messages
//
// 请求：SendReq{ content: string(必填) }；
// 响应：200 OK + SendResp{ assistant_message, tool_calls, token_usage }；
// 错误：
//   - 401 缺失/非法 X-User-Id（D15）
//   - 400 content 为空（gin binding required）
//   - 404 sessionID 不属于当前用户（service 层归属校验，绝不暴露存在性）
//   - 400 50004/50005/50006/50007（Runner 业务错误透传）
func (h *AIMessageHandler) send(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}

	sessionID := c.Param("id")
	var req SendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}

	result, err := h.msgSvc.Send(c.Request.Context(), uid, sessionID, req.Content)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.OK(c, toSendResp(result))
}

// =====================================================================
// DTO 转换
// =====================================================================

// toSendResp 把 service.SendResult 映射到 SendResp。
//
// ToolCalls 即使为空也返回 [] 而非 null（前端友好）。
// Arguments 用 json.Marshal 转 JSON 字节流；解析失败时返空 raw（不应阻塞响应）。
func toSendResp(r *service.SendResult) SendResp {
	resp := SendResp{
		ToolCalls:  make([]ToolCallDTO, 0, len(r.ToolCalls)),
		TokenUsage: r.TokenUsage,
	}
	if r.AssistantMessage != nil {
		resp.AssistantMessage = MessageDTO{
			ID:        r.AssistantMessage.ID,
			Role:      r.AssistantMessage.Role,
			Content:   r.AssistantMessage.Content,
			CreatedAt: r.AssistantMessage.CreatedAt, // 当前 service 层零值；前端可经 ListMessages 拿权威值
			// TokenUsage 在 SendResp 顶层结构化返回，不在 MessageDTO 里重复（避免 raw JSON 与
			// agent.TokenUsage 类型不一致，对前端更明确）
		}
	}
	for _, tc := range r.ToolCalls {
		dto := ToolCallDTO{
			Name:         tc.Name,
			StartedAt:    tc.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			FinishedAt:   tc.FinishedAt.Format("2006-01-02T15:04:05Z07:00"),
			Status:       tc.Status,
			ErrorMessage: tc.ErrorMessage,
		}
		if len(tc.Arguments) > 0 {
			if raw, err := json.Marshal(tc.Arguments); err == nil {
				dto.Arguments = raw
			}
		}
		resp.ToolCalls = append(resp.ToolCalls, dto)
	}
	return resp
}
