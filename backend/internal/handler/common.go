// Package handler 提供 HTTP 接口适配层（Gin）。
//
// 严格依赖：
//   - service / pkg/utils/response / pkg/errs
//   - 不直接依赖 repository / gorm / llm 实现
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// userIDFromHeader 从 X-User-Id 取用户 ID（默认 1）。
func userIDFromHeader(c *gin.Context) uint {
	if v := c.GetHeader("X-User-Id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil && id > 0 {
			return uint(id)
		}
	}
	return 1
}

// queryUint 从 query 取无符号整数；失败返回 0。
func queryUint(c *gin.Context, key string) uint {
	v := c.Query(key)
	if v == "" {
		return 0
	}
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return uint(id)
}

// queryInt 取整数（默认值 def）。
func queryInt(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
