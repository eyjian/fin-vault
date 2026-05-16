# LLMProvider 模式（fin-vault 项目级，强制）

> 本 Skill 是 ai-rd-team 内置规范的**新增**项，无 builtin 同名覆盖。
> 第一阶段**不自建 Agent 框架**，业务层只见 `LLMProvider` 接口，不直接 import `go-openai`。
> 任何"在 service 包看到 `import "github.com/sashabaranov/go-openai"`" 由 reviewer 直接打回。

## 适用场景

- 在 service 中调用 LLM（聊天、分析、问答、Tool Calling）
- 新增一个 LLM 厂商支持（DeepSeek / GLM / Kimi / 通义 / Ollama / OpenAI）
- 实现 ReAct / Tool Calling 多轮对话循环
- 写 LLM 相关单元测试（不调真实 API）
- 配置多模型路由（默认模型、按场景切换）

## 核心原则

### 1. 业务层只依赖 `LLMProvider` 接口

```go
// internal/llm/provider.go
type LLMProvider interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
    ChatWithTools(ctx context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error)
}
```

`internal/service/` 不得 import `github.com/sashabaranov/go-openai`，只 import `internal/llm` 自家接口包。

### 2. 所有 OpenAI-兼容厂商共用一个 Provider 实现

DeepSeek / GLM / Kimi / 通义千问 / Ollama 都走 OpenAI 协议，**只改 `BaseURL`**：

```go
// internal/llm/openai_provider.go
type OpenAIProvider struct {
    name   string
    client *openai.Client
    model  string
}

func NewOpenAIProvider(cfg ProviderConfig) *OpenAIProvider {
    oc := openai.DefaultConfig(cfg.APIKey)
    if cfg.BaseURL != "" {
        oc.BaseURL = cfg.BaseURL
    }
    if cfg.HTTPClient != nil {
        oc.HTTPClient = cfg.HTTPClient
    }
    return &OpenAIProvider{
        name:   cfg.Name,
        client: openai.NewClientWithConfig(oc),
        model:  cfg.Model,
    }
}
```

新增"OpenAI 兼容"厂商**不要**新建 struct，只在 `LLMRegistry` 里多注册一个 `ProviderConfig` 即可。

### 3. 真正不兼容才加新 Provider 实现

仅当厂商**协议层**与 OpenAI 不兼容（如自研 SDK / 非 chat-completions 路径）才新建 `XxxProvider`。
新建前必须写 ADR 说明：哪些字段不兼容、是否值得引入。

### 4. Provider 路由用 Registry，不在 service 散落 if/else

```go
type LLMRegistry interface {
    GetProvider(name string) (LLMProvider, error)
    Default() LLMProvider
    List() []string
}
```

service 决定用哪个 provider 时，调 `registry.GetProvider(name)` 或 `registry.Default()`。
**禁止**在 service 里写 `if model == "deepseek" {...} else if ... {...}`。

### 5. 中立的 Request / Response 结构

`internal/llm/types.go` 定义自家中立结构，**不**直接用 `openai.ChatCompletionRequest`：

```go
type Role string
const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

type Message struct {
    Role       Role        `json:"role"`
    Content    string      `json:"content"`
    Name       string      `json:"name,omitempty"`        // tool name
    ToolCallID string      `json:"tool_call_id,omitempty"`
    ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

type ChatRequest struct {
    Messages    []Message
    Temperature float32
    MaxTokens   int
    JSONMode    bool
    Stream      bool
}

type ChatResponse struct {
    Content   string
    ToolCalls []ToolCall
    Usage     Usage
    FinishReason string
}

type Tool struct {
    Name        string
    Description string
    Parameters  json.RawMessage // JSON Schema
}

type ToolCall struct {
    ID        string
    Name      string
    Arguments json.RawMessage
}
```

理由：换 SDK / 自研协议时，业务代码零改动。

### 6. Tool Calling 循环上限 + 显式终止条件

多轮 Tool Calling 是 service 层的工作，**必须**有：
- 最大轮次上限（默认 8 轮）
- 每轮 timeout（默认 30s）
- 终止条件：模型返回非 tool_calls（即纯文本回复）
- 异常工具失败时给 LLM 返回 error 字符串，**不要**直接 return

### 7. 测试用 mock，不调真实 API

单测必须用 mock 实现 `LLMProvider`，CI 零外部依赖。集成测试可用专门的 build tag。

## 常用模式

### 模式 A：定义 Tool（业务函数包装为 LLM 可调用工具）

```go
// internal/llm/tools/asset_tools.go
func GetHoldingsTool(svc service.HoldingService) llm.ToolDef {
    return llm.ToolDef{
        Tool: llm.Tool{
            Name:        "get_holdings",
            Description: "查询当前用户的全部持仓，返回 asset_code / quantity / market_value 列表",
            Parameters: json.RawMessage(`{
                "type": "object",
                "properties": {
                    "platform_id": {"type": "integer", "description": "可选，平台 ID"}
                }
            }`),
        },
        Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
            var p struct{ PlatformID *uint64 `json:"platform_id"` }
            _ = json.Unmarshal(args, &p)
            list, err := svc.List(ctx, service.ListOpt{PlatformID: p.PlatformID})
            if err != nil { return "", err }
            b, _ := json.Marshal(list)
            return string(b), nil
        },
    }
}
```

`ToolDef = Tool 元数据 + Handler 函数`，Registry 同时持有两者。

### 模式 B：ReAct 多轮 Tool Calling 循环（service 层）

```go
const (
    maxToolRounds   = 8
    perRoundTimeout = 30 * time.Second
)

func (s *aiChatService) Ask(ctx context.Context, userID uint64, question string) (string, error) {
    provider, err := s.registry.GetProvider("") // 默认
    if err != nil { return "", err }

    msgs := []llm.Message{
        {Role: llm.RoleSystem, Content: s.systemPrompt(userID)},
        {Role: llm.RoleUser, Content: question},
    }
    tools := s.toolRegistry.ListTools()

    for round := 0; round < maxToolRounds; round++ {
        rctx, cancel := context.WithTimeout(ctx, perRoundTimeout)
        resp, err := provider.ChatWithTools(rctx, llm.ChatRequest{Messages: msgs}, tools)
        cancel()
        if err != nil {
            return "", fmt.Errorf("llm round %d: %w", round, err)
        }

        if len(resp.ToolCalls) == 0 {
            return resp.Content, nil // 终止：纯文本回复
        }

        // 把 assistant 的 tool_calls 加入历史
        msgs = append(msgs, llm.Message{
            Role: llm.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls,
        })

        // 串行执行每个 tool_call（也可改并发，但要注意 ctx 超时）
        for _, tc := range resp.ToolCalls {
            result, terr := s.toolRegistry.Invoke(ctx, tc.Name, tc.Arguments)
            if terr != nil {
                result = fmt.Sprintf(`{"error": %q}`, terr.Error())
            }
            msgs = append(msgs, llm.Message{
                Role: llm.RoleTool, Content: result, Name: tc.Name, ToolCallID: tc.ID,
            })
        }
    }
    return "", fmt.Errorf("llm tool calling exceeded %d rounds", maxToolRounds)
}
```

### 模式 C：流式响应（StreamChat）

```go
func (s *aiChatService) Stream(ctx context.Context, in StreamInput,
                                onChunk func(string)) error {
    provider, _ := s.registry.GetProvider(in.Provider)
    ch, err := provider.StreamChat(ctx, llm.ChatRequest{
        Messages: in.Messages, Stream: true,
    })
    if err != nil { return err }
    for chunk := range ch {
        if chunk.Err != nil { return chunk.Err }
        onChunk(chunk.Delta)
    }
    return nil
}
```

handler 把 `onChunk` 实现为 SSE 写出（`c.SSEvent("message", chunk)`）。

### 模式 D：Registry 装配（bootstrap）

```go
// internal/bootstrap/llm.go
func ProvideLLMRegistry(cfg config.LLMConfig) llm.LLMRegistry {
    reg := llm.NewRegistry()
    for name, pc := range cfg.Providers {
        provider := llm.NewOpenAIProvider(llm.ProviderConfig{
            Name: name, BaseURL: pc.BaseURL, APIKey: pc.APIKey, Model: pc.Model,
        })
        reg.Register(provider)
    }
    reg.SetDefault(cfg.DefaultProvider)
    return reg
}
```

### 模式 E：mock provider 用于测试

```go
type fakeProvider struct {
    name      string
    responses []*llm.ChatResponse
    idx       int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) ChatWithTools(_ context.Context, _ llm.ChatRequest, _ []llm.Tool) (*llm.ChatResponse, error) {
    if f.idx >= len(f.responses) {
        return nil, errors.New("no more fake responses")
    }
    r := f.responses[f.idx]
    f.idx++
    return r, nil
}

// 测试：先返回 tool_call，再返回纯文本
func TestAsk_ReActLoop(t *testing.T) {
    fp := &fakeProvider{
        responses: []*llm.ChatResponse{
            {ToolCalls: []llm.ToolCall{{ID: "1", Name: "get_holdings", Arguments: []byte("{}")}}},
            {Content: "你的总市值是 100 万"},
        },
    }
    // ... 注入 fp，断言最终输出
}
```

## 禁止

| ❌ 反模式 | ✅ 正确做法 |
|---|---|
| service 里 `import "github.com/sashabaranov/go-openai"` | 只 import `internal/llm` 接口 |
| 给每个厂商各写一份 Provider 实现 | OpenAI 兼容只改 BaseURL |
| service 里 `if provider == "deepseek" {...}` | `registry.GetProvider(name)` |
| Tool Calling 无轮次上限（死循环风险） | 默认 8 轮上限 + 每轮 timeout |
| 工具失败直接 return（中断对话） | 把错误写回给 LLM，让它决定继续还是放弃 |
| 用 `openai.ChatCompletionRequest` 做内部传递 | 用自家中立 `llm.ChatRequest` |
| 引入 `cloudwego/eino` / `tmc/langchaingo` | 第一阶段不引入，确需写 ADR |
| 流式响应在 service 里直接 `c.SSEvent` | service 暴露 channel，handler 转 SSE |
| 把 API Key 硬编码 / 写进配置文件提交 | 仅环境变量或 secrets 注入 |
| LLM 单测调真实 API | 用 mock provider，CI 零外部依赖 |
| `provider.Chat` 不传 ctx 或传 `context.TODO()` | 全链路 ctx，且每轮带 timeout |

## 示例

### 完整 Provider 配置（YAML）

```yaml
llm:
  default_provider: deepseek
  providers:
    deepseek:
      base_url: https://api.deepseek.com/v1
      api_key: ${FV_LLM_DEEPSEEK_KEY}
      model: deepseek-chat
    glm:
      base_url: https://open.bigmodel.cn/api/paas/v4
      api_key: ${FV_LLM_GLM_KEY}
      model: glm-4
    kimi:
      base_url: https://api.moonshot.cn/v1
      api_key: ${FV_LLM_KIMI_KEY}
      model: moonshot-v1-32k
    qwen:
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      api_key: ${FV_LLM_QWEN_KEY}
      model: qwen-plus
    ollama:
      base_url: http://127.0.0.1:11434/v1
      api_key: ollama
      model: qwen2.5:7b
```

### Provider 接口的 OpenAI 实现要点（ChatWithTools）

```go
func (p *OpenAIProvider) ChatWithTools(ctx context.Context, req llm.ChatRequest,
                                       tools []llm.Tool) (*llm.ChatResponse, error) {
    oreq := openai.ChatCompletionRequest{
        Model:       p.model,
        Temperature: req.Temperature,
        MaxTokens:   req.MaxTokens,
        Messages:    toOpenAIMessages(req.Messages),
        Tools:       toOpenAITools(tools),
    }
    resp, err := p.client.CreateChatCompletion(ctx, oreq)
    if err != nil { return nil, err }
    if len(resp.Choices) == 0 { return nil, errors.New("empty choices") }
    c := resp.Choices[0]
    return &llm.ChatResponse{
        Content:      c.Message.Content,
        ToolCalls:    fromOpenAIToolCalls(c.Message.ToolCalls),
        FinishReason: string(c.FinishReason),
        Usage: llm.Usage{
            PromptTokens:     resp.Usage.PromptTokens,
            CompletionTokens: resp.Usage.CompletionTokens,
        },
    }, nil
}
```

> 业务规则细节见 `docs/domain-model.md`（AI 4 场景：问答 / 买卖建议 / 趋势分析 / 资产解读）。
> 错误处理见 `error-handling` skill。
