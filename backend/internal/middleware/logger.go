package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger 结构化访问日志，含 request_id / latency / status。
//
// 必须在 RequestID 之后挂载，才能拿到 ctx 里的 request_id。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method
		size := c.Writer.Size()
		if raw != "" {
			path = path + "?" + raw
		}
		rid, _ := c.Get(CtxRequestIDKey)

		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}

		slog.LogAttrs(c.Request.Context(), level, "http",
			slog.Any("request_id", rid),
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("ip", clientIP),
			slog.Int("size", size),
		)
	}
}
