package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// MatureService —— 理财到期定时任务
// =====================================================================
//
// 触发：cron 每天 00:30。
// 逻辑：扫描 ListMaturedWealth(asOfDate=今天) 的所有理财持仓，对每条：
//   1. 用 expected/actual yield 计算到期收益
//   2. 在事务里插入 Transaction(mature) + 更新 Holding(matured)
//   3. 重复扫到的（status 已 matured）由 repo 层自动过滤

// MatureService 理财到期处理服务。
type MatureService struct {
	uow         repository.UnitOfWork
	holdingRepo repository.HoldingRepository
	assetRepo   repository.AssetRepository
	txnRepo     repository.TransactionRepository
}

// NewMatureService 构造。
func NewMatureService(
	uow repository.UnitOfWork,
	holdingRepo repository.HoldingRepository,
	assetRepo repository.AssetRepository,
	txnRepo repository.TransactionRepository,
) *MatureService {
	return &MatureService{
		uow:         uow,
		holdingRepo: holdingRepo,
		assetRepo:   assetRepo,
		txnRepo:     txnRepo,
	}
}

// MatureRunStat 单次任务的执行统计。
type MatureRunStat struct {
	Scanned int      `json:"scanned"`
	Matured int      `json:"matured"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

// RunOnce 立即跑一次理财到期扫描。
//
// 用 context.Background 避免外层 ctx 因 HTTP 关闭被取消（cron job 独立生命周期）。
func (s *MatureService) RunOnce(parent context.Context) (*MatureRunStat, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	stat := &MatureRunStat{}
	today := time.Now()
	asOfDate := time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 59, 0, today.Location())

	holdings, err := s.holdingRepo.ListMaturedWealth(ctx, asOfDate)
	if err != nil {
		return stat, errs.New(90003, "cron mature scan failed").WithCause(err)
	}
	stat.Scanned = len(holdings)

	for _, h := range holdings {
		if h.Status == domain.HoldingStatusMatured {
			stat.Skipped++
			continue
		}
		if err := s.matureOne(ctx, &h); err != nil {
			stat.Errors = append(stat.Errors, fmt.Errorf("holding %d: %w", h.ID, err).Error())
			slog.Error("mature failed", "holding_id", h.ID, "err", err)
			continue
		}
		stat.Matured++
	}
	return stat, nil
}

func (s *MatureService) matureOne(ctx context.Context, h *domain.Holding) error {
	// 取 wealth detail；规范错误语义：未找到 → 30401；其他下层错误透传
	wealth, err := s.assetRepo.GetWealthDetail(ctx, h.AssetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errs.ErrWealthDetailMissing.WithMsg("wealth detail not found")
		}
		return err
	}
	if wealth == nil {
		return errs.ErrWealthDetailMissing.WithMsg("wealth detail nil")
	}
	if wealth.EndDate.Time() == nil {
		return errs.ErrWealthDetailMissing.WithMsg("missing end_date")
	}

	// 期望/实际 收益率（%）
	yield := wealth.ExpectedYield
	if !wealth.ActualYield.IsZero() {
		yield = wealth.ActualYield
	}
	// mature_amount = total_cost * (1 + yield/100 * term_days/365)
	termDays := decimal.NewFromInt(int64(wealth.TermDays))
	if termDays.IsZero() && wealth.StartDate.Time() != nil {
		days := wealth.EndDate.Time().Sub(*wealth.StartDate.Time()).Hours() / 24
		termDays = decimal.NewFromFloat(days).Round(0)
	}
	periodFactor := yield.Div(decimal.NewFromInt(100)).
		Mul(termDays).
		Div(decimal.NewFromInt(365))
	matureAmount := h.TotalCost.Add(h.TotalCost.Mul(periodFactor)).Round(2)

	// 在事务里写 Transaction(mature) + 更新 Holding(matured)
	return s.uow.Do(ctx, func(ctx context.Context) error {
		txn := &domain.Transaction{
			UserID:     h.UserID,
			HoldingID:  h.ID,
			AssetID:    h.AssetID,
			PlatformID: h.PlatformID,
			TxnType:    domain.TxnTypeMature,
			TxnTime:    *wealth.EndDate.Time(),
			Quantity:   h.Quantity,
			Price:      decimal.Zero,
			Amount:     matureAmount,
			Fee:        decimal.Zero,
			Tax:        decimal.Zero,
			NetAmount:  matureAmount,
			Currency:   "CNY",
			Source:     domain.TxnSourceAutoMature,
			Note:       "系统自动生成 - 理财到期",
		}
		if !h.Quantity.IsZero() {
			txn.Price = matureAmount.Div(h.Quantity).Round(8)
		}
		if err := s.txnRepo.Create(ctx, txn); err != nil {
			return err
		}
		// 更新 Holding：realized_pnl += (mature - cost)，quantity = 0，status=matured
		realizedDelta := matureAmount.Sub(h.TotalCost)
		h.RealizedPnL = h.RealizedPnL.Add(realizedDelta)
		h.Quantity = decimal.Zero
		h.Status = domain.HoldingStatusMatured
		now := time.Now()
		h.LastTxnAt = &now
		return s.holdingRepo.Update(ctx, h)
	})
}
