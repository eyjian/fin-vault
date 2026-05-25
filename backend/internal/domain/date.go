package domain

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// NullableDate 是可空日期类型，支持 JSON 解析：
//   - "2004-06-16"       （日期格式，前端 date picker 默认）
//   - "2004-06-16T00:00:00Z"（RFC3339，标准 JSON time）
//
// 同时实现 driver.Valuer + sql.Scanner，与 GORM 完全兼容
// （底层委托给内嵌的 time.Time）。
type NullableDate struct {
	t *time.Time
}

// NewNullableDate 构造。
func NewNullableDate(t *time.Time) NullableDate {
	return NullableDate{t: t}
}

// Time 取出底层 *time.Time（方便 service 层读取）。
func (d NullableDate) Time() *time.Time {
	return d.t
}

// Set 设置值（方便 service 层写入）。
func (d *NullableDate) Set(t *time.Time) {
	d.t = t
}

// Scan 从 DB 读取（GORM 调用）。
func (d *NullableDate) Scan(src any) error {
	if src == nil {
		d.t = nil
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		d.t = &v
		return nil
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return fmt.Errorf("NullableDate.Scan: %w", err)
		}
		d.t = &t
		return nil
	default:
		return fmt.Errorf("NullableDate.Scan: unsupported type %T", src)
	}
}

// Value 写入 DB（GORM 调用）。
func (d NullableDate) Value() (driver.Value, error) {
	if d.t == nil {
		return nil, nil
	}
	return *d.t, nil
}

// UnmarshalJSON 支持 "2004-06-16" 和 RFC3339。
func (d *NullableDate) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "" || s == "null" {
		d.t = nil
		return nil
	}
	// 尝试日期格式
	if t, err := time.Parse("2006-01-02", s); err == nil {
		d.t = &t
		return nil
	}
	// 尝试 RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		d.t = &t
		return nil
	}
	return fmt.Errorf("NullableDate: cannot parse %q as date or RFC3339", s)
}

// MarshalJSON 序列化为 "2006-01-02"。
func (d NullableDate) MarshalJSON() ([]byte, error) {
	if d.t == nil {
		return []byte("null"), nil
	}
	return []byte(`"` + d.t.Format("2006-01-02") + `"`), nil
}
