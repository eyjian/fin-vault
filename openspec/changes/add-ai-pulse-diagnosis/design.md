## Context

当前系统已具备以下基础设施：
- **AI Agent 运行时**：基于 `trpc-agent-go`，支持工具调用、会话管理、步骤事件持久化
- **现有 AI 工具集**：`search_fund`、`market_quote`、`history_query`、`holding_query`、`profit_calc`、`platform_summary`
- **持仓数据**：`HoldingSummary`（盈亏比率、市值等）、`HoldingView`（实时计算的持仓视图）
- **前端资产管理页面**：FundManage.vue / StockManage.vue / WealthManage.vue，均已展示持仓汇总列，操作栏有"录流水""编辑""删除"等按钮

当前痛点：
- 用户缺乏 AI 辅助决策能力，需要自行判断何时卖出/加仓/减仓
- 现有工具仅提供数据查询，不提供分析建议

## Product Goal（产品目标）

本变更服务于产品总目标：**帮助无理财投资经验的人提升投资理财能力，实现资产升值**。因此把脉功能不仅要给出建议，还要让用户在阅读建议的过程中**学到投资知识**。

## Goals / Non-Goals

**Goals:**
1. 在基金/股票/理财产品页面新增"AI 把脉"入口，支持单选/多选/全选后批量把脉
2. 把脉结果分为四类：建议卖出、继续持有、建议加仓、建议减仓；每个结果附带 **置信度**（high / medium / low）
3. **把脉原因分层展示**：
   - 简要层（列表可见）：关键数据引用 + 结论，如"近一年亏损 25%、PE 远高于同类均值 → 建议卖出"
   - 详细层（点击展开）：投资知识解释 + 完整分析逻辑，帮助初学者理解术语和推理过程
4. 把脉结果持久化到数据库，下次打开直接显示上次结果；用户可手动"重新把脉"刷新
5. 把脉逻辑可复用（封装为独立 service），便于后续定时把脉扩展
6. **可解释性强**：原因必须引用具体数据（涨跌幅、PE、盈亏比率、持仓时长等），避免泛泛而谈
7. **批量把脉支持并行化**，提升处理效率（多资产并发调用 LLM）
8. **把脉结果可追溯**：记录把脉时间戳和触发方式（manual / chat / scheduled）

**Non-Goals:**
- 不自动触发把脉（token 消耗大，用户手动触发）
- 不实时推送把脉结果（结果只在用户请求时返回）
- 不做把脉结果的历史版本管理（只保留最新一次）
- 不做"一键执行"把脉建议的操作（如自动卖出），只提供参考建议

## Decisions

### D1: 把脉结果存储模型

**决策**：新建 `t_fv_ai_pulse_diagnoses` 表，每条记录关联一个 asset_id，存储把脉结果（建议类型+原因+时间）。

**理由**：把脉结果是资产维度的，一个资产只有一条最新记录（后续定时把脉时覆盖更新）。不与 AI 会话消息耦合，因为把脉结果需要在资产管理页面直接展示，而非只在聊天窗口展示。

**备选方案**：
- A) 把脉结果存在 AI 会话消息中 → 不利于资产管理页面直接读取展示
- B) 把脉结果存在 Asset 表字段中 → 违反单一职责，Asset 是主数据不应承载分析结果

### D2: 把脉实现方式——Agent 工具 vs 独立 Service

**决策**：新增 `pulse_diagnosis` Agent 工具 + `PulseDiagnosisService` service 层。Agent 工具负责定义入参 schema 和声明，service 层封装可复用的把脉逻辑（查询持仓/行情/盈亏数据，构造分析 prompt，调用 LLM）。

**理由**：
- 作为 Agent 工具，可在 AI 对话中由模型自主调用（用户说"帮我把脉一下"即可触发）
- 作为独立 service，可被定时任务或其他入口直接调用，逻辑可复用

**备选方案**：
- A) 只做 Agent 工具，不做 service → 定时把脉无法复用
- B) 只做 service，不做 Agent 工具 → AI 对话中无法自然触发把脉

### D3: 把脉 prompt 构造（结构化 + 教育性 + 可解释性）

**决策**：把脉 service 内部组装结构化 prompt，将持仓数据（数量/成本/市值/盈亏比率）、行情数据（最新价/涨跌幅）、资产基本信息（类型/名称/基金类型/经理等）注入 prompt，要求模型按固定 JSON 格式输出结果。

LLM 输出 JSON schema：
```json
{
  "recommendation": "sell | reduce | hold | add",
  "confidence": "high | medium | low",
  "summary": "简要原因（≤80 字，列表可见，必须引用具体数据，如\"近一年亏损 25%、PE 高于同类均值\"）",
  "detail": "详细分析（≥150 字，含投资知识解释，初学者友好，含术语解释如\"PE = 市盈率，每 1 元利润对应的价格\"）",
  "data_references": [{ "metric": "yearly_return", "value": -0.25 }, ...]
}
```

**理由**：
- 结构化输出便于解析和持久化，避免自由文本解析不稳定
- `summary` 用于列表简要展示（Goal 3 简要层）；`detail` 用于展开后教育性展示（Goal 3 详细层）
- `data_references` 显式列出引用的数据，确保"可解释性强"（Goal 6），便于前端高亮关键指标
- prompt 内含"目标用户为无理财经验初学者"的角色设定，引导 LLM 在 detail 中加入术语解释

### D4: 把脉结果分类逻辑（4 类 + 置信度）

**决策**：把脉结果分类由 LLM 判断，prompt 中给出四分类定义和判断依据的指引，但不硬编码规则（避免规则过于僵化，让 LLM 综合判断）。同时要求 LLM 输出 **置信度**（high / medium / low），以表达对该建议的把握程度。

**分类定义**：
- **建议卖出**（sell）：亏损严重、趋势下行、基本面恶化
- **建议减仓**（reduce）：盈利较多可部分止盈、估值偏高、风险增大
- **继续持有**（hold）：表现稳定、估值合理、无明显操作信号
- **建议加仓**（add）：低估优质、趋势向好、回调即机会

**置信度定义**：
- **high**：数据充分、信号明确，建议可信度高
- **medium**：数据较完整、但存在一定不确定性
- **low**：数据不足或市场信号矛盾，建议仅供参考；前端可在 UI 上提示"请谨慎参考"

**理由**：保持四分类简洁不变，确保 LLM 判断准确；置信度可表达模型对结果的把握程度，给用户额外的决策参考。前端展示时，置信度 low 的结果会有视觉提示（如灰色或感叹号图标）。

**备选方案（已弃用）**：
- A) 增加第 5 类"观望" → "观望"和"继续持有"边界模糊，LLM 易混淆
- B) 细化为 6 类（坚定持有/谨慎持有/逐步减仓/大幅减仓等） → 颗粒度过细，LLM 区分准确性下降

### D5: 批量把脉实现（并行化）

**决策**：批量把脉时**并行调用** `PulseDiagnosisService.Diagnose`（每次一个资产），而非串行或一次性把所有资产信息塞进一个 prompt。并发度由配置项控制（默认 3，可调），避免触达 LLM 提供方的 RPM 限制。

**理由**：
- 每个 asset 的持仓/行情/盈亏数据需要单独查询和组装
- 单次 prompt token 限制，批量资产信息可能超限
- 并行化可显著降低批量把脉总耗时（10 个资产串行 30s+ → 并行可降至 10s 左右）
- 在 REST API 层并行（`errgroup` + 信号量）；在 Agent 工具层仍逐个调用（Agent 串行控制更稳定）

**实现要点**：
- REST API handler 收到 `asset_ids: [N1,N2,...]` 后，使用 `golang.org/x/sync/errgroup` + 信号量限流并发调用 service.Diagnose
- 任一资产失败不阻塞其他资产，最终返回每个资产的成功/失败状态
- 配置项：`ai.pulse_diagnosis.concurrency`（默认 3）

### D6: 前端把脉结果展示

**决策**：在资产管理列表中新增"把脉结论"列和"把脉原因"列（默认折叠，点击展开），同时在操作栏新增"AI 把脉"按钮。列表选中行后，工具栏出现"批量 AI 把脉"按钮。

**理由**：与现有页面风格一致（盈亏列的颜色标注方式），用户无需进入新页面即可看到结果。

### D7: 把脉触发方式

**决策**：
1. **单资产把脉**：操作栏"AI 把脉"按钮 → 创建/复用 AI 会话 → 发送把脉指令 → 展示结果
2. **批量把脉**：选中多个资产 → 工具栏"批量 AI 把脉"按钮 → 同上流程
3. **前端直调 REST API**：新增 `POST /api/v1/ai/pulse-diagnosis` 接口，前端可直接调用（不经过 AI 聊天窗口），调用后自动在 AI 会话中记录把脉过程

**理由**：用户在资产管理页面操作时，不一定要切换到 AI 聊天窗口。提供 REST API 直调方式更方便。

### D8: 把脉结果持久化

**决策**：把脉结果在 service 层写入 `t_fv_ai_pulse_diagnoses` 表，同时在 AI 会话消息中也记录把脉过程（作为 assistant 消息的一部分）。

**字段设计**：
- `f_id`：UUID 主键
- `f_user_id`：用户 ID
- `f_asset_id`：资产 ID
- `f_recommendation`：建议类型（sell / reduce / hold / add）
- `f_confidence`：置信度（high / medium / low）
- `f_summary`：简要原因（≤80 字，列表展示）
- `f_detail`：详细原因（含投资知识解释，展开展示）
- `f_data_references`：引用的关键数据（JSON 数组，可选，便于前端高亮）
- `f_raw_response`：LLM 原始 JSON 响应（保底，便于排查）
- `f_session_id`：关联的 AI 会话 ID
- `f_trigger_source`：触发方式（manual / chat / scheduled），支持 Goal 8 可追溯
- `f_created_at`：首次创建时间
- `f_updated_at`：最近更新时间（同一资产重新把脉时覆盖）

唯一约束：`(f_user_id, f_asset_id)`——每个用户的每个资产只保留最新一条把脉结果。

### D9: 把脉逻辑可复用设计

**决策**：把脉核心逻辑封装在 `PulseDiagnosisService.Diagnose(ctx, userID, assetID, triggerSource)` 方法中，可被以下场景调用：
- Agent 工具 `pulse_diagnosis`（AI 对话中调用）
- REST API handler（前端直调）
- 未来定时任务（ScheduledPulse）

**理由**：三种入口共享同一套把脉逻辑，避免重复实现。

### D11: 把脉 LLM 调用封装（避免 service 层 import SDK）

**决策**：在 `internal/llm/agent` 包新增业务侧接口 `ChatClient`：

```go
type ChatClient interface {
    Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}
```

由 `agent` 包提供 `NewSDKChatClient(model sdkmodel.Model) ChatClient` 实现（封装 `model.GenerateContent` 流式聚合与 token 用量解析）。

`PulseDiagnosisService` 只 import `agent.ChatClient`，不直接接触 SDK；bootstrap 在 `wireAI` 内构造 SDK Model 后顺便构造 `ChatClient` 注入 service。

**理由**：
- 把脉是单轮对话（非工具调用循环），不需要复用 `agent.Runner`（Runner 是基于 session 的多轮 + tool calling 流程）
- 直接调用 SDK `Model.GenerateContent` 比走 Runner 链路更轻量，省去工具集成与多轮迭代的开销
- 通过新接口隔离 SDK，service 层不破坏铁律 F2（0 SDK 命中）
- `ChatClient` 也可被未来其他单轮 LLM 任务复用（如基金摘要生成、风险提示重写）

**降级**：D16 LLM 不可用时，`PulseDiagnosisService` 也应降级为不可用（与 `AIMessageService` 同策略），handler 路由有 nil-check 兜底。

### D10: 前端 API 接口设计

**决策**：新增以下 API：

1. `POST /api/v1/ai/pulse-diagnosis`
   - 请求体：`{ asset_ids: number[] }`（支持批量）
   - 响应体：`{ items: [{ asset_id, recommendation, reason, diagnosed_at }] }`
   - 该接口创建/复用 AI 会话，在会话中发送把脉指令，返回结果

2. `GET /api/v1/ai/pulse-diagnosis`
   - 查询参数：`asset_id`（单个）
   - 响应体：`{ asset_id, recommendation, reason, diagnosed_at }`
   - 读取数据库中最近一次把脉结果（不触发新把脉）

**理由**：POST 触发把脉，GET 读取上次结果（满足"下次打开直接显示"需求）。

## Risks / Trade-offs

- **[Token 消耗大]** → 每次把脉需要调用 LLM 分析，消耗大量 token。缓解：不自动把脉，仅用户手动触发；把脉结果缓存到数据库，避免重复把脉
- **[LLM 输出不稳定]** → 把脉分类/置信度结果可能不一致。缓解：结构化 prompt + JSON schema 约束 + 重试机制（最多 1 次）；解析失败时返回业务错误而非降级假数据
- **[批量把脉耗时长]** → 逐个资产把脉串行耗时长。缓解：D5 并行化（默认并发 3）；前端显示进度
- **[数据不足]** → 新录入的资产可能没有持仓数据或行情数据。缓解：把脉 service 在数据不足时返回明确提示（recommendation=hold + confidence=low + summary="数据不足，建议补全后再把脉"），不勉强给出建议
- **[LLM 给出错误建议导致用户损失]** → 模型不是金融专家，可能给错建议。缓解：UI 明确标注"AI 建议仅供参考，投资决策请自行判断"；置信度 low 时视觉强提示；不提供"一键执行"操作（Non-Goal）
- **[置信度自评不准]** → LLM 可能高估自己的把握。缓解：prompt 中明确给出 high/medium/low 的判定标准（数据完整度 + 信号一致性）；后续可结合实际收益反向校准（v2 迭代）

## Migration Plan

1. 后端先实现 `pulse_diagnosis` 工具和 `PulseDiagnosisService`
2. 新增数据库表 `t_fv_ai_pulse_diagnoses` 并 AutoMigrate
3. 新增 REST API 端点（POST/GET）
4. 前端三个资产管理页面新增"AI 把脉"按钮和结果展示列
5. 注册 `pulse_diagnosis` 工具到 Agent 工具集
6. 端到端测试

## Open Questions

- 把脉结果是否需要支持"重新把脉"（手动刷新）？—— 当前设计支持，同一资产再次调用 POST 即覆盖
- 是否需要把脉结果的"置信度"字段？—— **已确认加入**，字段 `f_confidence`（high/medium/low）
- 批量并发度 3 是否合适？—— v1 默认 3，根据实际 LLM 提供方 RPM 调整
- `data_references` 是否需要前端结构化展示？—— v1 仅落库保留，前端展示由 `summary` 自然语言带出；v2 可考虑"指标卡片"
