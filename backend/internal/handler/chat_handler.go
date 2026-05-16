package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// ChatHandler AI 对话接口（含 SSE 流式）。
type ChatHandler struct {
	svc *service.ChatService
}

// NewChatHandler 构造。
func NewChatHandler(svc *service.ChatService) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// Register 挂在 /api/v1 下。
func (h *ChatHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/ai")
	g.GET("/conversations", h.ListConversations)
	g.POST("/conversations", h.CreateConversation)
	g.GET("/conversations/:id/messages", h.ListMessages)
	g.POST("/chat/stream", h.Stream)
}

type createConvReq struct {
	Title       string `json:"title"`
	Scene       string `json:"scene" binding:"required"`
	LLMProvider string `json:"llm_provider"`
}

// CreateConversation POST /api/v1/ai/conversations
func (h *ChatHandler) CreateConversation(c *gin.Context) {
	var req createConvReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	uid := userIDFromHeader(c)
	conv, err := h.svc.CreateConversation(c.Request.Context(), service.CreateConversationInput{
		UserID:      uid,
		Title:       req.Title,
		Scene:       domain.AIScene(req.Scene),
		LLMProvider: req.LLMProvider,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, conv)
}

// ListConversations GET /api/v1/ai/conversations
func (h *ChatHandler) ListConversations(c *gin.Context) {
	uid := userIDFromHeader(c)
	page := queryInt(c, "page", 1)
	size := queryInt(c, "page_size", 20)
	scene := c.Query("scene")
	filters := map[string]any{}
	if scene != "" {
		filters["scene"] = scene
	}
	convs, total, err := h.svc.ListConversations(c.Request.Context(), uid, repository.ListOptions{
		UserID:   uid,
		Page:     page,
		PageSize: size,
		Filters:  filters,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, convs, total, page, size)
}

// ListMessages GET /api/v1/ai/conversations/:id/messages
func (h *ChatHandler) ListMessages(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	limit := queryInt(c, "limit", 50)
	msgs, err := h.svc.ListMessages(c.Request.Context(), uint(id), limit)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, msgs)
}

type streamReq struct {
	ConversationID uint   `json:"conversation_id"`
	Scene          string `json:"scene"`
	Content        string `json:"content" binding:"required"`
	LLMProvider    string `json:"llm_provider"`
}

// Stream POST /api/v1/ai/chat/stream
//
// 响应 Content-Type: text/event-stream，按 SSE 协议持续推送 chunk。
func (h *ChatHandler) Stream(c *gin.Context) {
	var req streamReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	uid := userIDFromHeader(c)
	out, err := h.svc.Stream(c.Request.Context(), service.StreamRequest{
		UserID:         uid,
		ConversationID: req.ConversationID,
		Scene:          domain.AIScene(req.Scene),
		Content:        req.Content,
		LLMProvider:    req.LLMProvider,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Stream(func(w io.Writer) bool {
		ev, ok := <-out
		if !ok {
			return false
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(service.MarshalEvent(ev), "\n", "\\n"))
		return ev.Type != "done" && ev.Type != "error"
	})
}
