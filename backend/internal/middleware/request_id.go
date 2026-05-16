// Package middleware 提供 Gin 中间件实现。
//
// 中间件链顺序（强制，与 ARCHITECTURE.md §5 对齐）：
//   RequestID → Logger → Recovery → CORS → Auth(可选) → 业务
//
// Recovery 必须在 Logger 之后：panic 也能被结构化日志捕获且带 request_id。
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// HeaderRequestID HTTP Header 与日志键。
	HeaderRequestID = "X-Request-Id"
	// CtxRequestIDKey gin.Context 中存储 request_id 的 key。
	CtxRequestIDKey = "request_id"
)

// RequestID 生成或透传 X-Request-Id（UUID v7），同时写入响应头与 ctx。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(HeaderRequestID)
		if rid == "" {
			id, err := uuid.NewV7()
			if err != nil {
				rid = uuid.NewString()
			} else {
				rid = id.String()
			}
		}
		c.Set(CtxRequestIDKey, rid)
		c.Writer.Header().Set(HeaderRequestID, rid)
		c.Next()
	}
}
