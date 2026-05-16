// Package decimalx 提供 shopspring/decimal 的项目级辅助函数。
//
// 主要场景：
//   - 多币种折算（按 ExchangeRate 查最新汇率）
//   - 安全字符串解析（空串视为 0）
//   - 标准舍入（金额 2 位、收益率 6 位）
package decimalx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// FromString 把字符串安全转 Decimal。空串视为 0，无效字符串返回错误。
func FromString(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid decimal %q: %w", s, err)
	}
	return d, nil
}

// MustFromString 同 FromString，无效时返回 0（仅用于已验证场景）。
func MustFromString(s string) decimal.Decimal {
	d, _ := FromString(s)
	return d
}

// RoundMoney 金额标准舍入 2 位。
func RoundMoney(d decimal.Decimal) decimal.Decimal {
	return d.Round(2)
}

// RoundQty 数量/单价标准舍入 8 位。
func RoundQty(d decimal.Decimal) decimal.Decimal {
	return d.Round(8)
}

// RoundRatio 收益率/比例标准舍入 6 位。
func RoundRatio(d decimal.Decimal) decimal.Decimal {
	return d.Round(6)
}

// =====================================================================
// 多币种折算
// =====================================================================

// ErrMissingExchangeRate 缺少 from->to 的汇率快照（asOf 时点之前没有任何记录）。
var ErrMissingExchangeRate = errors.New("decimalx: missing exchange rate")

// Converter 多币种折算器。
//
// 内部缓存 (from,to,date) -> rate，避免一次接口里反复查 DB。
type Converter struct {
	rateRepo repository.RateRepository
	asOf     time.Time
	cache    map[string]decimal.Decimal
}

// NewConverter 构造一个折算器。asOf 表示用哪一日的"最新有效汇率"，零值表示当前。
func NewConverter(rateRepo repository.RateRepository, asOf time.Time) *Converter {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	return &Converter{
		rateRepo: rateRepo,
		asOf:     asOf,
		cache:    make(map[string]decimal.Decimal),
	}
}

// To 按指定目标币种折算金额。
//
//   - from == to：直接返回 amount
//   - 否则查 ExchangeRate(from,to,asOf 之前最新)，返回 amount * rate
//   - 缺失汇率时返回 ErrMissingExchangeRate
func (c *Converter) To(ctx context.Context, amount decimal.Decimal, from, to string) (decimal.Decimal, error) {
	if from == to || from == "" || to == "" {
		return amount, nil
	}
	rate, err := c.rate(ctx, from, to)
	if err != nil {
		return decimal.Zero, err
	}
	return amount.Mul(rate), nil
}

func (c *Converter) rate(ctx context.Context, from, to string) (decimal.Decimal, error) {
	key := from + "|" + to
	if v, ok := c.cache[key]; ok {
		return v, nil
	}
	r, err := c.rateRepo.GetLatest(ctx, from, to, c.asOf)
	if err == nil && r != nil {
		c.cache[key] = r.Rate
		return r.Rate, nil
	}
	// 反向汇率回退：to->from 的最新一条，倒数即可（精度损失：8 位足够日常使用）
	rev, err2 := c.rateRepo.GetLatest(ctx, to, from, c.asOf)
	if err2 == nil && rev != nil && !rev.Rate.IsZero() {
		inv := decimal.NewFromInt(1).Div(rev.Rate).Round(8)
		c.cache[key] = inv
		return inv, nil
	}
	return decimal.Zero, fmt.Errorf("%w: %s -> %s as of %s", ErrMissingExchangeRate, from, to, c.asOf.Format("2006-01-02"))
}
