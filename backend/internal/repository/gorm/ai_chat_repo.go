package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// =====================================================================
// AIConversation + AIMessage 一体仓储
// =====================================================================

type aiConvRepo struct{ db *gorm.DB }

// NewAIConversationRepository 构造 AIConversationRepository。
func NewAIConversationRepository(db *gorm.DB) repository.AIConversationRepository {
	return &aiConvRepo{db: db}
}

func (r *aiConvRepo) CreateConv(ctx context.Context, c *domain.AIConversation) error {
	return dbFrom(ctx, r.db).Create(c).Error
}

func (r *aiConvRepo) GetConv(ctx context.Context, id uint) (*domain.AIConversation, error) {
	var c domain.AIConversation
	if err := dbFrom(ctx, r.db).First(&c, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &c, nil
}

func (r *aiConvRepo) UpdateConv(ctx context.Context, c *domain.AIConversation) error {
	return dbFrom(ctx, r.db).Save(c).Error
}

func (r *aiConvRepo) IncrTokens(ctx context.Context, id uint, deltaMessages, deltaTokens int) error {
	return dbFrom(ctx, r.db).Model(&domain.AIConversation{}).
		Where("f_id = ?", id).
		Updates(map[string]interface{}{
			"f_message_count": gorm.Expr("f_message_count + ?", deltaMessages),
			"f_total_tokens":  gorm.Expr("f_total_tokens + ?", deltaTokens),
			"f_updated_at":    time.Now(),
		}).Error
}

func (r *aiConvRepo) DeleteConv(ctx context.Context, id uint) error {
	return dbFrom(ctx, r.db).Delete(&domain.AIConversation{}, id).Error
}

func (r *aiConvRepo) ListConversations(ctx context.Context, userID uint, opts repository.ListOptions) ([]domain.AIConversation, int64, error) {
	tx := dbFrom(ctx, r.db).Model(&domain.AIConversation{}).Where("f_user_id = ?", userID)
	for k, v := range opts.Filters {
		switch k {
		case "scene":
			tx = tx.Where("f_scene = ?", v)
		case "status":
			tx = tx.Where("f_status = ?", v)
		}
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := opts.OrderBy
	if order == "" {
		order = "f_updated_at DESC"
	}
	var list []domain.AIConversation
	if err := tx.Order(order).
		Offset(opts.Offset()).Limit(opts.Limit()).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *aiConvRepo) AppendMessage(ctx context.Context, m *domain.AIMessage) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return dbFrom(ctx, r.db).Create(m).Error
}

// ListMessages 默认 ASC 取早到晚（适合会话回放）。limit<=0 取全部。
func (r *aiConvRepo) ListMessages(ctx context.Context, convID uint, limit int) ([]domain.AIMessage, error) {
	tx := dbFrom(ctx, r.db).
		Where("f_conversation_id = ?", convID).
		Order("f_id ASC")
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	var list []domain.AIMessage
	if err := tx.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
