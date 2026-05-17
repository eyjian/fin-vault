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

// callHistoryQueryFn 把 SDK CallableTool 还原为底层 fn 调用。
//
// 单测原则：直接调底层 fn（绕开 SDK Call(jsonArgs) 的 unmarshal 路径），
// 这样我们可以注入任意 Args 结构、任意 ctx，专注验证 fn 业务逻辑 + D13 约束。
//
// 实现细节：tools 包的 NewXxxTool 返回 sdkfunction.NewFunctionTool(fn, opts...)，
// 但我们没有公开 fn。最稳的做法是直接走 SDK 的 Call(ctx, jsonArgs)：
// 这条路径已经覆盖了 unmarshal + fn 调用 + marshal 返回值，足以验证业务行为。
//
// 因此本文件统一用 SDK Call 路径调用工具。
func callTool(ctx context.Context, t *testing.T, tool interface {
	Call(ctx context.Context, jsonArgs []byte) (any, error)
}, args any) (string, error) {
	t.Helper()
	jsonArgs, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := tool.Call(ctx, jsonArgs)
	if err != nil {
		return "", err
	}
	// out 可能是 struct，也可能已是 []byte/string。统一序列化为 JSON 字符串便于断言。
	switch v := out.(type) {
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	default:
		b, mErr := json.Marshal(v)
		require.NoError(t, mErr)
		return string(b), nil
	}
}

// newHistoryQueryFixture 构造 history_query 工具 + mock 依赖 + 默认 user。
func newHistoryQueryFixture() (*MockHistoryTxnRepo, repository.TransactionRepository, uint) {
	uid := uint(42)
	repo := newMockHistoryTxnRepo()
	return repo, repo, uid
}

// MockHistoryTxnRepo 仅用于 history_query 测试：
// 与 testutil.MockTransactionRepo 的差别——本 mock 让 List 返回可配置切片 + 总数 +
// 错误，而非依赖 Insert 历史。这样可以直接构造场景。
type MockHistoryTxnRepo struct {
	ListResult []domain.Transaction
	ListTotal  int64
	ListErr    error
	LastOpts   repository.ListOptions
}

func newMockHistoryTxnRepo() *MockHistoryTxnRepo {
	return &MockHistoryTxnRepo{}
}

// Create / GetByID / ListByHolding / ExistsByExternalID 仅满足接口，不做业务。
func (m *MockHistoryTxnRepo) Create(_ context.Context, _ *domain.Transaction) error { return nil }
func (m *MockHistoryTxnRepo) GetByID(_ context.Context, _, _ uint) (*domain.Transaction, error) {
	return nil, repository.ErrNotFound
}
func (m *MockHistoryTxnRepo) ListByHolding(_ context.Context, _ uint) ([]domain.Transaction, error) {
	return nil, nil
}
func (m *MockHistoryTxnRepo) ExistsByExternalID(_ context.Context, _, _ uint, _ string) (bool, error) {
	return false, nil
}

// List 实现：记录 opts 便于测试断言 UserID 注入正确，并按 ListResult/ListErr 返回。
func (m *MockHistoryTxnRepo) List(_ context.Context, opts repository.ListOptions) ([]domain.Transaction, int64, error) {
	m.LastOpts = opts
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}
	return m.ListResult, m.ListTotal, nil
}

// 编译期接口断言。
var _ repository.TransactionRepository = (*MockHistoryTxnRepo)(nil)

// =====================================================================
// Test_HistoryQuery_Success — 正常路径
// =====================================================================
func Test_HistoryQuery_Success(t *testing.T) {
	repoMock, repo, uid := newHistoryQueryFixture()
	repoMock.ListResult = []domain.Transaction{
		{
			BaseModel:  domain.BaseModel{ID: 100},
			UserID:     uid,
			HoldingID:  10,
			AssetID:    20,
			PlatformID: 5,
			TxnType:    domain.TxnTypeBuy,
			TxnTime:    time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC),
			Quantity:   decimal.NewFromInt(100),
			Price:      decimal.NewFromFloat(1.234),
			Amount:     decimal.NewFromFloat(123.4),
			Fee:        decimal.NewFromFloat(0.5),
			Tax:        decimal.Zero,
			NetAmount:  decimal.NewFromFloat(123.9),
			Currency:   "CNY",
		},
	}
	repoMock.ListTotal = 1

	tool := NewHistoryQueryTool(HistoryQueryDeps{Transaction: repo})
	ctx := WithUserID(context.Background(), uid)

	out, err := callTool(ctx, t, tool, HistoryQueryArgs{
		HoldingID:  10,
		AssetID:    20,
		PlatformID: 5,
		TxnType:    "buy",
		Start:      "2026-01-01",
		End:        "2026-01-31",
		Limit:      50,
	})
	require.NoError(t, err)
	require.Contains(t, out, `"count":1`)
	require.Contains(t, out, `"total":1`)
	require.Contains(t, out, `"txn_type":"buy"`)
	require.Contains(t, out, `"asset_id":20`)
	require.Contains(t, out, `"quantity":"100"`)

	// 验证 D13 b：UserID 来自 ctx 注入，而非 args。
	require.Equal(t, uid, repoMock.LastOpts.UserID)
	require.Equal(t, 50, repoMock.LastOpts.PageSize)
	// 默认 limit 不超过 historyMaxLimit。
	require.LessOrEqual(t, repoMock.LastOpts.PageSize, historyMaxLimit)
}

// =====================================================================
// Test_HistoryQuery_Failure — repo.List 返回 error → 工具返回 wrapped error
// =====================================================================
func Test_HistoryQuery_RepoError(t *testing.T) {
	repoMock, repo, uid := newHistoryQueryFixture()
	repoMock.ListErr = errors.New("db connection failed")

	tool := NewHistoryQueryTool(HistoryQueryDeps{Transaction: repo})
	ctx := WithUserID(context.Background(), uid)

	_, err := callTool(ctx, t, tool, HistoryQueryArgs{})
	require.Error(t, err)
	// fn 内部错误链：fmt.Errorf("list transactions failed: %w", err) - 验证消息层语义。
	require.Contains(t, err.Error(), "list transactions failed")
}

// =====================================================================
// Test_HistoryQuery_NoUserIDInCtx_Errors — D13 安全回归
// =====================================================================
func Test_HistoryQuery_NoUserIDInCtx_Errors(t *testing.T) {
	_, repo, _ := newHistoryQueryFixture()
	tool := NewHistoryQueryTool(HistoryQueryDeps{Transaction: repo})

	// 不注入 user_id —— 应直接返错，不读 args。
	_, err := callTool(context.Background(), t, tool, HistoryQueryArgs{HoldingID: 99})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed),
		"应 wrap ErrAIToolCallFailed，便于 service 层错误映射；实际 err=%v", err)
}

// =====================================================================
// Test_HistoryQuery_ZeroUserIDInCtx_Errors — D13 c：禁止 user_id==0 兜底
// =====================================================================
func Test_HistoryQuery_ZeroUserIDInCtx_Errors(t *testing.T) {
	_, repo, _ := newHistoryQueryFixture()
	tool := NewHistoryQueryTool(HistoryQueryDeps{Transaction: repo})

	// 注入 user_id=0 —— UserIDFromContext 应返回 (0,false)，工具 fn 报错。
	ctx := WithUserID(context.Background(), 0)
	_, err := callTool(ctx, t, tool, HistoryQueryArgs{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// =====================================================================
// Test_HistoryQuery_NoUserIDFieldInSchema — D13 a：schema 物理上不暴露 user_id
// =====================================================================
//
// 虽然 architect 说仅 holding_query 必须，但我对所有涉用户工具都做这层防御性断言：
// 只要 Properties / required 含 user_id（任意大小写），立即失败。成本极低。
func Test_HistoryQuery_NoUserIDFieldInSchema(t *testing.T) {
	_, repo, _ := newHistoryQueryFixture()
	tool := NewHistoryQueryTool(HistoryQueryDeps{Transaction: repo})

	decl := tool.Declaration()
	require.NotNil(t, decl)
	require.NotNil(t, decl.InputSchema)

	// 1. Properties keys 不含 user_id / userID（case-insensitive）
	for k := range decl.InputSchema.Properties {
		lower := strings.ToLower(k)
		require.NotEqual(t, "user_id", lower, "input schema 不允许暴露 user_id 字段（D13 规则 1）")
		require.NotEqual(t, "userid", lower)
	}
	// 2. JSON marshal 后字符串也不应含 user_id（防 nested 字段 / 描述中暗藏）
	b, err := json.Marshal(decl.InputSchema)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(b)), `"user_id"`)
	require.NotContains(t, strings.ToLower(string(b)), `"userid"`)
}

// 引用 testutil 包以保持 import 通畅（即便本文件目前没用到 MockHoldingRepo，
// dev_2 可能在 search_fund/market_quote 测试用到；保留 import 在共享分支无影响）。
var _ = testutil.NewMockHoldingRepo
