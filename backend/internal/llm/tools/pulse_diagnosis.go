// Package tools —— pulse_diagnosis 工具（spec ai-tools "pulse_diagnosis 对单/批量资产把脉"）。
//
// 设计要点（与 design.md D2/D9 + spec ai-tools 对齐）：
//   - D13 规则 1：入参 schema 不含 user_id，身份从 ctx 注入（tools.UserIDFromContext）
//   - 工具内部调 PulseDiagnoser.Diagnose(triggerSource="chat")，由 service 完成
//     prompt 构造、LLM 调用、Upsert
//   - 通过 PulseDiagnoser 接口反向解耦：tools 不 import service，避免与 ai_message_service
//     的 tools.WithUserID 形成循环依赖；bootstrap 装配时把 *service.PulseDiagnosisService
//     适配为 PulseDiagnoser 注入
//   - 批量场景串行调用（Agent 工具层稳定优先；REST API 层走并行，由 handler 实现，与本工具不冲突）
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// PulseDiagnoseRequest 工具 → 业务的把脉请求（最小载荷，避免 import service 包）。
type PulseDiagnoseRequest struct {
	UserID        uint
	AssetID       uint
	TriggerSource string // "manual" / "chat" / "scheduled"
	SessionID     string
}

// PulseDiagnoseResult 业务 → 工具的把脉结果（最小载荷，与 service.PulseDiagnoseResult 字段一致）。
type PulseDiagnoseResult struct {
	AssetID        uint
	Recommendation string
	Confidence     string
	Summary        string
	Detail         string
	DataReferences json.RawMessage
	SessionID      string
	TriggerSource  string
	DiagnosedAt    time.Time
}

// PulseDiagnoser 业务侧把脉接口。
//
// bootstrap 把 *service.PulseDiagnosisService 适配为本接口注入工具；该适配让
// internal/llm/tools 不必 import internal/service，避免循环依赖。
type PulseDiagnoser interface {
	// Diagnose 执行一次把脉。
	Diagnose(ctx context.Context, req PulseDiagnoseRequest) (*PulseDiagnoseResult, error)
	// IsAvailable 反映 LLM 是否可用（D16 降级时返回 false，工具直接返错）。
	IsAvailable() bool
}

// PulseDiagnosisDeps 把脉工具依赖。
type PulseDiagnosisDeps struct {
	Pulse PulseDiagnoser
}

// PulseDiagnosisArgs LLM 传入参数。
//
// asset_id 与 asset_ids 二选一：
//   - asset_id 非 0：单资产把脉
//   - asset_ids 非空：批量把脉（串行）
//   - 都为空：返参数错误
type PulseDiagnosisArgs struct {
	AssetID  uint   `json:"asset_id,omitempty"  jsonschema:"description=单个资产 ID（与 asset_ids 二选一）"`
	AssetIDs []uint `json:"asset_ids,omitempty" jsonschema:"description=批量资产 ID 列表（与 asset_id 二选一）"`
}

// PulseDiagnosisItem 单个资产的把脉结果（LLM 可见）。
type PulseDiagnosisItem struct {
	AssetID        uint   `json:"asset_id"`
	Recommendation string `json:"recommendation"` // sell / reduce / hold / add
	Confidence     string `json:"confidence"`     // high / medium / low
	Summary        string `json:"summary"`        // 简要原因（可在对话中直接复述）
	Detail         string `json:"detail"`         // 详细分析（含投资知识解释）
	Status         string `json:"status"`         // success / failed
	ErrorMessage   string `json:"error_message,omitempty"`
}

// PulseDiagnosisOutput 把脉工具返回。
type PulseDiagnosisOutput struct {
	Items []PulseDiagnosisItem `json:"items"`
	Count int                  `json:"count"`
}

// NewPulseDiagnosisTool 构造 pulse_diagnosis 工具。
//
// D13 规则：身份从 ctx 提取，提取失败直接返错；调用 PulseDiagnoser.Diagnose
// 时透传 ctx + userID + assetID，业务实现完成所有把脉逻辑（含落库）。
//
// 错误处理：单资产模式失败也只填到 Items[0]，整体返回不报错（让 Agent 把"部分失败"
// 当成正常返回值，按 summary 内容回答用户）。这与 spec ai-tools "失败工具调用也返回"
// 设计一致。
func NewPulseDiagnosisTool(deps PulseDiagnosisDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args PulseDiagnosisArgs) (PulseDiagnosisOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return PulseDiagnosisOutput{}, fmt.Errorf("user_id not in context: %w", errs.ErrAIToolCallFailed)
			}
			if deps.Pulse == nil || !deps.Pulse.IsAvailable() {
				return PulseDiagnosisOutput{}, fmt.Errorf("pulse diagnosis service unavailable: %w", errs.ErrAIPulseUnavailable)
			}

			// 入参规范化
			ids := args.AssetIDs
			if args.AssetID != 0 {
				ids = append([]uint{args.AssetID}, ids...)
			}
			if len(ids) == 0 {
				return PulseDiagnosisOutput{}, fmt.Errorf("asset_id or asset_ids required: %w", errs.ErrInvalidParam)
			}

			// 批量串行调用（Agent 工具层稳定性优先；并发由 REST API 层负责）
			items := make([]PulseDiagnosisItem, 0, len(ids))
			for _, aid := range ids {
				if aid == 0 {
					items = append(items, PulseDiagnosisItem{
						AssetID:      aid,
						Status:       "failed",
						ErrorMessage: "invalid asset_id",
					})
					continue
				}
				res, err := deps.Pulse.Diagnose(ctx, PulseDiagnoseRequest{
					UserID:        uid,
					AssetID:       aid,
					TriggerSource: "chat",
				})
				if err != nil {
					items = append(items, PulseDiagnosisItem{
						AssetID:      aid,
						Status:       "failed",
						ErrorMessage: err.Error(),
					})
					continue
				}
				items = append(items, PulseDiagnosisItem{
					AssetID:        res.AssetID,
					Recommendation: res.Recommendation,
					Confidence:     res.Confidence,
					Summary:        res.Summary,
					Detail:         res.Detail,
					Status:         "success",
				})
			}
			return PulseDiagnosisOutput{Items: items, Count: len(items)}, nil
		},
		sdkfunction.WithName("pulse_diagnosis"),
		sdkfunction.WithDescription(
			"对当前用户持有的资产进行 AI 把脉分析，给出操作建议（sell/reduce/hold/add）、"+
				"置信度（high/medium/low）、简要原因（summary）和详细分析（detail，含投资知识解释，"+
				"对初学者友好）。结果会自动持久化到数据库，便于资产管理页面直接展示。"+
				"参数 asset_id 单个资产把脉；asset_ids 批量把脉（数组）。"+
				"用户身份从 ctx 自动注入，不接受 user_id 参数。"+
				"注意：本工具消耗较多 token，请仅在用户明确要求把脉时调用。",
		),
	)
}
