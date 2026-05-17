package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkevent "trpc.group/trpc-go/trpc-agent-go/event"
	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// silentLogger 不输出任何日志，避免测试时刷屏
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// chanFromEvents 把若干 *sdkevent.Event 包装成已关闭的只读 channel
func chanFromEvents(events ...*sdkevent.Event) <-chan *sdkevent.Event {
	ch := make(chan *sdkevent.Event, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch
}

func newChunkEvent(text string, usage *sdkmodel.Usage) *sdkevent.Event {
	return &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object: sdkmodel.ObjectTypeChatCompletionChunk,
			Choices: []sdkmodel.Choice{
				{Index: 0, Delta: sdkmodel.Message{Role: sdkmodel.RoleAssistant, Content: text}},
			},
			Usage:     usage,
			IsPartial: true,
			Timestamp: time.Now(),
		},
		Author:    "agent",
		Timestamp: time.Now(),
	}
}

func newChatCompletionEvent(content string, toolCalls []sdkmodel.ToolCall) *sdkevent.Event {
	return &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object: sdkmodel.ObjectTypeChatCompletion,
			Choices: []sdkmodel.Choice{
				{
					Index: 0,
					Message: sdkmodel.Message{
						Role:      sdkmodel.RoleAssistant,
						Content:   content,
						ToolCalls: toolCalls,
					},
				},
			},
			Done:      true,
			Timestamp: time.Now(),
		},
		Author:    "agent",
		Timestamp: time.Now(),
	}
}

func newToolResponseEvent(toolID, toolName, result string, errMsg string) *sdkevent.Event {
	var rerr *sdkmodel.ResponseError
	if errMsg != "" {
		rerr = &sdkmodel.ResponseError{
			Type:    sdkmodel.ErrorTypeRunError,
			Message: errMsg,
		}
	}
	return &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object: sdkmodel.ObjectTypeToolResponse,
			Choices: []sdkmodel.Choice{
				{
					Index: 0,
					Message: sdkmodel.Message{
						Role:     sdkmodel.RoleTool,
						ToolID:   toolID,
						ToolName: toolName,
						Content:  result,
					},
				},
			},
			Error:     rerr,
			Timestamp: time.Now(),
		},
		Author:    "tool",
		Timestamp: time.Now(),
	}
}

func newRunnerCompletionEvent(requestID, invocationID string) *sdkevent.Event {
	return &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object:    sdkmodel.ObjectTypeRunnerCompletion,
			Done:      true,
			Timestamp: time.Now(),
		},
		RequestID:    requestID,
		InvocationID: invocationID,
		Author:       "runner",
		Timestamp:    time.Now(),
	}
}

func newErrorEvent(rerr *sdkmodel.ResponseError) *sdkevent.Event {
	return &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object:    sdkmodel.ObjectTypeError,
			Error:     rerr,
			Timestamp: time.Now(),
		},
		Author:    "runner",
		Timestamp: time.Now(),
	}
}

// =====================================================================
// 正常路径
// =====================================================================

func TestAggregateEvents_StreamingChunks_AggregatesText(t *testing.T) {
	store := newFakeStore()
	ch := chanFromEvents(
		newChunkEvent("Hello, ", nil),
		newChunkEvent("world!", &sdkmodel.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}),
		newRunnerCompletionEvent("req-1", "inv-1"),
	)
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.NoError(t, err)
	assert.Equal(t, "Hello, world!", res.AssistantMsg)
	assert.Equal(t, 10, res.Usage.PromptTokens)
	assert.Equal(t, 5, res.Usage.CompletionTokens)
	assert.Equal(t, 15, res.Usage.TotalTokens)

	steps := store.snapshotSteps()
	// 至少：1 次 token_usage（chunk 携带 usage） + 1 次 step_boundary
	var hasUsage, hasBoundary bool
	for _, s := range steps {
		if s.EventType == "token_usage" {
			hasUsage = true
		}
		if s.EventType == "step_boundary" {
			hasBoundary = true
		}
	}
	assert.True(t, hasUsage, "应有 token_usage step")
	assert.True(t, hasBoundary, "应有 step_boundary step")
}

func TestAggregateEvents_ToolCallSuccess_PairsAndMarksSuccess(t *testing.T) {
	store := newFakeStore()
	toolCallID := "call-1"
	args := []byte(`{"keyword":"abc"}`)
	ch := chanFromEvents(
		// assistant 决定调工具
		newChatCompletionEvent("", []sdkmodel.ToolCall{
			{
				ID:       toolCallID,
				Type:     "function",
				Function: sdkmodel.FunctionDefinitionParam{Name: "search_fund", Arguments: args},
			},
		}),
		// 工具响应
		newToolResponseEvent(toolCallID, "search_fund", `{"results":[]}`, ""),
		// 完整 chat.completion（assistant 收尾文本）
		newChatCompletionEvent("Found 0 results.", nil),
		newRunnerCompletionEvent("req-1", "inv-1"),
	)
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.NoError(t, err)
	assert.Equal(t, "Found 0 results.", res.AssistantMsg)
	require.Len(t, res.ToolCalls, 1)
	tc := res.ToolCalls[0]
	assert.Equal(t, "search_fund", tc.Name)
	assert.Equal(t, "success", tc.Status)
	assert.Empty(t, tc.ErrorMessage)
	assert.Equal(t, "abc", tc.Arguments["keyword"])

	steps := store.snapshotSteps()
	var started, finished int
	for _, s := range steps {
		switch s.EventType {
		case "tool_call_started":
			started++
			assert.Equal(t, "search_fund", s.ToolName)
		case "tool_call_finished":
			finished++
			assert.Equal(t, "search_fund", s.ToolName)
		}
	}
	assert.Equal(t, 1, started, "应有 1 个 tool_call_started step")
	assert.Equal(t, 1, finished, "应有 1 个 tool_call_finished step")
}

func TestAggregateEvents_ToolCallFailed_SoftFailure_DoesNotReturnError(t *testing.T) {
	store := newFakeStore()
	ch := chanFromEvents(
		newChatCompletionEvent("", []sdkmodel.ToolCall{
			{ID: "c-1", Type: "function", Function: sdkmodel.FunctionDefinitionParam{Name: "broken_tool", Arguments: []byte(`{}`)}},
		}),
		newToolResponseEvent("c-1", "broken_tool", "", "tool internal error"),
		newChatCompletionEvent("Sorry, the tool failed.", nil),
	)
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.NoError(t, err, "D11 工具失败软处理：不返回错误")
	require.Len(t, res.ToolCalls, 1)
	assert.Equal(t, "failed", res.ToolCalls[0].Status)
	assert.Equal(t, "tool internal error", res.ToolCalls[0].ErrorMessage)
	assert.Equal(t, "Sorry, the tool failed.", res.AssistantMsg)

	// step 中应有 finished 且 payload.error 非空
	steps := store.snapshotSteps()
	var foundFinishedFailed bool
	for _, s := range steps {
		if s.EventType == "tool_call_finished" {
			var p map[string]interface{}
			require.NoError(t, json.Unmarshal(s.Payload, &p))
			if p["status"] == "failed" && p["error"] == "tool internal error" {
				foundFinishedFailed = true
			}
		}
	}
	assert.True(t, foundFinishedFailed, "应有 tool_call_finished step 标 failed")
}

func TestAggregateEvents_ErrorEvent_ReturnsMappedError(t *testing.T) {
	store := newFakeStore()
	ch := chanFromEvents(
		newErrorEvent(&sdkmodel.ResponseError{
			Type:    sdkmodel.ErrorTypeAPIError,
			Message: "Rate limit exceeded",
		}),
	)
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.Error(t, err)
	got := errs.As(err)
	require.NotNil(t, got)
	assert.Equal(t, errs.ErrAIProviderRateLimited.Code, got.Code, "应映射为 50006 限流")
	assert.NotNil(t, res, "返回部分聚合结果（虽然空）")
}

func TestAggregateEvents_UnknownToolError_ReturnsToolNotFound(t *testing.T) {
	store := newFakeStore()
	ch := chanFromEvents(
		newErrorEvent(&sdkmodel.ResponseError{
			Type:    sdkmodel.ErrorTypeFlowError,
			Message: "unknown tool: ghost",
		}),
	)
	_, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.Error(t, err)
	got := errs.As(err)
	require.NotNil(t, got)
	assert.Equal(t, errs.ErrAIToolNotFound.Code, got.Code)
}

func TestAggregateEvents_StepAppendFailure_LogsWarnButContinues(t *testing.T) {
	store := newFakeStore()
	store.stepErr = assertableStepErr // 注入 step 落库失败

	ch := chanFromEvents(
		newChunkEvent("hello", &sdkmodel.Usage{TotalTokens: 1}),
		newRunnerCompletionEvent("r", "i"),
	)
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	// spec 软依赖：step 落库失败不打断主流程
	require.NoError(t, err)
	assert.Equal(t, "hello", res.AssistantMsg)
	// 既然 stepErr 注入了，store.steps 应当为空
	assert.Empty(t, store.snapshotSteps())
}

func TestAggregateEvents_NilEvent_AndNilResponse_AreSkipped(t *testing.T) {
	store := newFakeStore()
	ch := make(chan *sdkevent.Event, 3)
	ch <- nil
	ch <- &sdkevent.Event{Response: nil}
	ch <- newChunkEvent("abc", nil)
	close(ch)
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.NoError(t, err)
	assert.Equal(t, "abc", res.AssistantMsg)
}

func TestAggregateEvents_UnknownObjectType_IsSkipped(t *testing.T) {
	store := newFakeStore()
	customEv := &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object:    sdkmodel.ObjectTypeStateUpdate,
			Timestamp: time.Now(),
		},
		Author: "system",
	}
	ch := chanFromEvents(customEv, newChunkEvent("done", nil))
	res, err := AggregateEvents(context.Background(), ch, store, "sess-1", silentLogger())
	require.NoError(t, err)
	assert.Equal(t, "done", res.AssistantMsg)
}

// assertableStepErr 是测试间共享的 sentinel
var assertableStepErr = errString("inject step append failure")

type errString string

func (e errString) Error() string { return string(e) }

// =====================================================================
// D14：step 与 assistant message 关联（appendStepSafe 内部封装方式）
// =====================================================================

// TestAppendStep_LinkedToAssistantMessageID_WhenInjected
//
// service 注入 assistantMessageID → 所有落库 step 的 MessageID 字段 == 注入值。
// 验证 D14 主路径。
func TestAppendStep_LinkedToAssistantMessageID_WhenInjected(t *testing.T) {
	store := newFakeStore()
	const wantMsgID = "msg-d14-injected"
	ctx := WithAssistantMessageID(context.Background(), wantMsgID)

	ch := chanFromEvents(
		newChatCompletionEvent("", []sdkmodel.ToolCall{
			{ID: "c-1", Type: "function", Function: sdkmodel.FunctionDefinitionParam{Name: "search_fund", Arguments: []byte(`{"keyword":"x"}`)}},
		}),
		newToolResponseEvent("c-1", "search_fund", `{"results":[]}`, ""),
		newChunkEvent("done", &sdkmodel.Usage{TotalTokens: 1}),
		newRunnerCompletionEvent("req", "inv"),
	)
	_, err := AggregateEvents(ctx, ch, store, "sess-1", silentLogger())
	require.NoError(t, err)

	steps := store.snapshotSteps()
	require.NotEmpty(t, steps, "应至少落 4 类 step（started/finished/usage/boundary）")
	for _, s := range steps {
		assert.Equal(t, wantMsgID, s.MessageID,
			"D14：所有 step.MessageID 必须 == ctx 注入的 assistantMessageID（event_type=%s）", s.EventType)
	}
}

// TestAppendStep_NoAssistantMessageIDInCtx_FallbacksToEmpty_DoesNotBlock
//
// service 未注入 assistantMessageID（直接调 Runner 的非主路径）→ step.MessageID 降级空串、
// 但主流程不阻塞，step 仍正常落库（spec 软依赖原则）。
func TestAppendStep_NoAssistantMessageIDInCtx_FallbacksToEmpty_DoesNotBlock(t *testing.T) {
	store := newFakeStore()
	// ctx 不注入 assistantMessageID
	ctx := context.Background()

	ch := chanFromEvents(
		newChunkEvent("hi", &sdkmodel.Usage{TotalTokens: 1}),
		newRunnerCompletionEvent("req", "inv"),
	)
	res, err := AggregateEvents(ctx, ch, store, "sess-2", silentLogger())
	require.NoError(t, err, "未注入 messageID 不应阻塞主流程")
	assert.Equal(t, "hi", res.AssistantMsg)

	steps := store.snapshotSteps()
	require.NotEmpty(t, steps, "step 仍应正常落库（仅 message_id 关联缺失）")
	for _, s := range steps {
		assert.Equal(t, "", s.MessageID,
			"D14 降级：未注入时 step.MessageID 应为空串（event_type=%s）", s.EventType)
	}
}
