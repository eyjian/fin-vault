// Package gormrepo —— PulseDiagnosisRepository 的 GORM 实现。
package gormrepo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// pulseDiagnosisRepo PulseDiagnosisRepository 的 GORM 实现。
type pulseDiagnosisRepo struct{ db *gorm.DB }

// NewPulseDiagnosisRepository 构造 PulseDiagnosisRepository。
func NewPulseDiagnosisRepository(db *gorm.DB) repository.PulseDiagnosisRepository {
	return &pulseDiagnosisRepo{db: db}
}

// Upsert 创建或更新把脉结果。
//
// 实现策略（与 design.md D8 + spec ai-pulse-diagnosis "重新把脉覆盖旧结果" 对齐）：
//  1. 先按 (UserID, AssetID) 查询是否已存在
//  2. 已存在：覆盖业务字段（保留原 ID + CreatedAt），UpdatedAt 取当前时间
//  3. 不存在：分配 UUID + 写入 CreatedAt/UpdatedAt
//
// 使用 GORM clause.OnConflict 来表达 ON CONFLICT (f_user_id, f_asset_id) DO UPDATE，
// SQLite / MySQL / Postgres 均支持（GORM 自动适配各 DB 的 UPSERT 语法）。
func (r *pulseDiagnosisRepo) Upsert(ctx context.Context, d *domain.PulseDiagnosis) error {
	now := time.Now()
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now

	// ON CONFLICT (f_user_id, f_asset_id) DO UPDATE SET ...
	// 注意：保留首次创建时的 CreatedAt 与原 ID 不变 —— 这两个字段不在 DoUpdates 列表中。
	return dbFrom(ctx, r.db).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "f_user_id"},
			{Name: "f_asset_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"f_recommendation",
			"f_confidence",
			"f_summary",
			"f_detail",
			"f_data_references",
			"f_raw_response",
			"f_session_id",
			"f_trigger_source",
			"f_updated_at",
		}),
	}).Create(d).Error
}

// GetByUserAsset 取单个 (UserID, AssetID) 的最新把脉结果；不存在返 (nil, nil)。
func (r *pulseDiagnosisRepo) GetByUserAsset(ctx context.Context, userID, assetID uint) (*domain.PulseDiagnosis, error) {
	var d domain.PulseDiagnosis
	if err := dbFrom(ctx, r.db).
		Where("f_user_id = ? AND f_asset_id = ?", userID, assetID).
		Take(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// ListByUser 列出用户的把脉结果（按 UpdatedAt 倒序）。
//
// 当 assetIDs 非空时，仅返回这些资产对应的记录（资产管理页批量预加载场景）；
// 为空切片或 nil 时不加 IN 过滤，返回该用户全部把脉结果。
func (r *pulseDiagnosisRepo) ListByUser(ctx context.Context, userID uint, assetIDs []uint) ([]domain.PulseDiagnosis, error) {
	tx := dbFrom(ctx, r.db).Where("f_user_id = ?", userID)
	if len(assetIDs) > 0 {
		tx = tx.Where("f_asset_id IN ?", assetIDs)
	}
	var list []domain.PulseDiagnosis
	if err := tx.Order("f_updated_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
