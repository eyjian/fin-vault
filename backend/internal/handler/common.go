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
//
// 普通业务路由（asset/holding/transaction 等）继续依赖该 fallback=1 行为。
// AI 路由请改用 requireUserIDFromHeader（D15 强校验，缺失返 401）。
func userIDFromHeader(c *gin.Context) uint {
	if v := c.GetHeader("X-User-Id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil && id > 0 {
			return uint(id)
		}
	}
	return 1
}

// requireUserIDFromHeader 强校验 X-User-Id Header（D15）。
//
// AI 路由专用：缺失 / 非法 / 0 → 返 (0, false)，调用方应返 401 Unauthorized
// （spec ai-session "未登录用户被拒绝"）。普通业务路由继续用 userIDFromHeader 的
// fallback=1 路径。二阶段切 JWT 时升级该 helper 即可，调用方代码不变。
func requireUserIDFromHeader(c *gin.Context) (uint, bool) {
	v := c.GetHeader("X-User-Id")
	if v == "" {
		return 0, false
	}
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
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
