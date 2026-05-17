package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	sdkagent "trpc.group/trpc-go/trpc-agent-go/agent"
	sdkevent "trpc.group/trpc-go/trpc-agent-go/event"
	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"
	sdkrunner "trpc.group/trpc-go/trpc-agent-go/runner"
	sdksession "trpc.group/trpc-go/trpc-agent-go/session"
	sdksessioninmem "trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
)

// DefaultAppName 是注入 SDK 的应用名（同时也是 SDK Runner 的 Trace 标识）。
const DefaultAppName = "fin-vault"

// userIDCtxKey 业务 ctx 中携带 user_id 的 key。service 层在调 Runner.Run 之前注入。
type userIDCtxKey struct{}

// WithUserID 在 ctx 中注入用户 ID（service 层在调 Runner.Run 之前调用）。
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDCtxKey{}, userID)
}

// userIDFromCtx 从 ctx 取 user_id；未注入时返回 "anonymous"（仅用作 SDK session 隔离 key 占位）。
//
// 业务侧的用户隔离由 SessionStore 保证（spec ai-session "列表只返回当前用户的会话"），
// SDK 这边的 userID 只影响 inmemory.SessionService 内部 key 拼接，单次 Run 后即丢弃。
func userIDFromCtx(ctx context.Context) string {
	if v := ctx.Value(userIDCtxKey{}); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "anonymous"
}

// AgentFactory 在每次 Run 时构造一个 SDK Agent 实例（用于支持 fake / mock 注入）。
//
// 生产装配（§9）会传一个固定的 sdkAgent；单测可以用 fake 实现。
type AgentFactory func(ctx context.Context, userID, sessionID string) (sdkagent.Agent, error)

// trpcRunner 是 agent.Runner 接口的 trpc-agent-go 实现。
//
// 设计要点（design.md D8 / D11 / D12）：
//   - SDK SessionService 用 inmemory（每次构造一个干净的 service，单次 Run 内的 working memory，
//     请求结束即丢弃）；持久化全在业务 SessionStore。
//   - 业务 Runner 在调 SDK Runner.Run 前先把历史灌进 SDK session（用 sdksession.AppendEvent
//     wrap 每条 domain.Message 为 *sdkevent.Event）。
//   - user / assistant 两条消息均由业务 Runner 自管落库，service 层不做重复写。
//   - 业务 Runner 通过 agent.Runner 接口暴露给 service / handler，trpc-agent-go 的所有 import
//     都封装在本包内（铁律 F2）。
type trpcRunner struct {
	agentFactory  AgentFactory
	store         session.SessionStore
	historyWindow int
	appName       string
	logger        *slog.Logger
}

// NewTRPCRunner 构造业务 Runner（实现 agent.Runner 接口）。
//
// 参数：
//   - factory       ：每次 Run 用 factory 现场构造 SDK Agent；§9 装配时传固定 closure，
//     单测可传 fake 实现以注入预制 channel。
//   - store         ：业务 SessionStore（持久化层）。
//   - historyWindow ：拉历史的窗口（≤0 兜底 20）。
//   - logger        ：slog logger（nil 时使用 slog.Default）。
func NewTRPCRunner(
	factory AgentFactory,
	store session.SessionStore,
	historyWindow int,
	logger *slog.Logger,
) Runner {
	if historyWindow <= 0 {
		historyWindow = 20
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &trpcRunner{
		agentFactory:  factory,
		store:         store,
		historyWindow: historyWindow,
		appName:       DefaultAppName,
		logger:        logger,
	}
}

// 编译期断言：trpcRunner 满足 agent.Runner 接口。
var _ Runner = (*trpcRunner)(nil)

// Run 实现 agent.Runner 接口（design.md D12 六步流程）：
//
//  1. 校验入参 + 拉业务历史（SessionStore.ListMessages）
//  2. 落 user msg（业务 Runner 主导，避免 service 双写）
//  3. 构造一个 SDK 临时 inmemory session.Service，把历史灌进去
//  4. 通过 factory 拿 SDK Agent，构造 SDK Runner，调 Run 拿 channel
//  5. 调 AggregateEvents 消费 channel，聚合 + step 落库
//  6. 落 assistant msg（含 token_usage JSON），返回业务结果
//
// 错误映射严格按 D11：SDK Run 直接返回的 error 走 MapSDKError；channel 中的 IsError 事件走
// MapResponseError；工具失败走"软失败"路径在聚合的 ToolCalls 里标 Status="failed"。
func (r *trpcRunner) Run(
	ctx context.Context,
	sessionID string,
	userMessage string,
) (string, []ToolCall, TokenUsage, error) {
	if sessionID == "" {
		return "", nil, TokenUsage{}, errors.New("session_id required")
	}
	userID := userIDFromCtx(ctx)
	now := time.Now()

	// step 1: 拉业务历史
	history, err := r.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return "", nil, TokenUsage{}, fmt.Errorf("load history: %w", err)
	}

	// step 2: 落 user msg（业务 Runner 自管，service 不双写）
	if err := r.store.AppendMessage(ctx, &domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      string(sdkmodel.RoleUser),
		Content:   userMessage,
		CreatedAt: now,
	}); err != nil {
		return "", nil, TokenUsage{}, fmt.Errorf("append user msg: %w", err)
	}

	// step 3: 构造 SDK 临时 session.Service 并灌历史
	sessionSvc := sdksessioninmem.NewSessionService()
	defer func() { _ = sessionSvc.Close() }()

	sdkKey := sdksession.Key{AppName: r.appName, UserID: userID, SessionID: sessionID}
	sdkSess, err := sessionSvc.CreateSession(ctx, sdkKey, nil)
	if err != nil {
		return "", nil, TokenUsage{}, MapSDKError(err)
	}
	for i := range history {
		if err := sessionSvc.AppendEvent(ctx, sdkSess, msgToEvent(&history[i])); err != nil {
			// 灌历史失败不致命：让 SDK 至少基于当前 user msg 工作
			r.logger.Warn("seed history into sdk session failed",
				"err", err, "session_id", sessionID, "msg_index", i)
		}
	}

	// step 4: 现场构造 SDK Agent + Runner
	sdkAg, err := r.agentFactory(ctx, userID, sessionID)
	if err != nil {
		return "", nil, TokenUsage{}, MapSDKError(err)
	}
	sdkR := sdkrunner.NewRunner(r.appName, sdkAg, sdkrunner.WithSessionService(sessionSvc))
	defer func() { _ = sdkR.Close() }()

	currentMsg := sdkmodel.Message{Role: sdkmodel.RoleUser, Content: userMessage}
	ch, err := sdkR.Run(ctx, userID, sessionID, currentMsg)
	if err != nil {
		return "", nil, TokenUsage{}, MapSDKError(err)
	}

	// step 5: 消费 channel + 聚合 + step 落库
	result, err := AggregateEvents(ctx, ch, r.store, sessionID, r.logger)
	if err != nil {
		return "", nil, TokenUsage{}, err
	}

	// step 6: 落 assistant msg（落库失败仅 warn，不阻塞结果返回）
	if result.AssistantMsg != "" {
		var usageJSON json.RawMessage
		if b, mErr := json.Marshal(result.Usage); mErr == nil {
			usageJSON = b
		}
		if err := r.store.AppendMessage(ctx, &domain.Message{
			ID:         uuid.NewString(),
			SessionID:  sessionID,
			Role:       string(sdkmodel.RoleAssistant),
			Content:    result.AssistantMsg,
			TokenUsage: usageJSON,
			CreatedAt:  time.Now(),
		}); err != nil {
			r.logger.Warn("append assistant msg failed",
				"err", err, "session_id", sessionID)
		}
	}

	return result.AssistantMsg, result.ToolCalls, result.Usage, nil
}

// msgToEvent 把业务 domain.Message 包装为 SDK *event.Event，供 sdksession.AppendEvent 使用。
//
// SDK Session 用 *event.Event 作为历史载体，此函数完成最小填充：Object 标 chat.completion，
// Choices[0].Message 装载 role + content。Done=true 表示该 event 是一段完整的轮次输出。
func msgToEvent(m *domain.Message) *sdkevent.Event {
	return &sdkevent.Event{
		Response: &sdkmodel.Response{
			Object: sdkmodel.ObjectTypeChatCompletion,
			Choices: []sdkmodel.Choice{
				{
					Index: 0,
					Message: sdkmodel.Message{
						Role:    sdkmodel.Role(m.Role),
						Content: m.Content,
					},
				},
			},
			Done:      true,
			Timestamp: m.CreatedAt,
		},
		Author:    m.Role,
		Timestamp: m.CreatedAt,
	}
}
