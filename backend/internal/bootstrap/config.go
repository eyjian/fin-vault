// Package bootstrap 负责进程启动期的依赖装配与生命周期管理。
//
// 按 ARCHITECTURE.md §3 / §10 划分文件：
//   - config.go   viper 加载（**唯一**调用 viper 的位置）
//   - logger.go   slog 初始化
//   - db.go       数据库工厂（sqlite/mysql/postgres 切换）
//   - cache.go    缓存工厂
//   - cron.go     cron 调度装配
//   - migrate.go  AutoMigrate + 初始化数据
//   - seed.go     默认 user/平台/汇率
//   - router.go   Gin 路由 + 中间件链
//   - wire.go     总装函数 Wire(cfg) -> *App
package bootstrap

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/eyjian/fin-vault/backend/internal/llm"
)

// Config 是 FinVault 后端的强类型配置。
//
// 加载流程：viper 仅在 LoadConfig 调用一次；其余模块通过本结构体取值，禁止再读 viper。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Cache    CacheConfig    `mapstructure:"cache"`
	Log      LogConfig      `mapstructure:"log"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Security SecurityConfig `mapstructure:"security"`
	LLM      llm.RegistryConfig `mapstructure:"llm"`
	Quote    QuoteConfig    `mapstructure:"quote"`
	Cron     CronConfig     `mapstructure:"cron"`
	JWT      JWTConfig      `mapstructure:"jwt"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Mode         string        `mapstructure:"mode"`           // debug/release/test
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	CORSOrigins  []string      `mapstructure:"cors_origins"`
}

// DatabaseConfig 数据库配置。
type DatabaseConfig struct {
	Driver          string        `mapstructure:"driver"` // sqlite/mysql/postgres
	DSN             string        `mapstructure:"dsn"`
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
	LogLevel        string        `mapstructure:"log_level"` // silent/error/warn/info
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// CacheConfig 缓存配置。
type CacheConfig struct {
	Driver string      `mapstructure:"driver"` // memory/local/redis
	Redis  RedisConfig `mapstructure:"redis"`
}

// RedisConfig redis 配置（生产可选）。
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// LogConfig 日志配置。
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug/info/warn/error
	Format string `mapstructure:"format"` // text/json/console
}

// AuthConfig 认证配置（一阶段单用户固定 X-User-Id=DefaultUserID）。
type AuthConfig struct {
	Mode          string `mapstructure:"mode"`            // local/jwt
	DefaultUserID uint   `mapstructure:"default_user_id"`
}

// SecurityConfig 安全配置（如 API Key/Secret 加密用）。
type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"`
}

// QuoteConfig 行情拉取配置。
type QuoteConfig struct {
	SourcePriority []string      `mapstructure:"source_priority"` // ["api_eastmoney","api_sina","api_tencent"]
	HTTPTimeoutSec int           `mapstructure:"http_timeout_sec"`
	CacheTTLSec    int           `mapstructure:"cache_ttl_sec"`
	PoolSize       int           `mapstructure:"pool_size"`
	HTTPTimeout    time.Duration `mapstructure:"-"` // 由 LoadConfig 派生
	CacheTTL       time.Duration `mapstructure:"-"` // 由 LoadConfig 派生
}

// CronConfig 定时任务配置。
type CronConfig struct {
	Mature string `mapstructure:"mature"` // 默认 "30 0 * * *"
}

// JWTConfig JWT 配置（一阶段未启用）。
type JWTConfig struct {
	Secret string        `mapstructure:"secret"`
	Expire time.Duration `mapstructure:"expire"`
}

// LoadConfig 从指定 yaml 文件加载配置（**整个进程唯一一次** viper 调用）。
//
// 同时支持环境变量覆盖：FINVAULT_DATABASE_DSN 等，分隔符 _。
// yaml 中 ${ENV_VAR:default} 形式的占位符会被自动展开。
func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// 环境变量覆盖
	v.SetEnvPrefix("FINVAULT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 默认值
	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	// 展开 yaml 中的 ${VAR:default} 占位符
	expandEnvInViper(v)

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// 派生字段
	if cfg.Quote.HTTPTimeoutSec > 0 {
		cfg.Quote.HTTPTimeout = time.Duration(cfg.Quote.HTTPTimeoutSec) * time.Second
	} else {
		cfg.Quote.HTTPTimeout = 5 * time.Second
	}
	if cfg.Quote.CacheTTLSec > 0 {
		cfg.Quote.CacheTTL = time.Duration(cfg.Quote.CacheTTLSec) * time.Second
	} else {
		cfg.Quote.CacheTTL = 60 * time.Second
	}
	if len(cfg.Quote.SourcePriority) == 0 {
		cfg.Quote.SourcePriority = []string{"api_eastmoney", "api_sina", "api_tencent"}
	}
	if cfg.Quote.PoolSize <= 0 {
		cfg.Quote.PoolSize = 16
	}
	if cfg.Cron.Mature == "" {
		cfg.Cron.Mature = "30 0 * * *"
	}
	if cfg.Auth.DefaultUserID == 0 {
		cfg.Auth.DefaultUserID = 1
	}

	return cfg, nil
}

// setDefaults 给 viper 注入默认值。
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "30s")
	v.SetDefault("server.cors_origins", []string{"*"})

	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "data/finvault.db")
	v.SetDefault("database.auto_migrate", true)
	v.SetDefault("database.log_level", "warn")
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.max_open_conns", 50)
	v.SetDefault("database.conn_max_lifetime", "1h")

	v.SetDefault("cache.driver", "memory")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")

	v.SetDefault("auth.mode", "local")
	v.SetDefault("auth.default_user_id", 1)

	v.SetDefault("llm.default", "deepseek")

	v.SetDefault("quote.source_priority", []string{"api_eastmoney", "api_sina", "api_tencent"})
	v.SetDefault("quote.http_timeout_sec", 5)
	v.SetDefault("quote.cache_ttl_sec", 60)
	v.SetDefault("quote.pool_size", 16)

	v.SetDefault("cron.mature", "30 0 * * *")

	v.SetDefault("jwt.secret", "fin-vault-secret-change-me")
	v.SetDefault("jwt.expire", "168h")
}

// expandEnvInViper 展开所有 string 类型配置项里的 ${VAR:default} 占位符。
func expandEnvInViper(v *viper.Viper) {
	for _, key := range v.AllKeys() {
		val := v.Get(key)
		if s, ok := val.(string); ok {
			expanded := expandEnv(s)
			if expanded != s {
				v.Set(key, expanded)
			}
		}
	}
}

// expandEnv 展开 ${VAR:default} / ${VAR}。未定义且无默认值时返回空串。
func expandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	var out strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.Index(s[i:], "}")
			if end == -1 {
				out.WriteString(s[i:])
				break
			}
			expr := s[i+2 : i+end]
			name := expr
			def := ""
			if colon := strings.Index(expr, ":"); colon >= 0 {
				name = expr[:colon]
				def = expr[colon+1:]
			}
			val, ok := os.LookupEnv(name)
			if !ok || val == "" {
				out.WriteString(def)
			} else {
				out.WriteString(val)
			}
			i += end + 1
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
