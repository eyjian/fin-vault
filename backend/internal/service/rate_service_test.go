package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// RateService.GetLatest 参数校验
// =====================================================================

func TestRateService_GetLatest_EmptyCurrency_ReturnsInvalidParam(t *testing.T) {
	svc := NewRateService(testutil.NewMockRateRepo())

	_, err := svc.GetLatest(context.Background(), "", "CNY", time.Now())
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)

	_, err = svc.GetLatest(context.Background(), "USD", "", time.Now())
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestRateService_GetLatest_HappyPath(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	repo.SetLatest("USD", "CNY", &domain.ExchangeRate{
		FromCurrency: "USD", ToCurrency: "CNY",
		Rate:      decimal.RequireFromString("7.18"),
		QuoteDate: time.Now(),
		Source:    domain.RateSourceManual,
	})
	svc := NewRateService(repo)

	got, err := svc.GetLatest(context.Background(), "USD", "CNY", time.Time{})
	require.NoError(t, err)
	assert.True(t, got.Rate.Equal(decimal.RequireFromString("7.18")))
}

func TestRateService_GetLatest_NotFound_BubblesUp(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	svc := NewRateService(repo)

	_, err := svc.GetLatest(context.Background(), "JPY", "CNY", time.Now())
	require.Error(t, err)
	assert.True(t, errors.Is(err, repository.ErrNotFound))
}

// =====================================================================
// RateService.Save：默认源 / 校验 / truncDate
// =====================================================================

func TestRateService_Save_NilOrInvalid(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	svc := NewRateService(repo)

	require.Error(t, svc.Save(context.Background(), nil))

	require.Error(t, svc.Save(context.Background(), &domain.ExchangeRate{
		FromCurrency: "", ToCurrency: "CNY", Rate: decimal.RequireFromString("1"),
	}))

	require.Error(t, svc.Save(context.Background(), &domain.ExchangeRate{
		FromCurrency: "USD", ToCurrency: "CNY", Rate: decimal.Zero,
	}))
}

func TestRateService_Save_DefaultsSourceAndTruncDate(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	svc := NewRateService(repo)

	now := time.Date(2026, 5, 16, 22, 30, 0, 0, time.Local)
	r := &domain.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.RequireFromString("7.18"),
		QuoteDate:    now, // 带时分秒，应被截到 00:00:00
	}
	require.NoError(t, svc.Save(context.Background(), r))
	require.Len(t, repo.History, 1)

	saved := repo.History[0]
	assert.Equal(t, domain.RateSourceManual, saved.Source, "should default to manual")
	assert.Equal(t, 0, saved.QuoteDate.Hour())
	assert.Equal(t, 0, saved.QuoteDate.Minute())
	assert.Equal(t, 0, saved.QuoteDate.Second())
	assert.Equal(t, 2026, saved.QuoteDate.Year())
}

func TestRateService_Save_ZeroQuoteDate_DefaultsToToday(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	svc := NewRateService(repo)

	r := &domain.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.RequireFromString("7.18"),
		// QuoteDate 留空
	}
	require.NoError(t, svc.Save(context.Background(), r))

	saved := repo.History[0]
	today := time.Now()
	assert.Equal(t, today.Year(), saved.QuoteDate.Year())
	assert.Equal(t, today.Month(), saved.QuoteDate.Month())
	assert.Equal(t, today.Day(), saved.QuoteDate.Day())
	assert.Equal(t, 0, saved.QuoteDate.Hour())
}

// =====================================================================
// RateService.List：零值 fromDate/toDate
// =====================================================================

func TestRateService_List_ZeroDatesGetWidened(t *testing.T) {
	repo := testutil.NewMockRateRepo()
	_ = repo.Insert(context.Background(), &domain.ExchangeRate{
		FromCurrency: "USD", ToCurrency: "CNY",
		Rate:      decimal.RequireFromString("7.18"),
		QuoteDate: time.Now(),
		Source:    domain.RateSourceManual,
	})
	svc := NewRateService(repo)

	list, err := svc.List(context.Background(), "USD", "CNY", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Len(t, list, 1)
}
