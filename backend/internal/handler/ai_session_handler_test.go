package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// fakeStore —— 仅供本 handler 测试，in-memory 实现 session.SessionStore
// =====================================================================
//
// service 包的 fakeAISessionStore 是 unexported，handler 包不能复用，所以这里
// 重写一份最小实现，覆盖本测试用到的所有方法（CRUD + ListSessions 分页排序 +
// AppendMessage + ListMessages 升序）。
type fakeStore struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session
	messages []domain.Message
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions: make(map[string]*domain.Session),
		messages: []domain.Message{},
	}
}

func (f *fakeStore) CreateSession(_ context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.sessions[s.ID] = &cp
	return nil
}

func (f *fakeStore) GetSession(_ context.Context, sessionID string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[sessionID]
	if !ok {
		return nil, errs.ErrAISessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeStore) UpdateSession(_ context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.sessions[s.ID]; !ok {
		return errs.ErrAISessionNotFound
	}
	cp := *s
	f.sessions[s.ID] = &cp
	return nil
}

func (f *fakeStore) DeleteSession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.sessions[sessionID]; !ok {
		return errs.ErrAISessionNotFound
	}
	delete(f.sessions, sessionID)
	// 级联清理 messages（与真 store 事务级联等价语义）
	out := f.messages[:0]
	for _, m := range f.messages {
		if m.SessionID != sessionID {
			out = append(out, m)
		}
	}
	f.messages = out
	return nil
}

func (f *fakeStore) ListSessions(_ context.Context, opts session.ListSessionsOptions) ([]domain.Session, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]domain.Session, 0, len(f.sessions))
	for _, s := range f.sessions {
		if s.UserID == opts.UserID {
			all = append(all, *s)
		}
	}
	// updated_at DESC，与 store 真实实现保持一致；同时间用 ID 作 tiebreaker 保证确定性
	sort.Slice(all, func(i, j int) bool {
		if all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	total := int64(len(all))

	page := opts.Page
	if page <= 0 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	if offset >= len(all) {
		return []domain.Session{}, total, nil
	}
	end := offset + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (f *fakeStore) ListMessages(_ context.Context, sessionID string, limit int) ([]domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Message{}
	for _, m := range f.messages {
		if m.SessionID == sessionID {
			out = append(out, m)
		}
	}
	// 按 CreatedAt 升序（与 spec §72 时间线展示一致）
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (f *fakeStore) AppendMessage(_ context.Context, m *domain.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, *m)
	return nil
}

func (f *fakeStore) AppendStep(_ context.Context, _ *domain.AgentStep) error { return nil }
func (f *fakeStore) EstimateStepsSize(_ context.Context) (int64, error)      { return 0, nil }

var _ session.SessionStore = (*fakeStore)(nil)

// =====================================================================
// 测试基础设施
// =====================================================================

// setupRouter 装配 gin engine + handler，返回 (router, store) 便于测试灌数据。
func setupRouter(t *testing.T) (*gin.Engine, *fakeStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	sessSvc := service.NewAISessionService(store)
	h := NewAISessionHandler(sessSvc)

	r := gin.New()
	v1 := r.Group("/api/v1")
	h.Register(v1)
	return r, store
}

// doReq 发起请求，返回 ResponseRecorder。userID="" 表示不设置 X-User-Id。
func doReq(r *gin.Engine, method, url, userID string, body interface{}) *httptest.ResponseRecorder {
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

// parseBody 把 ResponseRecorder 的 body 解到 response.Body 容器。
func parseBody(t *testing.T, w *httptest.ResponseRecorder) response.Body {
	t.Helper()
	var b response.Body
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &b))
	return b
}

// dataAs 把 response.Body.Data 重新解到 dst（json round-trip）。
func dataAs(t *testing.T, body response.Body, dst interface{}) {
	t.Helper()
	raw, err := json.Marshal(body.Data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, dst))
}

// seedSession 直接通过 store 灌一个 session（绕过 handler，用于跨用户测试）。
func seedSession(t *testing.T, store *fakeStore, userID uint, title string) *domain.Session {
	t.Helper()
	now := time.Now()
	s := &domain.Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, store.CreateSession(context.Background(), s))
	return s
}

// =====================================================================
// 路由测试 —— Create
// =====================================================================

// TestCreate_Success 验证 201 + session_id 36 位 UUID + title。
func TestCreate_Success(t *testing.T) {
	r, _ := setupRouter(t)
	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions", "1", SessionCreateReq{Title: "  hello  "})
	require.Equal(t, http.StatusCreated, w.Code, "POST 创建应返 201")

	body := parseBody(t, w)
	assert.Equal(t, 0, body.Code)
	var resp SessionCreateResp
	dataAs(t, body, &resp)
	assert.Len(t, resp.SessionID, 36, "session_id 应为 UUID 36 位")
	assert.Equal(t, "hello", resp.Title, "title 应被 TrimSpace（service 层行为）")
	assert.False(t, resp.CreatedAt.IsZero())
}

// TestCreate_EmptyBody_OK 验证 body 为空也能创建成功（spec §9 "请求体可为空"）。
func TestCreate_EmptyBody_OK(t *testing.T) {
	r, _ := setupRouter(t)
	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions", "1", nil)
	require.Equal(t, http.StatusCreated, w.Code, "空 body 应仍可创建")

	body := parseBody(t, w)
	var resp SessionCreateResp
	dataAs(t, body, &resp)
	assert.Equal(t, "", resp.Title, "默认 title 为空串")
}

// TestCreate_Missing_X_User_Id_Returns401 D15 红线。
func TestCreate_Missing_X_User_Id_Returns401(t *testing.T) {
	r, _ := setupRouter(t)
	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions", "", SessionCreateReq{Title: "x"})
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"D15: 缺失 X-User-Id 必须返 401，绝不走 fallback=1")
}

// TestCreate_Invalid_X_User_Id_Returns401 "0"/"abc" 都视为非法。
func TestCreate_Invalid_X_User_Id_Returns401(t *testing.T) {
	cases := []string{"0", "abc", "-1"}
	for _, uid := range cases {
		t.Run(fmt.Sprintf("uid=%q", uid), func(t *testing.T) {
			r, _ := setupRouter(t)
			w := doReq(r, http.MethodPost, "/api/v1/ai/sessions", uid, SessionCreateReq{Title: "x"})
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"D15: 非法 X-User-Id (%q) 必须返 401", uid)
		})
	}
}

// =====================================================================
// 路由测试 —— List
// =====================================================================

// TestList_Pagination spec §29-32：25 条，page=2&page_size=20 返 5 条 + total=25。
func TestList_Pagination(t *testing.T) {
	r, store := setupRouter(t)
	// 灌 25 条，UpdatedAt 按递增顺序，确保分页排序确定（DESC：i=24 最新）
	base := time.Now()
	for i := 0; i < 25; i++ {
		s := &domain.Session{
			ID:        uuid.NewString(),
			UserID:    1,
			Title:     fmt.Sprintf("s%d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, store.CreateSession(context.Background(), s))
	}
	w := doReq(r, http.MethodGet, "/api/v1/ai/sessions?page=2&page_size=20", "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	var page response.PageData
	dataAs(t, body, &page)
	assert.EqualValues(t, 25, page.Total, "total 必须 25")
	assert.Equal(t, 2, page.Page)
	assert.Equal(t, 20, page.Size)

	var list []domain.Session
	listRaw, _ := json.Marshal(page.List)
	require.NoError(t, json.Unmarshal(listRaw, &list))
	assert.Len(t, list, 5, "page=2 应剩 5 条（25 - 20）")
}

// TestList_OnlyOwnSessions spec §23-27：用户隔离。
func TestList_OnlyOwnSessions(t *testing.T) {
	r, store := setupRouter(t)
	seedSession(t, store, 1, "a1")
	seedSession(t, store, 1, "a2")
	seedSession(t, store, 2, "b1")

	w := doReq(r, http.MethodGet, "/api/v1/ai/sessions", "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	var page response.PageData
	dataAs(t, body, &page)
	assert.EqualValues(t, 2, page.Total, "user=1 只应看到自己的 2 条")

	var list []domain.Session
	listRaw, _ := json.Marshal(page.List)
	require.NoError(t, json.Unmarshal(listRaw, &list))
	for _, s := range list {
		assert.Equal(t, uint(1), s.UserID, "list 必须只含 user=1 的 session")
	}
}

// =====================================================================
// 路由测试 —— Get
// =====================================================================

// TestGet_Success
func TestGet_Success(t *testing.T) {
	r, store := setupRouter(t)
	sess := seedSession(t, store, 1, "mine")

	w := doReq(r, http.MethodGet, "/api/v1/ai/sessions/"+sess.ID, "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	var got domain.Session
	dataAs(t, body, &got)
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, "mine", got.Title)
}

// TestGet_OtherUser_Returns404_NotExposing spec §44-47：跨用户必须 404，不暴露存在性。
func TestGet_OtherUser_Returns404_NotExposing(t *testing.T) {
	r, store := setupRouter(t)
	sessA := seedSession(t, store, 1, "secret")

	// 用户 2 试图读用户 1 的 session
	w := doReq(r, http.MethodGet, "/api/v1/ai/sessions/"+sessA.ID, "2", nil)
	assert.Equal(t, http.StatusNotFound, w.Code,
		"D2: 跨用户 Get 必须返 404 不暴露存在性，绝不返 403")

	body := parseBody(t, w)
	assert.Equal(t, errs.ErrAISessionNotFound.Code, body.Code,
		"业务错误码必须是 50001 ErrAISessionNotFound")
}

// =====================================================================
// 路由测试 —— Update
// =====================================================================

// TestUpdate_Title 验证 title 更新成功 + 返回完整 session。
func TestUpdate_Title(t *testing.T) {
	r, store := setupRouter(t)
	sess := seedSession(t, store, 1, "old")

	newTitle := "  brand-new  "
	w := doReq(r, http.MethodPut, "/api/v1/ai/sessions/"+sess.ID, "1",
		SessionUpdateReq{Title: &newTitle})
	require.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	var got domain.Session
	dataAs(t, body, &got)
	assert.Equal(t, "brand-new", got.Title, "title 应被 TrimSpace（service 行为）")
}

// TestUpdate_OtherUser_Returns404 跨用户更新 → 404。
func TestUpdate_OtherUser_Returns404(t *testing.T) {
	r, store := setupRouter(t)
	sessA := seedSession(t, store, 1, "a")

	hijack := "hijacked"
	w := doReq(r, http.MethodPut, "/api/v1/ai/sessions/"+sessA.ID, "2",
		SessionUpdateReq{Title: &hijack})
	assert.Equal(t, http.StatusNotFound, w.Code, "D2: 跨用户 Update 必须 404")

	// 受害人 title 未被改
	got, err := store.GetSession(context.Background(), sessA.ID)
	require.NoError(t, err)
	assert.Equal(t, "a", got.Title, "误改保护：跨用户 Update 不应改动")
}

// =====================================================================
// 路由测试 —— Delete
// =====================================================================

// TestDelete_Success_Returns204 spec §38-42 强制 204 No Content。
func TestDelete_Success_Returns204(t *testing.T) {
	r, store := setupRouter(t)
	sess := seedSession(t, store, 1, "del-me")

	w := doReq(r, http.MethodDelete, "/api/v1/ai/sessions/"+sess.ID, "1", nil)
	assert.Equal(t, http.StatusNoContent, w.Code,
		"spec §38-42: DELETE 自有会话强制 204 No Content")
	assert.Equal(t, 0, w.Body.Len(), "204 响应体必须为空")

	_, err := store.GetSession(context.Background(), sess.ID)
	assert.ErrorIs(t, err, errs.ErrAISessionNotFound, "session 应被真删")
}

// TestDelete_OtherUser_Returns404 跨用户删除 → 404，且受害人 session 仍存在。
func TestDelete_OtherUser_Returns404(t *testing.T) {
	r, store := setupRouter(t)
	sessA := seedSession(t, store, 1, "victim")

	w := doReq(r, http.MethodDelete, "/api/v1/ai/sessions/"+sessA.ID, "2", nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "D2: 跨用户 Delete 必须 404")

	// 受害人 session 仍存在
	got, err := store.GetSession(context.Background(), sessA.ID)
	require.NoError(t, err)
	assert.Equal(t, sessA.ID, got.ID, "误删保护：跨用户 Delete 不应真删")
}

// =====================================================================
// 路由测试 —— ListMessages
// =====================================================================

// TestListMessages_Success 验证升序 + 仅 user/assistant + token_usage 透传。
func TestListMessages_Success(t *testing.T) {
	r, store := setupRouter(t)
	sess := seedSession(t, store, 1, "sess")

	now := time.Now()
	usage := json.RawMessage(`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`)
	require.NoError(t, store.AppendMessage(context.Background(), &domain.Message{
		ID: uuid.NewString(), SessionID: sess.ID, Role: "user", Content: "hi", CreatedAt: now,
	}))
	require.NoError(t, store.AppendMessage(context.Background(), &domain.Message{
		ID: uuid.NewString(), SessionID: sess.ID, Role: "assistant", Content: "hello",
		TokenUsage: usage, CreatedAt: now.Add(time.Second),
	}))
	// 灌一条 tool 消息验证被过滤
	require.NoError(t, store.AppendMessage(context.Background(), &domain.Message{
		ID: uuid.NewString(), SessionID: sess.ID, Role: "tool", Content: "tool-internal",
		CreatedAt: now.Add(2 * time.Second),
	}))

	w := doReq(r, http.MethodGet, "/api/v1/ai/sessions/"+sess.ID+"/messages", "1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	var msgs []MessageDTO
	dataAs(t, body, &msgs)
	require.Len(t, msgs, 2, "spec §73: 仅 user/assistant，tool 必须被过滤")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "hi", msgs[0].Content)
	assert.True(t, msgs[0].CreatedAt.Before(msgs[1].CreatedAt), "升序")
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "hello", msgs[1].Content)
	// token_usage 透传：assistant 有 + user 无
	assert.Empty(t, msgs[0].TokenUsage, "user 消息无 token_usage")
	assert.JSONEq(t, string(usage), string(msgs[1].TokenUsage),
		"assistant token_usage 必须按原 raw JSON 透传")
}

// TestListMessages_OtherUser_Returns404 跨用户读消息 → 404。
func TestListMessages_OtherUser_Returns404(t *testing.T) {
	r, store := setupRouter(t)
	sessA := seedSession(t, store, 1, "a")
	require.NoError(t, store.AppendMessage(context.Background(), &domain.Message{
		ID: uuid.NewString(), SessionID: sessA.ID, Role: "user", Content: "leak-me", CreatedAt: time.Now(),
	}))

	w := doReq(r, http.MethodGet, "/api/v1/ai/sessions/"+sessA.ID+"/messages", "2", nil)
	assert.Equal(t, http.StatusNotFound, w.Code,
		"D2: 跨用户 ListMessages 必须 404，绝不返 403")

	body := parseBody(t, w)
	assert.Equal(t, errs.ErrAISessionNotFound.Code, body.Code)
	assert.NotContains(t, w.Body.String(), "leak-me", "失败响应不应泄漏内容")
}
