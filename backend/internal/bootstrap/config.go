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

	"github.com/eyjian/fin-vault/backend/internal/llm/model"
)

// Config 是 FinVault 后端的强类型配置。
//
// 加载流程：viper 仅在 LoadConfig 调用一次；其余模块通过本结构体取值，禁止再读 viper。
type Config struct {
	Server        ServerConfig         `mapstructure:"server"`
	Database      DatabaseConfig       `mapstructure:"database"`
	Cache         CacheConfig          `mapstructure:"cache"`
	Log           LogConfig            `mapstructure:"log"`
	Auth          AuthConfig           `mapstructure:"auth"`
	Security      SecurityConfig       `mapstructure:"security"`
	LLM           model.RegistryConfig `mapstructure:"llm"`
	AI            AIConfig             `mapstructure:"ai"`
	Quote         QuoteConfig          `mapstructure:"quote"`
	DataProviders DataProvidersConfig  `mapstructure:"data_providers"`
	Cron          CronConfig           `mapstructure:"cron"`
	JWT           JWTConfig            `mapstructure:"jwt"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Mode         string        `mapstructure:"mode"` // debug/release/test
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
//
// 文件输出 + 大小滚动备份（基于 lumberjack）：
//   - File：日志文件路径，留空则仅输出到 stdout
//   - MaxSizeMB：单个日志文件最大体积（MB），超过即滚动
//   - MaxBackups：最多保留多少个旧日志文件
//   - MaxAgeDays：旧日志最长保留天数（0=不按天清理）
//   - Compress：是否对滚动后的旧日志做 gzip 压缩
//   - Console：当 File 非空时，是否同时输出到 stdout（默认 true）
type LogConfig struct {
	Level      string `mapstructure:"level"`        // debug/info/warn/error
	Format     string `mapstructure:"format"`       // text/json/console
	File       string `mapstructure:"file"`         // 日志文件路径，例如 logs/finvault.log
	MaxSizeMB  int    `mapstructure:"max_size_mb"`  // 单文件最大体积（MB）
	MaxBackups int    `mapstructure:"max_backups"`  // 最多保留旧文件数量
	MaxAgeDays int    `mapstructure:"max_age_days"` // 最长保留天数
	Compress   bool   `mapstructure:"compress"`     // 是否压缩旧文件
	Console    bool   `mapstructure:"console"`      // 是否同时输出到控制台
}

// AuthConfig 认证配置（一阶段单用户固定 X-User-Id=DefaultUserID）。
type AuthConfig struct {
	Mode          string `mapstructure:"mode"` // local/jwt
	DefaultUserID uint   `mapstructure:"default_user_id"`
}

// SecurityConfig 安全配置（如 API Key/Secret 加密用）。
type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"`
}

// AIConfig AI 模块配置（基于 trpc-agent-go 的会话与运行时）。
//
// LLMConfig（多 Provider 路由）由 internal/llm/model 包内的 RegistryConfig 提供，
// 这里只承载 trpc-agent-go 引入后新增的会话/运行时层配置项。
type AIConfig struct {
	Session        SessionConfig        `mapstructure:"session"`
	PulseDiagnosis PulseDiagnosisConfig `mapstructure:"pulse_diagnosis"`
}

// SessionConfig AI 会话相关配置。
//
//   - MaxStepsSizeMB：ai_agent_steps 表大小估算上限（MB），0 = 不清理。
//   - HistoryWindow ：单次推理拼接的历史消息条数上限，必须 > 0。
type SessionConfig struct {
	MaxStepsSizeMB int `mapstructure:"max_steps_size_mb"`
	HistoryWindow  int `mapstructure:"history_window"`
}

// PulseDiagnosisConfig AI 把脉相关配置（spec ai-pulse-diagnosis "批量并行化"）。
//
//   - Concurrency ：REST API 批量把脉的并发度（errgroup 信号量），默认 3。
//     过大会触达 LLM 提供方 RPM 限制；过小会导致总耗时偏长。
type PulseDiagnosisConfig struct {
	Concurrency int `mapstructure:"concurrency"`
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

// DataProvidersConfig 多 API 服务商配置（用于基金净值等数据获取）。
//
// 设计原则：
//   - 每个服务商独立配置 token / base_url / enabled；
//   - 支持多个服务商并行注册，由 service 层按优先级尝试；
//   - token 通过环境变量注入，避免提交到仓库。
type DataProvidersConfig struct {
	Tushare TushareConfig `mapstructure:"tushare"`
	// 未来可扩展：AKShare AKShareConfig `mapstructure:"akshare"`
}

// TushareConfig Tushare Pro API 配置。
//
// Tushare Pro 是一个免费的金融数据接口，注册后赠送 200 积分，
// 基金净值接口（fund_nav）需 120 积分，完全覆盖。
//
// 接口文档：https://tushare.pro/document/2?doc_id=119
type TushareConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`    // Tushare API token，从环境变量 FINVAULT_DATA_PROVIDERS_TUSHARE_TOKEN 读取
	BaseURL string `mapstructure:"base_url"` // 默认 https://api.tushare.pro
}

// LoadConfig 从指定 yaml 文件加载配置（**整个进程唯一一次** viper 调用）。
//
// 同时支持环境变量覆盖：FINVAULT_DATABASE_DSN 等，分隔符 _。
// yaml 中 ${ENV_VAR:default} 形式的占位符会被自动展开。
func LoadConfig(path string) (*Config, error) {
	configPath = path // 记录路径，供 SaveConfig 回写

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

	// AI 会话配置校验：max_steps_size_mb 允许 0（=不清理），不允许负值；
	// history_window 必须 > 0，避免推理时拼不到历史消息。
	if cfg.AI.Session.MaxStepsSizeMB < 0 {
		return nil, fmt.Errorf("ai.session.max_steps_size_mb must be >= 0, got %d", cfg.AI.Session.MaxStepsSizeMB)
	}
	if cfg.AI.Session.HistoryWindow <= 0 {
		return nil, fmt.Errorf("ai.session.history_window must be > 0, got %d", cfg.AI.Session.HistoryWindow)
	}

	// AI 把脉并发度（spec ai-pulse-diagnosis "批量并行化"）：未配置或非法值时默认 3。
	if cfg.AI.PulseDiagnosis.Concurrency <= 0 {
		cfg.AI.PulseDiagnosis.Concurrency = 3
	}
	if cfg.AI.PulseDiagnosis.Concurrency > 20 {
		// 上限保护：避免一次发起过多并发请求触达 LLM 提供方 RPM 限制
		cfg.AI.PulseDiagnosis.Concurrency = 20
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
	v.SetDefault("log.file", "logs/finvault.log")
	v.SetDefault("log.max_size_mb", 50)
	v.SetDefault("log.max_backups", 10)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.compress", true)
	v.SetDefault("log.console", true)

	v.SetDefault("auth.mode", "local")
	v.SetDefault("auth.default_user_id", 1)

	v.SetDefault("llm.default", "deepseek")

	v.SetDefault("ai.session.max_steps_size_mb", 100)
	v.SetDefault("ai.session.history_window", 20)

	v.SetDefault("quote.source_priority", []string{"api_eastmoney", "api_sina", "api_tencent"})
	v.SetDefault("quote.http_timeout_sec", 5)
	v.SetDefault("quote.cache_ttl_sec", 60)
	v.SetDefault("quote.pool_size", 16)

	v.SetDefault("cron.mature", "30 0 * * *")

	v.SetDefault("jwt.secret", "fin-vault-secret-change-me")
	v.SetDefault("jwt.expire", "168h")

	// 多 API 服务商默认值
	v.SetDefault("data_providers.tushare.enabled", false)
	v.SetDefault("data_providers.tushare.base_url", "https://api.tushare.pro")
	v.SetDefault("data_providers.tushare.token", "")
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

// configPath 记录配置文件路径，用于 SaveConfig 回写。
var configPath string

// SaveConfig 将内存中的非敏感配置回写到 yaml 文件。
//
// 注意：data_providers.tushare.token 和 llm.providers.*.api_key 等敏感字段
// 已保存到 DB（通过页面设置页配置），不再写入配置文件。
// 仅更新非敏感段（如 server/database/cache/log 等）。
func SaveConfig(cfg *Config) error {
	if configPath == "" {
		return fmt.Errorf("config path not set, cannot save")
	}

	// 1. 读取原始文件作为基底（保留注释与格式）
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config for update: %w", err)
	}

	// 2. 合并非敏感段
	nonSensitiveMap := map[string]any{
		"server": map[string]any{"host": cfg.Server.Host, "port": cfg.Server.Port, "mode": cfg.Server.Mode},
		"cache":  map[string]any{"driver": cfg.Cache.Driver},
		"log":    map[string]any{"level": cfg.Log.Level, "format": cfg.Log.Format},
	}
	if err := v.MergeConfigMap(nonSensitiveMap); err != nil {
		return fmt.Errorf("merge non-sensitive config: %w", err)
	}

	// 3. 写回
	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("write config to %s: %w", configPath, err)
	}
	return nil
}
