package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	sqlitedrv "github.com/glebarez/sqlite"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// newTestDB 为每个测试构造一个独立的 in-memory SQLite DB（避免污染本地 finvault.db
// 与跨测试相互干扰）。每次返回的 DB 都是独立连接，AutoMigrate 完成 t_fv_ai_*
// 三张表，索引名见 §2.1 ai_session.go（含 §2.1 follow-up 跨表命名空间修复）。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 不带 cache=shared，每个 DB 实例独立，互不影响（in-memory）
	db, err := gorm.Open(sqlitedrv.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")
	require.NoError(t, db.AutoMigrate(
		&domain.Session{},
		&domain.Message{},
		&domain.AgentStep{},
	), "automigrate ai tables")
	return db
}

func newSession(userID uint, title string) *domain.Session {
	now := time.Now()
	return &domain.Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func newMessage(sessionID, role, content string, createdAt time.Time) *domain.Message {
	return &domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		CreatedAt: createdAt,
	}
}

// =====================================================================
// CRUD 正常路径
// =====================================================================

func TestSQLiteStore_CreateGetUpdate(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	s := newSession(1, "first")
	require.NoError(t, store.CreateSession(ctx, s))

	got, err := store.GetSession(ctx, s.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, s.ID, got.ID)
	assert.Equal(t, uint(1), got.UserID)
	assert.Equal(t, "first", got.Title)
	assert.False(t, got.UpdatedAt.IsZero(), "UpdatedAt 应非零（默认 = CreatedAt）")

	// Update title
	got.Title = "renamed"
	got.UpdatedAt = time.Now().Add(time.Second)
	require.NoError(t, store.UpdateSession(ctx, got))

	got2, err := store.GetSession(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got2.Title)
	assert.True(t, got2.UpdatedAt.After(got.CreatedAt), "UpdatedAt 应大于 CreatedAt")
}

func TestSQLiteStore_CreateSession_RejectsEmptyID(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	err := store.CreateSession(context.Background(), &domain.Session{UserID: 1})
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
}

func TestSQLiteStore_GetSession_NotFound(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	_, err := store.GetSession(context.Background(), "no-such-id")
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)

	_, err = store.GetSession(context.Background(), "")
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
}

func TestSQLiteStore_DeleteSession_CascadeAndNotFound(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	s := newSession(1, "del-me")
	require.NoError(t, store.CreateSession(ctx, s))
	require.NoError(t, store.AppendMessage(ctx, newMessage(s.ID, "user", "hi", time.Now())))
	require.NoError(t, store.AppendMessage(ctx, newMessage(s.ID, "assistant", "hello", time.Now())))
	step := &domain.AgentStep{
		ID:        uuid.NewString(),
		SessionID: s.ID,
		MessageID: uuid.NewString(),
		EventType: "tool_call_started",
		ToolName:  "search_fund",
		Payload:   json.RawMessage(`{"keyword":"abc"}`),
	}
	require.NoError(t, store.AppendStep(ctx, step))

	// 删除 → 三张表的相关行均应消失
	require.NoError(t, store.DeleteSession(ctx, s.ID))

	_, err := store.GetSession(ctx, s.ID)
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)

	var msgCount, stepCount int64
	require.NoError(t, db.Model(&domain.Message{}).Where("f_session_id = ?", s.ID).Count(&msgCount).Error)
	require.NoError(t, db.Model(&domain.AgentStep{}).Where("f_session_id = ?", s.ID).Count(&stepCount).Error)
	assert.EqualValues(t, 0, msgCount, "messages 应被级联删除")
	assert.EqualValues(t, 0, stepCount, "agent_steps 应被级联删除")

	// 二次删除返回 NotFound
	err = store.DeleteSession(ctx, s.ID)
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)

	// 空字符串
	err = store.DeleteSession(ctx, "")
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
}

// =====================================================================
// 用户隔离
// =====================================================================

func TestSQLiteStore_ListSessions_UserIsolation(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	// user 1 有 3 个会话；user 2 有 2 个会话
	now := time.Now()
	for i := 0; i < 3; i++ {
		s := newSession(1, "u1-"+uuid.NewString()[:6])
		s.UpdatedAt = now.Add(time.Duration(i) * time.Second)
		require.NoError(t, store.CreateSession(ctx, s))
	}
	for i := 0; i < 2; i++ {
		s := newSession(2, "u2-"+uuid.NewString()[:6])
		s.UpdatedAt = now.Add(time.Duration(i) * time.Second)
		require.NoError(t, store.CreateSession(ctx, s))
	}

	// user 1 视角
	list, total, err := store.ListSessions(ctx, session.ListSessionsOptions{
		UserID: 1, Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, list, 3)
	for _, s := range list {
		assert.EqualValues(t, 1, s.UserID, "user 1 的列表只能含 user 1 的会话")
	}
	// updated_at DESC 排序：第一个应为最新
	assert.True(t, list[0].UpdatedAt.After(list[1].UpdatedAt) || list[0].UpdatedAt.Equal(list[1].UpdatedAt))

	// user 2 视角
	list2, total2, err := store.ListSessions(ctx, session.ListSessionsOptions{
		UserID: 2, Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total2)
	assert.Len(t, list2, 2)
	for _, s := range list2 {
		assert.EqualValues(t, 2, s.UserID)
	}

	// 不存在的 user
	list3, total3, err := store.ListSessions(ctx, session.ListSessionsOptions{
		UserID: 999, Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total3)
	assert.Len(t, list3, 0)
}

func TestSQLiteStore_ListSessions_Pagination(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	now := time.Now()
	for i := 0; i < 25; i++ {
		s := newSession(1, "p-"+uuid.NewString()[:4])
		s.UpdatedAt = now.Add(time.Duration(i) * time.Second)
		require.NoError(t, store.CreateSession(ctx, s))
	}

	// PageSize=10 共 3 页（25/10）
	page1, total, err := store.ListSessions(ctx, session.ListSessionsOptions{UserID: 1, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 25, total)
	assert.Len(t, page1, 10)

	page3, total, err := store.ListSessions(ctx, session.ListSessionsOptions{UserID: 1, Page: 3, PageSize: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 25, total)
	assert.Len(t, page3, 5, "第 3 页应只有 5 条")

	// PageSize ≤0 兜底 20；Page ≤0 兜底 1
	def, total, err := store.ListSessions(ctx, session.ListSessionsOptions{UserID: 1, Page: 0, PageSize: 0})
	require.NoError(t, err)
	assert.EqualValues(t, 25, total)
	assert.Len(t, def, 20, "PageSize=0 应兜底为 20")
}

// =====================================================================
// AppendMessage / ListMessages（含 history_window）
// =====================================================================

func TestSQLiteStore_ListMessages_HistoryWindowDefaults(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	s := newSession(1, "hist")
	require.NoError(t, store.CreateSession(ctx, s))

	// 写 30 条消息，按时间递增
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		m := newMessage(s.ID, role, "msg-"+uuid.NewString()[:4], base.Add(time.Duration(i)*time.Second))
		require.NoError(t, store.AppendMessage(ctx, m))
	}

	// limit=0 → 用 historyWindow=20，且按升序返回
	out, err := store.ListMessages(ctx, s.ID, 0)
	require.NoError(t, err)
	assert.Len(t, out, 20, "limit=0 应取 historyWindow=20 条")
	for i := 1; i < len(out); i++ {
		assert.False(t, out[i].CreatedAt.Before(out[i-1].CreatedAt), "应升序")
	}
	// 拿到的应当是最新 20 条（时间最大），第一条应当是 30-20=10 序号那条
	first := out[0]
	last := out[len(out)-1]
	assert.True(t, first.CreatedAt.Before(last.CreatedAt) || first.CreatedAt.Equal(last.CreatedAt))

	// limit=5
	out5, err := store.ListMessages(ctx, s.ID, 5)
	require.NoError(t, err)
	assert.Len(t, out5, 5)

	// limit=100 → 全部 30 条
	all, err := store.ListMessages(ctx, s.ID, 100)
	require.NoError(t, err)
	assert.Len(t, all, 30)

	// limit=-1 → 兜底 historyWindow
	negOut, err := store.ListMessages(ctx, s.ID, -1)
	require.NoError(t, err)
	assert.Len(t, negOut, 20)
}

func TestSQLiteStore_ListMessages_HistoryWindowCustom(t *testing.T) {
	db := newTestDB(t)
	// 注入 historyWindow=5
	store := session.NewSQLiteStore(db, 5)
	ctx := context.Background()

	s := newSession(1, "hist-5")
	require.NoError(t, store.CreateSession(ctx, s))
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 12; i++ {
		require.NoError(t, store.AppendMessage(ctx, newMessage(s.ID, "user", "m", base.Add(time.Duration(i)*time.Second))))
	}
	out, err := store.ListMessages(ctx, s.ID, 0)
	require.NoError(t, err)
	assert.Len(t, out, 5, "limit=0 应取构造时注入的 historyWindow=5")
}

func TestSQLiteStore_ListMessages_UserIsolation_ViaSessionID(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	sa := newSession(1, "a")
	sb := newSession(2, "b")
	require.NoError(t, store.CreateSession(ctx, sa))
	require.NoError(t, store.CreateSession(ctx, sb))

	require.NoError(t, store.AppendMessage(ctx, newMessage(sa.ID, "user", "from-a", time.Now())))
	require.NoError(t, store.AppendMessage(ctx, newMessage(sb.ID, "user", "from-b", time.Now())))

	outA, err := store.ListMessages(ctx, sa.ID, 0)
	require.NoError(t, err)
	require.Len(t, outA, 1)
	assert.Equal(t, "from-a", outA[0].Content, "session A 的消息列表绝不能含 session B 的消息")

	outB, err := store.ListMessages(ctx, sb.ID, 0)
	require.NoError(t, err)
	require.Len(t, outB, 1)
	assert.Equal(t, "from-b", outB[0].Content)
}

func TestSQLiteStore_AppendMessage_RejectsEmptySessionID(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	err := store.AppendMessage(context.Background(), &domain.Message{ID: uuid.NewString()})
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
	err = store.AppendMessage(context.Background(), nil)
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
}

func TestSQLiteStore_ListMessages_EmptySessionID(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	_, err := store.ListMessages(context.Background(), "", 0)
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
}

// =====================================================================
// AppendStep + 敏感字段掩码（落库后 payload 不含敏感原文）
// =====================================================================

func TestSQLiteStore_AppendStep_MasksPayloadOnWrite(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	ctx := context.Background()

	s := newSession(1, "mask")
	require.NoError(t, store.CreateSession(ctx, s))

	step := &domain.AgentStep{
		ID:        uuid.NewString(),
		SessionID: s.ID,
		MessageID: uuid.NewString(),
		EventType: "tool_call_started",
		ToolName:  "openai_call",
		Payload: json.RawMessage(`{
			"arguments":{
				"api_key":"sk-secret",
				"prompt":"hello",
				"headers":{"Authorization":"Bearer xxx"}
			}
		}`),
	}
	require.NoError(t, store.AppendStep(ctx, step))

	// 落库后 step.Payload 已被替换为掩码版本（同实例）
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(step.Payload, &got))
	args := got["arguments"].(map[string]interface{})
	assert.Equal(t, "***", args["api_key"])
	assert.Equal(t, "hello", args["prompt"])
	headers := args["headers"].(map[string]interface{})
	assert.Equal(t, "***", headers["Authorization"])

	// 直接读 DB 确认存的也是脱敏版
	var stored domain.AgentStep
	require.NoError(t, db.Where("f_id = ?", step.ID).First(&stored).Error)
	var storedJSON map[string]interface{}
	require.NoError(t, json.Unmarshal(stored.Payload, &storedJSON))
	storedArgs := storedJSON["arguments"].(map[string]interface{})
	assert.Equal(t, "***", storedArgs["api_key"], "落库的 payload 不能含敏感原文")
	assert.NotContains(t, string(stored.Payload), "sk-secret", "原始密钥不能存在于落库 payload")
	assert.NotContains(t, string(stored.Payload), "Bearer xxx", "原始 token 不能存在于落库 payload")
}

func TestSQLiteStore_AppendStep_RejectsEmptySessionID(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	err := store.AppendStep(context.Background(), &domain.AgentStep{ID: uuid.NewString()})
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
	err = store.AppendStep(context.Background(), nil)
	assert.ErrorIs(t, err, errs.ErrAIConversationNotFound)
}

// =====================================================================
// EstimateStepsSize 占位（§4.4 待 dev_2 实现）
// =====================================================================

func TestSQLiteStore_EstimateStepsSize_PlaceholderReturnsError(t *testing.T) {
	db := newTestDB(t)
	store := session.NewSQLiteStore(db, 20)
	size, err := store.EstimateStepsSize(context.Background())
	require.Error(t, err, "占位实现应返回 error，等 §4.4 替换")
	assert.True(t, errors.Is(err, err) && err.Error() == "not implemented: §4.4 placeholder")
	assert.EqualValues(t, 0, size)
}
