package domain

import "time"

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
