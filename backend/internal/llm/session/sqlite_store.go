package session

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// sqliteStore 是 SessionStore 接口的 SQLite 实现，由 GORM 操作 t_fv_ai_*
// 三张表（详见 §2.1 ai_session.go）。
//
// 设计要点（与 design.md D4/D5/D7 + spec ai-session 对齐）：
//   - 写入侧严格"受信调用"：CreateSession/GetSession/UpdateSession/DeleteSession
//     不重复校验 user_id，service 层负责用户隔离；ListSessions 必须按
//     opts.UserID 过滤，承担"列表只返回当前用户的会话"的安全语义。
//   - 删除采用硬删 + 事务级联：domain.Session/Message/AgentStep 三 struct 均无
//     DeletedAt 字段（§2.1 设计），删 Session 时同事务删 messages + agent_steps。
//   - AppendStep 写库前对 Payload 做敏感字段掩码（design D7 + mask.go）。
//
// historyWindow 是构造时注入的默认窗口大小，对应 ai.session.history_window
// 配置项；ListMessages 调用方传入 limit > 0 时按其覆盖，否则用 historyWindow。
type sqliteStore struct {
	db            *gorm.DB
	historyWindow int
}

// NewSQLiteStore 构造 SQLite 实现。
//
// historyWindow 必须 > 0（bootstrap/config.go 已校验 cfg.AI.Session.HistoryWindow > 0，
// 进入此处必然合法）。出于防御传入 ≤0 时构造仍成功但兜底为 20，避免 panic 影响整个进程启动。
func NewSQLiteStore(db *gorm.DB, historyWindow int) SessionStore {
	if historyWindow <= 0 {
		historyWindow = 20
	}
	return &sqliteStore{db: db, historyWindow: historyWindow}
}

// 编译期断言：sqliteStore 满足 SessionStore 接口。
var _ SessionStore = (*sqliteStore)(nil)

// =====================================================================
// 会话 CRUD（receiver method 顺序与 SessionStore 接口顺序一致）
// =====================================================================

// CreateSession 写一行到 t_fv_ai_sessions。
//
// 要求 s.ID 由 service 层用 google/uuid 生成（store 层不再兜底）；
// CreatedAt / UpdatedAt 留空时由本方法填 now，且 UpdatedAt 默认 = CreatedAt
// （spec ListSessions 用 updated_at 排序，未编辑前应等于创建时间）。
func (r *sqliteStore) CreateSession(ctx context.Context, s *domain.Session) error {
	if s == nil || s.ID == "" {
		return errs.ErrAIConversationNotFound
	}
	now := time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
	return r.db.WithContext(ctx).Create(s).Error
}

// GetSession 按 sessionID 查询单条。
//
// 受信调用：不强制 user_id 匹配，业务层校验由 service 做。
// 未找到时返回 errs.ErrAIConversationNotFound（语义复用，§7.1 评估改名）。
func (r *sqliteStore) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	if sessionID == "" {
		return nil, errs.ErrAIConversationNotFound
	}
	var s domain.Session
	err := r.db.WithContext(ctx).Where("f_id = ?", sessionID).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrAIConversationNotFound
		}
		return nil, err
	}
	return &s, nil
}

// UpdateSession 按主键直接 Save（受信调用）。
//
// service 层负责设置 UpdatedAt = time.Now()；store 层兜底：UpdatedAt 为零值时填 now。
func (r *sqliteStore) UpdateSession(ctx context.Context, s *domain.Session) error {
	if s == nil || s.ID == "" {
		return errs.ErrAIConversationNotFound
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Save(s).Error
}

// DeleteSession 硬删 + 事务级联。
//
// 在单一事务内按 f_session_id 删 t_fv_ai_agent_steps、t_fv_ai_messages，
// 最后按 f_id 删 t_fv_ai_sessions。任一步骤失败整个事务回滚。
//
// 不存在的 sessionID 返回 errs.ErrAIConversationNotFound（spec "DELETE 返回 204"
// 的前提是 service 层语义层判断；store 层用 RowsAffected==0 识别"不存在"，让 service
// 决定语义）。
func (r *sqliteStore) DeleteSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errs.ErrAIConversationNotFound
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先确认存在；不存在直接返回 NotFound，避免误删空集
		var existing domain.Session
		if err := tx.Where("f_id = ?", sessionID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errs.ErrAIConversationNotFound
			}
			return err
		}
		// 级联删 agent_steps（最末叶子，先删）
		if err := tx.Where("f_session_id = ?", sessionID).Delete(&domain.AgentStep{}).Error; err != nil {
			return err
		}
		// 级联删 messages
		if err := tx.Where("f_session_id = ?", sessionID).Delete(&domain.Message{}).Error; err != nil {
			return err
		}
		// 删 session 自身
		if err := tx.Where("f_id = ?", sessionID).Delete(&domain.Session{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// ListSessions 按 opts 过滤分页（必须用 opts.UserID 过滤）。
//
// 排序：f_updated_at DESC（spec "按最近更新时间排序"）；
// PageSize ≤ 0 兜底 20，Page ≤ 0 兜底 1（service 层应当已校验，这里防御）。
//
// 同条件 SELECT COUNT(*) 一并返回，便于前端展示分页总数。
func (r *sqliteStore) ListSessions(
	ctx context.Context, opts ListSessionsOptions,
) (sessions []domain.Session, total int64, err error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	q := r.db.WithContext(ctx).Model(&domain.Session{}).Where("f_user_id = ?", opts.UserID)
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []domain.Session{}, 0, nil
	}
	offset := (opts.Page - 1) * opts.PageSize
	if err = q.Order("f_updated_at DESC").
		Limit(opts.PageSize).Offset(offset).
		Find(&sessions).Error; err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

// =====================================================================
// 消息：AppendMessage + ListMessages
// =====================================================================

// AppendMessage 写一行消息到 t_fv_ai_messages。
//
// SessionID 为空字符串时直接返回 errs.ErrAIConversationNotFound（基本防御）。
// service 层负责保证 sessionID 存在且属于当前用户，store 层不再二次查询。
func (r *sqliteStore) AppendMessage(ctx context.Context, m *domain.Message) error {
	if m == nil || m.SessionID == "" {
		return errs.ErrAIConversationNotFound
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(m).Error
}

// ListMessages 按 f_created_at 升序拉取最近 N 条消息（送给模型的上下文窗口）。
//
// limit 解释：
//   - limit > 0：按传入值
//   - limit ≤ 0：使用构造时注入的 historyWindow（默认 20）
//
// 实现策略：先按 f_created_at DESC + f_id DESC（次序键防同毫秒并发写入乱序）取最近 N 条，
// 再在内存反转为升序——SQL 层一次查询完成，便于业务层直接拼 prompt。
func (r *sqliteStore) ListMessages(
	ctx context.Context, sessionID string, limit int,
) ([]domain.Message, error) {
	if sessionID == "" {
		return nil, errs.ErrAIConversationNotFound
	}
	if limit <= 0 {
		limit = r.historyWindow
	}
	var out []domain.Message
	err := r.db.WithContext(ctx).
		Where("f_session_id = ?", sessionID).
		Order("f_created_at DESC, f_id DESC").
		Limit(limit).
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	// 反转为升序（旧 → 新），便于业务层直接按时间顺序拼接 prompt
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// =====================================================================
// 步骤：AppendStep（含敏感字段掩码）+ EstimateStepsSize 占位
// =====================================================================

// AppendStep 写一行步骤事件，写库前对 f_payload JSON 做敏感字段掩码。
//
// 仅追加从不更新；step 不与 session/message 强制 FK（GORM AutoMigrate 不创建
// SQLite FK 约束，详见 ai_session.go 注释）。Payload 的脱敏由 mask.go::MaskSensitiveJSON
// 完成（design D7：api_key / password / token / authorization 等敏感字段写库前替换为 "***"）。
func (r *sqliteStore) AppendStep(ctx context.Context, step *domain.AgentStep) error {
	if step == nil || step.SessionID == "" {
		return errs.ErrAIConversationNotFound
	}
	if step.CreatedAt.IsZero() {
		step.CreatedAt = time.Now()
	}
	step.Payload = MaskSensitiveJSON(step.Payload)
	return r.db.WithContext(ctx).Create(step).Error
}

// EstimateStepsSize 由 §4.4 dev_2 实现，本占位仅满足接口编译期断言。
func (r *sqliteStore) EstimateStepsSize(ctx context.Context) (int64, error) {
	return 0, errors.New("not implemented: §4.4 placeholder")
}
