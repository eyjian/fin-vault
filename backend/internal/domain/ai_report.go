package domain

import "time"

// AIConversation AI 对话会话。
type AIConversation struct {
	BaseModel
	UserID       uint    `gorm:"not null;index:idx_user_status_updated,priority:1;column:f_user_id" json:"user_id"`
	Title        string  `gorm:"size:128;not null;column:f_title" json:"title"`
	Scene        AIScene `gorm:"size:20;not null;default:chat;column:f_scene" json:"scene"`
	LLMProvider  string  `gorm:"size:32;not null;column:f_llm_provider" json:"llm_provider"`
	LLMModel     string  `gorm:"size:64;not null;column:f_llm_model" json:"llm_model"`
	MessageCount int     `gorm:"not null;default:0;column:f_message_count" json:"message_count"`
	TotalTokens  int     `gorm:"not null;default:0;column:f_total_tokens" json:"total_tokens"`
	Status       string  `gorm:"size:20;not null;default:active;index:idx_user_status_updated,priority:2;column:f_status" json:"status"`
}

// TableName 显式表名。
func (AIConversation) TableName() string { return "t_fv_ai_conversations" }

// AIMessage AI 对话消息。
//
// ToolCallID 用于承载 OpenAI Tool Calling 协议中 tool 角色消息的 tool_call_id，
// 这是多轮 Tool Calling 回填的必备字段。
type AIMessage struct {
	ID             uint      `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	ConversationID uint      `gorm:"not null;index:idx_conv_time,priority:1;column:f_conversation_id" json:"conversation_id"`
	Role           string    `gorm:"size:20;not null;column:f_role" json:"role"`
	Content        string    `gorm:"type:text;not null;column:f_content" json:"content"`
	ToolCallID     string    `gorm:"size:64;column:f_tool_call_id" json:"tool_call_id,omitempty"`
	ToolName       string    `gorm:"size:64;column:f_tool_name" json:"tool_name,omitempty"`
	ToolArgs       string    `gorm:"type:text;column:f_tool_args" json:"tool_args,omitempty"`
	ToolResult     string    `gorm:"type:text;column:f_tool_result" json:"tool_result,omitempty"`
	TokenCount     int       `gorm:"column:f_token_count" json:"token_count"`
	CreatedAt      time.Time `gorm:"not null;index:idx_conv_time,priority:2;column:f_created_at" json:"created_at"`
}

// TableName 显式表名。
func (AIMessage) TableName() string { return "t_fv_ai_messages" }

// Report 报表缓存（一阶段建表，二阶段实现生成）。
type Report struct {
	ID              uint       `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	UserID          uint       `gorm:"not null;uniqueIndex:uk_user_type_period,priority:1;index:idx_user_status,priority:1;column:f_user_id" json:"user_id"`
	ReportType      string     `gorm:"size:20;not null;uniqueIndex:uk_user_type_period,priority:2;column:f_report_type" json:"report_type"`
	PeriodStart     time.Time  `gorm:"type:date;not null;uniqueIndex:uk_user_type_period,priority:3;column:f_period_start" json:"period_start"`
	PeriodEnd       time.Time  `gorm:"type:date;not null;uniqueIndex:uk_user_type_period,priority:4;column:f_period_end" json:"period_end"`
	DisplayCurrency string     `gorm:"size:10;not null;default:CNY;column:f_display_currency" json:"display_currency"`
	SnapshotData    string     `gorm:"type:text;not null;column:f_snapshot_data" json:"snapshot_data"`
	AnalysisData    string     `gorm:"type:text;not null;column:f_analysis_data" json:"analysis_data"`
	AISummary       string     `gorm:"type:text;column:f_ai_summary" json:"ai_summary"`
	Status          string     `gorm:"size:20;not null;default:generating;index:idx_user_status,priority:2;column:f_status" json:"status"`
	GeneratedAt     *time.Time `gorm:"column:f_generated_at" json:"generated_at,omitempty"`
	CreatedAt       time.Time  `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (Report) TableName() string { return "t_fv_report_reports" }
