package bootstrap

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
)

// SeedInitialData 在首次启动写入幂等的初始化数据：
//   - 默认用户 ID=1
//   - 11 条平台字典（招行/工行/农行/建行/中行/天天基金/富途/老虎/理财通/支付宝/微信）
//   - 4 条基础汇率（USD/HKD/EUR/JPY → CNY）
//
// 已存在的数据会被跳过（按唯一键 / ID 判定）。
func SeedInitialData(db *gorm.DB) error {
	if err := seedDefaultUser(db); err != nil {
		return err
	}
	if err := seedPlatforms(db); err != nil {
		return err
	}
	if err := seedRates(db); err != nil {
		return err
	}
	return nil
}

func seedDefaultUser(db *gorm.DB) error {
	var u domain.User
	err := db.First(&u, 1).Error
	if err == nil {
		return nil // 已存在
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	now := time.Now()
	defaultUser := &domain.User{
		Username:        "admin",
		PasswordHash:    "$2a$10$placeholder.bcrypt.hash.for.local.user.no.auth.required",
		DisplayName:     "本地用户",
		DefaultCurrency: "CNY",
		Status:          domain.StatusActive,
	}
	defaultUser.ID = 1
	defaultUser.CreatedAt = now
	defaultUser.UpdatedAt = now
	return db.Create(defaultUser).Error
}

func seedPlatforms(db *gorm.DB) error {
	platforms := []struct {
		Code, Name, Type string
	}{
		{"zsbank", "招商银行APP", domain.PlatformTypeBank},
		{"icbc", "工商银行APP", domain.PlatformTypeBank},
		{"abc", "农业银行APP", domain.PlatformTypeBank},
		{"ccb", "建设银行APP", domain.PlatformTypeBank},
		{"boc", "中国银行APP", domain.PlatformTypeBank},
		{"ttfund", "天天基金", domain.PlatformTypeFundPlatform},
		{"futu", "富途牛牛", domain.PlatformTypeBroker},
		{"tiger", "老虎证券", domain.PlatformTypeBroker},
		{"licai_tong", "理财通", domain.PlatformTypeInternet},
		{"alipay", "支付宝", domain.PlatformTypeInternet},
		{"wechat", "微信支付", domain.PlatformTypeInternet},
	}
	now := time.Now()
	for _, p := range platforms {
		var existing domain.Platform
		err := db.Where("f_code = ?", p.Code).First(&existing).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row := &domain.Platform{
			Code:         p.Code,
			Name:         p.Name,
			PlatformType: p.Type,
			IsSystem:     true,
			Status:       domain.StatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := db.Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}

func seedRates(db *gorm.DB) error {
	today := time.Now()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	rates := []struct {
		From, To string
		Rate     string
	}{
		{"USD", "CNY", "7.200000"},
		{"HKD", "CNY", "0.920000"},
		{"EUR", "CNY", "7.800000"},
		{"JPY", "CNY", "0.048000"},
	}
	for _, r := range rates {
		var existing domain.ExchangeRate
		err := db.Where("f_from_currency = ? AND f_to_currency = ? AND f_quote_date = ? AND f_source = ?",
			r.From, r.To, today, domain.RateSourceManual).First(&existing).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		rate, _ := decimal.NewFromString(r.Rate)
		row := &domain.ExchangeRate{
			FromCurrency: r.From,
			ToCurrency:   r.To,
			Rate:         rate,
			QuoteDate:    today,
			Source:       domain.RateSourceManual,
			CreatedAt:    time.Now(),
		}
		if err := db.Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}
