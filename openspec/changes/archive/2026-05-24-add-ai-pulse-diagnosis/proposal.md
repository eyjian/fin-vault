## Why

产品总目标是**帮助无理财投资经验的人提升投资理财能力，实现资产升值**。用户持有的基金、股票和理财产品缺乏 AI 辅助决策手段，用户需要自行判断何时卖出、加仓或减仓，这对非专业投资者门槛较高。现在系统已具备持仓数据、行情数据和 AI Agent 运行时基础设施，可以利用 AI 大模型对这些资产进行综合分析，给出"建议卖出 / 继续持有 / 建议加仓 / 建议减仓"等操作建议及原因，降低用户决策负担，同时在原因说明中**渗透投资知识**，让用户边看建议边学投资。

此外，AI 把脉消耗大量 token，因此不应自动触发把脉，而应由用户手动触发。但把脉逻辑需要可复用，以便后续扩展定时把脉等功能。

## What Changes

- 在基金管理、股票管理、理财产品管理三个页面新增"AI 把脉"功能
- 用户可以在列表中选择（单选、多选或全选）资产，然后点击"AI 把脉"按钮；也可以在单个资产的操作栏直接点击"AI 把脉"
- 把脉结果包含三部分：
  - **建议类型**：四类之一——建议卖出 / 继续持有 / 建议加仓 / 建议减仓
  - **置信度**：high / medium / low，表达模型对建议的把握程度
  - **分层原因**：
    - 简要原因（summary）——列表可见，引用关键数据
    - 详细原因（detail）——点击展开，含投资知识解释，初学者友好
- 把脉结果持久化到数据库，下次打开页面时直接显示上次的把脉结果
- 批量把脉采用并行化实现（默认并发 3），提升多资产把脉效率
- 后端新增 `pulse_diagnosis` 工具，供 Agent 调用执行把脉逻辑
- 把脉逻辑封装为可复用的 service，便于后续定时把脉等场景调用
- 记录把脉触发方式（manual / chat / scheduled）和时间戳，保证可追溯

## Capabilities

### New Capabilities

- `ai-pulse-diagnosis`: AI 把脉功能——对用户持有的基金、股票、理财产品进行 AI 综合分析，给出操作建议（建议卖出/继续持有/建议加仓/建议减仓）及分析原因

### Modified Capabilities

- `ai-tools`: 新增 `pulse_diagnosis` 工具到内置工具集合
- `ai-session`: 把脉结果需存入会话消息，并关联到资产维度便于查询

## Impact

- **后端**：新增 `pulse_diagnosis` AI 工具（`backend/internal/llm/tools/pulse_diagnosis.go`）、把脉 service 层逻辑、数据库表 `t_fv_ai_pulse_diagnoses` 存储把脉结果
- **前端**：三个资产管理页面（FundManage.vue / StockManage.vue / WealthManage.vue）新增"AI 把脉"按钮和结果展示
- **API**：新增 `POST /api/v1/ai/pulse-diagnosis` 接口
- **依赖**：依赖现有 `holding_query`、`market_quote`、`profit_calc` 工具的数据，以及 `trpc-agent-go` 的 Agent 运行时
