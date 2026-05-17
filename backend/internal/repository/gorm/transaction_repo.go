package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

type transactionRepo struct{ db *gorm.DB }

// NewTransactionRepository 构造 TransactionRepository。
func NewTransactionRepository(db *gorm.DB) repository.TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) Create(ctx context.Context, t *domain.Transaction) error {
	return dbFrom(ctx, r.db).Create(t).Error
}

func (r *transactionRepo) GetByID(ctx context.Context, userID, id uint) (*domain.Transaction, error) {
	var t domain.Transaction
	q := dbFrom(ctx, r.db)
	if userID > 0 {
		q = q.Where("f_user_id = ?", userID)
	}
	if err := q.First(&t, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &t, nil
}

func (r *transactionRepo) List(ctx context.Context, opts repository.ListOptions) ([]domain.Transaction, int64, error) {
	tx := dbFrom(ctx, r.db).Model(&domain.Transaction{})
	if opts.UserID > 0 {
		tx = tx.Where("f_user_id = ?", opts.UserID)
	}
	for k, v := range opts.Filters {
		switch k {
		case "holding_id":
			tx = tx.Where("f_holding_id = ?", v)
		case "asset_id":
			tx = tx.Where("f_asset_id = ?", v)
		case "platform_id":
			tx = tx.Where("f_platform_id = ?", v)
		case "txn_type":
			tx = tx.Where("f_txn_type = ?", v)
		case "start_time":
			tx = tx.Where("f_txn_time >= ?", v)
		case "end_time":
			tx = tx.Where("f_txn_time <= ?", v)
		}
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := opts.OrderBy
	if order == "" {
		order = "f_txn_time DESC, f_id DESC"
	}
	var list []domain.Transaction
	if err := tx.Order(order).
		Preload("Asset").
		Offset(opts.Offset()).Limit(opts.Limit()).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *transactionRepo) ListByHolding(ctx context.Context, holdingID uint) ([]domain.Transaction, error) {
	var list []domain.Transaction
	if err := dbFrom(ctx, r.db).
		Where("f_holding_id = ?", holdingID).
		Order("f_txn_time ASC, f_id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ExistsByExternalID externalID 为空直接 (false, nil)。
func (r *transactionRepo) ExistsByExternalID(ctx context.Context, userID, platformID uint, externalID string) (bool, error) {
	if externalID == "" {
		return false, nil
	}
	var count int64
	err := dbFrom(ctx, r.db).Model(&domain.Transaction{}).
		Where("f_user_id = ? AND f_platform_id = ? AND f_external_id = ?", userID, platformID, externalID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// 防止编译期 gorm 包未引用警告（事务版连接由 dbFrom 内部使用）。
var _ = gorm.ErrRecordNotFound
