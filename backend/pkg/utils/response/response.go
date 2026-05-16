// Package response 提供统一 HTTP 响应封装。
package response

import (
	"errors"
	"net/http"

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
func Fail(c *gin.Context, err error) {
	if err == nil {
		OK(c, nil)
		return
	}
	var be *errs.Error
	if errors.As(err, &be) {
		c.JSON(httpStatus(be.Code), Body{
			Code:      be.Code,
			Message:   be.Message,
			RequestID: getReqID(c),
		})
		return
	}
	c.JSON(http.StatusInternalServerError, Body{
		Code:      errs.ErrInternal.Code,
		Message:   err.Error(),
		RequestID: getReqID(c),
	})
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
