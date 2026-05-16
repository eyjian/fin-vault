package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// AuthConfig 中间件配置（避免 import bootstrap 包造成循环依赖）。
type AuthConfig struct {
	// Mode "local"=单用户模式（直接信任 X-User-Id 或 fallback 到 DefaultUserID）。
	// "jwt"=校验 JWT token；当前一阶段未启用。
	Mode string
	// DefaultUserID 当 Header 缺失时使用的默认用户 ID（一阶段固定 1）。
	DefaultUserID uint
}

const (
	// HeaderUserID HTTP Header 用户 ID。
	HeaderUserID = "X-User-Id"
	// CtxUserIDKey gin.Context 中存储 user_id 的 key。
	CtxUserIDKey = "user_id"
)

// Auth 第一阶段单用户认证：从 X-User-Id Header 取用户 ID，缺失时用 DefaultUserID。
//
// 二阶段切到 JWT 时只需扩展 Mode=="jwt" 分支，不影响 handler 取值方式。
func Auth(cfg AuthConfig) gin.HandlerFunc {
	def := cfg.DefaultUserID
	if def == 0 {
		def = 1
	}
	return func(c *gin.Context) {
		uid := def
		if v := c.GetHeader(HeaderUserID); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 {
				uid = uint(n)
			}
		}
		c.Set(CtxUserIDKey, uid)
		c.Next()
	}
}

// UserIDFrom 从 ctx 取 user_id，handler 层可直接调用。
func UserIDFrom(c *gin.Context) uint {
	if v, ok := c.Get(CtxUserIDKey); ok {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 1
}
