package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// Test_HoldingQuery_Success — 正常路径：返回当前用户持仓
// =====================================================================
func Test_HoldingQuery_Success(t *testing.T) {
	uid := uint(7)
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()

	asset1 := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    uid,
		AssetCode: "510300",
		Name:      "沪深300ETF",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset1)
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:     uid,
		AssetID:    100,
		PlatformID: 5,
		Quantity:   decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(3.5),
		TotalCost:  decimal.NewFromInt(3500),
		Status:     domain.HoldingStatusHolding,
		// Asset 字段不预填，让 fn 走 deps.Asset.GetByID 路径，覆盖该分支。
	})

	tool := NewHoldingQueryTool(HoldingQueryDeps{Holding: holdingRepo, Asset: assetRepo})
	ctx := WithUserID(context.Background(), uid)

	out, err := callTool(ctx, t, tool, HoldingQueryArgs{
		AssetType:  "fund",
		PlatformID: 5,
		Status:     "holding",
	})
	require.NoError(t, err)
	require.Contains(t, out, `"count":1`)
	require.Contains(t, out, `"asset_code":"510300"`)
	require.Contains(t, out, `"asset_name":"沪深300ETF"`)
	require.Contains(t, out, `"quantity":"1000"`)
	require.Contains(t, out, `"platform_id":5`)
}

// =====================================================================
// Test_HoldingQuery_RepoError — repo.ListByUser 返错 → 工具 wrap
// =====================================================================
func Test_HoldingQuery_RepoError(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	holdingRepo.ListByUserErr = errors.New("db down")

	tool := NewHoldingQueryTool(HoldingQueryDeps{Holding: holdingRepo, Asset: assetRepo})
	ctx := WithUserID(context.Background(), 7)

	_, err := callTool(ctx, t, tool, HoldingQueryArgs{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list holdings failed")
}

// =====================================================================
// Test_HoldingQuery_NoUserIDInCtx_Errors — D13 安全回归（最关键）
//
// 这是 §6.3 越权漏洞的核心防护测试：
//  1. 旧版本：args 含 UserID，user_id==0 时默认兜底为 1（→ 任意调用方都被路由到 user 1 数据）
//  2. 修复后：args 无 UserID 字段 + ctx 不注入 → 直接报错
//
// 本用例验证 fn 在 ctx 缺失 user_id 时立即返错 + 不查 repo。
// =====================================================================
func Test_HoldingQuery_NoUserIDInCtx_Errors(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	// 注入一条 user=1 的持仓，模拟旧 bug 路径——若兜底逻辑被复活，会读到这条数据
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:  domain.BaseModel{ID: 999},
		UserID:     1,
		AssetID:    100,
		PlatformID: 5,
		Quantity:   decimal.NewFromInt(1),
		Status:     domain.HoldingStatusHolding,
	})

	tool := NewHoldingQueryTool(HoldingQueryDeps{Holding: holdingRepo, Asset: assetRepo})

	// 不注入 user_id —— 应直接返错
	_, err := callTool(context.Background(), t, tool, HoldingQueryArgs{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed),
		"无 ctx user_id 时应 wrap ErrAIToolCallFailed；当前 err=%v", err)
}

// =====================================================================
// Test_HoldingQuery_ZeroUserIDInCtx_Errors — D13 c：禁止兜底
// =====================================================================
func Test_HoldingQuery_ZeroUserIDInCtx_Errors(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	tool := NewHoldingQueryTool(HoldingQueryDeps{Holding: holdingRepo, Asset: assetRepo})

	ctx := WithUserID(context.Background(), 0) // user_id=0 应被 UserIDFromContext 视为不存在
	_, err := callTool(ctx, t, tool, HoldingQueryArgs{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// =====================================================================
// Test_HoldingQuery_NoUserIDFieldInSchema — main 验收硬约束
//
// architect 明确："仅 holding_query 必须" — 用 Declaration().InputSchema marshal
// 后断言不含 user_id / userID（case-insensitive）。
// =====================================================================
func Test_HoldingQuery_NoUserIDFieldInSchema(t *testing.T) {
	tool := NewHoldingQueryTool(HoldingQueryDeps{
		Holding: testutil.NewMockHoldingRepo(),
		Asset:   testutil.NewMockAssetRepo(),
	})

	decl := tool.Declaration()
	require.NotNil(t, decl)
	require.NotNil(t, decl.InputSchema)
	require.Equal(t, "holding_query", decl.Name)

	// 1. Properties keys 不含 user_id / userID（case-insensitive）
	for k := range decl.InputSchema.Properties {
		lower := strings.ToLower(k)
		require.NotEqual(t, "user_id", lower, "InputSchema.Properties 不允许含 user_id（main 验收硬约束）")
		require.NotEqual(t, "userid", lower)
	}

	// 2. Required 列表不含 user_id
	for _, r := range decl.InputSchema.Required {
		lower := strings.ToLower(r)
		require.NotEqual(t, "user_id", lower)
		require.NotEqual(t, "userid", lower)
	}

	// 3. 整个 InputSchema 树 marshal 后字符串不含 user_id（兜底防 nested / description 暗藏）
	b, err := json.Marshal(decl.InputSchema)
	require.NoError(t, err)
	lower := strings.ToLower(string(b))
	require.NotContains(t, lower, `"user_id"`, "InputSchema 序列化结果不应包含 user_id 字段（包括嵌套）")
	require.NotContains(t, lower, `"userid"`)
}
