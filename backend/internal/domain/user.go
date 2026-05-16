package domain

import "time"

// User 用户实体（第一阶段单用户固定 ID=1）。
type User struct {
	BaseModel
	Username        string `gorm:"size:64;uniqueIndex:uk_username;not null;column:f_username" json:"username"`
	PasswordHash    string `gorm:"size:128;not null;column:f_password_hash" json:"-"`
	DisplayName     string `gorm:"size:64;column:f_display_name" json:"display_name"`
	Email           string `gorm:"size:128;column:f_email" json:"email"`
	DefaultCurrency string `gorm:"size:10;not null;default:CNY;column:f_default_currency" json:"default_currency"`
	Status          string `gorm:"size:20;not null;default:active;column:f_status" json:"status"`
}

// TableName 显式表名。
func (User) TableName() string { return "t_fv_user_users" }

// Platform 平台字典实体。
type Platform struct {
	ID           uint      `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	Code         string    `gorm:"size:32;uniqueIndex:uk_code;not null;column:f_code" json:"code"`
	Name         string    `gorm:"size:64;not null;column:f_name" json:"name"`
	PlatformType string    `gorm:"size:20;not null;index:idx_type_status,priority:1;column:f_platform_type" json:"platform_type"`
	IconURL      string    `gorm:"size:255;column:f_icon_url" json:"icon_url"`
	IsSystem     bool      `gorm:"not null;default:false;column:f_is_system" json:"is_system"`
	Status       string    `gorm:"size:20;not null;default:active;index:idx_type_status,priority:2;column:f_status" json:"status"`
	CreatedAt    time.Time `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt    time.Time `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (Platform) TableName() string { return "t_fv_dict_platforms" }
