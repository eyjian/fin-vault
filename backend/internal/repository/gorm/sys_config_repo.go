package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

type sysConfigRepo struct{ db *gorm.DB }

// NewSysConfigRepository 构造。
func NewSysConfigRepository(db *gorm.DB) repository.SysConfigRepository {
	return &sysConfigRepo{db: db}
}

func (r *sysConfigRepo) GetByCategory(ctx context.Context, category string) ([]repository.SysConfigEntry, error) {
	var list []domain.SysConfig
	if err := dbFrom(ctx, r.db).Where("f_category = ?", category).Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]repository.SysConfigEntry, 0, len(list))
	for _, sc := range list {
		out = append(out, toSysConfigEntry(&sc))
	}
	return out, nil
}

func (r *sysConfigRepo) Get(ctx context.Context, category, key string) (*repository.SysConfigEntry, error) {
	var sc domain.SysConfig
	err := dbFrom(ctx, r.db).Where("f_category = ? AND f_key = ?", category, key).First(&sc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	e := toSysConfigEntry(&sc)
	return &e, nil
}

func (r *sysConfigRepo) Upsert(ctx context.Context, entry *repository.SysConfigEntry) error {
	now := time.Now()
	sc := &domain.SysConfig{
		Category: entry.Category,
		Key:      entry.Key,
		Value:    entry.Value,
		Remark:   entry.Remark,
	}
	if entry.ID > 0 {
		sc.ID = entry.ID
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = now
	}
	sc.UpdatedAt = now
	return dbFrom(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "f_category"}, {Name: "f_key"}},
		UpdateAll: true,
	}).Create(sc).Error
}

func (r *sysConfigRepo) Delete(ctx context.Context, category, key string) error {
	return dbFrom(ctx, r.db).
		Where("f_category = ? AND f_key = ?", category, key).
		Delete(&domain.SysConfig{}).Error
}

func (r *sysConfigRepo) GetAll(ctx context.Context) ([]repository.SysConfigEntry, error) {
	var list []domain.SysConfig
	if err := dbFrom(ctx, r.db).Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]repository.SysConfigEntry, 0, len(list))
	for _, sc := range list {
		out = append(out, toSysConfigEntry(&sc))
	}
	return out, nil
}

func toSysConfigEntry(sc *domain.SysConfig) repository.SysConfigEntry {
	return repository.SysConfigEntry{
		ID:       sc.ID,
		Category: sc.Category,
		Key:      sc.Key,
		Value:    sc.Value,
		Remark:   sc.Remark,
	}
}
