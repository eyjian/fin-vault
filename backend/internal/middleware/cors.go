package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件。
//
// dev 模式可以传 ["*"] 全开；prod 用白名单。
// 不依赖第三方包，直接处理 Origin / Methods / Headers / Credentials。
func CORS(origins []string) gin.HandlerFunc {
	allowAll := len(origins) == 0
	for _, o := range origins {
		if o == "*" {
			allowAll = true
			break
		}
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if allowAll {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			} else if _, ok := allowed[origin]; ok {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			}
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-Id, X-Request-Id")
			c.Writer.Header().Set("Access-Control-Expose-Headers", "X-Request-Id")
			c.Writer.Header().Set("Access-Control-Max-Age", "3600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
