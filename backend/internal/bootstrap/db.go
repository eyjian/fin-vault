package bootstrap

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewDB 根据 DatabaseConfig 创建 *gorm.DB。
//
// 第一阶段只支持 sqlite（glebarez 纯 Go 驱动，无 CGO 依赖）。
// 接 mysql/postgres 时新增 case，业务层无需改动（GORM 抽象统一）。
func NewDB(cfg DatabaseConfig) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger:      newGormLogger(cfg.LogLevel),
		PrepareStmt: true,
		// SQLite 单连接模式更稳，减少 "database is locked"
		SkipDefaultTransaction: true,
	}

	var db *gorm.DB
	var err error
	switch cfg.Driver {
	case "sqlite", "":
		// 确保 DSN 父目录存在
		if dir := filepath.Dir(cfg.DSN); dir != "" && dir != "." {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				return nil, fmt.Errorf("mkdir %s: %w", dir, mkErr)
			}
		}
		// SQLite busy_timeout + WAL 提升并发友好度
		dsn := cfg.DSN
		if dsn == "" {
			dsn = "data/finvault.db"
		}
		if !contains(dsn, "?") {
			dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(0)"
		}
		db, err = gorm.Open(sqlite.Open(dsn), gormCfg)
	default:
		return nil, fmt.Errorf("unsupported db driver %q (only sqlite is implemented in M1)", cfg.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	// SQLite 建议 1 个 writer，避免锁竞争
	if cfg.Driver == "sqlite" || cfg.Driver == "" {
		sqlDB.SetMaxOpenConns(1)
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	slog.Info("database connected", "driver", cfg.Driver, "dsn", cfg.DSN)
	return db, nil
}

// newGormLogger 把字符串日志级别映射到 gorm logger 的 LogLevel。
func newGormLogger(level string) logger.Interface {
	var lv logger.LogLevel
	switch level {
	case "silent":
		lv = logger.Silent
	case "error":
		lv = logger.Error
	case "warn", "":
		lv = logger.Warn
	case "info":
		lv = logger.Info
	default:
		lv = logger.Warn
	}
	return logger.New(
		log.New(os.Stdout, "[gorm] ", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  lv,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
