// Package service —— PulseDiagnosisService（AI 把脉）实现。
//
// 设计要点（与 design.md D2/D3/D4/D5/D9/D11 + spec ai-pulse-diagnosis 对齐）：
//   - D9 可复用：Diagnose 是唯一公开入口，被 REST handler / Agent 工具 / 未来定时任务共享
//   - D11 SDK 隔离：通过 agent.ChatClient 调 LLM，不 import trpc-agent-go SDK
//   - D3 prompt 结构化：system 设定初学者教育角色 + 强约束 JSON 输出 schema
//   - D4 四分类 + 置信度：LLM 输出 recommendation/confidence/summary/detail/data_references
//   - 数据不足兜底：查不到持仓/行情时不调 LLM，直接返 hold + low + 提示性 summary（不落库）
//   - 解析失败重试 1 次；均失败返 ErrAIPulseParseFailed，不落库
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/agent"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// 入参 / 返回值
// =====================================================================

// PulseDiagnoseInput 把脉入参。
type PulseDiagnoseInput struct {
	UserID        uint
	AssetID       uint
	TriggerSource domain.PulseTriggerSource // manual / chat / scheduled
	SessionID     string                    // 可选：chat 路径关联到具体会话；其它路径留空串
}

// PulseDiagnoseResult 把脉结果（service 层视图，含落库时间用于"距今 N 天"提示）。
type PulseDiagnoseResult struct {
	AssetID        uint                       `json:"asset_id"`
	Recommendation domain.PulseRecommendation `json:"recommendation"`
	Confidence     domain.PulseConfidence     `json:"confidence"`
	Summary        string                     `json:"summary"`
	Detail         string                     `json:"detail"`
	DataReferences json.RawMessage            `json:"data_references,omitempty"`
	SessionID      string                     `json:"session_id,omitempty"`
	TriggerSource  domain.PulseTriggerSource  `json:"trigger_source"`
	DiagnosedAt    time.Time                  `json:"diagnosed_at"`
}

// =====================================================================
// LLM 输出解析模型（内部）
// =====================================================================

// llmPulseOutput 严格对应 prompt 中要求 LLM 输出的 JSON schema。
type llmPulseOutput struct {
	Recommendation string                   `json:"recommendation"`
	Confidence     string                   `json:"confidence"`
	Summary        string                   `json:"summary"`
	Detail         string                   `json:"detail"`
	DataReferences []map[string]interface{} `json:"data_references"`
}

// =====================================================================
// PulseDiagnosisService
// =====================================================================

// PulseDiagnosisService AI 把脉服务。
//
// 当 chat == nil 时（D16 LLM 不可用降级），所有 Diagnose 调用直接返
// errs.ErrAIPulseUnavailable，handler 透传，前端展示降级提示。
type PulseDiagnosisService struct {
	chat       agent.ChatClient
	holdingSvc *HoldingService
	assetRepo  repository.AssetRepository
	pulseRepo  repository.PulseDiagnosisRepository
	quoteRepo  repository.QuoteRepository
}

// NewPulseDiagnosisService 构造。
//
// 参数 chat 允许为 nil（降级路径）；service 在 Diagnose 内自感知并返业务错误。
func NewPulseDiagnosisService(
	chat agent.ChatClient,
	holdingSvc *HoldingService,
	assetRepo repository.AssetRepository,
	pulseRepo repository.PulseDiagnosisRepository,
	quoteRepo repository.QuoteRepository,
) *PulseDiagnosisService {
	return &PulseDiagnosisService{
		chat:       chat,
		holdingSvc: holdingSvc,
		assetRepo:  assetRepo,
		pulseRepo:  pulseRepo,
		quoteRepo:  quoteRepo,
	}
}

// IsAvailable 上层（handler / 路由注册）可据此决定是否暴露相关接口。
func (s *PulseDiagnosisService) IsAvailable() bool {
	return s != nil && s.chat != nil
}

// =====================================================================
// tools.PulseDiagnoser 适配（避免 internal/llm/tools 反向 import service）
// =====================================================================
//
// 这里不直接 import internal/llm/tools 包以保持依赖方向 service ← tools 单向；
// 而是把适配器作为独立类型放在 internal/llm/tools 同级（实际由 bootstrap 装配，
// 见 wire_ai.go）。本文件只暴露 service 内部需要的工具方法 DiagnoseForTool，
// 让适配器（在别处定义）调用。

// DiagnoseForTool 是给 Agent 工具层的语义透明入口。
//
// 与 Diagnose 的差别仅在于：返回值是 *PulseDiagnoseResult（与 Diagnose 一致）；
// 适配器负责把它再映射到 tools.PulseDiagnoseResult。本方法存在的意义是给 wire
// 层一个清晰的"工具入口"语义锚点（避免后续误把把脉改成多入口）。
func (s *PulseDiagnosisService) DiagnoseForTool(ctx context.Context, userID, assetID uint) (*PulseDiagnoseResult, error) {
	return s.Diagnose(ctx, PulseDiagnoseInput{
		UserID:        userID,
		AssetID:       assetID,
		TriggerSource: domain.PulseTriggerChat,
	})
}

// GetCached 取数据库中最近一次把脉结果，不触发新把脉；未把脉过返回 (nil, nil)。
//
// 用于 GET /api/v1/ai/pulse-diagnosis 接口与资产管理页预加载场景。
func (s *PulseDiagnosisService) GetCached(ctx context.Context, userID, assetID uint) (*PulseDiagnoseResult, error) {
	if userID == 0 || assetID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id and asset_id required")
	}
	d, err := s.pulseRepo.GetByUserAsset(ctx, userID, assetID)
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	if d == nil {
		return nil, nil
	}
	return diagnosisToResult(d), nil
}

// ListCached 批量取已有把脉结果（资产管理页预加载）。
//
// assetIDs 为空 → 返回该用户全部把脉结果；非空 → 仅返回这些资产的记录。
func (s *PulseDiagnosisService) ListCached(ctx context.Context, userID uint, assetIDs []uint) ([]PulseDiagnoseResult, error) {
	if userID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id required")
	}
	list, err := s.pulseRepo.ListByUser(ctx, userID, assetIDs)
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	out := make([]PulseDiagnoseResult, 0, len(list))
	for i := range list {
		out = append(out, *diagnosisToResult(&list[i]))
	}
	return out, nil
}

// Diagnose 执行一次 AI 把脉。
//
// 步骤：
//  1. 入参校验
//  2. 拉数据：资产基本信息（含 detail）+ 持仓汇总（HoldingSummary）+ 最新行情
//  3. 数据不足兜底：无持仓 → 返 hold + low + "数据不足..."（不落库）
//  4. 构造 prompt（system 角色 + user JSON 数据）
//  5. 调 LLM，解析 JSON；解析失败重试 1 次；均失败返 ErrAIPulseParseFailed
//  6. Upsert 到 t_fv_ai_pulse_diagnoses，返回结果
//
// 错误透传：LLM 错误（ErrAIRequestFailed / ErrAIProviderRateLimited）由 ChatClient 抛出。
func (s *PulseDiagnosisService) Diagnose(ctx context.Context, in PulseDiagnoseInput) (*PulseDiagnoseResult, error) {
	if in.UserID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id required")
	}
	if in.AssetID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("asset_id required")
	}
	if !in.TriggerSource.IsValid() {
		in.TriggerSource = domain.PulseTriggerManual
	}
	if !s.IsAvailable() {
		return nil, errs.ErrAIPulseUnavailable
	}

	// 1) 资产基本信息（含 detail，由 GetByID 内部按需 Preload；如果当前实现未 Preload，
	// 这里只用主表字段也够 prompt 用，detail 缺失时 LLM 仍可基于持仓 + 行情判断）
	asset, err := s.assetRepo.GetByID(ctx, in.UserID, in.AssetID)
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	if asset == nil {
		return nil, errs.ErrAssetNotFound
	}

	// 2) 持仓汇总（跨平台汇总，含市值/盈亏/分红）
	holdingSummary, err := s.holdingSvc.GetSummaryByAsset(ctx, in.UserID, in.AssetID)
	if err != nil {
		return nil, err
	}

	// 3) 数据不足兜底：完全无持仓时不调 LLM，直接返提示信息（不落库，避免污染缓存）
	if holdingSummary == nil || holdingSummary.Quantity.IsZero() {
		return &PulseDiagnoseResult{
			AssetID:        in.AssetID,
			Recommendation: domain.PulseRecHold,
			Confidence:     domain.PulseConfLow,
			Summary:        "数据不足，建议补全持仓与行情后再把脉",
			Detail:         "当前未查询到该资产的持仓数据。AI 把脉依赖持仓汇总（数量/成本/盈亏）和最新行情综合判断，数据缺失时无法给出有效建议。请先录入持仓流水或刷新行情，再发起把脉。",
			TriggerSource:  in.TriggerSource,
			SessionID:      in.SessionID,
			DiagnosedAt:    time.Now(),
		}, nil
	}

	// 4) 最新行情（可能 nil，prompt 中作为可选输入；持仓汇总已自带 LatestPrice/MarketValue）
	latestQuote, _ := s.quoteRepo.GetLatest(ctx, in.AssetID)

	// 5) 构造 prompt
	systemPrompt := pulseSystemPrompt
	userPrompt := buildPulseUserPrompt(asset, holdingSummary, latestQuote)

	// 6) 调 LLM（解析失败重试 1 次）
	var raw string
	var parsed *llmPulseOutput
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		raw, lastErr = s.chat.Chat(ctx, systemPrompt, userPrompt)
		if lastErr != nil {
			// 业务错误（rate limit / request failed）直接返，无需重试
			return nil, lastErr
		}
		parsed, lastErr = parsePulseOutput(raw)
		if lastErr == nil {
			break
		}
	}
	if parsed == nil {
		return nil, errs.ErrAIPulseParseFailed.WithCause(lastErr)
	}

	// 7) 校验枚举值
	rec := domain.PulseRecommendation(strings.ToLower(strings.TrimSpace(parsed.Recommendation)))
	if !rec.IsValid() {
		return nil, errs.ErrAIPulseParseFailed.WithMsg("invalid recommendation: " + parsed.Recommendation)
	}
	conf := domain.PulseConfidence(strings.ToLower(strings.TrimSpace(parsed.Confidence)))
	if !conf.IsValid() {
		// 置信度缺失时降级为 medium，不视为致命错误（部分模型可能漏字段）
		conf = domain.PulseConfMedium
	}

	dataRefs, _ := json.Marshal(parsed.DataReferences) // 失败给空，不阻塞主流程

	// 8) Upsert
	d := &domain.PulseDiagnosis{
		ID:             uuid.NewString(), // 仅新建路径生效（Upsert 命中时保留原 ID）
		UserID:         in.UserID,
		AssetID:        in.AssetID,
		Recommendation: rec,
		Confidence:     conf,
		Summary:        strings.TrimSpace(parsed.Summary),
		Detail:         strings.TrimSpace(parsed.Detail),
		DataReferences: dataRefs,
		RawResponse:    raw,
		SessionID:      in.SessionID,
		TriggerSource:  in.TriggerSource,
	}
	if err := s.pulseRepo.Upsert(ctx, d); err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}

	// 9) 重新读取以拿到 ID/CreatedAt/UpdatedAt（Upsert 后 d 的字段可能未回填）
	saved, err := s.pulseRepo.GetByUserAsset(ctx, in.UserID, in.AssetID)
	if err != nil || saved == nil {
		// 读不回不致命，构造最小返回
		return &PulseDiagnoseResult{
			AssetID:        in.AssetID,
			Recommendation: rec,
			Confidence:     conf,
			Summary:        d.Summary,
			Detail:         d.Detail,
			DataReferences: dataRefs,
			SessionID:      in.SessionID,
			TriggerSource:  in.TriggerSource,
			DiagnosedAt:    time.Now(),
		}, nil
	}
	return diagnosisToResult(saved), nil
}

// =====================================================================
// Prompt 构造
// =====================================================================

// pulseSystemPrompt 系统 prompt：角色设定 + 输出 schema 强约束。
//
// 设计要点（与 design.md D3/D4 + spec ai-pulse-diagnosis "分层原因展示" 对齐）：
//   - 角色：面向"无理财投资经验的初学者"的投资顾问助手
//   - 输出严格 JSON（不要 markdown 代码块），便于 service 直接 json.Unmarshal
//   - summary ≤ 80 字（列表显示），detail ≥ 150 字（含投资知识解释，初学者友好）
//   - data_references 显式列出引用的关键指标（涨跌幅 / 估值 / 盈亏比率等）
const pulseSystemPrompt = `你是 fin-vault 个人理财助手中的"AI 把脉"专家，目标是帮助"没有理财投资经验的初学者"做出投资决策并学习投资知识。

请基于用户提供的【资产信息】、【持仓汇总】、【最新行情】，对该资产给出操作建议。

【输出要求】严格输出一个 JSON 对象（不要使用 markdown 代码块，不要任何前缀或后缀解释），字段如下：

{
  "recommendation": "sell | reduce | hold | add",     // 四类之一
  "confidence":     "high | medium | low",            // 置信度
  "summary":        "简要原因（≤80 字，必须引用具体数据，例如 '近一年亏损 25%，PE 高于同类均值 → 建议卖出'）",
  "detail":         "详细分析（≥150 字，含投资知识解释，对初学者友好。请在适当处嵌入术语解释，例如 'PE = 市盈率，每 1 元利润对应的价格'）",
  "data_references": [
    { "metric": "指标英文名", "value": "数值", "note": "中文说明（可选）" }
  ]
}

【建议分类指引】
- sell（建议卖出）：亏损严重 / 趋势下行 / 基本面恶化
- reduce（建议减仓）：盈利较多可部分止盈 / 估值偏高 / 风险增大
- hold（继续持有）：表现稳定 / 估值合理 / 无明显操作信号
- add（建议加仓）：低估优质 / 趋势向好 / 回调即机会

【置信度判定】
- high：数据充分、信号明确
- medium：数据较完整但存在不确定性
- low：数据不足或市场信号矛盾

【风格】
- 中性、客观，不使用绝对化表述
- 必须基于提供的数据，禁止编造数值
- 在 detail 中适当嵌入投资术语解释（如 PE / 仓位 / 盈亏比率），帮助初学者学习`

// buildPulseUserPrompt 构造 user prompt（结构化资产 + 持仓 + 行情数据）。
func buildPulseUserPrompt(asset *domain.Asset, summary *domain.HoldingSummary, latest *domain.PriceQuote) string {
	var sb strings.Builder
	sb.WriteString("请对以下资产进行 AI 把脉：\n\n")

	// 资产基本信息
	sb.WriteString("【资产信息】\n")
	sb.WriteString(fmt.Sprintf("- 资产代码: %s\n", asset.AssetCode))
	sb.WriteString(fmt.Sprintf("- 资产名称: %s\n", asset.Name))
	sb.WriteString(fmt.Sprintf("- 资产类型: %s\n", asset.AssetType))
	sb.WriteString(fmt.Sprintf("- 计价币种: %s\n", asset.Currency))
	if asset.RiskLevel != "" {
		sb.WriteString(fmt.Sprintf("- 风险等级: %s\n", asset.RiskLevel))
	}
	if asset.FundDetail != nil {
		sb.WriteString(fmt.Sprintf("- 基金类型: %s\n", asset.FundDetail.FundType))
		if asset.FundDetail.Manager != "" {
			sb.WriteString(fmt.Sprintf("- 基金经理: %s\n", asset.FundDetail.Manager))
		}
		if asset.FundDetail.Company != "" {
			sb.WriteString(fmt.Sprintf("- 基金公司: %s\n", asset.FundDetail.Company))
		}
	}
	if asset.StockDetail != nil {
		sb.WriteString(fmt.Sprintf("- 上市市场: %s\n", asset.StockDetail.Market))
		if asset.StockDetail.Industry != "" {
			sb.WriteString(fmt.Sprintf("- 所属行业: %s\n", asset.StockDetail.Industry))
		}
	}
	if asset.WealthDetail != nil {
		sb.WriteString(fmt.Sprintf("- 产品类型: %s\n", asset.WealthDetail.ProductType))
		if !asset.WealthDetail.ExpectedYield.IsZero() {
			sb.WriteString(fmt.Sprintf("- 预期收益率: %s\n", asset.WealthDetail.ExpectedYield.String()))
		}
	}

	// 持仓汇总
	sb.WriteString("\n【持仓汇总】（跨平台汇总）\n")
	sb.WriteString(fmt.Sprintf("- 持有数量: %s\n", summary.Quantity.String()))
	sb.WriteString(fmt.Sprintf("- 平均成本: %s\n", summary.AvgCost.String()))
	sb.WriteString(fmt.Sprintf("- 总成本: %s\n", summary.TotalCost.String()))
	if !summary.LatestPrice.IsZero() {
		sb.WriteString(fmt.Sprintf("- 最新价/净值: %s\n", summary.LatestPrice.String()))
		sb.WriteString(fmt.Sprintf("- 当前市值: %s\n", summary.MarketValue.String()))
		sb.WriteString(fmt.Sprintf("- 未实现盈亏: %s\n", summary.UnrealizedPnL.String()))
		sb.WriteString(fmt.Sprintf("- 总盈亏: %s\n", summary.TotalPnL.String()))
		sb.WriteString(fmt.Sprintf("- 盈亏比率: %s\n", summary.PnLRatio.String()))
	} else {
		sb.WriteString("- (无最新行情数据)\n")
	}
	if !summary.RealizedPnL.IsZero() {
		sb.WriteString(fmt.Sprintf("- 已实现盈亏: %s\n", summary.RealizedPnL.String()))
	}
	if !summary.TotalDividend.IsZero() {
		sb.WriteString(fmt.Sprintf("- 累计分红/利息: %s\n", summary.TotalDividend.String()))
	}

	// 最新行情（如果 latest 非 nil 且与 summary 提供的字段不重复，可加入时间）
	if latest != nil {
		sb.WriteString("\n【最新行情】\n")
		sb.WriteString(fmt.Sprintf("- 最新价: %s\n", latest.Price.String()))
		if !latest.QuoteTime.IsZero() {
			sb.WriteString(fmt.Sprintf("- 行情时间: %s\n", latest.QuoteTime.Format("2006-01-02 15:04:05")))
		}
	}

	sb.WriteString("\n请按系统约定的 JSON 格式输出把脉结果。")
	return sb.String()
}

// =====================================================================
// LLM 输出解析
// =====================================================================

// jsonObjectRe 用于从模型回复中抽取第一个 {...} JSON 对象（容错：部分模型会
// 在 JSON 前后多输出 markdown 代码块或思考过程）。非贪婪匹配第一个 {...}。
var jsonObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// parsePulseOutput 解析 LLM 回复为 llmPulseOutput。
//
// 容错策略：
//  1. 直接 json.Unmarshal 整个回复
//  2. 失败时用正则抽取第一个 {...} 子串再 Unmarshal
//  3. 仍失败则返 error（service 会重试一次）
func parsePulseOutput(raw string) (*llmPulseOutput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty llm output")
	}

	// 去除可能的 markdown 代码块标记
	if strings.HasPrefix(raw, "```") {
		// 去掉首行 ```json 或 ```
		if idx := strings.Index(raw, "\n"); idx >= 0 {
			raw = raw[idx+1:]
		}
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}

	var out llmPulseOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return &out, nil
	}

	// 抽取第一个 JSON 对象再尝试
	if match := jsonObjectRe.FindString(raw); match != "" {
		if err := json.Unmarshal([]byte(match), &out); err == nil {
			return &out, nil
		}
	}
	return nil, fmt.Errorf("llm output not valid json")
}

// =====================================================================
// 工具：domain → service result 视图转换
// =====================================================================

// diagnosisToResult 把 domain.PulseDiagnosis 转为 service 层视图。
func diagnosisToResult(d *domain.PulseDiagnosis) *PulseDiagnoseResult {
	return &PulseDiagnoseResult{
		AssetID:        d.AssetID,
		Recommendation: d.Recommendation,
		Confidence:     d.Confidence,
		Summary:        d.Summary,
		Detail:         d.Detail,
		DataReferences: d.DataReferences,
		SessionID:      d.SessionID,
		TriggerSource:  d.TriggerSource,
		DiagnosedAt:    d.UpdatedAt,
	}
}
