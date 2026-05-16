package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// Recovery 捕获 panic 并以 90001 系统错误码返回，同时打印堆栈日志。
//
// 必须挂在 Logger 之后，确保 panic 也能被结构化日志记录（带 request_id）。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				rid, _ := c.Get(CtxRequestIDKey)
				slog.Error("panic recovered",
					slog.Any("request_id", rid),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				if c.Writer.Written() {
					return
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError, response.Body{
					Code:    errs.ErrInternal.Code,
					Message: fmt.Sprintf("internal error: %v", r),
				})
			}
		}()
		c.Next()
	}
}
