// Package response 提供统一 HTTP 响应封装。
package response

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// Body 是统一响应结构。
type Body struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

// PageData 分页响应数据。
type PageData struct {
	List  interface{} `json:"list"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Size  int         `json:"size"`
}

const requestIDKey = "request_id"

// OK 成功响应。
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Code:      0,
		Message:   "success",
		Data:      data,
		RequestID: getReqID(c),
	})
}

// Page 分页成功响应。
func Page(c *gin.Context, list interface{}, total int64, page, size int) {
	OK(c, PageData{List: list, Total: total, Page: page, Size: size})
}

// Fail 通用失败响应。任何 error 都可以传进来，会被自动识别为 errs.Error 或归一化为 ErrInternal。
//
// 同时会按 HTTP 状态码自动写一条日志（4xx=Warn，5xx=Error），包含底层 cause，
// 便于事后排查类似 "invalid parameter" 的真实根因。日志的 source 会指向**调用 Fail 的位置**
// （即业务 handler），而不是本文件，方便快速定位。
func Fail(c *gin.Context, err error) {
	if err == nil {
		OK(c, nil)
		return
	}
	rid := getReqID(c)
	method, path := requestInfo(c)

	var be *errs.Error
	if errors.As(err, &be) {
		status := httpStatus(be.Code)
		level := slog.LevelWarn
		if status >= 500 {
			level = slog.LevelError
		}
		attrs := []slog.Attr{
			slog.String("request_id", rid),
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Int("code", be.Code),
			slog.String("message", be.Message),
		}
		if be.Cause != nil {
			attrs = append(attrs, slog.String("cause", be.Cause.Error()))
		}
		logAtCaller(c.Request.Context(), level, "http_error", attrs...)

		c.JSON(status, Body{
			Code:      be.Code,
			Message:   be.Message,
			RequestID: rid,
		})
		return
	}

	// 非业务错误统一按 500 处理
	logAtCaller(c.Request.Context(), slog.LevelError, "http_error",
		slog.String("request_id", rid),
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", http.StatusInternalServerError),
		slog.Int("code", errs.ErrInternal.Code),
		slog.String("err", err.Error()),
	)
	c.JSON(http.StatusInternalServerError, Body{
		Code:      errs.ErrInternal.Code,
		Message:   err.Error(),
		RequestID: rid,
	})
}

// logAtCaller 写一条 slog 日志，并把 source 指向 Fail 的调用方（跳过 Fail/logAtCaller 自身）。
//
// slog.LogAttrs/Logger.Log 默认会把 source 指向调用 slog 的位置，如果直接在 Fail 里调用，
// 所有错误日志都会指向本文件。这里通过 runtime.Callers 取上层 PC，构造 Record 再交给 Handler。
func logAtCaller(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	logger := slog.Default()
	if !logger.Enabled(ctx, level) {
		return
	}
	// skip = 2 跳过 runtime.Callers + logAtCaller，落到 Fail
	// 再 +1 跳过 Fail 自身，落到真正调用 Fail 的 handler 行
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.AddAttrs(attrs...)
	_ = logger.Handler().Handle(ctx, r)
}

func requestInfo(c *gin.Context) (method, path string) {
	if c == nil || c.Request == nil {
		return "", ""
	}
	method = c.Request.Method
	path = c.Request.URL.Path
	if c.Request.URL.RawQuery != "" {
		path += "?" + c.Request.URL.RawQuery
	}
	return
}

// httpStatus 把业务码映射到 HTTP 状态码。
func httpStatus(code int) int {
	switch {
	case code == 0:
		return http.StatusOK
	case code == errs.ErrInvalidParam.Code:
		return http.StatusBadRequest
	case code == errs.ErrUnauthorized.Code:
		return http.StatusUnauthorized
	case code == errs.ErrForbidden.Code:
		return http.StatusForbidden
	case code == errs.ErrNotFound.Code,
		code == errs.ErrAssetNotFound.Code,
		code == errs.ErrHoldingNotFound.Code,
		code == errs.ErrTxnNotFound.Code,
		code == errs.ErrPlatformNotFound.Code,
		code == errs.ErrPriceQuoteNotFound.Code,
		code == errs.ErrExchangeRateNotFound.Code,
		code == errs.ErrAIConversationNotFound.Code:
		return http.StatusNotFound
	case code == errs.ErrConflict.Code,
		code == errs.ErrAssetDuplicated.Code,
		code == errs.ErrHoldingDuplicated.Code,
		code == errs.ErrTxnDuplicated.Code,
		code == errs.ErrPlatformCodeDuplicated.Code:
		return http.StatusConflict
	case code == errs.ErrTooManyRequest.Code:
		return http.StatusTooManyRequests
	}
	// core/quote/ai 业务校验错误 → 400；系统级错误 → 500
	if code >= 30000 && code < 90000 {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func getReqID(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
