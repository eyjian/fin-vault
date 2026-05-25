package session_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	sqlitedrv "github.com/glebarez/sqlite"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
)

// newCleanupTestDB 与 sqlite_store_test 中 newTestDB 等价（独立文件不能跨包复用
// helper），为每个 cleanup 测试构造一个独立的 in-memory SQLite DB。
func newCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlitedrv.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")
	require.NoError(t, db.AutoMigrate(
		&domain.Session{},
		&domain.Message{},
		&domain.AgentStep{},
	), "automigrate ai tables")
	return db
}

// makeFatPayload 构造一个体积可控的 JSON payload（json.RawMessage），方便测试估算放大。
//
// 比如 size=1024 大致返回 1KB JSON，可让 PRAGMA page_count 显著增长。
func makeFatPayload(size int) json.RawMessage {
	// 用 strings.Repeat 直接拼一个固定模式的字符串，避免 sql 驱动对超长字符串
	// 转码消耗影响测试速度。
	body := strings.Repeat("a", size)
	raw, _ := json.Marshal(map[string]string{"data": body, "k": "v"})
	return raw
}

// insertAgentStep 直接 GORM Create 一条 step（绕开 sqliteStore.AppendStep 的掩码逻辑，
// 让测试可控写入预期 payload；mask 行为另有 mask_test.go 覆盖）。
func insertAgentStep(t *testing.T, db *gorm.DB, sessionID, messageID string, createdAt time.Time, payload json.RawMessage) {
	t.Helper()
	step := &domain.AgentStep{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		MessageID: messageID,
		EventType: "tool_call_finished",
		ToolName:  "search_fund",
		Payload:   payload,
		CreatedAt: createdAt,
	}
	require.NoError(t, db.Create(step).Error)
}

// =====================================================================
// EstimateStepsSize 真实实现（§4.4）测试
// =====================================================================

// TestSQLiteStore_EstimateStepsSize_ReturnsPositive 干跑空库 + 写入若干 step 后，
// 估算应当 > 0 且写入后 ≥ 写入前。
func TestSQLiteStore_EstimateStepsSize_ReturnsPositive(t *testing.T) {
	db := newCleanupTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	// 空库估算：page_count × page_size 至少有 schema page，> 0
	sizeEmpty, err := store.EstimateStepsSize(ctx)
	require.NoError(t, err)
	assert.Greater(t, sizeEmpty, int64(0), "空库估算 > 0（至少含 schema page）")

	// 写 200 条带 1KB payload 的 step
	sessionID := uuid.NewString()
	messageID := uuid.NewString()
	now := time.Now()
	for i := 0; i < 200; i++ {
		insertAgentStep(t, db, sessionID, messageID, now.Add(time.Duration(i)*time.Millisecond), makeFatPayload(1024))
	}

	sizeAfter, err := store.EstimateStepsSize(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sizeAfter, sizeEmpty, "写入后估算 ≥ 写入前")
}

// =====================================================================
// CleanupSteps 边界与触发场景（§4.5）
// =====================================================================

// TestCleanupSteps_MaxBytesZero 验证 maxBytes=0 时立即返回 (0, nil)，且不删任何 step。
//
// 对应 spec ai-session "Scenario: 配置为 0 表示不清理"。
func TestCleanupSteps_MaxBytesZero(t *testing.T) {
	db := newCleanupTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	// 写 100 条 step
	sessionID := uuid.NewString()
	messageID := uuid.NewString()
	now := time.Now()
	for i := 0; i < 100; i++ {
		insertAgentStep(t, db, sessionID, messageID, now.Add(time.Duration(i)*time.Millisecond), json.RawMessage(`{}`))
	}

	deleted, err := session.CleanupSteps(ctx, db, store, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted, "maxBytes=0 时不删任何 step")

	// 表里仍应有 100 行
	var count int64
	require.NoError(t, db.Model(&domain.AgentStep{}).Count(&count).Error)
	assert.EqualValues(t, 100, count, "表仍有 100 条 step")
}

// TestCleanupSteps_NegativeMaxBytes 验证 maxBytes<0（防御）也立即返回 (0, nil)，
// 不进入循环。
func TestCleanupSteps_NegativeMaxBytes(t *testing.T) {
	db := newCleanupTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	// 写 10 条 step
	sessionID := uuid.NewString()
	messageID := uuid.NewString()
	now := time.Now()
	for i := 0; i < 10; i++ {
		insertAgentStep(t, db, sessionID, messageID, now.Add(time.Duration(i)*time.Millisecond), json.RawMessage(`{}`))
	}

	deleted, err := session.CleanupSteps(ctx, db, store, -1)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted, "maxBytes<0 视为非法，store 层兜底跳过")
}

// TestCleanupSteps_TriggerDeletion 写入大量 step 让 PRAGMA 估算明显超阈值，
// 调 CleanupSteps 后期望删除若干最旧 step，剩下的是较新的。
func TestCleanupSteps_TriggerDeletion(t *testing.T) {
	db := newCleanupTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	// 写 1500 条 step，每条 payload ~ 1KB，让整库估算明显增长
	sessionID := uuid.NewString()
	messageID := uuid.NewString()
	base := time.Now().Add(-1500 * time.Millisecond)
	for i := 0; i < 1500; i++ {
		insertAgentStep(t, db, sessionID, messageID, base.Add(time.Duration(i)*time.Millisecond), makeFatPayload(1024))
	}

	sizeBefore, err := store.EstimateStepsSize(ctx)
	require.NoError(t, err)
	require.Greater(t, sizeBefore, int64(0))

	// 选一个明确小于 sizeBefore 的阈值（一半），让 cleanup 一定触发
	threshold := sizeBefore / 2
	t.Logf("sizeBefore=%d threshold=%d", sizeBefore, threshold)

	// 记录最旧一条 step 的 ID（为了断言它被删了）
	var oldestStep domain.AgentStep
	require.NoError(t, db.Model(&domain.AgentStep{}).
		Order("f_created_at ASC").
		First(&oldestStep).Error)

	deleted, err := session.CleanupSteps(ctx, db, store, threshold)
	require.NoError(t, err)
	t.Logf("deleted=%d", deleted)
	assert.Greater(t, deleted, int64(0), "应当删除若干 step")

	// 校验最旧 step 已被删除
	var found domain.AgentStep
	err = db.Where("f_id = ?", oldestStep.ID).First(&found).Error
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "最旧 step 应已被删除")

	// 校验 sessions/messages 不受影响（spec "清理不影响用户消息"）
	// 这里没有写 messages/sessions，所以表是空的；构造一条 message 验证：
	require.NoError(t, db.Create(&domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "user",
		Content:   "marker",
		CreatedAt: time.Now(),
	}).Error)
	// 再触发一次 cleanup，message 仍应存在
	_, err = session.CleanupSteps(ctx, db, store, threshold)
	require.NoError(t, err)
	var msgCount int64
	require.NoError(t, db.Model(&domain.Message{}).Count(&msgCount).Error)
	assert.EqualValues(t, 1, msgCount, "messages 表不受 step 清理影响")
}

// TestCleanupSteps_NoDeletionWhenUnderThreshold 验证当估算 ≤ maxBytes 时一行不删。
func TestCleanupSteps_NoDeletionWhenUnderThreshold(t *testing.T) {
	db := newCleanupTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	// 不写 step，仅依靠 schema pages，size 应当较小
	sizeNow, err := store.EstimateStepsSize(ctx)
	require.NoError(t, err)
	// 阈值取 size×10，远超当前估算
	threshold := sizeNow * 10

	deleted, err := session.CleanupSteps(ctx, db, store, threshold)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted, "estimate ≤ maxBytes 时一行不删")
}
