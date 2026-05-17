package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// AISessionService —— AI 会话 CRUD（spec ai-session）
// =====================================================================
//
// 设计要点（与 design.md D2 / D12 + spec ai-session 对齐）：
//   - 用户隔离在本层完成：所有按 sessionID 操作的方法第一步取出 session 后
//     校验 session.UserID == userID；不匹配返 errs.ErrAISessionNotFound（404
//     不暴露存在性，spec "拒绝删除他人会话"）。**绝不**返 ErrForbidden / 403。
//   - userID 由 handler 从 header 取后显式传入（不从 ctx 取，避免与 agent / tools
//     ctx 注入混淆）；与 AssetService / HoldingService / TransactionService 同风格。
//   - SessionStore 写入侧是受信调用，只承担存储逻辑；用户隔离由本层保证。
//   - D12 边界：本文件不 import trpc-agent-go SDK；只 import internal/llm/session（store）
//     + internal/domain + pkg/errs + google/uuid。

// SessionPatch 更新会话的可选字段（零值表示不更新）。
type SessionPatch struct {
	Title *string
}

// SessionListInput 列表查询入参。
type SessionListInput struct {
	UserID   uint
	Page     int
	PageSize int
}

// AISessionService AI 会话服务。
type AISessionService struct {
	store session.SessionStore
}

// NewAISessionService 构造 AI 会话服务。
func NewAISessionService(store session.SessionStore) *AISessionService {
	return &AISessionService{store: store}
}

// Create 创建会话。userID 必填，title 允许空（默认空串，前端可后续 Update）。
func (s *AISessionService) Create(ctx context.Context, userID uint, title string) (*domain.Session, error) {
	if userID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id required")
	}
	now := time.Now()
	sess := &domain.Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		Title:     strings.TrimSpace(title),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	return sess, nil
}

// Get 取单条会话。归属不匹配返 ErrAISessionNotFound（404 不暴露存在性）。
func (s *AISessionService) Get(ctx context.Context, userID uint, sessionID string) (*domain.Session, error) {
	if userID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id required")
	}
	if sessionID == "" {
		return nil, errs.ErrAISessionNotFound
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		// store 层未找到时已返 ErrAISessionNotFound（sqlite_store §4 落地），透传即可
		if errors.Is(err, errs.ErrAISessionNotFound) {
			return nil, errs.ErrAISessionNotFound
		}
		return nil, errs.ErrDB.WithCause(err)
	}
	if sess.UserID != userID {
		// 归属不匹配 → 同 404 语义，不暴露存在性（spec "拒绝删除他人会话 404 不暴露存在性"
		// 同语义，扩展到 Get/Update/Delete 全部按 sessionID 的单条操作）。
		return nil, errs.ErrAISessionNotFound
	}
	return sess, nil
}

// Update 更新会话（目前仅 title）。归属校验同 Get。
func (s *AISessionService) Update(ctx context.Context, userID uint, sessionID string, patch SessionPatch) error {
	sess, err := s.Get(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	dirty := false
	if patch.Title != nil {
		sess.Title = strings.TrimSpace(*patch.Title)
		dirty = true
	}
	if !dirty {
		return nil
	}
	sess.UpdatedAt = time.Now()
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return errs.ErrDB.WithCause(err)
	}
	return nil
}

// Delete 删除会话（级联删 messages + agent_steps，由 store 层事务保证）。
//
// 归属不匹配返 ErrAISessionNotFound（spec "拒绝删除他人会话 404 不暴露存在性"）。
func (s *AISessionService) Delete(ctx context.Context, userID uint, sessionID string) error {
	if _, err := s.Get(ctx, userID, sessionID); err != nil {
		return err
	}
	if err := s.store.DeleteSession(ctx, sessionID); err != nil {
		return errs.ErrDB.WithCause(err)
	}
	return nil
}

// List 列出当前用户的会话（分页 + 按 updated_at 倒序，由 store 实现保证）。
//
// 用户隔离在 store 层 ListSessions 用 opts.UserID 过滤，spec "列表只返回当前用户的会话"
// 在 store 层兜底，service 层只确保 opts.UserID 来自 handler 传入而非 args。
func (s *AISessionService) List(ctx context.Context, in SessionListInput) ([]domain.Session, int64, error) {
	if in.UserID == 0 {
		return nil, 0, errs.ErrInvalidParam.WithMsg("user_id required")
	}
	list, total, err := s.store.ListSessions(ctx, session.ListSessionsOptions{
		UserID:   in.UserID,
		Page:     in.Page,
		PageSize: in.PageSize,
	})
	if err != nil {
		return nil, 0, errs.ErrDB.WithCause(err)
	}
	return list, total, nil
}

// ListMessages 列出指定会话的消息。归属校验先于消息拉取。
func (s *AISessionService) ListMessages(ctx context.Context, userID uint, sessionID string, limit int) ([]domain.Message, error) {
	if _, err := s.Get(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	msgs, err := s.store.ListMessages(ctx, sessionID, limit)
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	return msgs, nil
}
