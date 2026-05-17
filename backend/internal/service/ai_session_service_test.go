package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/agent"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// fakeAISessionStore —— 单测用 in-memory SessionStore
// =====================================================================
//
// 仅实现 AISessionService / AIMessageService 单测涉及的方法；ListMessages /
// AppendMessage / AppendStep 在本期 service 层不直接调用（业务 Runner 内部用），
// 但需满足接口编译期断言，所以提供最小实现。
type fakeAISessionStore struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session
	createErr error
	updateErr error
	deleteErr error
}

func newFakeAISessionStore() *fakeAISessionStore {
	return &fakeAISessionStore{sessions: make(map[string]*domain.Session)}
}

func (f *fakeAISessionStore) CreateSession(_ context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	cp := *s
	f.sessions[s.ID] = &cp
	return nil
}

func (f *fakeAISessionStore) GetSession(_ context.Context, sessionID string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[sessionID]
	if !ok {
		return nil, errs.ErrAISessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeAISessionStore) UpdateSession(_ context.Context, s *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	if _, ok := f.sessions[s.ID]; !ok {
		return errs.ErrAISessionNotFound
	}
	cp := *s
	f.sessions[s.ID] = &cp
	return nil
}

func (f *fakeAISessionStore) DeleteSession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.sessions[sessionID]; !ok {
		return errs.ErrAISessionNotFound
	}
	delete(f.sessions, sessionID)
	return nil
}

func (f *fakeAISessionStore) ListSessions(_ context.Context, opts session.ListSessionsOptions) ([]domain.Session, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Session, 0, len(f.sessions))
	for _, s := range f.sessions {
		if s.UserID == opts.UserID {
			out = append(out, *s)
		}
	}
	return out, int64(len(out)), nil
}

func (f *fakeAISessionStore) ListMessages(_ context.Context, _ string, _ int) ([]domain.Message, error) {
	return nil, nil
}
func (f *fakeAISessionStore) AppendMessage(_ context.Context, _ *domain.Message) error { return nil }
func (f *fakeAISessionStore) AppendStep(_ context.Context, _ *domain.AgentStep) error  { return nil }
func (f *fakeAISessionStore) EstimateStepsSize(_ context.Context) (int64, error)        { return 0, nil }

var _ session.SessionStore = (*fakeAISessionStore)(nil)

// =====================================================================
// fakeRunner —— 捕获 ctx 内三注入值，驱动双 ctx + D14 测试
// =====================================================================

type fakeRunner struct {
	gotToolsUserID    uint
	gotToolsUserOK    bool
	gotAgentUserID    string
	gotAssistantMsgID string
	gotAssistantOK    bool
	gotSessionID      string
	gotUserMessage    string

	returnText      string
	returnToolCalls []agent.ToolCall
	returnUsage     agent.TokenUsage
	returnErr       error
}

// 编译期断言 fakeRunner 满足 agent.Runner 接口（保证签名漂移立即被检出）。
//
// 注意 agent.userIDFromCtx / assistantMessageIDFromCtx 是 unexported，外包无法直接调；
// 但 service 注入用的是 agent.WithUserID（公开）+ agent.WithAssistantMessageID（公开），
// fakeRunner 只能间接验证：通过 agent 包对外暴露的 helper 没有时，借助一个内部 trick
// —— 我们再次调一遍 With 函数取出再 ValueAsString 是不可行的；改为：在 ctx 上注入时
// agent 包用的是 *unexported* struct key，service 的 ctx 已传到 fakeRunner，所以 fakeRunner
// 只能从 ctx 用 *相同的 With/From 公开 API* 验证。tools.UserIDFromContext 是公开的（可用）；
// agent.userIDFromCtx / assistantMessageIDFromCtx 不公开，所以 service 双 ctx 注入测试
// 改为依赖"runner 看到 ctx 后的可观察行为"（fakeRunner 调 tools.UserIDFromContext 必须命中
// + agent ctx key 不可外部读取，但我们可断言注入 + 解注入往返后 tools 能读到）。
// 完整验证 agent userID 注入需走 agent 包内部的 white-box 测试（§7.5 单测覆盖），本文件
// 验证 service 这一侧：
//   1) tools.WithUserID 注入正确（fakeRunner 内 tools.UserIDFromContext 必须命中）
//   2) agent.WithAssistantMessageID 注入正确（用导出的 helper：见下方 agent.AssistantMessageIDFromContext...
//      但实际 agent 包没导出这个 helper；为不破坏 D13 风格 unexported，本测试改用
//      "Runner 自报"风格：fakeRunner 直接在 Run 入口存档 ctx，用 agent 包内单测验证 ID 字段
//      已经在 §5 covered；service 这侧测 tool ctx + sessionID 归属 + 错误透传即可）。
//
// 简化方案：fakeRunner 只用 tools.UserIDFromContext 验证一次注入，agent ctx 部分由
// agent 包 D14 单测 (TestAppendStep_LinkedToAssistantMessageID_WhenInjected) 端到端兜底。
var _ agent.Runner = (*fakeRunner)(nil)

func (f *fakeRunner) Run(ctx context.Context, sessionID, userMessage string) (string, []agent.ToolCall, agent.TokenUsage, error) {
	f.gotSessionID = sessionID
	f.gotUserMessage = userMessage
	if uid, ok := tools.UserIDFromContext(ctx); ok {
		f.gotToolsUserID = uid
		f.gotToolsUserOK = true
	}
	// agent 的 userID / assistantMessageID 是 unexported helper，外包不能直读 ctx；
	// 这里我们通过"二次注入相同 key + 取出"的等价路径不可行（key type 私有）。
	// 所以 service 单测只能验证：注入流程不 panic + Runner 收到 ctx 派发到下游正确链路。
	// agent 侧 D14 关联性已由 agent 包单测 TestAppendStep_LinkedToAssistantMessageID_WhenInjected 兜底。
	if f.returnErr != nil {
		return "", nil, agent.TokenUsage{}, f.returnErr
	}
	return f.returnText, f.returnToolCalls, f.returnUsage, nil
}

// =====================================================================
// AISessionService 测试
// =====================================================================

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

func TestAISessionService_Create_Success(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	got, err := svc.Create(context.Background(), 7, "  hello  ")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, uint(7), got.UserID)
	assert.Equal(t, "hello", got.Title, "title 应被 TrimSpace")
}

func TestAISessionService_Create_RejectsZeroUserID(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	_, err := svc.Create(context.Background(), 0, "x")
	require.Error(t, err)
	got := errs.As(err)
	require.NotNil(t, got)
	assert.Equal(t, errs.ErrInvalidParam.Code, got.Code)
}

// TestAISessionService_Get_OtherUser_Returns404
//
// 跨用户 Get → 必须返 ErrAISessionNotFound（404 不暴露存在性，spec D2 安全边界）。
func TestAISessionService_Get_OtherUser_Returns404(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	sessA, err := svc.Create(context.Background(), 1, "a")
	require.NoError(t, err)

	_, err = svc.Get(context.Background(), 2, sessA.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrAISessionNotFound,
		"跨用户 Get 必须返 404 ErrAISessionNotFound（不暴露存在性，绝不返 403/Forbidden）")
}

// TestAISessionService_Delete_OtherUser_Returns404
//
// 跨用户 Delete → 同 404 语义。spec "拒绝删除他人会话 404 不暴露存在性"。
func TestAISessionService_Delete_OtherUser_Returns404(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	sessA, err := svc.Create(context.Background(), 1, "a")
	require.NoError(t, err)

	err = svc.Delete(context.Background(), 2, sessA.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrAISessionNotFound)

	// 受害人 sessA 仍存在
	got, err := svc.Get(context.Background(), 1, sessA.ID)
	require.NoError(t, err)
	assert.Equal(t, sessA.ID, got.ID, "误删保护：跨用户 Delete 不应真删")
}

// TestAISessionService_Update_OtherUser_Returns404
func TestAISessionService_Update_OtherUser_Returns404(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	sessA, err := svc.Create(context.Background(), 1, "a")
	require.NoError(t, err)

	newTitle := "hijacked"
	err = svc.Update(context.Background(), 2, sessA.ID, SessionPatch{Title: &newTitle})
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrAISessionNotFound)

	// 受害人 sessA 标题不变
	got, err := svc.Get(context.Background(), 1, sessA.ID)
	require.NoError(t, err)
	assert.Equal(t, "a", got.Title)
}

// TestAISessionService_List_OnlyOwnSessions
//
// spec ai-session "列表只返回当前用户的会话"。
func TestAISessionService_List_OnlyOwnSessions(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	_, err := svc.Create(context.Background(), 1, "a1")
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), 1, "a2")
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), 2, "b1")
	require.NoError(t, err)

	list, total, err := svc.List(context.Background(), SessionListInput{UserID: 1})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Len(t, list, 2)
	for _, s := range list {
		assert.Equal(t, uint(1), s.UserID, "list 必须只含 user_id=1 的 session")
	}
}

func TestAISessionService_Update_TitleOnly(t *testing.T) {
	store := newFakeAISessionStore()
	svc := NewAISessionService(store)
	sess, err := svc.Create(context.Background(), 1, "old")
	require.NoError(t, err)
	newTitle := "  new  "
	require.NoError(t, svc.Update(context.Background(), 1, sess.ID, SessionPatch{Title: &newTitle}))

	got, err := svc.Get(context.Background(), 1, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "new", got.Title, "Update 应 TrimSpace")
	assert.True(t, got.UpdatedAt.After(sess.UpdatedAt) || got.UpdatedAt.Equal(sess.UpdatedAt),
		"Update 应推进 UpdatedAt")
}

// =====================================================================
// AIMessageService 测试
// =====================================================================

func TestAIMessageService_Send_RejectsForeignSession(t *testing.T) {
	store := newFakeAISessionStore()
	sessSvc := NewAISessionService(store)
	runner := &fakeRunner{returnText: "should-not-reach"}
	msgSvc := NewAIMessageService(sessSvc, runner)

	sessA, err := sessSvc.Create(context.Background(), 1, "a")
	require.NoError(t, err)

	_, err = msgSvc.Send(context.Background(), 2, sessA.ID, "hi")
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrAISessionNotFound,
		"跨用户 Send 必须返 404；防越权先于 Runner.Run")
	assert.Equal(t, "", runner.gotSessionID, "Runner.Run 不应被调用")
}

// TestAIMessageService_Send_DoubleCtxInjection
//
// service.Send 必须在调 Runner.Run 之前完成 tools.WithUserID 注入（D13）。
// agent.WithUserID + agent.WithAssistantMessageID 注入在 §7.5 单测里通过白盒方式
// 验证（agent ctxKey 是 unexported），这里只覆盖 service 边界可观察的部分。
func TestAIMessageService_Send_DoubleCtxInjection(t *testing.T) {
	store := newFakeAISessionStore()
	sessSvc := NewAISessionService(store)
	runner := &fakeRunner{returnText: "hello", returnUsage: agent.TokenUsage{TotalTokens: 3}}
	msgSvc := NewAIMessageService(sessSvc, runner)

	sess, err := sessSvc.Create(context.Background(), 42, "s")
	require.NoError(t, err)

	res, err := msgSvc.Send(context.Background(), 42, sess.ID, "ping")
	require.NoError(t, err)

	assert.Equal(t, sess.ID, runner.gotSessionID)
	assert.Equal(t, "ping", runner.gotUserMessage)

	// D13：tools.WithUserID 必须把 uint user_id 注入到传给 Runner 的 ctx
	require.True(t, runner.gotToolsUserOK, "Runner 收到的 ctx 必须能取出 tools user_id")
	assert.Equal(t, uint(42), runner.gotToolsUserID,
		"D13：tools.WithUserID 必须把 service 入口的 userID 注入 ctx")

	// SendResult 字段
	require.NotNil(t, res.AssistantMessage)
	assert.Equal(t, sess.ID, res.AssistantMessage.SessionID)
	assert.Equal(t, "assistant", res.AssistantMessage.Role)
	assert.Equal(t, "hello", res.AssistantMessage.Content)
	assert.NotEmpty(t, res.AssistantMessage.ID,
		"D14：service 预生成的 assistantMessageID 必须出现在返回的 Message.ID")
	_, parseErr := uuid.Parse(res.AssistantMessage.ID)
	assert.NoError(t, parseErr, "D14：assistantMessageID 必须是合法 uuid")
	assert.Equal(t, agent.TokenUsage{TotalTokens: 3}, res.TokenUsage)
}

// TestAIMessageService_Send_PropagatesRunnerError
//
// Runner 返回业务错误（含 50004/50005/50006/50007）→ service 直接透传，不重新映射。
func TestAIMessageService_Send_PropagatesRunnerError(t *testing.T) {
	store := newFakeAISessionStore()
	sessSvc := NewAISessionService(store)
	runner := &fakeRunner{returnErr: errs.ErrAIProviderRateLimited}
	msgSvc := NewAIMessageService(sessSvc, runner)

	sess, err := sessSvc.Create(context.Background(), 1, "s")
	require.NoError(t, err)

	_, err = msgSvc.Send(context.Background(), 1, sess.ID, "ping")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrAIProviderRateLimited),
		"Runner 返回的业务错误必须透传，service 不重新映射")
}
