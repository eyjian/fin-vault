package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/agent"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// fakeRunner —— agent.Runner 的 in-memory fake，仅供 §8.B handler 测试
// =====================================================================
//
// 设计目标：
//   - 端到端验证 AIMessageService.Send → Runner.Run 链路上 ctx 注入正确（D13 tools.UserIDFromContext）
//   - 可注入 returnText / returnToolCalls / returnUsage / returnErr 控制返回行为
//   - 记录 Run 入参（ctx 关键字段 / sessionID / userMsg），供断言 e2e 链路一致性
type fakeRunner struct {
	mu sync.Mutex

	// 配置：注入返回值
	returnText      string
	returnToolCalls []agent.ToolCall
	returnUsage     agent.TokenUsage
	returnErr       error

	// 观察：Run 入口捕获
	gotSessionID   string
	gotUserMessage string
	gotToolsUserID uint
	gotToolsUserOK bool
}

func (f *fakeRunner) Run(ctx context.Context, sessionID, userMessage string) (string, []agent.ToolCall, agent.TokenUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotSessionID = sessionID
	f.gotUserMessage = userMessage
	if uid, ok := tools.UserIDFromContext(ctx); ok {
		f.gotToolsUserID = uid
		f.gotToolsUserOK = true
	}
	if f.returnErr != nil {
		return "", nil, agent.TokenUsage{}, f.returnErr
	}
	return f.returnText, f.returnToolCalls, f.returnUsage, nil
}

var _ agent.Runner = (*fakeRunner)(nil)

// =====================================================================
// setupFullRouter —— 同时挂 AISession + AIMessage 两 handler，e2e flow 用
// =====================================================================

// setupFullRouter 装配 gin engine + AISession + AIMessage handlers，返回 (router, store, runner)。
// 复用 dev_2 §8.A 在 ai_session_handler_test.go 定义的 fakeStore（同包 unexported 直接可见）。
func setupFullRouter(t *testing.T, runner agent.Runner) (*gin.Engine, *fakeStore, *fakeRunner) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	sessSvc := service.NewAISessionService(store)
	msgSvc := service.NewAIMessageService(sessSvc, runner)

	r := gin.New()
	v1 := r.Group("/api/v1")
	NewAISessionHandler(sessSvc).Register(v1)
	NewAIMessageHandler(msgSvc).Register(v1)

	fr, _ := runner.(*fakeRunner)
	return r, store, fr
}

// =====================================================================
// §8.5a 路由测试 —— 7 用例
// =====================================================================

// TestSend_Success
//
// fake Runner 返 assistantText + 1 success + 1 failed tool_call → 200，断言 SendResp 完整字段。
func TestSend_Success(t *testing.T) {
	startedAt := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)
	runner := &fakeRunner{
		returnText: "Here is your answer.",
		returnToolCalls: []agent.ToolCall{
			{
				Name:       "search_fund",
				Arguments:  map[string]interface{}{"keyword": "abc"},
				StartedAt:  startedAt,
				FinishedAt: finishedAt,
				Status:     "success",
			},
			{
				Name:         "broken_tool",
				Arguments:    map[string]interface{}{},
				StartedAt:    startedAt,
				FinishedAt:   finishedAt,
				Status:       "failed",
				ErrorMessage: "tool internal error",
			},
		},
		returnUsage: agent.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	r, store, _ := setupFullRouter(t, runner)
	sess := seedSession(t, store, 1, "chat")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sess.ID+"/messages", "1",
		SendReq{Content: "hello"})
	require.Equal(t, http.StatusOK, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, 0, body.Code)
	var resp SendResp
	dataAs(t, body, &resp)

	// AssistantMessage：service 预生成的 uuid，role=assistant，content=Runner 返回值
	assert.Len(t, resp.AssistantMessage.ID, 36, "assistant_message.id 应为 service 注入的 uuid（D14）")
	_, parseErr := uuid.Parse(resp.AssistantMessage.ID)
	assert.NoError(t, parseErr)
	assert.Equal(t, "assistant", resp.AssistantMessage.Role)
	assert.Equal(t, "Here is your answer.", resp.AssistantMessage.Content)

	// ToolCalls：含 success + failed（spec ai-tools §53-57）
	require.Len(t, resp.ToolCalls, 2, "spec §53-57：失败的 tool_call 也应被返回")
	assert.Equal(t, "search_fund", resp.ToolCalls[0].Name)
	assert.Equal(t, "success", resp.ToolCalls[0].Status)
	assert.Empty(t, resp.ToolCalls[0].ErrorMessage)
	assert.Equal(t, "broken_tool", resp.ToolCalls[1].Name)
	assert.Equal(t, "failed", resp.ToolCalls[1].Status)
	assert.Equal(t, "tool internal error", resp.ToolCalls[1].ErrorMessage)

	// TokenUsage：透传 Runner 返回
	assert.Equal(t, 10, resp.TokenUsage.PromptTokens)
	assert.Equal(t, 5, resp.TokenUsage.CompletionTokens)
	assert.Equal(t, 15, resp.TokenUsage.TotalTokens)
}

// TestSend_Missing_X_User_Id_Returns401
//
// D15 红线：缺失 X-User-Id 必须 401。
func TestSend_Missing_X_User_Id_Returns401(t *testing.T) {
	r, store, _ := setupFullRouter(t, &fakeRunner{returnText: "x"})
	sess := seedSession(t, store, 1, "s")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sess.ID+"/messages", "",
		SendReq{Content: "hi"})
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"D15: 缺失 X-User-Id 必须返 401，绝不走 fallback=1")

	body := parseBody(t, w)
	assert.Equal(t, errs.ErrUnauthorized.Code, body.Code)
}

// TestSend_OtherUser_Returns404_NotExposing
//
// 用户 1 创建 session，用户 2 试图发消息 → 404 ErrAISessionNotFound（不暴露存在性）。
// service.AIMessageService.Send 内部 sessionSvc.Get 会先做归属校验。
func TestSend_OtherUser_Returns404_NotExposing(t *testing.T) {
	r, store, runner := setupFullRouter(t, &fakeRunner{returnText: "secret-reply"})
	sessA := seedSession(t, store, 1, "victim")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sessA.ID+"/messages", "2",
		SendReq{Content: "leak-please"})
	assert.Equal(t, http.StatusNotFound, w.Code,
		"D2: 跨用户 Send 必须 404 不暴露存在性，绝不返 403")

	body := parseBody(t, w)
	assert.Equal(t, errs.ErrAISessionNotFound.Code, body.Code)

	// 防越权先于 Runner.Run：fakeRunner 不应被调到
	assert.Equal(t, "", runner.gotSessionID,
		"防越权：归属校验失败时 Runner.Run 不应被调用")
	assert.NotContains(t, w.Body.String(), "secret-reply",
		"失败响应不应泄漏 Runner 返回内容")
}

// TestSend_Empty_Body_Returns400
//
// content 必填（gin binding required）→ 400 ErrInvalidParam。
func TestSend_Empty_Body_Returns400(t *testing.T) {
	r, store, _ := setupFullRouter(t, &fakeRunner{returnText: "x"})
	sess := seedSession(t, store, 1, "s")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sess.ID+"/messages", "1",
		SendReq{Content: ""})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	body := parseBody(t, w)
	assert.Equal(t, errs.ErrInvalidParam.Code, body.Code)
}

// TestSend_DoubleCtxInjection_E2E
//
// 端到端验证 service.Send → Runner.Run 链路上 D13 tools.UserIDFromContext 注入正确。
// agent.WithUserID / agent.WithAssistantMessageID 的 ctxKey 是 unexported，本层不可
// 直接观测；其端到端正确性已由 agent 包 D14 单测兜底（落 step.MessageID == 注入值），
// 与 §7.3 service 单测策略一致。
func TestSend_DoubleCtxInjection_E2E(t *testing.T) {
	r, store, runner := setupFullRouter(t, &fakeRunner{returnText: "ok"})
	sess := seedSession(t, store, 42, "ctx-test")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sess.ID+"/messages", "42",
		SendReq{Content: "ping"})
	require.Equal(t, http.StatusOK, w.Code)

	require.True(t, runner.gotToolsUserOK,
		"D13：Runner 收到的 ctx 必须能取出 tools user_id（service.Send 通过 tools.WithUserID 注入）")
	assert.Equal(t, uint(42), runner.gotToolsUserID,
		"D13：tools ctx user_id 必须 == X-User-Id 头中的值")
	assert.Equal(t, sess.ID, runner.gotSessionID, "Runner 收到的 sessionID 必须等于路径参数")
	assert.Equal(t, "ping", runner.gotUserMessage)
}

// TestSend_RunnerError_50004_Propagated
//
// Runner 返 ErrAIRequestFailed → service 层透传 → response.Fail 自动映射 HTTP 状态码（400）。
func TestSend_RunnerError_50004_Propagated(t *testing.T) {
	r, store, _ := setupFullRouter(t, &fakeRunner{returnErr: errs.ErrAIRequestFailed})
	sess := seedSession(t, store, 1, "s")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sess.ID+"/messages", "1",
		SendReq{Content: "hello"})
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"50004 ErrAIRequestFailed 在 50000-89999 区间走 400 映射")

	body := parseBody(t, w)
	assert.Equal(t, errs.ErrAIRequestFailed.Code, body.Code, "业务错误码必须 50004 透传")
}

// TestSend_ToolCalls_Failed_AlsoIncluded
//
// spec ai-tools §53-57：失败的 tool_call 也必须包含在 tool_calls 数组中（前端展示失败原因）。
// 本用例独立验证"失败"路径以避免与 TestSend_Success 的混合用例耦合。
func TestSend_ToolCalls_Failed_AlsoIncluded(t *testing.T) {
	startedAt := time.Now().Add(-time.Second)
	finishedAt := time.Now()
	runner := &fakeRunner{
		returnText: "Sorry, the tool failed.",
		returnToolCalls: []agent.ToolCall{
			{
				Name:         "broken_tool",
				Arguments:    map[string]interface{}{"q": "x"},
				StartedAt:    startedAt,
				FinishedAt:   finishedAt,
				Status:       "failed",
				ErrorMessage: "upstream timeout",
			},
		},
		returnUsage: agent.TokenUsage{TotalTokens: 1},
	}
	r, store, _ := setupFullRouter(t, runner)
	sess := seedSession(t, store, 1, "s")

	w := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+sess.ID+"/messages", "1",
		SendReq{Content: "try"})
	require.Equal(t, http.StatusOK, w.Code,
		"工具失败属软失败：Runner 不返业务错误，整体响应仍 200")

	body := parseBody(t, w)
	var resp SendResp
	dataAs(t, body, &resp)

	require.Len(t, resp.ToolCalls, 1,
		"spec §53-57：失败的 tool_call 也必须在数组里返回")
	assert.Equal(t, "failed", resp.ToolCalls[0].Status)
	assert.Equal(t, "upstream timeout", resp.ToolCalls[0].ErrorMessage)
	assert.NotEmpty(t, resp.ToolCalls[0].Arguments, "Arguments 应被序列化为 JSON 透传")
	// arguments 应是合法 JSON
	var args map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.ToolCalls[0].Arguments, &args))
	assert.Equal(t, "x", args["q"])

	// assistant 文本仍正常返回
	assert.Equal(t, "Sorry, the tool failed.", resp.AssistantMessage.Content)
}

// =====================================================================
// §8.5a e2e 联跑骨架 —— 5 步全流程
// =====================================================================

// TestE2E_Flow
//
// 5 步全流程（spec ai-session + ai-agent-runtime 联跑）：
//   1) POST /ai/sessions          → 201 拿 session_id
//   2) GET  /ai/sessions          → list 含上面 session_id
//   3) POST /ai/sessions/:id/messages content="hello" → 200 fake Runner reply
//   4) DELETE /ai/sessions/:id    → 204
//   5) GET  /ai/sessions          → list 不含已删 session_id
//
// 第 3 步 Runner 是 fake；§10 真 LLM 联跑由 main 决策推进，本期 mock 即可。
func TestE2E_Flow(t *testing.T) {
	runner := &fakeRunner{
		returnText:  "Hi! I am the assistant.",
		returnUsage: agent.TokenUsage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
	}
	r, _, _ := setupFullRouter(t, runner)

	// step 1：创建 session
	w1 := doReq(r, http.MethodPost, "/api/v1/ai/sessions", "1",
		SessionCreateReq{Title: "e2e-flow"})
	require.Equal(t, http.StatusCreated, w1.Code, "step1: POST /sessions 201")
	var created SessionCreateResp
	dataAs(t, parseBody(t, w1), &created)
	require.Len(t, created.SessionID, 36)

	// step 2：list 应含
	w2 := doReq(r, http.MethodGet, "/api/v1/ai/sessions", "1", nil)
	require.Equal(t, http.StatusOK, w2.Code, "step2: GET /sessions 200")
	var page2 response.PageData
	dataAs(t, parseBody(t, w2), &page2)
	var list2 []domain.Session
	listRaw, _ := json.Marshal(page2.List)
	require.NoError(t, json.Unmarshal(listRaw, &list2))
	var found bool
	for _, s := range list2 {
		if s.ID == created.SessionID {
			found = true
		}
	}
	assert.True(t, found, "step2: list 应含 step1 创建的 session_id")

	// step 3：发消息（fake Runner reply）
	w3 := doReq(r, http.MethodPost, "/api/v1/ai/sessions/"+created.SessionID+"/messages", "1",
		SendReq{Content: "hello"})
	require.Equal(t, http.StatusOK, w3.Code, "step3: POST /messages 200")
	var sendResp SendResp
	dataAs(t, parseBody(t, w3), &sendResp)
	assert.Equal(t, "Hi! I am the assistant.", sendResp.AssistantMessage.Content)
	assert.Equal(t, 15, sendResp.TokenUsage.TotalTokens)
	// 验证 Runner 收到正确 sessionID + ctx user_id
	assert.Equal(t, created.SessionID, runner.gotSessionID)
	assert.Equal(t, uint(1), runner.gotToolsUserID)

	// step 4：删除
	w4 := doReq(r, http.MethodDelete, "/api/v1/ai/sessions/"+created.SessionID, "1", nil)
	assert.Equal(t, http.StatusNoContent, w4.Code, "step4: DELETE /sessions/:id 204")

	// step 5：list 应不含
	w5 := doReq(r, http.MethodGet, "/api/v1/ai/sessions", "1", nil)
	require.Equal(t, http.StatusOK, w5.Code, "step5: GET /sessions 200")
	var page5 response.PageData
	dataAs(t, parseBody(t, w5), &page5)
	var list5 []domain.Session
	listRaw5, _ := json.Marshal(page5.List)
	require.NoError(t, json.Unmarshal(listRaw5, &list5))
	for _, s := range list5 {
		assert.NotEqual(t, created.SessionID, s.ID, "step5: 已删 session 不应再出现在 list")
	}
}
