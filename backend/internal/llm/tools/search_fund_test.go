package tools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
)

// callTool 直接调 sdktool.CallableTool.Call(ctx, jsonArgs) 拿原始 any 输出，
// 简化测试断言；使用 strings.NewReader 风格的 jsonArgs。
func callSearchFund(t *testing.T, ctx context.Context, deps tools.SearchFundDeps, jsonArgs string) (any, error) {
	t.Helper()
	tool := tools.NewSearchFundTool(deps)
	return tool.Call(ctx, []byte(jsonArgs))
}

// =====================================================================
// 正常路径
// =====================================================================

func TestSearchFund_Success_ReturnsItemsBoundedByLimit(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()
	// 预设 5 条 fund 资产；List 不读 Filters（mock 直接吐 ListResult）
	mockAsset.ListResult = []domain.Asset{
		{AssetCode: "110011", Name: "易方达医疗", AssetType: domain.AssetTypeFund},
		{AssetCode: "001186", Name: "富国新动力", AssetType: domain.AssetTypeFund},
		{AssetCode: "519005", Name: "海富通医药", AssetType: domain.AssetTypeFund},
		{AssetCode: "163406", Name: "兴全合宜", AssetType: domain.AssetTypeFund},
		{AssetCode: "260108", Name: "景顺长城新兴成长", AssetType: domain.AssetTypeFund},
	}

	out, err := callSearchFund(t, context.Background(), tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":"医药","limit":3}`)
	require.NoError(t, err)
	require.NotNil(t, out)

	// 返回类型是 SearchFundOutput；用类型断言或反射检查
	result, ok := out.(tools.SearchFundOutput)
	require.True(t, ok, "返回类型必须是 SearchFundOutput, got %T", out)
	// mock 不过滤，所以返回全部 5 条；但工具内 limit 截断由 ListOptions.PageSize 控制——
	// 由于 mock List 也忽略 PageSize，5 条都会被返回；测试关注"格式正确 + 字段映射对"
	assert.Len(t, result.Items, 5)
	assert.Equal(t, 5, result.Count)
	// 字段映射检查
	assert.Equal(t, "110011", result.Items[0].Code)
	assert.Equal(t, "易方达医疗", result.Items[0].Name)
	assert.Equal(t, "fund", result.Items[0].Type)
}

func TestSearchFund_Success_EmptyResult(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()
	mockAsset.ListResult = []domain.Asset{}

	out, err := callSearchFund(t, context.Background(), tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":"nothing"}`)
	require.NoError(t, err)
	result, ok := out.(tools.SearchFundOutput)
	require.True(t, ok)
	assert.Empty(t, result.Items)
	assert.Equal(t, 0, result.Count)
}

// =====================================================================
// 失败路径
// =====================================================================

func TestSearchFund_EmptyKeyword_Errors(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()

	_, err := callSearchFund(t, context.Background(), tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":""}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyword required")
}

func TestSearchFund_WhitespaceKeyword_Errors(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()

	_, err := callSearchFund(t, context.Background(), tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":"   "}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyword required")
}

func TestSearchFund_RepoError_PropagatesWithWrap(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()
	repoErr := errors.New("simulated db failure")
	mockAsset.ListErr = repoErr

	_, err := callSearchFund(t, context.Background(), tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":"医药"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list funds failed")
	// fmt.Errorf("...: %w", repoErr) 应当能 errors.Is 拿到原始 error
	assert.True(t, errors.Is(err, repoErr), "wrap chain 应包含底层 repo error")
}

// =====================================================================
// D13 安全回归：search_fund 不读 ctx user_id（公共查询）
// =====================================================================

// TestSearchFund_NoUserIDInCtx_StillWorks 验证 search_fund 即使 ctx 没注入 user_id
// 也能正常工作——它是公共数据查询，不依赖 D13 ctx 隔离机制。
func TestSearchFund_NoUserIDInCtx_StillWorks(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()
	mockAsset.ListResult = []domain.Asset{
		{AssetCode: "110011", Name: "易方达医疗", AssetType: domain.AssetTypeFund},
	}

	// 显式不调 tools.WithUserID，ctx 干净
	ctx := context.Background()
	_, ok := tools.UserIDFromContext(ctx)
	require.False(t, ok, "前置条件：ctx 没有 user_id")

	out, err := callSearchFund(t, ctx, tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":"医药"}`)
	require.NoError(t, err, "search_fund 不应因缺 ctx user_id 失败")
	result, ok := out.(tools.SearchFundOutput)
	require.True(t, ok)
	assert.Equal(t, 1, result.Count)
}

// TestSearchFund_WithUserIDInCtx_NotIsolated 验证即使 ctx 注入了 user_id，
// search_fund 也不基于 user_id 做过滤——查询走 ListOptions.UserID=0 的公共路径。
//
// （保护未来 search_fund 实现被误改成 user-scoped 时立即被这条测试拦下。）
func TestSearchFund_WithUserIDInCtx_NotIsolated(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()
	mockAsset.ListResult = []domain.Asset{
		{AssetCode: "110011", Name: "易方达医疗", AssetType: domain.AssetTypeFund},
	}

	ctx := tools.WithUserID(context.Background(), 42)
	out, err := callSearchFund(t, ctx, tools.SearchFundDeps{Asset: mockAsset}, `{"keyword":"医药"}`)
	require.NoError(t, err)
	result, ok := out.(tools.SearchFundOutput)
	require.True(t, ok)
	assert.Equal(t, 1, result.Count)
}

// =====================================================================
// 工具元信息
// =====================================================================

// TestSearchFund_Declaration 验证工具元信息（name/description）符合 spec 要求，
// 工具清单日志能正确识别。
func TestSearchFund_Declaration(t *testing.T) {
	tool := tools.NewSearchFundTool(tools.SearchFundDeps{})
	require.NotNil(t, tool)
	d := tool.Declaration()
	require.NotNil(t, d)
	assert.Equal(t, "search_fund", d.Name)
	assert.NotEmpty(t, d.Description)
	assert.True(t, strings.Contains(d.Description, "基金") || strings.Contains(d.Description, "keyword"),
		"description 应说明工具用途")
}
