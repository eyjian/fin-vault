package decimalx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
)

// =====================================================================
// FromString / Round 系列：纯函数
// =====================================================================

func TestFromString_EmptyAndWhitespace_ReturnsZero(t *testing.T) {
	d, err := FromString("")
	require.NoError(t, err)
	assert.True(t, d.IsZero())

	d, err = FromString("   ")
	require.NoError(t, err)
	assert.True(t, d.IsZero())
}

func TestFromString_ValidAndInvalid(t *testing.T) {
	d, err := FromString("12.345")
	require.NoError(t, err)
	assert.True(t, d.Equal(decimal.RequireFromString("12.345")))

	_, err = FromString("not a number")
	require.Error(t, err)
}

func TestMustFromString_InvalidReturnsZero(t *testing.T) {
	assert.True(t, MustFromString("invalid").IsZero())
	assert.True(t, MustFromString("3.14").Equal(decimal.RequireFromString("3.14")))
}

func TestRoundMoney_TwoPlaces(t *testing.T) {
	in := decimal.RequireFromString("3.14159265")
	got := RoundMoney(in)
	assert.True(t, got.Equal(decimal.RequireFromString("3.14")))
}

func TestRoundQty_EightPlaces(t *testing.T) {
	in := decimal.RequireFromString("3.123456789012")
	got := RoundQty(in)
	assert.True(t, got.Equal(decimal.RequireFromString("3.12345679"))) // banker's round to 8
}

func TestRoundRatio_SixPlaces(t *testing.T) {
	in := decimal.RequireFromString("0.123456789")
	got := RoundRatio(in)
	assert.True(t, got.Equal(decimal.RequireFromString("0.123457")))
}

// =====================================================================
// Converter
// =====================================================================

func TestConverter_SameCurrency_NoLookup(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	c := NewConverter(repo, time.Now())

	amt := decimal.RequireFromString("1234.56")
	got, err := c.To(context.Background(), amt, "CNY", "CNY")
	require.NoError(t, err)
	assert.True(t, got.Equal(amt))
}

func TestConverter_EmptyCurrency_ReturnsAmountAsIs(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	c := NewConverter(repo, time.Now())

	amt := decimal.RequireFromString("100")
	got, err := c.To(context.Background(), amt, "", "CNY")
	require.NoError(t, err)
	assert.True(t, got.Equal(amt))
}

func TestConverter_Direct_LookupAndCache(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	repo.SetLatest("HKD", "CNY", &domain.ExchangeRate{
		FromCurrency: "HKD", ToCurrency: "CNY",
		Rate: decimal.RequireFromString("0.92"),
	})
	c := NewConverter(repo, time.Now())

	// 第一次：100 HKD -> 92 CNY
	got, err := c.To(context.Background(), decimal.RequireFromString("100"), "HKD", "CNY")
	require.NoError(t, err)
	assert.True(t, got.Equal(decimal.RequireFromString("92")), "got=%s", got)

	// 第二次：把 repo 设为返回错误，仍能从 cache 命中。
	repo.GetLatestErr = errors.New("repo offline")
	got2, err := c.To(context.Background(), decimal.RequireFromString("200"), "HKD", "CNY")
	require.NoError(t, err)
	assert.True(t, got2.Equal(decimal.RequireFromString("184")), "should hit cache, got=%s", got2)
}

func TestConverter_ReverseFallback(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	// 只配置反向：CNY->USD = 0.14
	repo.SetLatest("CNY", "USD", &domain.ExchangeRate{
		FromCurrency: "CNY", ToCurrency: "USD",
		Rate: decimal.RequireFromString("0.14"),
	})
	c := NewConverter(repo, time.Now())

	// 100 USD -> CNY，倒数 = 1/0.14 ≈ 7.14285714
	got, err := c.To(context.Background(), decimal.RequireFromString("100"), "USD", "CNY")
	require.NoError(t, err)
	expected := decimal.NewFromInt(1).Div(decimal.RequireFromString("0.14")).Round(8).
		Mul(decimal.RequireFromString("100"))
	assert.True(t, got.Equal(expected), "expected=%s got=%s", expected, got)
}

func TestConverter_MissingRate_ReturnsErrMissingExchangeRate(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	c := NewConverter(repo, time.Now())

	_, err := c.To(context.Background(), decimal.RequireFromString("100"), "JPY", "CNY")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingExchangeRate),
		"want ErrMissingExchangeRate, got %v", err)
}

func TestConverter_NewConverter_ZeroAsOf_DefaultsToNow(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	c := NewConverter(repo, time.Time{})
	assert.False(t, c.asOf.IsZero(), "zero asOf should default to now")
}
