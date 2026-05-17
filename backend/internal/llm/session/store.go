package session

import (
	"context"

	"github.com/eyjian/fin-vault/backend/internal/domain"
)

// ListSessionsOptions 列表分页查询参数。
//
// PageSize 默认 20，Page 从 1 开始；service 层负责校验并填默认值。
type ListSessionsOptions struct {
	UserID   uint
	Page     int
	PageSize int
}

// SessionStore AI 会话存储抽象。
//
// 实现位于本包 sqlite_store.go（§4 落地）。所有写入方法应在 service 层就完成
// 用户隔离校验（spec ai-session "列表只返回当前用户的会话" / "拒绝删除他人会话"），
// store 层仅做受信调用。
//
// 全部方法必须接受 context.Context 用于取消与请求级 trace。
type SessionStore interface {
	// 会话 CRUD
	CreateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	UpdateSession(ctx context.Context, s *domain.Session) error
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context, opts ListSessionsOptions) (sessions []domain.Session, total int64, err error)

	// 消息：按 created_at 升序拉取最近 N 条（spec "历史窗口生效"），N≤0 表示拉全部
	ListMessages(ctx context.Context, sessionID string, limit int) ([]domain.Message, error)
	AppendMessage(ctx context.Context, m *domain.Message) error

	// 步骤：仅追加，从不更新
	AppendStep(ctx context.Context, step *domain.AgentStep) error

	// EstimateStepsSize 估算 t_fv_ai_agent_steps 表占用空间（字节）。
	// 用于 ai.session.max_steps_size_mb 滚动清理（spec "阈值生效"）。
	// 实现可基于 SQLite pragma page_count * page_size 等近似手段。
	EstimateStepsSize(ctx context.Context) (sizeBytes int64, err error)
}
