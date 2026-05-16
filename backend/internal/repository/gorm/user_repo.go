package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// =====================================================================
// User
// =====================================================================

type userRepo struct{ db *gorm.DB }

// NewUserRepository 构造 UserRepository。
func NewUserRepository(db *gorm.DB) repository.UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) GetByID(ctx context.Context, id uint) (*domain.User, error) {
	var u domain.User
	if err := dbFrom(ctx, r.db).First(&u, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &u, nil
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	var u domain.User
	if err := dbFrom(ctx, r.db).Where("f_username = ?", username).First(&u).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &u, nil
}

func (r *userRepo) Create(ctx context.Context, u *domain.User) error {
	return dbFrom(ctx, r.db).Create(u).Error
}

func (r *userRepo) Update(ctx context.Context, u *domain.User) error {
	return dbFrom(ctx, r.db).Save(u).Error
}

// =====================================================================
// Platform
// =====================================================================

type platformRepo struct{ db *gorm.DB }

// NewPlatformRepository 构造 PlatformRepository。
func NewPlatformRepository(db *gorm.DB) repository.PlatformRepository {
	return &platformRepo{db: db}
}

func (r *platformRepo) List(ctx context.Context) ([]domain.Platform, error) {
	var list []domain.Platform
	if err := dbFrom(ctx, r.db).Order("f_id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *platformRepo) GetByID(ctx context.Context, id uint) (*domain.Platform, error) {
	var p domain.Platform
	if err := dbFrom(ctx, r.db).First(&p, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &p, nil
}

func (r *platformRepo) GetByCode(ctx context.Context, code string) (*domain.Platform, error) {
	var p domain.Platform
	if err := dbFrom(ctx, r.db).Where("f_code = ?", code).First(&p).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &p, nil
}

func (r *platformRepo) Create(ctx context.Context, p *domain.Platform) error {
	return dbFrom(ctx, r.db).Create(p).Error
}

func (r *platformRepo) Update(ctx context.Context, p *domain.Platform) error {
	return dbFrom(ctx, r.db).Save(p).Error
}

func (r *platformRepo) Delete(ctx context.Context, id uint) error {
	return dbFrom(ctx, r.db).Delete(&domain.Platform{}, id).Error
}
