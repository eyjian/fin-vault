package service

import (
	"context"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// RateService —— 汇率查询与录入
// =====================================================================

// RateService 汇率服务。
type RateService struct {
	repo repository.RateRepository
}

// NewRateService 构造汇率服务。
func NewRateService(repo repository.RateRepository) *RateService {
	return &RateService{repo: repo}
}

// GetLatest 取 from->to 在 asOf 之前最新汇率（asOf 零值=今天）。
func (s *RateService) GetLatest(ctx context.Context, from, to string, asOf time.Time) (*domain.ExchangeRate, error) {
	if from == "" || to == "" {
		return nil, errs.ErrInvalidParam.WithMsg("from/to currency required")
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	r, err := s.repo.GetLatest(ctx, from, to, asOf)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Save 录入或更新一条汇率。
//
// 唯一键：(from, to, quote_date, source)；冲突时调用方应先删除再写或 ignore。
// 这里我们按"先尝试 Insert，冲突则降级返回业务错误"的策略，留给上层（service）和前端处理。
func (s *RateService) Save(ctx context.Context, r *domain.ExchangeRate) error {
	if r == nil {
		return errs.ErrInvalidParam.WithMsg("rate required")
	}
	if r.FromCurrency == "" || r.ToCurrency == "" {
		return errs.ErrInvalidParam.WithMsg("from/to currency required")
	}
	if r.Rate.IsZero() {
		return errs.ErrInvalidParam.WithMsg("rate must not be zero")
	}
	if r.QuoteDate.IsZero() {
		r.QuoteDate = truncDate(time.Now())
	} else {
		r.QuoteDate = truncDate(r.QuoteDate)
	}
	if r.Source == "" {
		r.Source = domain.RateSourceManual
	}
	return s.repo.Insert(ctx, r)
}

// List 时间范围查询。fromDate/toDate 零值视为「不限」（很早 / 很久之后）。
func (s *RateService) List(ctx context.Context, from, to string, fromDate, toDate time.Time) ([]*domain.ExchangeRate, error) {
	if fromDate.IsZero() {
		fromDate = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if toDate.IsZero() {
		toDate = time.Now().AddDate(10, 0, 0)
	}
	return s.repo.List(ctx, from, to, fromDate, toDate)
}

func truncDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
