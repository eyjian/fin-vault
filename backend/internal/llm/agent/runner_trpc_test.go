package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkagent "trpc.group/trpc-go/trpc-agent-go/agent"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// 入参校验 + AgentFactory 错误路径
// 注：SDK Runner 端到端调用（含真实流式）由 tester 用 e2e 覆盖；
// 本文件聚焦于 trpcRunner 自身代码路径（前 step 1/2/3/4 之前的逻辑）。
// =====================================================================

func TestTRPCRunner_EmptySessionID_ReturnsError(t *testing.T) {
	store := newFakeStore()
	r := NewTRPCRunner(
		func(_ context.Context, _, _ string) (sdkagent.Agent, error) {
			t.Fatal("agentFactory 不应被调用")
			return nil, nil
		},
		store,
		20,
		silentLogger(),
	)
	_, _, _, err := r.Run(context.Background(), "", "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id required")
}

func TestTRPCRunner_AgentFactoryError_ReturnsMapped(t *testing.T) {
	store := newFakeStore()
	factoryErr := errors.New("rate limit reached")
	r := NewTRPCRunner(
		func(_ context.Context, _, _ string) (sdkagent.Agent, error) {
			return nil, factoryErr
		},
		store,
		20,
		silentLogger(),
	)
	// 提前预填一条 session 让 ListMessages 工作
	require.NoError(t, store.AppendMessage(context.Background(), &domain.Message{
		ID: "m-1", SessionID: "s-1", Role: "user", Content: "older",
	}))
	_, _, _, err := r.Run(context.Background(), "s-1", "hello")
	require.Error(t, err)
	got := errs.As(err)
	require.NotNil(t, got)
	assert.Equal(t, errs.ErrAIProviderRateLimited.Code, got.Code, "rate limit 关键字应映射为 50006")

	// step 2 已经落了 user msg
	msgs := store.snapshotMessages()
	var foundUser bool
	for _, m := range msgs {
		if m.SessionID == "s-1" && m.Role == "user" && m.Content == "hello" {
			foundUser = true
		}
	}
	assert.True(t, foundUser, "step 2 落 user msg 应在 factory 失败前完成（业务 Runner 自管）")
}

func TestTRPCRunner_HistoryWindowDefault_FallbackTo20(t *testing.T) {
	store := newFakeStore()
	r := NewTRPCRunner(
		func(_ context.Context, _, _ string) (sdkagent.Agent, error) {
			return nil, errors.New("stop here") // 让流程在 factory 处提前终止
		},
		store,
		0, // 触发 ≤0 兜底到 20
		silentLogger(),
	)
	// 不验真实 historyWindow 的拉取行为（那需要 SDK 实跑），仅验构造时不 panic + 流程走到 factory
	_, _, _, err := r.Run(context.Background(), "s-1", "x")
	require.Error(t, err) // factory 注入了 error
}

func TestTRPCRunner_NilLogger_FallbackToDefault(t *testing.T) {
	store := newFakeStore()
	r := NewTRPCRunner(
		func(_ context.Context, _, _ string) (sdkagent.Agent, error) {
			return nil, errors.New("stop")
		},
		store,
		20,
		nil, // 触发 nil 兜底到 slog.Default
	)
	_, _, _, err := r.Run(context.Background(), "s-1", "x")
	require.Error(t, err)
}

func TestWithUserID_AndUserIDFromCtx(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "anonymous", userIDFromCtx(ctx), "未注入 user_id 时返回 anonymous")

	ctx2 := WithUserID(ctx, "u-42")
	assert.Equal(t, "u-42", userIDFromCtx(ctx2))

	// 类型不匹配的情况（注入空字符串视为未注入）
	ctx3 := WithUserID(ctx, "")
	assert.Equal(t, "anonymous", userIDFromCtx(ctx3))
}

func TestMsgToEvent_FillsMinimalShape(t *testing.T) {
	m := &domain.Message{
		ID:      "m-1",
		Role:    "user",
		Content: "hello",
	}
	ev := msgToEvent(m)
	require.NotNil(t, ev)
	require.NotNil(t, ev.Response)
	assert.Equal(t, "chat.completion", ev.Response.Object)
	require.Len(t, ev.Response.Choices, 1)
	assert.Equal(t, "user", string(ev.Response.Choices[0].Message.Role))
	assert.Equal(t, "hello", ev.Response.Choices[0].Message.Content)
	assert.True(t, ev.Response.Done)
	assert.Equal(t, "user", ev.Author)
}
