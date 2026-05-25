package domain

import (
	"encoding/json"
	"time"
)

// Session AI 多轮会话。
//
// 主键为 RFC 4122 UUID 字符串（design.md D9），由 service 层用 google/uuid 生成；
// 不继承 BaseModel——BaseModel 用 uint 自增主键，与本模块按 UUID 字符串关联的设计不兼容。
//
// 索引 idx_user_updated 复合 (f_user_id, f_updated_at)，支撑"按用户列出会话并按最近更新排序"的高频查询。
type Session struct {
	ID        string    `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
	UserID    uint      `gorm:"column:f_user_id;not null;index:idx_user_updated,priority:1" json:"user_id"`
	Title     string    `gorm:"column:f_title;type:varchar(128);not null;default:''" json:"title"`
	CreatedAt time.Time `gorm:"column:f_created_at;not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:f_updated_at;not null;index:idx_user_updated,priority:2" json:"updated_at"`
}

// TableName 显式表名（fin-vault 命名规范 t_fv_{module}_{name}）。
func (Session) TableName() string { return "t_fv_ai_sessions" }

// Message AI 会话内的消息。
//
// SessionID 通过应用层逻辑保证关联到 t_fv_ai_sessions.f_id（design.md "FK 仅在 spec 层承诺，
// 实际由 service 层级联删除"，因 SQLite 默认 PRAGMA foreign_keys=OFF 且 GORM AutoMigrate 对 SQLite
// 不生成 FK 约束）。
//
// TokenUsage 用 json.RawMessage 承载上游模型返回的 token 用量结构（prompt_tokens / completion_tokens 等），
// 在 SQLite 上 GORM `type:json` 会落成 TEXT 列，跨库通用。
type Message struct {
	ID         string          `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
	SessionID  string          `gorm:"column:f_session_id;type:varchar(36);not null;index:idx_session_created,priority:1" json:"session_id"`
	Role       string          `gorm:"column:f_role;type:varchar(20);not null" json:"role"`
	Content    string          `gorm:"column:f_content;type:text;not null" json:"content"`
	TokenUsage json.RawMessage `gorm:"column:f_token_usage;type:json" json:"token_usage,omitempty"`
	CreatedAt  time.Time       `gorm:"column:f_created_at;not null;index:idx_session_created,priority:2" json:"created_at"`
}

// TableName 显式表名。
func (Message) TableName() string { return "t_fv_ai_messages" }

// AgentStep Agent 运行时步骤事件。
//
// EventType 取值集合见 spec ai-agent-runtime：
//   - tool_call_started / tool_call_finished：工具调用生命周期事件
//   - token_usage：单步 token 计量
//   - step_boundary：step/turn 边界
//
// Payload 用 json.RawMessage 承载事件负载（参数、结果、token 数等），写入前由 service 层
// 对敏感字段（api_key / token / secret 等）做掩码（design.md D7）。
//
// 索引设计（注意：SQLite 索引名是库级命名空间，跨表不能同名，因此 AgentStep
// 复合索引前缀 idx_step_ 与 Message 的 idx_session_created 区分）：
//   - idx_step_session_created (f_session_id, f_created_at)：按会话拉取步骤时间线
//   - idx_step_session         (f_session_id)              ：按会话清理或筛选
//   - idx_step_message         (f_message_id)              ：按消息回溯产生的步骤
//   - idx_created              (f_created_at)              ：max_steps_size_mb 滚动清理按时间扫描
type AgentStep struct {
	ID        string          `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
	SessionID string          `gorm:"column:f_session_id;type:varchar(36);not null;index:idx_step_session_created,priority:1;index:idx_step_session" json:"session_id"`
	MessageID string          `gorm:"column:f_message_id;type:varchar(36);not null;index:idx_step_message" json:"message_id"`
	EventType string          `gorm:"column:f_event_type;type:varchar(32);not null" json:"event_type"`
	ToolName  string          `gorm:"column:f_tool_name;type:varchar(64)" json:"tool_name,omitempty"`
	Payload   json.RawMessage `gorm:"column:f_payload;type:json" json:"payload"`
	CreatedAt time.Time       `gorm:"column:f_created_at;not null;index:idx_step_session_created,priority:2;index:idx_created" json:"created_at"`
}

// TableName 显式表名。
func (AgentStep) TableName() string { return "t_fv_ai_agent_steps" }
