// Package handler —— PulseDiagnosisHandler 单元测试。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// 测试 mocks（与 service 单测共用同一套思路：真 service + fake chat + 内存 repo）
// =====================================================================

// fakePulseChatClient handler 单测用的 ChatClient mock。
type fakePulseChatClient struct {
	responses []string
	err       error
	calls     atomic.Int32
}

func (f *fakePulseChatClient) Chat(_ context.Context, _, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	idx := int(f.calls.Add(1)) - 1
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	if idx < 0 {
		return "", nil
	}
	return f.responses[idx], nil
}

// memPulseRepo 内存版 PulseDiagnosisRepository（与 service 测试中的 mock 等价）。
type memPulseRepo struct {
	byKey map[string]*domain.PulseDiagnosis
}

func newMemPulseRepo() *memPulseRepo {
	return &memPulseRepo{byKey: map[string]*domain.PulseDiagnosis{}}
}

func memPulseKey(uid, aid uint) string {
	return string(rune(uid)) + "|" + string(rune(aid))
}

func (m *memPulseRepo) Upsert(_ context.Context, d *domain.PulseDiagnosis) error {
	k := memPulseKey(d.UserID, d.AssetID)
	if existing, ok := m.byKey[k]; ok {
		d.ID = existing.ID
		d.CreatedAt = existing.CreatedAt
	}
	cp := *d
	m.byKey[k] = &cp
	return nil
}

func (m *memPulseRepo) GetByUserAsset(_ context.Context, userID, assetID uint) (*domain.PulseDiagnosis, error) {
	d, ok := m.byKey[memPulseKey(userID, assetID)]
	if !ok {
		return nil, nil
	}
	cp := *d
	return &cp, nil
}

func (m *memPulseRepo) ListByUser(_ context.Context, userID uint, assetIDs []uint) ([]domain.PulseDiagnosis, error) {
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
// 测试基础设施
// =====================================================================

// setupPulseHandler 装配一个真 PulseDiagnosisService（fake chat + 内存 repo）+ handler，
// 返回 (router, fakeChat, pulseRepo, holdingRepo, assetRepo, quoteRepo) 便于灌数据/断言。
func setupPulseHandler(t *testing.T, concurrency int) (
	*gin.Engine, *fakePulseChatClient, *memPulseRepo,
	*testutil.MockHoldingRepo, *testutil.MockAssetRepo, *testutil.MockQuoteRepo,
) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	chat := &fakePulseChatClient{}
	pulse := newMemPulseRepo()
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	rateRepo := testutil.NewMockRateRepo()
	platformRepo := testutil.NewMockPlatformRepo()
	holdingSvc := service.NewHoldingService(holdingRepo, assetRepo, quoteRepo, rateRepo, platformRepo)
	pulseSvc := service.NewPulseDiagnosisService(chat, holdingSvc, assetRepo, pulse, quoteRepo)

	h := NewPulseDiagnosisHandler(pulseSvc, concurrency)
	r := gin.New()
	v1 := r.Group("/api/v1")
	h.Register(v1)

	return r, chat, pulse, holdingRepo, assetRepo, quoteRepo
}

// pulseDoReq 发起请求；userID="" 不带 X-User-Id。
func pulseDoReq(r *gin.Engine, method, url, userID string, body interface{}) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, url, buf)
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// pulseParseBody 解析响应到 response.Body。
func pulseParseBody(t *testing.T, w *httptest.ResponseRecorder) response.Body {
	t.Helper()
	var b response.Body
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &b))
	return b
}

// pulseDataAs 把 response.Body.Data 解到 dst。
func pulseDataAs(t *testing.T, body response.Body, dst interface{}) {
	t.Helper()
	raw, err := json.Marshal(body.Data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, dst))
}

// seedPulseAssetWithHolding 灌一个有持仓 + 行情的 asset，让把脉路径能进 LLM。
func seedPulseAssetWithHolding(t *testing.T, assetID uint,
	assetRepo *testutil.MockAssetRepo, holdingRepo *testutil.MockHoldingRepo, quoteRepo *testutil.MockQuoteRepo) {
	t.Helper()
	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: assetID},
		UserID:    1,
		AssetCode: "F" + string(rune(assetID)),
		Name:      "测试基金",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset)
	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: assetID},
		UserID:     1,
		AssetID:    assetID,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(1.0),
		TotalCost:  decimal.NewFromInt(1000),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)
	quoteRepo.Latest[assetID] = &domain.PriceQuote{
		AssetID: assetID,
		Price:   decimal.NewFromFloat(1.5),
	}
}

// =====================================================================
// 路由 / 鉴权
// =====================================================================

func TestPulseHandler_Create_Missing_X_User_Id_Returns401(t *testing.T) {
	r, _, _, _, _, _ := setupPulseHandler(t, 3)
	w := pulseDoReq(r, http.MethodPost, "/api/v1/ai/pulse-diagnosis", "", PulseDiagnoseReq{AssetIDs: []uint{1}})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPulseHandler_Create_EmptyAssetIDs_Returns400(t *testing.T) {
	r, _, _, _, _, _ := setupPulseHandler(t, 3)
	w := pulseDoReq(r, http.MethodPost, "/api/v1/ai/pulse-diagnosis", "1", PulseDiagnoseReq{AssetIDs: []uint{}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =====================================================================
// POST 单/批量把脉成功
// =====================================================================

func TestPulseHandler_Create_Single_Success(t *testing.T) {
	r, chat, pulse, hRepo, aRepo, qRepo := setupPulseHandler(t, 3)
	seedPulseAssetWithHolding(t, 100, aRepo, hRepo, qRepo)
	chat.responses = []string{`{"recommendation":"hold","confidence":"high","summary":"稳定持有","detail":"盈亏在合理区间内，维持现有仓位。术语：仓位 = 持仓占总资产的比例，建议合理控制单一资产仓位避免风险集中。"}`}

	w := pulseDoReq(r, http.MethodPost, "/api/v1/ai/pulse-diagnosis", "1", PulseDiagnoseReq{AssetIDs: []uint{100}})
	require.Equal(t, http.StatusOK, w.Code)

	body := pulseParseBody(t, w)
	assert.Equal(t, 0, body.Code)
	var resp PulseDiagnoseResp
	pulseDataAs(t, body, &resp)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "success", resp.Items[0].Status)
	assert.Equal(t, "hold", resp.Items[0].Recommendation)
	assert.Equal(t, "high", resp.Items[0].Confidence)
	assert.NotEmpty(t, resp.Items[0].Summary)
	assert.NotEmpty(t, resp.Items[0].Detail)

	// 落库
	assert.Len(t, pulse.byKey, 1)
}

func TestPulseHandler_Create_Batch_PartialFailure(t *testing.T) {
	r, chat, _, hRepo, aRepo, qRepo := setupPulseHandler(t, 3)
	// asset 100/102 有持仓 + asset 注册；asset 101 不在 assetRepo（service 层返 ErrAssetNotFound）
	seedPulseAssetWithHolding(t, 100, aRepo, hRepo, qRepo)
	seedPulseAssetWithHolding(t, 102, aRepo, hRepo, qRepo)
	chat.responses = []string{
		`{"recommendation":"hold","confidence":"medium","summary":"持有","detail":"持仓盈亏比率为 50%，已实现可观回报。考虑到市场不确定性，建议先持有观察一段时间。基础知识：盈亏比率 = (市值 - 成本) / 成本 × 100%，反映投资回报水平。"}`,
		`{"recommendation":"reduce","confidence":"medium","summary":"减仓部分锁定","detail":"已实现 50% 收益，建议减仓 30% 锁定部分利润，避免回撤吞噬已有收益。这种策略叫'分批止盈'，是控制风险的常用方法之一。"}`,
	}

	w := pulseDoReq(r, http.MethodPost, "/api/v1/ai/pulse-diagnosis", "1",
		PulseDiagnoseReq{AssetIDs: []uint{100, 101, 102}})
	require.Equal(t, http.StatusOK, w.Code)

	body := pulseParseBody(t, w)
	var resp PulseDiagnoseResp
	pulseDataAs(t, body, &resp)
	require.Len(t, resp.Items, 3)

	// 状态汇总
	statuses := map[uint]string{}
	for _, it := range resp.Items {
		statuses[it.AssetID] = it.Status
	}
	assert.Equal(t, "success", statuses[100])
	assert.Equal(t, "failed", statuses[101], "asset 101 不存在 → 单项失败但不阻塞其他")
	assert.Equal(t, "success", statuses[102])

	// 失败项含 error_message
	for _, it := range resp.Items {
		if it.AssetID == 101 {
			assert.NotEmpty(t, it.ErrorMessage)
		}
	}
}

// =====================================================================
// 并发：多资产应实际并行执行（验证 concurrency 边界 + 不阻塞）
// =====================================================================

// slowChatClient 每次调用等待 d 后才返回，便于测并发是否真在并行执行。
type slowChatClient struct {
	d        time.Duration
	response string
	calls    atomic.Int32
}

func (s *slowChatClient) Chat(ctx context.Context, _, _ string) (string, error) {
	s.calls.Add(1)
	select {
	case <-time.After(s.d):
		return s.response, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestPulseHandler_Create_Concurrent_FasterThanSerial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	chat := &slowChatClient{
		d:        50 * time.Millisecond,
		response: `{"recommendation":"hold","confidence":"medium","summary":"sum","detail":"detail-content-meets-min-length-requirement-with-some-investment-knowledge-explanation-for-beginners"}`,
	}
	pulse := newMemPulseRepo()
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	rateRepo := testutil.NewMockRateRepo()
	platformRepo := testutil.NewMockPlatformRepo()
	holdingSvc := service.NewHoldingService(holdingRepo, assetRepo, quoteRepo, rateRepo, platformRepo)
	pulseSvc := service.NewPulseDiagnosisService(chat, holdingSvc, assetRepo, pulse, quoteRepo)

	// 灌 6 个 asset
	for aid := uint(100); aid < 106; aid++ {
		seedPulseAssetWithHolding(t, aid, assetRepo, holdingRepo, quoteRepo)
	}
	h := NewPulseDiagnosisHandler(pulseSvc, 3)
	r := gin.New()
	v1 := r.Group("/api/v1")
	h.Register(v1)

	start := time.Now()
	w := pulseDoReq(r, http.MethodPost, "/api/v1/ai/pulse-diagnosis", "1",
		PulseDiagnoseReq{AssetIDs: []uint{100, 101, 102, 103, 104, 105}})
	elapsed := time.Since(start)
	require.Equal(t, http.StatusOK, w.Code)

	// 串行 6 * 50ms = 300ms；并发=3 时应 ≈ 2 * 50ms = 100ms（含调度开销给宽容到 250ms）
	assert.Less(t, elapsed, 250*time.Millisecond,
		"并发=3 时 6 个资产应在 ~100ms 完成，远快于串行 300ms（实际 %v）", elapsed)
	assert.EqualValues(t, 6, chat.calls.Load(), "6 个资产都应被调用一次")

	body := pulseParseBody(t, w)
	var resp PulseDiagnoseResp
	pulseDataAs(t, body, &resp)
	assert.Len(t, resp.Items, 6)
}

// =====================================================================
// GET 缓存查询
// =====================================================================

func TestPulseHandler_Get_Single(t *testing.T) {
	r, _, pulse, _, _, _ := setupPulseHandler(t, 3)

	pulse.byKey[memPulseKey(1, 100)] = &domain.PulseDiagnosis{
		ID: "x", UserID: 1, AssetID: 100,
		Recommendation: domain.PulseRecHold, Confidence: domain.PulseConfHigh,
		Summary: "稳定", Detail: "...",
		TriggerSource: domain.PulseTriggerManual, UpdatedAt: time.Now(),
	}

	w := pulseDoReq(r, http.MethodGet, "/api/v1/ai/pulse-diagnosis?asset_id=100", "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := pulseParseBody(t, w)
	var resp PulseDiagnoseResp
	pulseDataAs(t, body, &resp)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, uint(100), resp.Items[0].AssetID)
	assert.Equal(t, "hold", resp.Items[0].Recommendation)
}

func TestPulseHandler_Get_BatchByCommaSeparated(t *testing.T) {
	r, _, pulse, _, _, _ := setupPulseHandler(t, 3)
	for _, aid := range []uint{100, 101, 102} {
		pulse.byKey[memPulseKey(1, aid)] = &domain.PulseDiagnosis{
			ID: "x", UserID: 1, AssetID: aid,
			Recommendation: domain.PulseRecHold, Confidence: domain.PulseConfMedium,
			TriggerSource: domain.PulseTriggerManual, UpdatedAt: time.Now(),
		}
	}
	// 用户 2 的把脉，不应被返回
	pulse.byKey[memPulseKey(2, 100)] = &domain.PulseDiagnosis{
		ID: "y", UserID: 2, AssetID: 100,
		Recommendation: domain.PulseRecSell, Confidence: domain.PulseConfHigh,
		TriggerSource: domain.PulseTriggerManual, UpdatedAt: time.Now(),
	}

	w := pulseDoReq(r, http.MethodGet, "/api/v1/ai/pulse-diagnosis?asset_ids=100,101,102", "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := pulseParseBody(t, w)
	var resp PulseDiagnoseResp
	pulseDataAs(t, body, &resp)
	assert.Len(t, resp.Items, 3, "应仅返回当前用户 (1) 的 3 条，跨用户 (2) 不可见")
}

func TestPulseHandler_Get_EmptyForNeverDiagnosed(t *testing.T) {
	r, _, _, _, _, _ := setupPulseHandler(t, 3)
	w := pulseDoReq(r, http.MethodGet, "/api/v1/ai/pulse-diagnosis?asset_id=999", "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := pulseParseBody(t, w)
	var resp PulseDiagnoseResp
	pulseDataAs(t, body, &resp)
	assert.Empty(t, resp.Items)
}

// =====================================================================
// LLM 不可用降级
// =====================================================================

func TestPulseHandler_Create_Unavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// chat=nil → svc.IsAvailable()=false
	pulseSvc := service.NewPulseDiagnosisService(
		nil,
		service.NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), testutil.NewMockPlatformRepo()),
		testutil.NewMockAssetRepo(),
		newMemPulseRepo(),
		testutil.NewMockQuoteRepo(),
	)
	h := NewPulseDiagnosisHandler(pulseSvc, 3)
	r := gin.New()
	v1 := r.Group("/api/v1")
	h.Register(v1)

	w := pulseDoReq(r, http.MethodPost, "/api/v1/ai/pulse-diagnosis", "1", PulseDiagnoseReq{AssetIDs: []uint{100}})

	body := pulseParseBody(t, w)
	assert.Equal(t, errs.ErrAIPulseUnavailable.Code, body.Code)
}
