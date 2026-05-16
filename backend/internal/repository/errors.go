// Package repository: errors.go 仅暴露 Repository 层语义级"哨兵错误"，
// 上层 Service 用 errors.Is 判定，并按需转换为 pkg/errs 业务错误码。
package repository

import "errors"

// 通用 Repository 错误。
var (
	// ErrNotFound 记录不存在。
	ErrNotFound = errors.New("repository: record not found")
	// ErrConflict 唯一索引或约束冲突。
	ErrConflict = errors.New("repository: conflict")
	// ErrInsufficientQty 库存（持仓量）不足，无法完成卖出/取现等操作。
	ErrInsufficientQty = errors.New("repository: insufficient quantity")
)
