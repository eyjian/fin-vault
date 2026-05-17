package bootstrap

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// InitLogger 根据 LogConfig 初始化 slog 默认 Logger。
//
// 输出策略：
//   - cfg.File 非空 → 写入文件（lumberjack 按大小滚动 + 保留 N 份 + 可选 gzip）
//   - cfg.File 非空且 cfg.Console=true → 同时写 stdout（开发期方便观察）
//   - cfg.File 为空 → 仅写 stdout
//
// 始终启用 source（文件名:行号），并将绝对路径截短为相对路径，便于排查问题时快速定位。
//
// 调用一次即可，main 在 LoadConfig 之后立即调用。
func InitLogger(cfg LogConfig) *slog.Logger {
	level := parseLogLevel(cfg.Level)
	opts := &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: shortSourceAttr,
	}

	writer := buildLogWriter(cfg)

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	case "text", "console", "":
		handler = slog.NewTextHandler(writer, opts)
	default:
		handler = slog.NewTextHandler(writer, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// shortSourceAttr 把 slog 默认的绝对路径 source 截短为更易读的相对路径。
//
// 截取策略（按优先级）：
//  1. 命中 "/backend/" → 保留之后的部分（backend 下的相对路径）
//  2. 否则保留最后两段路径（pkg/file.go），避免过长
//
// text 格式输出形如：source=internal/handler/asset_handler.go:53
func shortSourceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key != slog.SourceKey {
		return a
	}
	src, ok := a.Value.Any().(*slog.Source)
	if !ok || src == nil || src.File == "" {
		return a
	}
	src.File = shortenPath(src.File)
	// 同步隐藏 Function 字段（默认会以函数名输出，太长且不必要）
	src.Function = ""
	return a
}

func shortenPath(p string) string {
	const marker = "/backend/"
	if i := strings.Index(p, marker); i >= 0 {
		return p[i+len(marker):]
	}
	// 回退：仅保留最后两段
	parts := strings.Split(p, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return p
}

// buildLogWriter 构造日志输出 writer，按需启用 lumberjack 滚动。
func buildLogWriter(cfg LogConfig) io.Writer {
	if strings.TrimSpace(cfg.File) == "" {
		return os.Stdout
	}

	// 确保目录存在；失败时回退到 stdout，避免进程因日志目录写入失败而无法启动
	if dir := filepath.Dir(cfg.File); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			slog.Warn("create log dir failed, fallback to stdout", "dir", dir, "err", err)
			return os.Stdout
		}
	}

	maxSize := cfg.MaxSizeMB
	if maxSize <= 0 {
		maxSize = 50
	}
	maxBackups := cfg.MaxBackups
	if maxBackups < 0 {
		maxBackups = 0
	}
	maxAge := cfg.MaxAgeDays
	if maxAge < 0 {
		maxAge = 0
	}

	rotator := &lumberjack.Logger{
		Filename:   cfg.File,
		MaxSize:    maxSize,    // MB
		MaxBackups: maxBackups, // 保留旧文件数量
		MaxAge:     maxAge,     // 天
		Compress:   cfg.Compress,
		LocalTime:  true,
	}

	if cfg.Console {
		return io.MultiWriter(os.Stdout, rotator)
	}
	return rotator
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}
