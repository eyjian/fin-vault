// Package handler —— AI 会话路由（spec ai-session）。
//
// 设计要点（与 design.md D12 / D15 + spec ai-session 对齐）：
//   - D12 边界：handler 不 import trpc-agent-go SDK；domain.Message.TokenUsage
//     已是 json.RawMessage，DTO 直接透传 raw JSON 给前端，避免再 import agent 包。
//   - D15 强校验：所有路由入口用 requireUserIDFromHeader 取 X-User-Id；
//     缺失 / 非法 / 0 → 401 ErrUnauthorized（绝不走 fallback=1 路径）。
//   - 跨用户隔离：service 层在 OtherUser 场景返 ErrAISessionNotFound（404 不暴露
//     存在性）；handler 直接通过 response.Fail 透传，绝不返 403。
//   - 删除返 204 No Content（spec §38-42 强制）；其他成功路径走 response.OK 统一包封。
package handler

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// DTO
// =====================================================================

// SessionCreateReq POST /api/v1/ai/sessions 请求体（可空）。
type SessionCreateReq struct {
	Title string `json:"title"`
}

// SessionCreateResp POST /api/v1/ai/sessions 响应（spec §10：字段名为 session_id 而非 id）。
type SessionCreateResp struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionUpdateReq PUT /api/v1/ai/sessions/:id 请求体。
//
// Title 用 *string 指针：nil 表示不更新，空串表示清空 title。
type SessionUpdateReq struct {
	Title *string `json:"title"`
}

// MessageDTO listMessages 响应单元（spec §72：role / content / created_at / token_usage）。
//
// TokenUsage 用 json.RawMessage 透传 domain 层 raw JSON：
//   - user 消息无 usage → omitempty 隐藏字段
//   - assistant 消息有完整 usage 结构（PromptTokens/CompletionTokens/TotalTokens）
//   - 不在 handler 层反序列化，避免 D12 边界引入 agent 包依赖
type MessageDTO struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	CreatedAt  time.Time       `json:"created_at"`
	TokenUsage json.RawMessage `json:"token_usage,omitempty"`
}

// =====================================================================
// Handler
// =====================================================================

// AISessionHandler 暴露 AI 会话 CRUD + 历史消息读路由。
type AISessionHandler struct {
	sessionSvc *service.AISessionService
}

// NewAISessionHandler 构造。
func NewAISessionHandler(sessionSvc *service.AISessionService) *AISessionHandler {
	return &AISessionHandler{sessionSvc: sessionSvc}
}

// Register 挂载到 /api/v1。
func (h *AISessionHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/ai/sessions")
	g.POST("", h.create)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
	g.GET("/:id/messages", h.listMessages)
}

// =====================================================================
// 路由实现
// =====================================================================

// create POST /api/v1/ai/sessions
//
// 请求体可空（spec §9）；响应 201 Created + SessionCreateResp。
func (h *AISessionHandler) create(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	var req SessionCreateReq
	// body 可空：ShouldBindJSON 在 body 为空时返 EOF，业务上视为合法的"无 title"请求
	_ = c.ShouldBindJSON(&req)

	sess, err := h.sessionSvc.Create(c.Request.Context(), uid, req.Title)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.JSON(201, response.Body{
		Code:    0,
		Message: "success",
		Data: SessionCreateResp{
			SessionID: sess.ID,
			Title:     sess.Title,
			CreatedAt: sess.CreatedAt,
		},
	})
}

// list GET /api/v1/ai/sessions?page=1&page_size=20
//
// 仅返回当前用户的会话（spec §23-27）；按 updated_at DESC 排序由 service/store 实现保证。
func (h *AISessionHandler) list(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	page := queryInt(c, "page", 1)
	pageSize := queryInt(c, "page_size", 20)

	list, total, err := h.sessionSvc.List(c.Request.Context(), service.SessionListInput{
		UserID:   uid,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 兜底：list 为 nil 时返 [] 而非 null（前端友好）
	if list == nil {
		list = []domain.Session{}
	}
	response.Page(c, list, total, page, pageSize)
}

// get GET /api/v1/ai/sessions/:id
//
// :id 是 string（UUID），跨用户访问 → ErrAISessionNotFound (404，不暴露存在性)。
func (h *AISessionHandler) get(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	sess, err := h.sessionSvc.Get(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, sess)
}

// update PUT /api/v1/ai/sessions/:id
//
// 当前仅支持改 title；body 解析失败 → 400 ErrInvalidParam。
// 跨用户访问 → ErrAISessionNotFound（404 不暴露存在性）。
func (h *AISessionHandler) update(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	var req SessionUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}

	sessionID := c.Param("id")
	if err := h.sessionSvc.Update(c.Request.Context(), uid, sessionID, service.SessionPatch{
		Title: req.Title,
	}); err != nil {
		response.Fail(c, err)
		return
	}
	// 回读最新状态（spec 暗示更新后返回完整 session 更利于前端同步）
	sess, err := h.sessionSvc.Get(c.Request.Context(), uid, sessionID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, sess)
}

// delete DELETE /api/v1/ai/sessions/:id
//
// 删除自有会话 → 204 No Content（spec §38-42 强制）。
// 跨用户访问 → ErrAISessionNotFound（404 不暴露存在性）。
func (h *AISessionHandler) delete(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	if err := h.sessionSvc.Delete(c.Request.Context(), uid, c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// listMessages GET /api/v1/ai/sessions/:id/messages?limit=N
//
// 按 created_at 升序（store 实现保证）；仅 user/assistant 两 role（spec §73：
// "不返回 tool 中间消息（仅作为附属事件由 step 接口提供）"）。
//
// limit query 可选；缺省走 service 默认（store 注入的 historyWindow）。
func (h *AISessionHandler) listMessages(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	limit := queryInt(c, "limit", 0)

	msgs, err := h.sessionSvc.ListMessages(c.Request.Context(), uid, c.Param("id"), limit)
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]MessageDTO, 0, len(msgs))
	for _, m := range msgs {
		// spec §73：仅 user/assistant；其他 role（system/tool）过滤
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		out = append(out, MessageDTO{
			ID:         m.ID,
			Role:       m.Role,
			Content:    m.Content,
			CreatedAt:  m.CreatedAt,
			TokenUsage: m.TokenUsage,
		})
	}
	response.OK(c, out)
}
