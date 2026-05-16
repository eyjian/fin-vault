package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// =====================================================================
// QuoteRepository
// =====================================================================

type quoteRepo struct{ db *gorm.DB }

// NewQuoteRepository 构造 QuoteRepository。
func NewQuoteRepository(db *gorm.DB) repository.QuoteRepository {
	return &quoteRepo{db: db}
}

func (r *quoteRepo) Insert(ctx context.Context, q *domain.PriceQuote) error {
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now()
	}
	return dbFrom(ctx, r.db).Create(q).Error
}

func (r *quoteRepo) GetLatest(ctx context.Context, assetID uint) (*domain.PriceQuote, error) {
	var q domain.PriceQuote
	if err := dbFrom(ctx, r.db).
		Where("f_asset_id = ?", assetID).
		Order("f_quote_time DESC, f_id DESC").Limit(1).
		First(&q).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &q, nil
}

func (r *quoteRepo) BatchGetLatest(ctx context.Context, assetIDs []uint) (map[uint]*domain.PriceQuote, error) {
	result := make(map[uint]*domain.PriceQuote, len(assetIDs))
	if len(assetIDs) == 0 {
		return result, nil
	}
	// 用相关子查询：每个 asset_id 取 max(id) 对应的行（同分钟入两条时按 id 降序取最新）。
	sub := dbFrom(ctx, r.db).Model(&domain.PriceQuote{}).
		Select("MAX(f_id)").
		Where("f_asset_id IN ?", assetIDs).
		Group("f_asset_id")
	var quotes []*domain.PriceQuote
	if err := dbFrom(ctx, r.db).Model(&domain.PriceQuote{}).
		Where("f_id IN (?)", sub).Find(&quotes).Error; err != nil {
		return nil, err
	}
	for _, q := range quotes {
		result[q.AssetID] = q
	}
	return result, nil
}

func (r *quoteRepo) ListHistory(ctx context.Context, assetID uint, from, to time.Time) ([]*domain.PriceQuote, error) {
	var list []*domain.PriceQuote
	if err := dbFrom(ctx, r.db).
		Where("f_asset_id = ? AND f_quote_time BETWEEN ? AND ?", assetID, from, to).
		Order("f_quote_time ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// =====================================================================
// RateRepository
// =====================================================================

type rateRepo struct{ db *gorm.DB }

// NewRateRepository 构造 RateRepository。
func NewRateRepository(db *gorm.DB) repository.RateRepository {
	return &rateRepo{db: db}
}

func (r *rateRepo) Insert(ctx context.Context, rate *domain.ExchangeRate) error {
	if rate.CreatedAt.IsZero() {
		rate.CreatedAt = time.Now()
	}
	return dbFrom(ctx, r.db).Create(rate).Error
}

func (r *rateRepo) GetLatest(ctx context.Context, from, to string, asOf time.Time) (*domain.ExchangeRate, error) {
	var rate domain.ExchangeRate
	if err := dbFrom(ctx, r.db).
		Where("f_from_currency = ? AND f_to_currency = ? AND f_quote_date <= ?", from, to, asOf).
		Order("f_quote_date DESC, f_id DESC").Limit(1).
		First(&rate).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &rate, nil
}

func (r *rateRepo) List(ctx context.Context, from, to string, fromDate, toDate time.Time) ([]*domain.ExchangeRate, error) {
	tx := dbFrom(ctx, r.db).Model(&domain.ExchangeRate{})
	if from != "" {
		tx = tx.Where("f_from_currency = ?", from)
	}
	if to != "" {
		tx = tx.Where("f_to_currency = ?", to)
	}
	tx = tx.Where("f_quote_date BETWEEN ? AND ?", fromDate, toDate)
	var list []*domain.ExchangeRate
	if err := tx.Order("f_quote_date ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
