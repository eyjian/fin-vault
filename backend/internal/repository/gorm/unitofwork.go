// Package gormrepo 是 repository 接口的 GORM 实现。
//
// 目录名为 gorm（架构师定稿），包名为 gormrepo（避免与 gorm.io/gorm 包名冲突）。
// 这是工程内**唯一**允许 import gorm.io/gorm 的位置（除了 bootstrap/db.go）。
package gormrepo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// txKey 用于在 ctx 中传递事务版 *gorm.DB。
type txKey struct{}

// withTx 把事务 DB 注入 ctx。
func withTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// dbFrom 从 ctx 中取事务 DB；不存在则用全局 DB。
//
// 所有 Repository 实现都通过本函数取连接，事务版 / 非事务版仓储共用同一份代码。
func dbFrom(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if v, ok := ctx.Value(txKey{}).(*gorm.DB); ok && v != nil {
		return v.WithContext(ctx)
	}
	return fallback.WithContext(ctx)
}

// translateNotFound 把 gorm.ErrRecordNotFound 翻译成 repository.ErrNotFound。
func translateNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	}
	return err
}

// =====================================================================
// UnitOfWork —— 基于 GORM Transaction
// =====================================================================

type unitOfWork struct {
	db *gorm.DB
}

// NewUnitOfWork 构造 UnitOfWork（GORM 实现）。
func NewUnitOfWork(db *gorm.DB) repository.UnitOfWork {
	return &unitOfWork{db: db}
}

// Do 在 GORM 事务内执行 fn。fn 内通过同一个 ctx 调任意 Repository 即自动复用事务连接。
func (u *unitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(withTx(ctx, tx))
	})
}
