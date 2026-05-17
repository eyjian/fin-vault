package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sqlitedrv "github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/model"
	"github.com/eyjian/fin-vault/backend/internal/llm/session"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	gormrepo "github.com/eyjian/fin-vault/backend/internal/repository/gorm"
)

// =====================================================================
// 测试基础设施
// =====================================================================

// newTestDB 构造一个独立的 in-memory SQLite DB，AutoMigrate 含 t_fv_ai_* 三张表
// + 业务表（与真 wire.go 装配链一致：repos.Asset / repos.Quote 等都依赖 GORM 真实表）。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlitedrv.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")
	require.NoError(t, db.AutoMigrate(
		// AI 表
		&domain.Session{},
		&domain.Message{},
		&domain.AgentStep{},
		// 业务表（tools 工具依赖的 repository 落库点）
		&domain.User{},
		&domain.Platform{},
		&domain.Asset{},
		&domain.FundDetail{},
		&domain.StockDetail{},
		&domain.WealthDetail{},
		&domain.Holding{},
		&domain.CostLot{},
		&domain.Transaction{},
		&domain.PriceQuote{},
		&domain.ExchangeRate{},
	), "automigrate")
	return db
}

// newTestRepos 构造 repos（与真 wire.go 装配链一致），用于 wireAI 内部 buildAITools。
func newTestRepos(db *gorm.DB) *repository.Repositories {
	return &repository.Repositories{
		UoW:         gormrepo.NewUnitOfWork(db),
		User:        gormrepo.NewUserRepository(db),
		Platform:    gormrepo.NewPlatformRepository(db),
		Asset:       gormrepo.NewAssetRepository(db),
		Holding:     gormrepo.NewHoldingRepository(db),
		Transaction: gormrepo.NewTransactionRepository(db),
		CostLot:     gormrepo.NewCostLotRepository(db),
		Portfolio:   gormrepo.NewPortfolioRepository(db),
		Quote:       gormrepo.NewQuoteRepository(db),
		Rate:        gormrepo.NewRateRepository(db),
	}
}

// captureLogger 返回一个把全部日志写入 buf 的 slog.Logger（JSON handler，便于断言）。
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// withEnabledProvider 构造含 1 个可用 fake provider 的 cfg.LLM。
//
// fake provider：APIKey="dummy" / BaseURL="http://localhost:1234" / Model="fake-model" /
// Enabled=true → isProviderUsable=true，model.NewDefaultModel 会进入正常路径
// 调 sdkopenai.New 构造 SDK 实例（纯结构体构造，不真发 HTTP 请求，单测安全）。
func withEnabledProvider() model.RegistryConfig {
	enabled := true
	return model.RegistryConfig{
		Default: "fake",
		Providers: map[string]model.ProviderConfig{
			"fake": {
				Enabled: &enabled,
				APIKey:  "dummy",
				BaseURL: "http://localhost:1234",
				Model:   "fake-model",
			},
		},
	}
}

// withDisabledProviders 构造全 enabled=false 的 cfg.LLM（D16 降级路径触发条件 a）。
func withDisabledProviders() model.RegistryConfig {
	disabled := false
	return model.RegistryConfig{
		Default: "off",
		Providers: map[string]model.ProviderConfig{
			"off": {
				Enabled: &disabled,
				APIKey:  "x",
				BaseURL: "http://x",
				Model:   "x",
			},
		},
	}
}

// withEmptyProviders 构造 Providers 为 nil 的 cfg.LLM（D16 降级路径触发条件 b）。
func withEmptyProviders() model.RegistryConfig {
	return model.RegistryConfig{}
}

// makeCfg 包装最小可用 Config（仅含 wireAI 所需字段）。
func makeCfg(llm model.RegistryConfig) *Config {
	return &Config{
		LLM: llm,
		AI: AIConfig{
			Session: SessionConfig{
				HistoryWindow:  20,
				MaxStepsSizeMB: 0,
			},
		},
	}
}

// =====================================================================
// §9.1 buildAITools 测试
// =====================================================================

// TestBuildAITools_Returns7Tools
//
// 验证 7 个工具齐全 + name 集合精确等于规范期望。
// spec ai-tools 议题"首发 6 个 + history_query"。
func TestBuildAITools_Returns7Tools(t *testing.T) {
	db := newTestDB(t)
	repos := newTestRepos(db)

	got := buildAITools(repos)
	require.Len(t, got, 7, "首发 6 工具 + history_query 共 7 个")

	wantNames := map[string]bool{
		"search_fund":      false,
		"market_quote":     false,
		"market_data":      false,
		"holding_query":    false,
		"profit_calc":      false,
		"platform_summary": false,
		"history_query":    false,
	}
	for _, tl := range got {
		require.NotNil(t, tl, "tool 实例不应为 nil")
		decl := tl.Declaration()
		require.NotNil(t, decl, "Declaration() 不应为 nil")
		require.NotEmpty(t, decl.Name, "tool name 不应为空")
		_, expected := wantNames[decl.Name]
		assert.True(t, expected, "未预期的工具 name: %s", decl.Name)
		wantNames[decl.Name] = true
	}
	for name, hit := range wantNames {
		assert.True(t, hit, "缺失工具: %s", name)
	}
}

// =====================================================================
// §9.1 wireAI 测试
// =====================================================================

// TestWireAI_NormalPath_FullAssembly
//
// 含 1 个可用 fake provider → wireAI 走正常路径，sessionH + messageH 均非 nil；
// 启动日志含完整 5 条契约（providers loaded / provider selected / tools registered /
// session config / endpoints status message_enabled=true）。
func TestWireAI_NormalPath_FullAssembly(t *testing.T) {
	db := newTestDB(t)
	repos := newTestRepos(db)
	store := session.NewSQLiteStore(db, 20)
	cfg := makeCfg(withEnabledProvider())

	var buf bytes.Buffer
	logger := captureLogger(&buf)

	sessionH, messageH := wireAI(cfg, repos, store, logger)

	require.NotNil(t, sessionH, "AISessionHandler 始终非 nil")
	require.NotNil(t, messageH, "正常路径 AIMessageHandler 必须非 nil")

	logs := parseAllLogs(t, &buf)
	assertLogContains(t, logs, "ai providers loaded", map[string]any{"default": "fake"})
	assertLogContains(t, logs, "llm provider selected (default)", map[string]any{"provider": "fake"})
	assertLogContains(t, logs, "llm tools registered", nil)
	assertLogContains(t, logs, "ai session config", map[string]any{
		"history_window": float64(20),
	})
	assertLogContains(t, logs, "ai endpoints status", map[string]any{
		"session_enabled": true,
		"message_enabled": true,
	})
}

// TestWireAI_DegradePath_NoUsableProvider
//
// D16 降级条件 a：cfg.LLM 全 enabled=false → NewDefaultModel error → 仅装 AISession。
// 启动日志必须含 "AI message endpoint disabled" + reason 非空 + endpoints status
// message_enabled=false。
func TestWireAI_DegradePath_NoUsableProvider(t *testing.T) {
	db := newTestDB(t)
	repos := newTestRepos(db)
	store := session.NewSQLiteStore(db, 20)
	cfg := makeCfg(withDisabledProviders())

	var buf bytes.Buffer
	logger := captureLogger(&buf)

	sessionH, messageH := wireAI(cfg, repos, store, logger)

	require.NotNil(t, sessionH, "D16 降级：AISessionHandler 仍始终装")
	assert.Nil(t, messageH, "D16 降级：AIMessageHandler 必须为 nil")

	logs := parseAllLogs(t, &buf)
	disabledLog := findLog(logs, "AI message endpoint disabled (D16 degrade)")
	require.NotNil(t, disabledLog, "应有 D16 降级 Warn 日志")
	assert.NotEmpty(t, disabledLog["reason"], "降级日志 reason 非空")

	statusLog := findLog(logs, "ai endpoints status")
	require.NotNil(t, statusLog, "应有 ai endpoints status 日志（降级路径也打）")
	assert.Equal(t, true, statusLog["session_enabled"])
	assert.Equal(t, false, statusLog["message_enabled"])
	assert.NotEmpty(t, statusLog["reason"], "降级路径 endpoints status 必须含 reason")
}

// TestWireAI_DegradePath_EmptyProviders
//
// D16 降级条件 b：cfg.LLM.Providers 为空 map → NewDefaultModel error。
func TestWireAI_DegradePath_EmptyProviders(t *testing.T) {
	db := newTestDB(t)
	repos := newTestRepos(db)
	store := session.NewSQLiteStore(db, 20)
	cfg := makeCfg(withEmptyProviders())

	var buf bytes.Buffer
	logger := captureLogger(&buf)

	sessionH, messageH := wireAI(cfg, repos, store, logger)

	require.NotNil(t, sessionH)
	assert.Nil(t, messageH, "Providers 空时 AIMessageHandler 必须为 nil")

	logs := parseAllLogs(t, &buf)
	loadedLog := findLog(logs, "ai providers loaded")
	require.NotNil(t, loadedLog)
	// configured 是空数组（slog JSON 处理可能 omit 空切片或输出 []）
	if v, ok := loadedLog["configured"]; ok {
		if arr, ok := v.([]interface{}); ok {
			assert.Empty(t, arr, "空 Providers 时 configured 应为空数组")
		}
	}

	statusLog := findLog(logs, "ai endpoints status")
	require.NotNil(t, statusLog)
	assert.Equal(t, false, statusLog["message_enabled"])
}

// TestWireAI_NilLogger_FallbackToDefault
//
// 防御性测试：logger==nil 时 wireAI 内部 fallback 到 slog.Default()，不应 panic。
func TestWireAI_NilLogger_FallbackToDefault(t *testing.T) {
	db := newTestDB(t)
	repos := newTestRepos(db)
	store := session.NewSQLiteStore(db, 20)
	cfg := makeCfg(withEmptyProviders())

	// 不应 panic
	require.NotPanics(t, func() {
		_, _ = wireAI(cfg, repos, store, nil)
	})
}

// TestBuildAITools_NameOrder
//
// 验证工具构造顺序与 NewToolsetAgentFactory 启动日志的预期顺序一致
// （tools 切片顺序决定 factory 内 Declaration().Name 提取顺序）。
func TestBuildAITools_NameOrder(t *testing.T) {
	db := newTestDB(t)
	repos := newTestRepos(db)

	got := buildAITools(repos)
	wantOrder := []string{
		"search_fund",
		"market_quote",
		"market_data",
		"holding_query",
		"profit_calc",
		"platform_summary",
		"history_query",
	}
	for i, tl := range got {
		assert.Equal(t, wantOrder[i], tl.Declaration().Name,
			"工具构造顺序应与 NewToolsetAgentFactory 启动日志期望顺序一致 (idx=%d)", i)
	}
}

// =====================================================================
// 日志解析 helpers
// =====================================================================

// parseAllLogs 把 buf 内多行 JSON 日志解析为 []map[string]any。
func parseAllLogs(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(buf)
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：buf 可能含 non-JSON 行（Logger 拼接错误）→ 跳过空白行后继续
			if err.Error() == "EOF" || strings.Contains(err.Error(), "EOF") {
				break
			}
			require.NoError(t, err, "解析日志 JSON")
		}
		out = append(out, m)
	}
	return out
}

// findLog 找到第一条 msg == want 的日志记录；无则 nil。
func findLog(logs []map[string]any, want string) map[string]any {
	for _, l := range logs {
		if l["msg"] == want {
			return l
		}
	}
	return nil
}

// assertLogContains 断言存在一条 msg==want 的日志，且 attrs 全部命中（值用 == 比较）。
func assertLogContains(t *testing.T, logs []map[string]any, want string, attrs map[string]any) {
	t.Helper()
	got := findLog(logs, want)
	require.NotNil(t, got, "应有日志 msg=%q", want)
	for k, v := range attrs {
		assert.Equal(t, v, got[k], "日志 %q 字段 %s 不匹配", want, k)
	}
}

// 让 import context 不被 IDE 标 unused（部分测试 helper 后续可能用）。
var _ = context.Background
