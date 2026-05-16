// Package testutil 提供单元测试用的手写 mock 实现。
//
// 这些 mock 仅用于 *_test.go：实现 repository、cache、llm 等接口，
// 配合 testify/require 即可完成 Service 层、纯函数层的单元测试，无需起 DB / Redis / 第三方 API。
package testutil

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// =====================================================================
// MockUoW —— 直接执行 fn，不做事务隔离
// =====================================================================

// MockUoW 测试用 UnitOfWork：直接执行 fn(ctx)。
type MockUoW struct {
	// FailOnce 若为 true，下一次 Do 调用直接返回 ErrTransactionFailed，模拟事务失败。
	FailOnce bool
	// Calls 记录 Do 调用次数。
	Calls int
}

// ErrTransactionFailed 模拟事务失败错误。
var ErrTransactionFailed = errors.New("mock uow: transaction failed")

// Do 直接执行 fn，不嵌套事务。
func (m *MockUoW) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	m.Calls++
	if m.FailOnce {
		m.FailOnce = false
		return ErrTransactionFailed
	}
	return fn(ctx)
}

// =====================================================================
// MockRateRepo
// =====================================================================

// MockRateRepo 汇率仓储 mock。Latest 按 (from,to) 索引最新一条。
type MockRateRepo struct {
	mu      sync.Mutex
	Latest  map[string]*domain.ExchangeRate // key = from + "|" + to
	History []*domain.ExchangeRate
	// 若 GetLatestErr 非空，所有 GetLatest 返回该错误（不命中 Latest）。
	GetLatestErr error
}

// NewMockRateRepo 构造 MockRateRepo。
func NewMockRateRepo() *MockRateRepo {
	return &MockRateRepo{Latest: make(map[string]*domain.ExchangeRate)}
}

// SetLatest 设定 (from,to) 的最新汇率，便于测试 Setup。
func (m *MockRateRepo) SetLatest(from, to string, r *domain.ExchangeRate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Latest[from+"|"+to] = r
}

// Insert 插入。
func (m *MockRateRepo) Insert(_ context.Context, r *domain.ExchangeRate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.History = append(m.History, r)
	m.Latest[r.FromCurrency+"|"+r.ToCurrency] = r
	return nil
}

// GetLatest 返回 Latest 中的对应条目；不存在返回 ErrNotFound（service 层期望的语义）。
func (m *MockRateRepo) GetLatest(_ context.Context, from, to string, _ time.Time) (*domain.ExchangeRate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.GetLatestErr != nil {
		return nil, m.GetLatestErr
	}
	r, ok := m.Latest[from+"|"+to]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return r, nil
}

// List 返回 History（不按 currency 过滤，足以覆盖 RateService.List 测试）。
func (m *MockRateRepo) List(_ context.Context, from, to string, _ time.Time, _ time.Time) ([]*domain.ExchangeRate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*domain.ExchangeRate, 0)
	for _, r := range m.History {
		if (from == "" || r.FromCurrency == from) && (to == "" || r.ToCurrency == to) {
			out = append(out, r)
		}
	}
	return out, nil
}

// =====================================================================
// MockQuoteRepo
// =====================================================================

// MockQuoteRepo 行情仓储 mock。
type MockQuoteRepo struct {
	mu      sync.Mutex
	Latest  map[uint]*domain.PriceQuote
	Inserts []*domain.PriceQuote
	// InsertErr 若非空，Insert 直接返回该错误。
	InsertErr error
}

// NewMockQuoteRepo 构造。
func NewMockQuoteRepo() *MockQuoteRepo {
	return &MockQuoteRepo{Latest: make(map[uint]*domain.PriceQuote)}
}

// Insert 实现接口。
func (m *MockQuoteRepo) Insert(_ context.Context, q *domain.PriceQuote) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.InsertErr != nil {
		return m.InsertErr
	}
	m.Inserts = append(m.Inserts, q)
	m.Latest[q.AssetID] = q
	return nil
}

// GetLatest 实现接口。
func (m *MockQuoteRepo) GetLatest(_ context.Context, assetID uint) (*domain.PriceQuote, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.Latest[assetID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return q, nil
}

// BatchGetLatest 实现接口。
func (m *MockQuoteRepo) BatchGetLatest(_ context.Context, ids []uint) (map[uint]*domain.PriceQuote, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[uint]*domain.PriceQuote, len(ids))
	for _, id := range ids {
		if q, ok := m.Latest[id]; ok {
			out[id] = q
		}
	}
	return out, nil
}

// ListHistory 简化实现，返回空列表（足以满足当前测试场景）。
func (m *MockQuoteRepo) ListHistory(_ context.Context, _ uint, _, _ time.Time) ([]*domain.PriceQuote, error) {
	return nil, nil
}

// =====================================================================
// MockAssetRepo
// =====================================================================

// MockAssetRepo 资产仓储 mock。仅实现测试需要的方法，其余返回 ErrNotFound。
type MockAssetRepo struct {
	mu            sync.Mutex
	ByID          map[uint]*domain.Asset
	ByCode        map[string]*domain.Asset // userID + "|" + code + "|" + type
	WealthDetails map[uint]*domain.WealthDetail
	FundDetails   map[uint]*domain.FundDetail
	StockDetails  map[uint]*domain.StockDetail
	ListResult    []domain.Asset
	ListErr       error
}

// NewMockAssetRepo 构造。
func NewMockAssetRepo() *MockAssetRepo {
	return &MockAssetRepo{
		ByID:          make(map[uint]*domain.Asset),
		ByCode:        make(map[string]*domain.Asset),
		WealthDetails: make(map[uint]*domain.WealthDetail),
		FundDetails:   make(map[uint]*domain.FundDetail),
		StockDetails:  make(map[uint]*domain.StockDetail),
	}
}

// SetAsset 注入测试数据。
func (m *MockAssetRepo) SetAsset(a *domain.Asset) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ByID[a.ID] = a
	m.ByCode[codeKey(a.UserID, a.AssetCode, a.AssetType)] = a
}

func codeKey(userID uint, code string, t domain.AssetType) string {
	return fmt.Sprintf("%d|%s|%s", userID, code, string(t))
}

// Create 实现接口。
func (m *MockAssetRepo) Create(_ context.Context, a *domain.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := codeKey(a.UserID, a.AssetCode, a.AssetType)
	if _, ok := m.ByCode[key]; ok {
		return repository.ErrConflict
	}
	if a.ID == 0 {
		a.ID = uint(len(m.ByID) + 1)
	}
	m.ByID[a.ID] = a
	m.ByCode[key] = a
	return nil
}

// Update 实现接口。
func (m *MockAssetRepo) Update(_ context.Context, a *domain.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ByID[a.ID]; !ok {
		return repository.ErrNotFound
	}
	m.ByID[a.ID] = a
	return nil
}

// GetByID 实现接口。
func (m *MockAssetRepo) GetByID(_ context.Context, _userID, id uint) (*domain.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.ByID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return a, nil
}

// GetByCode 实现接口。
func (m *MockAssetRepo) GetByCode(_ context.Context, userID uint, code string, t domain.AssetType) (*domain.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.ByCode[codeKey(userID, code, t)]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return a, nil
}

// List 实现接口。
func (m *MockAssetRepo) List(_ context.Context, _ repository.ListOptions) ([]domain.Asset, int64, error) {
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}
	return m.ListResult, int64(len(m.ListResult)), nil
}

// Delete 实现接口。
func (m *MockAssetRepo) Delete(_ context.Context, _userID, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ByID, id)
	return nil
}

// UpsertFundDetail 实现接口。
func (m *MockAssetRepo) UpsertFundDetail(_ context.Context, d *domain.FundDetail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FundDetails[d.AssetID] = d
	return nil
}

// UpsertStockDetail 实现接口。
func (m *MockAssetRepo) UpsertStockDetail(_ context.Context, d *domain.StockDetail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StockDetails[d.AssetID] = d
	return nil
}

// UpsertWealthDetail 实现接口。
func (m *MockAssetRepo) UpsertWealthDetail(_ context.Context, d *domain.WealthDetail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WealthDetails[d.AssetID] = d
	return nil
}

// GetFundDetail 实现接口。
func (m *MockAssetRepo) GetFundDetail(_ context.Context, assetID uint) (*domain.FundDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.FundDetails[assetID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return d, nil
}

// GetStockDetail 实现接口。
func (m *MockAssetRepo) GetStockDetail(_ context.Context, assetID uint) (*domain.StockDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.StockDetails[assetID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return d, nil
}

// GetWealthDetail 实现接口。
func (m *MockAssetRepo) GetWealthDetail(_ context.Context, assetID uint) (*domain.WealthDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.WealthDetails[assetID]
	if !ok {
		return nil, nil // 注意：Service 层用 nil 判空，这里返回 (nil, nil) 而不是 ErrNotFound
	}
	return d, nil
}

// =====================================================================
// MockHoldingRepo
// =====================================================================

// MockHoldingRepo 持仓仓储 mock。
type MockHoldingRepo struct {
	mu              sync.Mutex
	ByID            map[uint]*domain.Holding
	Updates         []*domain.Holding
	MaturedList     []domain.Holding // ListMaturedWealth 返回的固定列表
	MaturedListErr  error
	ListByUserErr   error // 注入 ListByUser 错误
	UpdateErr       error
	UpdateErrAfter  int // 第 N 次（从 1 计）Update 触发 UpdateErr，0 表示不触发
	updateCallCount int
}

// NewMockHoldingRepo 构造。
func NewMockHoldingRepo() *MockHoldingRepo {
	return &MockHoldingRepo{ByID: make(map[uint]*domain.Holding)}
}

// SetHolding 注入测试数据。
func (m *MockHoldingRepo) SetHolding(h *domain.Holding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ByID[h.ID] = h
}

// Create 实现接口。
func (m *MockHoldingRepo) Create(_ context.Context, h *domain.Holding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h.ID == 0 {
		h.ID = uint(len(m.ByID) + 1)
	}
	m.ByID[h.ID] = h
	return nil
}

// Update 实现接口。
func (m *MockHoldingRepo) Update(_ context.Context, h *domain.Holding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCallCount++
	if m.UpdateErrAfter > 0 && m.updateCallCount == m.UpdateErrAfter {
		return m.UpdateErr
	}
	m.ByID[h.ID] = h
	cp := *h
	m.Updates = append(m.Updates, &cp)
	return nil
}

// GetByID 实现接口。
func (m *MockHoldingRepo) GetByID(_ context.Context, _userID, id uint) (*domain.Holding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.ByID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return h, nil
}

// GetOrCreate 实现接口。
func (m *MockHoldingRepo) GetOrCreate(_ context.Context, userID, assetID, platformID uint) (*domain.Holding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range m.ByID {
		if h.UserID == userID && h.AssetID == assetID && h.PlatformID == platformID {
			return h, nil
		}
	}
	h := &domain.Holding{
		UserID:     userID,
		AssetID:    assetID,
		PlatformID: platformID,
		Status:     domain.HoldingStatusHolding,
		CostMethod: domain.CostMethodWeightedAvg,
	}
	h.ID = uint(len(m.ByID) + 1)
	m.ByID[h.ID] = h
	return h, nil
}

// ListByUser 实现接口（按 ID 升序，便于测试断言顺序稳定）。
func (m *MockHoldingRepo) ListByUser(_ context.Context, _ repository.ListOptions) ([]domain.Holding, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ListByUserErr != nil {
		return nil, 0, m.ListByUserErr
	}
	ids := make([]uint, 0, len(m.ByID))
	for id := range m.ByID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]domain.Holding, 0, len(ids))
	for _, id := range ids {
		out = append(out, *m.ByID[id])
	}
	return out, int64(len(out)), nil
}

// ListMaturedWealth 实现接口。
func (m *MockHoldingRepo) ListMaturedWealth(_ context.Context, _ time.Time) ([]domain.Holding, error) {
	if m.MaturedListErr != nil {
		return nil, m.MaturedListErr
	}
	// 拷贝一份，避免外部修改影响 mock 内部
	out := make([]domain.Holding, len(m.MaturedList))
	copy(out, m.MaturedList)
	return out, nil
}

// =====================================================================
// MockTransactionRepo
// =====================================================================

// MockTransactionRepo 交易仓储 mock。
type MockTransactionRepo struct {
	mu        sync.Mutex
	Inserts   []*domain.Transaction
	CreateErr error
	NextID    uint
}

// NewMockTransactionRepo 构造。
func NewMockTransactionRepo() *MockTransactionRepo {
	return &MockTransactionRepo{NextID: 1}
}

// Create 实现接口。
func (m *MockTransactionRepo) Create(_ context.Context, t *domain.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateErr != nil {
		return m.CreateErr
	}
	t.ID = m.NextID
	m.NextID++
	cp := *t
	m.Inserts = append(m.Inserts, &cp)
	return nil
}

// GetByID 实现接口。
func (m *MockTransactionRepo) GetByID(_ context.Context, _userID, id uint) (*domain.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.Inserts {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, repository.ErrNotFound
}

// List 实现接口（简化）。
func (m *MockTransactionRepo) List(_ context.Context, _ repository.ListOptions) ([]domain.Transaction, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.Transaction, 0, len(m.Inserts))
	for _, t := range m.Inserts {
		out = append(out, *t)
	}
	return out, int64(len(out)), nil
}

// ListByHolding 实现接口。
func (m *MockTransactionRepo) ListByHolding(_ context.Context, holdingID uint) ([]domain.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.Transaction, 0)
	for _, t := range m.Inserts {
		if t.HoldingID == holdingID {
			out = append(out, *t)
		}
	}
	return out, nil
}

// ExistsByExternalID 实现接口。
func (m *MockTransactionRepo) ExistsByExternalID(_ context.Context, userID, platformID uint, externalID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if externalID == "" {
		return false, nil
	}
	for _, t := range m.Inserts {
		if t.UserID == userID && t.PlatformID == platformID && t.ExternalID == externalID {
			return true, nil
		}
	}
	return false, nil
}

// =====================================================================
// MockAIConversationRepo —— 一体接口（CreateConv / GetConv / IncrTokens / AppendMessage 等）
// =====================================================================

// MockAIConversationRepo AI 会话与消息仓储 mock。
type MockAIConversationRepo struct {
	mu             sync.Mutex
	Convs          map[uint]*domain.AIConversation
	Messages       map[uint][]domain.AIMessage // convID → 消息列表（按 append 顺序）
	IncrTokensLog  []IncrTokensCall            // IncrTokens 调用流水
	CreateConvErr  error                       // 注入 CreateConv 错误
	AppendMsgErr   error                       // 注入 AppendMessage 错误
	GetConvErr     error                       // 注入 GetConv 错误
	nextConvID     uint
}

// IncrTokensCall 记录一次 IncrTokens 调用。
type IncrTokensCall struct {
	ConvID        uint
	DeltaMessages int
	DeltaTokens   int
}

// NewMockAIConvRepo 构造。
func NewMockAIConvRepo() *MockAIConversationRepo {
	return &MockAIConversationRepo{
		Convs:    make(map[uint]*domain.AIConversation),
		Messages: make(map[uint][]domain.AIMessage),
	}
}

// CreateConv 实现接口。
func (m *MockAIConversationRepo) CreateConv(_ context.Context, c *domain.AIConversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateConvErr != nil {
		return m.CreateConvErr
	}
	m.nextConvID++
	c.ID = m.nextConvID
	m.Convs[c.ID] = c
	return nil
}

// GetConv 实现接口。
func (m *MockAIConversationRepo) GetConv(_ context.Context, id uint) (*domain.AIConversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.GetConvErr != nil {
		return nil, m.GetConvErr
	}
	c, ok := m.Convs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return c, nil
}

// UpdateConv 实现接口。
func (m *MockAIConversationRepo) UpdateConv(_ context.Context, c *domain.AIConversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Convs[c.ID]; !ok {
		return repository.ErrNotFound
	}
	m.Convs[c.ID] = c
	return nil
}

// IncrTokens 实现接口。
func (m *MockAIConversationRepo) IncrTokens(_ context.Context, id uint, deltaMessages, deltaTokens int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.Convs[id]; ok {
		c.MessageCount += deltaMessages
		c.TotalTokens += deltaTokens
	}
	m.IncrTokensLog = append(m.IncrTokensLog, IncrTokensCall{
		ConvID: id, DeltaMessages: deltaMessages, DeltaTokens: deltaTokens,
	})
	return nil
}

// DeleteConv 实现接口（软删，简化为直接 delete map）。
func (m *MockAIConversationRepo) DeleteConv(_ context.Context, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Convs, id)
	delete(m.Messages, id)
	return nil
}

// ListConversations 实现接口。
func (m *MockAIConversationRepo) ListConversations(_ context.Context, userID uint, _ repository.ListOptions) ([]domain.AIConversation, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.AIConversation, 0)
	for _, c := range m.Convs {
		if c.UserID == userID {
			out = append(out, *c)
		}
	}
	return out, int64(len(out)), nil
}

// AppendMessage 实现接口。
func (m *MockAIConversationRepo) AppendMessage(_ context.Context, msg *domain.AIMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AppendMsgErr != nil {
		return m.AppendMsgErr
	}
	cp := *msg
	cp.ID = uint(len(m.Messages[msg.ConversationID]) + 1)
	m.Messages[msg.ConversationID] = append(m.Messages[msg.ConversationID], cp)
	return nil
}

// ListMessages 实现接口。
func (m *MockAIConversationRepo) ListMessages(_ context.Context, convID uint, limit int) ([]domain.AIMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := m.Messages[convID]
	if limit > 0 && len(msgs) > limit {
		// 取最近 limit 条
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]domain.AIMessage, len(msgs))
	copy(out, msgs)
	return out, nil
}

// MessagesByRole 测试辅助：按 role 过滤指定会话的消息。
func (m *MockAIConversationRepo) MessagesByRole(convID uint, role string) []domain.AIMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.AIMessage, 0)
	for _, msg := range m.Messages[convID] {
		if msg.Role == role {
			out = append(out, msg)
		}
	}
	return out
}
