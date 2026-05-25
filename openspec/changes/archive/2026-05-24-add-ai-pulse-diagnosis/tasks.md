## 1. 数据库层

- [x] 1.1 新增 `PulseDiagnosis` 领域模型（`backend/internal/domain/pulse_diagnosis.go`），包含 ID/UserID/AssetID/Recommendation/**Confidence**/**Summary**/**Detail**/**DataReferences**/RawResponse/SessionID/**TriggerSource**/CreatedAt/UpdatedAt 字段
- [x] 1.2 在 GORM AutoMigrate 中注册 `PulseDiagnosis` 模型，表名 `t_fv_ai_pulse_diagnoses`，唯一索引 `(f_user_id, f_asset_id)`
- [x] 1.3 新增 `PulseDiagnosisRepository` 接口（`backend/internal/repository/interfaces.go`），包含 `Upsert/GetByUserAsset/ListByUser` 方法
- [x] 1.4 实现 GORM 版 `PulseDiagnosisRepository`（`backend/internal/repository/gorm/pulse_diagnosis_repo.go`），`Upsert` 使用 `ON CONFLICT (f_user_id, f_asset_id) DO UPDATE`

## 2. 把脉 Service 层

- [x] 2.0 在 `internal/llm/agent/` 新增 `ChatClient` 接口（`Chat(ctx, systemPrompt, userPrompt) (string, error)`），并实现 `sdkChatClient`（封装 `sdkmodel.Model.GenerateContent` 的流式聚合）
- [x] 2.1 新增 `PulseDiagnosisService`（`backend/internal/service/pulse_diagnosis_service.go`），封装 `Diagnose(ctx, userID, assetID, triggerSource)` 方法（triggerSource 枚举：manual/chat/scheduled）
- [x] 2.2 实现 `Diagnose` 方法逻辑：查询持仓数据（HoldingSummary）、行情数据（LatestPrice）、资产详情 → 构造结构化分析 prompt（含初学者角色设定 + 分层输出要求） → 调用 LLM → 解析 JSON（recommendation/confidence/summary/detail/data_references）→ Upsert 到 `t_fv_ai_pulse_diagnoses`
- [x] 2.3 数据不足场景：查不到持仓或行情时，直接返回 `recommendation=hold + confidence=low + summary="数据不足..."`，不调用 LLM
- [x] 2.4 LLM 输出解析失败重试 1 次；均失败后返回业务错误，不落库
- [x] 2.5 编写 `PulseDiagnosisService` 单元测试（mock LLM client 、repository；覆盖各类 recommendation/confidence 、数据不足、解析失败、Upsert 覆盖场景）

## 3. Agent 工具层

- [x] 3.1 新增 `pulse_diagnosis` 工具（`backend/internal/llm/tools/pulse_diagnosis.go`），遵循现有工具模式（Deps 结构体 + Args + Output + NewPulseDiagnosisTool）
- [x] 3.2 工具入参 schema：`asset_id`（单个，必填）或 `asset_ids`（数组，可选批量）
- [x] 3.3 工具内部调用 `PulseDiagnosisService.Diagnose(triggerSource="chat")`，返回 `PulseDiagnosisOutput`（含 recommendation + confidence + summary + detail）；批量场景串行调用
- [x] 3.4 编写 `pulse_diagnosis` 工具单元测试
- [x] 3.5 将 `pulse_diagnosis` 注册到 `agent.NewToolsetAgentFactory` 的 tools 列表中

## 4. REST API 层

- [x] 4.1 新增 `POST /api/v1/ai/pulse-diagnosis` 接口 handler（`backend/internal/handler/pulse_diagnosis_handler.go`），请求体 `{ asset_ids: number[] }`
- [x] 4.2 handler 内部使用 `golang.org/x/sync/errgroup` + 信号量**并行调用** `PulseDiagnosisService.Diagnose`（triggerSource="manual"），并发度取配置项 `ai.pulse_diagnosis.concurrency`（默认 3）
- [x] 4.3 单个资产失败不阻塞其他资产，响应体 `{ items: [{ asset_id, recommendation, confidence, summary, detail, diagnosed_at, trigger_source, status, error_message }] }`
- [x] 4.4 新增 `GET /api/v1/ai/pulse-diagnosis` 接口，查询参数 `asset_id`（单个）或 `asset_ids`（批量，逗号分隔），返回数据库中最近一次把脉结果
- [x] 4.5 在 `gin.RouterGroup` 上注册路由
- [x] 4.6 编写 handler 单元测试（包含并发场景、单资产失败隔离场景）

## 5. 前端 API 层

- [x] 5.1 新增前端 API 函数（`frontend/src/api/pulse-diagnosis.ts`）：`pulseDiagnosis.create(assetIds)` 和 `pulseDiagnosis.batchGet(assetIds)`
- [x] 5.2 新增前端类型定义：`PulseDiagnosisResult`（asset_id/recommendation/**confidence**/**summary**/**detail**/diagnosed_at/trigger_source）；Recommendation = "sell" | "reduce" | "hold" | "add"；Confidence = "high" | "medium" | "low"

## 6. 基金管理页改动（`frontend/src/views/asset/FundManage.vue`）

- [x] 6.1 在列表操作栏新增"AI 把脉"按钮（使用 `MagicStick` 图标）
- [x] 6.2 在表格中新增"把脉结论"列、"置信度"小标记、"简要原因"列
- [x] 6.3 "把脉结论"列用彩色 `el-tag` 显示（sell→danger、reduce→warning、hold→success、add→primary）
- [x] 6.4 置信度 `low` 时在结论旁加灰色感叹号图标 + tooltip"请谨慎参考"
- [x] 6.5 点击行可展开查看 `detail` 详细原因（`el-table` 的 expand 行或 popover）
- [x] 6.6 工具栏新增"批量 AI 把脉"按钮（选中行后启用）
- [x] 6.7 页面加载时调用 GET 接口预加载已有把脉结果（批量 `asset_ids` 查询）
- [x] 6.8 把脉请求发出后显示 loading；并行批量把脉时显示进度提示（“X/Y 完成”）
- [x] 6.9 在表格顶部加静态风险提示文案："AI 建议仅供参考，投资决策请自行判断"
- [x] 6.10 根据 `diagnosed_at` 计算距今天数，结果超过 7 天时显示"上次把脉于 X 天前"提醒

## 7. 股票管理页改动（`frontend/src/views/asset/StockManage.vue`）

- [x] 7.1 同 6.1-6.10 的改动，适配股票页面

## 8. 理财产品管理页改动（`frontend/src/views/asset/WealthManage.vue`）

- [x] 8.1 同 6.1-6.10 的改动，适配理财产品页面- [ ] 9.2 测试数据不足时的提示信息（hold + low + "数据不足..."）
- [ ] 9.3 测试批量并行把脉场景（验证并发度 3、单资产失败不阻塞其他、返回项含 status 字段）
- [ ] 9.4 测试页面刷新后把脉结果仍然显示（缓存命中，且能看到 trigger_source 和 diagnosed_at）
- [ ] 9.5 测试 AI 对话中调用 `pulse_diagnosis` 工具的场景（triggerSource=chat，验证与手动触发记录互不冲突）
