package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// fakePlatformRepoLite 实现 PlatformRepository（testutil 暂未提供）。
//
// 测试只用 List —— 其它方法返回 ErrNotFound 占位即可。
type fakePlatformRepoLite struct {
	platforms []domain.Platform
	listErr   error
}

func (f *fakePlatformRepoLite) List(_ context.Context) ([]domain.Platform, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.platforms, nil
}

func (f *fakePlatformRepoLite) GetByID(_ context.Context, id uint) (*domain.Platform, error) {
	for i := range f.platforms {
		if f.platforms[i].ID == id {
			return &f.platforms[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakePlatformRepoLite) GetByCode(_ context.Context, code string) (*domain.Platform, error) {
	for i := range f.platforms {
		if f.platforms[i].Code == code {
			return &f.platforms[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakePlatformRepoLite) Create(_ context.Context, p *domain.Platform) error {
	f.platforms = append(f.platforms, *p)
	return nil
}

func (f *fakePlatformRepoLite) Update(_ context.Context, _ *domain.Platform) error { return nil }
func (f *fakePlatformRepoLite) Delete(_ context.Context, _ uint) error              { return nil }

var _ repository.PlatformRepository = (*fakePlatformRepoLite)(nil)

// =====================================================================
// Test_PlatformSummary_Success — 多平台聚合
// =====================================================================
func Test_PlatformSummary_Success(t *testing.T) {
	uid := uint(7)
	holdingRepo := testutil.NewMockHoldingRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	platformRepo := &fakePlatformRepoLite{
		platforms: []domain.Platform{
			{ID: 5, Code: "A", Name: "招商证券"},
			{ID: 6, Code: "B", Name: "天天基金"},
		},
	}

	// 平台 5：1 笔 (cost=3500, market=4000, pnl=500)
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:     uid,
		AssetID:    100,
		PlatformID: 5,
		Quantity:   decimal.NewFromInt(1000),
		TotalCost:  decimal.NewFromInt(3500),
		Status:     domain.HoldingStatusHolding,
	})
	// 平台 6：1 笔 (cost=2000, price=0 → market=0, pnl=-2000)
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:  domain.BaseModel{ID: 2},
		UserID:     uid,
		AssetID:    200,
		PlatformID: 6,
		Quantity:   decimal.NewFromInt(500),
		TotalCost:  decimal.NewFromInt(2000),
		Status:     domain.HoldingStatusHolding,
	})

	require.NoError(t, quoteRepo.Insert(context.Background(), &domain.PriceQuote{
		AssetID:   100,
		Price:     decimal.NewFromInt(4),
		QuoteTime: time.Now(),
	}))
	// 平台 6 的 asset 200 没行情 → 市值视为 0

	tool := NewPlatformSummaryTool(PlatformSummaryDeps{
		Holding: holdingRepo, Platform: platformRepo, Quote: quoteRepo,
	})
	ctx := WithUserID(context.Background(), uid)

	out, err := callTool(ctx, t, tool, PlatformSummaryArgs{})
	require.NoError(t, err)
	require.Contains(t, out, `"count":2`)
	require.Contains(t, out, `"platform_name":"招商证券"`)
	require.Contains(t, out, `"platform_name":"天天基金"`)
	require.Contains(t, out, `"holding_count":1`)
	// 平台 5 的 total_cost=3500
	require.Contains(t, out, `"total_cost":"3500"`)
}

// =====================================================================
// Test_PlatformSummary_RepoError
// =====================================================================
func Test_PlatformSummary_RepoError(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	holdingRepo.ListByUserErr = errors.New("db down")
	tool := NewPlatformSummaryTool(PlatformSummaryDeps{
		Holding:  holdingRepo,
		Platform: &fakePlatformRepoLite{},
		Quote:    testutil.NewMockQuoteRepo(),
	})
	ctx := WithUserID(context.Background(), 7)
	_, err := callTool(ctx, t, tool, PlatformSummaryArgs{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list holdings failed")
}

// =====================================================================
// Test_PlatformSummary_NoUserIDInCtx_Errors — D13 安全回归
// =====================================================================
func Test_PlatformSummary_NoUserIDInCtx_Errors(t *testing.T) {
	tool := NewPlatformSummaryTool(PlatformSummaryDeps{
		Holding:  testutil.NewMockHoldingRepo(),
		Platform: &fakePlatformRepoLite{},
		Quote:    testutil.NewMockQuoteRepo(),
	})
	_, err := callTool(context.Background(), t, tool, PlatformSummaryArgs{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// =====================================================================
// Test_PlatformSummary_EmptySchema — args 是空 struct，schema 应为 {"type":"object"}
// =====================================================================
//
// PlatformSummaryArgs 是空 struct，按设计文档说明 schema 应渲染为 {"type":"object"}（无 properties）。
// 这是合规且符合 D13 规则 1 的（物理上无任何字段，更别提 user_id）。
func Test_PlatformSummary_EmptySchema_NoUserID(t *testing.T) {
	tool := NewPlatformSummaryTool(PlatformSummaryDeps{
		Holding:  testutil.NewMockHoldingRepo(),
		Platform: &fakePlatformRepoLite{},
		Quote:    testutil.NewMockQuoteRepo(),
	})
	decl := tool.Declaration()
	require.NotNil(t, decl.InputSchema)
	require.Equal(t, "platform_summary", decl.Name)
	// 空 struct → Properties 为 nil 或 0 长，必然不含 user_id
	require.LessOrEqual(t, len(decl.InputSchema.Properties), 0)
	b, err := json.Marshal(decl.InputSchema)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(b)), `"user_id"`)
}
