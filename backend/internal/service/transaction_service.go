package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// TransactionService —— 13 种交易类型 + 事务一致性
// =====================================================================
//
// 类型映射（与 domain-model.md §4.8 对齐）：
//   buy               quantity↑ total_cost↑ avg_cost 重算
//   sell              quantity↓ realized_pnl↑ total_cost↓ avg_cost 不变
//   dividend          total_dividend↑（不动 quantity / cost）
//   dividend_reinvest quantity↑（不动 cost）
//   split             quantity 调整 + avg_cost 调整（用 quantity 字段作为新份额，price=拆分比）
//   bonus             quantity↑ avg_cost 摊薄
//   mature            理财到期：全量平仓，realized_pnl 累加
//   interest          total_dividend↑（wealth/cash）
//   deposit           quantity↑（cash 充值）
//   withdraw          quantity↓（cash 提现）
//   cash_in           quantity↑（卖出回款联动 cash）
//   cash_out          quantity↓（买入扣款联动 cash）
//   adjust            自由调整，需要 note

// TransactionService 交易服务。
type TransactionService struct {
	uow         repository.UnitOfWork
	txnRepo     repository.TransactionRepository
	holdingRepo repository.HoldingRepository
	assetRepo   repository.AssetRepository
}

// NewTransactionService 构造。
func NewTransactionService(
	uow repository.UnitOfWork,
	txnRepo repository.TransactionRepository,
	holdingRepo repository.HoldingRepository,
	assetRepo repository.AssetRepository,
) *TransactionService {
	return &TransactionService{
		uow:         uow,
		txnRepo:     txnRepo,
		holdingRepo: holdingRepo,
		assetRepo:   assetRepo,
	}
}

// CreateTxnInput 创建交易入参（统一入口，按 TxnType 分发）。
type CreateTxnInput struct {
	UserID     uint
	AssetID    uint
	PlatformID uint
	TxnType    domain.TxnType
	TxnTime    time.Time
	Quantity   decimal.Decimal
	Price      decimal.Decimal
	Amount     decimal.Decimal
	Fee        decimal.Decimal
	Tax        decimal.Decimal
	Currency   string
	Source     string
	ExternalID string
	Note       string
}

// Create 写入交易并联动更新 Holding（事务一致）。
func (s *TransactionService) Create(ctx context.Context, in CreateTxnInput) (*domain.Transaction, error) {
	if err := s.validateInput(in); err != nil {
		return nil, err
	}

	// 防重导入
	if in.ExternalID != "" {
		exists, err := s.txnRepo.ExistsByExternalID(ctx, in.UserID, in.PlatformID, in.ExternalID)
		if err != nil {
			return nil, errs.ErrDB.WithCause(err)
		}
		if exists {
			return nil, errs.ErrTxnDuplicated
		}
	}

	// 取或建 Holding
	holding, err := s.holdingRepo.GetOrCreate(ctx, in.UserID, in.AssetID, in.PlatformID)
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}

	// 业务校验（卖出 / 提现 类型需要持仓量）
	switch in.TxnType {
	case domain.TxnTypeSell, domain.TxnTypeWithdraw, domain.TxnTypeCashOut:
		if holding.Quantity.LessThan(in.Quantity) {
			return nil, errs.ErrInsufficientQuantity
		}
	}

	// 默认值
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.Source == "" {
		in.Source = domain.TxnSourceManual
	}
	if in.Fee.IsZero() {
		in.Fee = decimal.Zero
	}
	if in.Tax.IsZero() {
		in.Tax = decimal.Zero
	}
	if in.Amount.IsZero() && !in.Price.IsZero() && !in.Quantity.IsZero() {
		in.Amount = in.Price.Mul(in.Quantity).Round(2)
	}

	// net_amount = 买入: amount+fee+tax；卖出: amount-fee-tax；其余: amount
	netAmount := in.Amount
	switch in.TxnType {
	case domain.TxnTypeBuy, domain.TxnTypeDividendReinvest, domain.TxnTypeDeposit, domain.TxnTypeCashIn:
		netAmount = in.Amount.Add(in.Fee).Add(in.Tax)
	case domain.TxnTypeSell, domain.TxnTypeMature, domain.TxnTypeWithdraw, domain.TxnTypeCashOut:
		netAmount = in.Amount.Sub(in.Fee).Sub(in.Tax)
	}

	now := time.Now()
	if in.TxnTime.IsZero() {
		in.TxnTime = now
	}

	txn := &domain.Transaction{
		UserID:     in.UserID,
		HoldingID:  holding.ID,
		AssetID:    in.AssetID,
		PlatformID: in.PlatformID,
		TxnType:    in.TxnType,
		TxnTime:    in.TxnTime,
		Quantity:   in.Quantity,
		Price:      in.Price,
		Amount:     in.Amount,
		Fee:        in.Fee,
		Tax:        in.Tax,
		NetAmount:  netAmount,
		Currency:   in.Currency,
		Source:     in.Source,
		ExternalID: in.ExternalID,
		Note:       in.Note,
	}
	txn.CreatedAt = now
	txn.UpdatedAt = now

	err = s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.txnRepo.Create(ctx, txn); err != nil {
			return errs.ErrDB.WithCause(err)
		}
		if err := s.applyToHolding(holding, txn); err != nil {
			return err
		}
		holding.UpdatedAt = time.Now()
		if err := s.holdingRepo.Update(ctx, holding); err != nil {
			return errs.ErrDB.WithCause(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return txn, nil
}

// validateInput 通用入参校验。
func (s *TransactionService) validateInput(in CreateTxnInput) error {
	if in.UserID == 0 {
		return errs.ErrInvalidParam.WithMsg("user_id required")
	}
	if in.AssetID == 0 || in.PlatformID == 0 {
		return errs.ErrInvalidParam.WithMsg("asset_id/platform_id required")
	}
	if !in.TxnType.IsValid() {
		return errs.ErrTxnTypeInvalid
	}
	// adjust 类型允许任意正负 + 必须有 note
	if in.TxnType == domain.TxnTypeAdjust {
		if in.Note == "" {
			return errs.ErrInvalidParam.WithMsg("adjust requires note")
		}
		return nil
	}
	if in.Quantity.LessThanOrEqual(decimal.Zero) {
		return errs.ErrTxnQuantityInvalid
	}
	// dividend / interest 是金额型，price/amount 必须 > 0；quantity 可视为份额或忽略
	if in.TxnType == domain.TxnTypeDividend || in.TxnType == domain.TxnTypeInterest {
		if in.Amount.LessThanOrEqual(decimal.Zero) {
			return errs.ErrTxnAmountInvalid
		}
		return nil
	}
	if in.Price.LessThanOrEqual(decimal.Zero) && in.TxnType != domain.TxnTypeMature && in.TxnType != domain.TxnTypeBonus {
		return errs.ErrTxnPriceInvalid
	}
	return nil
}

// applyToHolding 把交易效应应用到 Holding 上。
//
// 该函数在事务内调用，**不**自己更新 last_txn_at 之外的时间字段（由 caller 控制）。
func (s *TransactionService) applyToHolding(h *domain.Holding, t *domain.Transaction) error {
	now := t.TxnTime
	switch t.TxnType {
	case domain.TxnTypeBuy:
		newQty := h.Quantity.Add(t.Quantity)
		newCost := h.TotalCost.Add(t.NetAmount).Round(2)
		h.Quantity = newQty
		h.TotalCost = newCost
		if !newQty.IsZero() {
			h.AvgCost = newCost.Div(newQty).Round(8)
		}
		if h.FirstBuyAt == nil {
			tt := now
			h.FirstBuyAt = &tt
		}

	case domain.TxnTypeSell:
		// 加权平均：sold_cost = avg_cost × qty；this_pnl = net_amount - sold_cost
		soldCost := h.AvgCost.Mul(t.Quantity).Round(2)
		thisPnL := t.NetAmount.Sub(soldCost).Round(2)
		h.Quantity = h.Quantity.Sub(t.Quantity)
		h.TotalCost = h.TotalCost.Sub(soldCost).Round(2)
		h.RealizedPnL = h.RealizedPnL.Add(thisPnL).Round(2)
		// avg_cost 不变
		if h.Quantity.IsZero() {
			h.Status = domain.HoldingStatusClosed
			h.TotalCost = decimal.Zero
		}

	case domain.TxnTypeDividend, domain.TxnTypeInterest:
		h.TotalDividend = h.TotalDividend.Add(t.NetAmount).Round(2)

	case domain.TxnTypeDividendReinvest:
		// 红利再投：份额增加，成本不变
		h.Quantity = h.Quantity.Add(t.Quantity)
		// 重算 avg_cost
		if !h.Quantity.IsZero() {
			h.AvgCost = h.TotalCost.Div(h.Quantity).Round(8)
		}

	case domain.TxnTypeBonus:
		// 送股：份额↑，成本不变，avg_cost 摊薄
		h.Quantity = h.Quantity.Add(t.Quantity)
		if !h.Quantity.IsZero() {
			h.AvgCost = h.TotalCost.Div(h.Quantity).Round(8)
		}

	case domain.TxnTypeSplit:
		// 拆股 / 合股：用 t.Quantity 作为"调整后份额"，t.Price 作为"拆分比 new/old"。
		// 简化处理：直接把当前 quantity 乘以 price（>1 拆股，<1 合股），avg_cost 反向调整。
		if t.Price.GreaterThan(decimal.Zero) {
			h.Quantity = h.Quantity.Mul(t.Price).Round(8)
			if !t.Price.IsZero() {
				h.AvgCost = h.AvgCost.Div(t.Price).Round(8)
			}
		}

	case domain.TxnTypeMature:
		// 理财到期：全量平仓
		realizedDelta := t.NetAmount.Sub(h.TotalCost).Round(2)
		h.RealizedPnL = h.RealizedPnL.Add(realizedDelta).Round(2)
		h.Quantity = decimal.Zero
		h.TotalCost = decimal.Zero
		h.Status = domain.HoldingStatusMatured

	case domain.TxnTypeDeposit, domain.TxnTypeCashIn:
		// 现金充值 / 卖出回款：quantity == 金额
		h.Quantity = h.Quantity.Add(t.Quantity)
		h.TotalCost = h.TotalCost.Add(t.NetAmount).Round(2)

	case domain.TxnTypeWithdraw, domain.TxnTypeCashOut:
		h.Quantity = h.Quantity.Sub(t.Quantity)
		h.TotalCost = h.TotalCost.Sub(t.NetAmount).Round(2)
		if h.Quantity.IsZero() {
			h.Status = domain.HoldingStatusClosed
		}

	case domain.TxnTypeAdjust:
		// 直接覆盖（用 quantity / price 作为目标值）
		if !t.Quantity.IsZero() {
			h.Quantity = t.Quantity
		}
		if !t.Price.IsZero() {
			h.AvgCost = t.Price
		}

	default:
		return errs.ErrTxnTypeInvalid
	}
	tt := now
	h.LastTxnAt = &tt
	return nil
}

// =====================================================================
// 查询接口
// =====================================================================

// TxnListInput 流水查询入参。
type TxnListInput struct {
	UserID     uint
	HoldingID  uint
	AssetID    uint
	PlatformID uint
	TxnType    domain.TxnType
	StartTime  *time.Time
	EndTime    *time.Time
	Page       int
	PageSize   int
}

// List 列出交易流水。
func (s *TransactionService) List(ctx context.Context, in TxnListInput) ([]domain.Transaction, int64, error) {
	opts := repository.ListOptions{
		UserID:   in.UserID,
		Page:     in.Page,
		PageSize: in.PageSize,
		Filters:  map[string]any{},
	}
	if in.HoldingID > 0 {
		opts.Filters["holding_id"] = in.HoldingID
	}
	if in.AssetID > 0 {
		opts.Filters["asset_id"] = in.AssetID
	}
	if in.PlatformID > 0 {
		opts.Filters["platform_id"] = in.PlatformID
	}
	if in.TxnType != "" {
		opts.Filters["txn_type"] = string(in.TxnType)
	}
	if in.StartTime != nil {
		opts.Filters["start_time"] = *in.StartTime
	}
	if in.EndTime != nil {
		opts.Filters["end_time"] = *in.EndTime
	}
	list, total, err := s.txnRepo.List(ctx, opts)
	if err != nil {
		return nil, 0, errs.ErrDB.WithCause(err)
	}
	return list, total, nil
}

// Get 取单条流水。
func (s *TransactionService) Get(ctx context.Context, userID, id uint) (*domain.Transaction, error) {
	t, err := s.txnRepo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.ErrTxnNotFound
		}
		return nil, errs.ErrDB.WithCause(err)
	}
	return t, nil
}

// =====================================================================
// CSV 导入
// =====================================================================

// ImportRowError 单行导入错误。
type ImportRowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ImportResult 批量导入结果。
type ImportResult struct {
	Succeed int              `json:"succeed"`
	Failed  int              `json:"failed"`
	Errors  []ImportRowError `json:"errors,omitempty"`
}

// BatchImport 批量创建交易（CSV 导入入口）。dryRun=true 时仅校验不写库。
func (s *TransactionService) BatchImport(ctx context.Context, rows []CreateTxnInput, dryRun bool) (*ImportResult, error) {
	result := &ImportResult{}
	for i, in := range rows {
		if err := s.validateInput(in); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportRowError{Row: i + 1, Message: err.Error()})
			continue
		}
		if dryRun {
			result.Succeed++
			continue
		}
		if _, err := s.Create(ctx, in); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportRowError{Row: i + 1, Message: err.Error()})
			continue
		}
		result.Succeed++
	}
	return result, nil
}

// 防止 fmt 未使用警告（保留供 future error formatting 使用）
var _ = fmt.Sprintf
