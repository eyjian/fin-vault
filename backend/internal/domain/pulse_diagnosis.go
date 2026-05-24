// Package domain 领域模型 —— AI 把脉结果。
package domain

import (
	"encoding/json"
	"time"
)

// PulseRecommendation AI 把脉建议类型。
//
// 与 spec ai-pulse-diagnosis 四分类一致：
//   - PulseRecSell   建议卖出：亏损严重、趋势下行、基本面恶化
//   - PulseRecReduce 建议减仓：盈利较多可部分止盈、估值偏高、风险增大
//   - PulseRecHold   继续持有：表现稳定、估值合理、无明显操作信号
//   - PulseRecAdd    建议加仓：低估优质、趋势向好、回调即机会
type PulseRecommendation string

const (
	PulseRecSell   PulseRecommendation = "sell"
	PulseRecReduce PulseRecommendation = "reduce"
	PulseRecHold   PulseRecommendation = "hold"
	PulseRecAdd    PulseRecommendation = "add"
)

// IsValid 校验把脉建议合法性。
func (r PulseRecommendation) IsValid() bool {
	switch r {
	case PulseRecSell, PulseRecReduce, PulseRecHold, PulseRecAdd:
		return true
	}
	return false
}

// PulseConfidence AI 把脉置信度。
//
// 表达模型对建议的把握程度：
//   - PulseConfHigh   数据充分、信号明确，建议可信度高
//   - PulseConfMedium 数据较完整、但存在一定不确定性
//   - PulseConfLow    数据不足或市场信号矛盾，建议仅供参考
type PulseConfidence string

const (
	PulseConfHigh   PulseConfidence = "high"
	PulseConfMedium PulseConfidence = "medium"
	PulseConfLow    PulseConfidence = "low"
)

// IsValid 校验置信度合法性。
func (c PulseConfidence) IsValid() bool {
	switch c {
	case PulseConfHigh, PulseConfMedium, PulseConfLow:
		return true
	}
	return false
}

// PulseTriggerSource 把脉触发方式（用于审计与可追溯）。
//
//   - PulseTriggerManual    用户在资产管理页面手动触发（REST API 直调）
//   - PulseTriggerChat      AI 对话中由 Agent 调用 pulse_diagnosis 工具触发
//   - PulseTriggerScheduled 定时任务触发（未来扩展）
type PulseTriggerSource string

const (
	PulseTriggerManual    PulseTriggerSource = "manual"
	PulseTriggerChat      PulseTriggerSource = "chat"
	PulseTriggerScheduled PulseTriggerSource = "scheduled"
)

// IsValid 校验触发方式合法性。
func (t PulseTriggerSource) IsValid() bool {
	switch t {
	case PulseTriggerManual, PulseTriggerChat, PulseTriggerScheduled:
		return true
	}
	return false
}

// PulseDiagnosis AI 把脉结果。
//
// 设计要点（与 design.md D1/D8/D11 + spec ai-pulse-diagnosis 对齐）：
//   - 资产维度：每个 (UserID, AssetID) 唯一一条最新记录，由 Service 层 Upsert 保证
//   - 主键 ID 使用 UUID 字符串（与 Session/Message 风格一致），不继承 BaseModel
//   - DataReferences / RawResponse 用 json.RawMessage 承载 LLM 原始结构
//   - SessionID 关联 t_fv_ai_sessions.f_id（chat 路径），manual/scheduled 路径可为空串
//   - TriggerSource 记录把脉来源，便于审计与未来定时把脉区分
//
// 索引设计：
//   - uk_user_asset (f_user_id, f_asset_id)：唯一约束 + 按 (user, asset) 主查询
//   - idx_pulse_user_updated (f_user_id, f_updated_at desc)：按时间倒序列出用户全部把脉
//     （索引名前缀 idx_pulse_ 避免与 t_fv_ai_sessions 的 idx_user_updated 冲突 ——
//     SQLite 索引名是库级唯一）
type PulseDiagnosis struct {
	ID             string              `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
	UserID         uint                `gorm:"column:f_user_id;not null;uniqueIndex:uk_user_asset,priority:1;index:idx_pulse_user_updated,priority:1" json:"user_id"`
	AssetID        uint                `gorm:"column:f_asset_id;not null;uniqueIndex:uk_user_asset,priority:2" json:"asset_id"`
	Recommendation PulseRecommendation `gorm:"column:f_recommendation;type:varchar(16);not null" json:"recommendation"`
	Confidence     PulseConfidence     `gorm:"column:f_confidence;type:varchar(16);not null" json:"confidence"`
	Summary        string              `gorm:"column:f_summary;type:varchar(512);not null;default:''" json:"summary"`
	Detail         string              `gorm:"column:f_detail;type:text;not null;default:''" json:"detail"`
	DataReferences json.RawMessage     `gorm:"column:f_data_references;type:json" json:"data_references,omitempty"`
	RawResponse    string              `gorm:"column:f_raw_response;type:text;not null;default:''" json:"-"` // 仅服务端审计用，不外露
	SessionID      string              `gorm:"column:f_session_id;type:varchar(36);not null;default:''" json:"session_id,omitempty"`
	TriggerSource  PulseTriggerSource  `gorm:"column:f_trigger_source;type:varchar(16);not null" json:"trigger_source"`
	CreatedAt      time.Time           `gorm:"column:f_created_at;not null" json:"created_at"`
	UpdatedAt      time.Time           `gorm:"column:f_updated_at;not null;index:idx_pulse_user_updated,priority:2,sort:desc" json:"updated_at"`
}

// TableName 显式表名（fin-vault 命名规范 t_fv_{module}_{name}）。
func (PulseDiagnosis) TableName() string { return "t_fv_ai_pulse_diagnoses" }
