// Package service —— PulseDiagnosisService 单元测试。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// 测试辅助：mock ChatClient + mock PulseDiagnosisRepository
// =====================================================================

// fakeChat 简单 mock：按调用顺序返回预设回复，错误优先。
type fakeChat struct {
	responses []string
	err       error
	calls     int
}

func (f *fakeChat) Chat(_ context.Context, _, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	idx := f.calls
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	f.calls++
	if idx < 0 {
		return "", nil
	}
	return f.responses[idx], nil
}

// mockPulseRepo 内存版 PulseDiagnosisRepository。
type mockPulseRepo struct {
	byKey     map[string]*domain.PulseDiagnosis
	upsertErr error
	getErr    error
}

func newMockPulseRepo() *mockPulseRepo {
	return &mockPulseRepo{byKey: map[string]*domain.PulseDiagnosis{}}
}

func pulseKey(uid, aid uint) string {
	return string(rune(uid)) + "|" + string(rune(aid))
}

func (m *mockPulseRepo) Upsert(_ context.Context, d *domain.PulseDiagnosis) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	k := pulseKey(d.UserID, d.AssetID)
	if existing, ok := m.byKey[k]; ok {
		// 保留 ID 与 CreatedAt
		d.ID = existing.ID
		d.CreatedAt = existing.CreatedAt
	}
	cp := *d
	m.byKey[k] = &cp
	return nil
}

func (m *mockPulseRepo) GetByUserAsset(_ context.Context, userID, assetID uint) (*domain.PulseDiagnosis, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	d, ok := m.byKey[pulseKey(userID, assetID)]
	if !ok {
		return nil, nil
	}
	cp := *d
	return &cp, nil
}

func (m *mockPulseRepo) ListByUser(_ context.Context, userID uint, assetIDs []uint) ([]domain.PulseDiagnosis, error) {
	want := map[uint]bool{}
	for _, a := range assetIDs {
		want[a] = true
	}
	out := make([]domain.PulseDiagnosis, 0, len(m.byKey))
	for _, d := range m.byKey {
		if d.UserID != userID {
			continue
		}
		if len(want) > 0 && !want[d.AssetID] {
			continue
		}
		out = append(out, *d)
	}
	return out, nil
}

// =====================================================================
// 构造测试 service 的便捷函数
// =====================================================================

func newPulseTestSvc(chat *fakeChat, pulse *mockPulseRepo) (
	*PulseDiagnosisService,
	*testutil.MockHoldingRepo,
	*testutil.MockAssetRepo,
	*testutil.MockQuoteRepo,
) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	rateRepo := testutil.NewMockRateRepo()
	platformRepo := testutil.NewMockPlatformRepo()

	holdingSvc := NewHoldingService(holdingRepo, assetRepo, quoteRepo, rateRepo, platformRepo)
	if chat == nil {
		// nil chat 用于"不可用"路径测试
		return NewPulseDiagnosisService(nil, holdingSvc, assetRepo, pulse, quoteRepo),
			holdingRepo, assetRepo, quoteRepo
	}
	return NewPulseDiagnosisService(chat, holdingSvc, assetRepo, pulse, quoteRepo),
		holdingRepo, assetRepo, quoteRepo
}

// 准备一个有持仓 + 行情的资产，让把脉路径能跑到 LLM。
func setupAssetWithHolding(
	t *testing.T,
	assetRepo *testutil.MockAssetRepo,
	holdingRepo *testutil.MockHoldingRepo,
	quoteRepo *testutil.MockQuoteRepo,
) (*domain.Asset, *domain.Holding) {
	t.Helper()
	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "测试基金",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset)

	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:     1,
		AssetID:    100,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(1.2),
		TotalCost:  decimal.NewFromInt(1200),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)

	quoteRepo.Latest[100] = &domain.PriceQuote{
		AssetID: 100,
		Price:   decimal.NewFromFloat(1.5),
	}
	return asset, holding
}

// =====================================================================
// IsAvailable / Diagnose 不可用路径
// =====================================================================

func TestPulseDiagnosis_NotAvailable_WhenChatNil(t *testing.T) {
	svc, _, _, _ := newPulseTestSvc(nil, newMockPulseRepo())
	assert.False(t, svc.IsAvailable())

	_, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       1,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrAIPulseUnavailable)
}

// =====================================================================
// 数据不足兜底（无持仓 → 直接返 hold + low，不调 LLM，不落库）
// =====================================================================

func TestPulseDiagnosis_DataInsufficient_NoHolding(t *testing.T) {
	chat := &fakeChat{responses: []string{"should not be called"}}
	pulse := newMockPulseRepo()
	svc, _, assetRepo, _ := newPulseTestSvc(chat, pulse)

	// 仅创建资产，没有任何持仓
	assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "测试基金",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	})

	res, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, domain.PulseRecHold, res.Recommendation)
	assert.Equal(t, domain.PulseConfLow, res.Confidence)
	assert.Contains(t, res.Summary, "数据不足")
	assert.Equal(t, 0, chat.calls, "数据不足路径不应调用 LLM")
	assert.Empty(t, pulse.byKey, "数据不足路径不应落库")
}

// =====================================================================
// 正常把脉：LLM 返回合法 JSON → 解析 + Upsert
// =====================================================================

func TestPulseDiagnosis_Normal_ParseAndUpsert(t *testing.T) {
	chat := &fakeChat{responses: []string{`{
		"recommendation": "reduce",
		"confidence": "medium",
		"summary": "盈利已达 25%，估值偏高，建议减仓 30%",
		"detail": "当前持仓盈利比率 25%，已达常见的减仓阈值。市盈率（PE）= 每 1 元利润对应的价格，当前 PE 高于同类基金均值，估值偏高。建议先减仓 30%，锁定部分利润，剩余仓位继续观察。这种策略叫做'分批止盈'，可以避免一次性卖出后错过后续上涨。",
		"data_references": [
			{ "metric": "pnl_ratio", "value": "0.25", "note": "盈亏比率" }
		]
	}`}}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	res, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, domain.PulseRecReduce, res.Recommendation)
	assert.Equal(t, domain.PulseConfMedium, res.Confidence)
	assert.Contains(t, res.Summary, "减仓")
	assert.NotEmpty(t, res.Detail)
	assert.Equal(t, domain.PulseTriggerManual, res.TriggerSource)

	// 落库验证
	assert.Len(t, pulse.byKey, 1)
	saved := pulse.byKey[pulseKey(1, 100)]
	require.NotNil(t, saved)
	assert.Equal(t, domain.PulseRecReduce, saved.Recommendation)
	assert.NotEmpty(t, saved.RawResponse)
}

// =====================================================================
// 重新把脉覆盖旧结果（ID/CreatedAt 保留）
// =====================================================================

func TestPulseDiagnosis_RePulse_Overwrites(t *testing.T) {
	chat := &fakeChat{responses: []string{
		`{"recommendation":"hold","confidence":"high","summary":"稳定持有","detail":"盈亏比率仅 2%，市场无明显信号，继续持有即可。术语：盈亏比率 = 总盈亏 / 总成本，反映单位成本的回报率。当前数据未触发任何调仓阈值，不建议频繁操作。"}`,
		`{"recommendation":"add","confidence":"medium","summary":"低估优质，建议加仓","detail":"经过一段时间观察，估值已显著回落。市盈率（PE）= 每元利润对应的价格，当前 PE 低于行业 30%，属于低估区间。建议小幅加仓 10%-20%，分批建仓可以平滑买入成本（这种策略称为'定投'）。"}`,
	}}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	// 第 1 次把脉
	r1, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.NoError(t, err)
	saved1 := pulse.byKey[pulseKey(1, 100)]
	require.NotNil(t, saved1)
	firstID := saved1.ID
	firstCreatedAt := saved1.CreatedAt
	assert.Equal(t, domain.PulseRecHold, r1.Recommendation)

	// 第 2 次把脉，应覆盖
	r2, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerChat,
		SessionID:     "sess-1",
	})
	require.NoError(t, err)
	saved2 := pulse.byKey[pulseKey(1, 100)]
	require.NotNil(t, saved2)

	assert.Equal(t, domain.PulseRecAdd, r2.Recommendation)
	assert.Equal(t, firstID, saved2.ID, "ID 必须保留")
	assert.Equal(t, firstCreatedAt.UnixNano(), saved2.CreatedAt.UnixNano(), "CreatedAt 必须保留")
	assert.Equal(t, "sess-1", saved2.SessionID)
	assert.Equal(t, domain.PulseTriggerChat, saved2.TriggerSource)
	assert.Len(t, pulse.byKey, 1, "只保留一条记录")
}

// =====================================================================
// LLM 输出含 markdown 代码块时仍能解析
// =====================================================================

func TestPulseDiagnosis_ParseWithMarkdownCodeBlock(t *testing.T) {
	raw := "```json\n" + `{"recommendation":"sell","confidence":"high","summary":"亏损 30% 触发止损线","detail":"当前未实现盈亏 -30%，已突破常见的 -20% 止损线。止损线（Stop Loss）= 预设的最大可承受亏损比例，目的是防止亏损扩大。建议立即止损卖出，把资金挪到表现更稳定的标的，避免被深套后长期套牢。这是初学者最重要的纪律之一。"}` + "\n```"
	chat := &fakeChat{responses: []string{raw}}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	res, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.PulseRecSell, res.Recommendation)
	assert.Equal(t, domain.PulseConfHigh, res.Confidence)
}

// =====================================================================
// LLM 输出非 JSON：第一次解析失败，重试也失败 → 返 ErrAIPulseParseFailed
// =====================================================================

func TestPulseDiagnosis_ParseFailed_AfterRetry(t *testing.T) {
	chat := &fakeChat{responses: []string{
		"我建议你卖出。",
		"还是卖出更好。",
	}}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	_, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.Error(t, err)
	bizErr := errs.As(err)
	require.NotNil(t, bizErr)
	assert.Equal(t, errs.ErrAIPulseParseFailed.Code, bizErr.Code)
	assert.Equal(t, 2, chat.calls, "应该重试 1 次（共调用 2 次）")
	assert.Empty(t, pulse.byKey, "解析失败不应落库")
}

// =====================================================================
// LLM 业务错误（rate limit）直接透传，不重试
// =====================================================================

func TestPulseDiagnosis_LLMError_PassThrough(t *testing.T) {
	chat := &fakeChat{err: errs.ErrAIProviderRateLimited}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	_, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrAIProviderRateLimited))
}

// =====================================================================
// 非法 recommendation 字段 → 解析失败错误
// =====================================================================

func TestPulseDiagnosis_InvalidRecommendation(t *testing.T) {
	chat := &fakeChat{responses: []string{
		`{"recommendation":"unknown_action","confidence":"high","summary":"x","detail":"y"}`,
		`{"recommendation":"???","confidence":"high","summary":"x","detail":"y"}`,
	}}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	_, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.Error(t, err)
	bizErr := errs.As(err)
	require.NotNil(t, bizErr)
	assert.Equal(t, errs.ErrAIPulseParseFailed.Code, bizErr.Code)
}

// =====================================================================
// 缺失 confidence 时降级为 medium（非致命）
// =====================================================================

func TestPulseDiagnosis_MissingConfidence_FallbackMedium(t *testing.T) {
	chat := &fakeChat{responses: []string{
		`{"recommendation":"hold","summary":"稳定持有","detail":"盈亏在合理范围内，无需操作。建议关注大盘走势，等待明确信号再做调整。术语：仓位 = 持仓占资产组合的比例，合理仓位通常在 30-70% 之间，避免单一资产过重。"}`,
	}}
	pulse := newMockPulseRepo()
	svc, holdingRepo, assetRepo, quoteRepo := newPulseTestSvc(chat, pulse)
	setupAssetWithHolding(t, assetRepo, holdingRepo, quoteRepo)

	res, err := svc.Diagnose(context.Background(), PulseDiagnoseInput{
		UserID:        1,
		AssetID:       100,
		TriggerSource: domain.PulseTriggerManual,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.PulseRecHold, res.Recommendation)
	assert.Equal(t, domain.PulseConfMedium, res.Confidence)
}

// =====================================================================
// GetCached / ListCached
// =====================================================================

func TestPulseDiagnosis_GetCached_Empty(t *testing.T) {
	pulse := newMockPulseRepo()
	svc, _, _, _ := newPulseTestSvc(&fakeChat{}, pulse)

	res, err := svc.GetCached(context.Background(), 1, 100)
	require.NoError(t, err)
	assert.Nil(t, res, "未把脉过的资产应返回 nil")
}

func TestPulseDiagnosis_ListCached_FiltersByAssetIDs(t *testing.T) {
	pulse := newMockPulseRepo()
	pulse.byKey[pulseKey(1, 100)] = &domain.PulseDiagnosis{
		ID: "x", UserID: 1, AssetID: 100, Recommendation: domain.PulseRecHold, Confidence: domain.PulseConfHigh, TriggerSource: domain.PulseTriggerManual,
	}
	pulse.byKey[pulseKey(1, 200)] = &domain.PulseDiagnosis{
		ID: "y", UserID: 1, AssetID: 200, Recommendation: domain.PulseRecAdd, Confidence: domain.PulseConfMedium, TriggerSource: domain.PulseTriggerManual,
	}
	pulse.byKey[pulseKey(2, 100)] = &domain.PulseDiagnosis{
		ID: "z", UserID: 2, AssetID: 100, Recommendation: domain.PulseRecSell, Confidence: domain.PulseConfHigh, TriggerSource: domain.PulseTriggerManual,
	}
	svc, _, _, _ := newPulseTestSvc(&fakeChat{}, pulse)

	// 用户 1 全部
	all, err := svc.ListCached(context.Background(), 1, nil)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// 用户 1，仅 asset_id=100
	filtered, err := svc.ListCached(context.Background(), 1, []uint{100})
	require.NoError(t, err)
	assert.Len(t, filtered, 1)
	assert.Equal(t, uint(100), filtered[0].AssetID)
}
