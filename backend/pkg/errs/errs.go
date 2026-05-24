// Package errs 定义 FinVault 业务错误码与错误对象。
//
// 错误码区间约定：
//   - 10000-19999  通用错误（参数/认证/权限/系统）
//   - 30000-39999  core 模块（asset / holding / transaction / portfolio）
//   - 40000-49999  quote 模块（行情 / 汇率）
//   - 50000-59999  ai 模块（对话 / Provider）
//   - 90000-99999  系统级错误（DB / Cache / 不可恢复）
package errs

import (
	"errors"
	"fmt"
)

// Error 业务错误，承载错误码 + 错误消息 + 可选 cause。
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %s", e.Code, e.Message, e.Cause.Error())
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap 返回底层错误。
func (e *Error) Unwrap() error { return e.Cause }

// WithCause 附加底层错误信息（不会修改 Code/Message）。
func (e *Error) WithCause(cause error) *Error {
	cp := *e
	cp.Cause = cause
	return &cp
}

// WithMsg 在不修改错误码的前提下覆盖消息（用于追加上下文）。
func (e *Error) WithMsg(msg string) *Error {
	cp := *e
	cp.Message = msg
	return &cp
}

// New 创建一个新错误。
func New(code int, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// As 把任意 error 解包为业务错误，未识别时返回 nil。
func As(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// =====================================================================
// 通用错误（10000-19999）
// =====================================================================

var (
	ErrSuccess        = New(0, "success")
	ErrInvalidParam   = New(10001, "invalid parameter")
	ErrUnauthorized   = New(10002, "unauthorized")
	ErrForbidden      = New(10003, "forbidden")
	ErrNotFound       = New(10004, "resource not found")
	ErrConflict       = New(10005, "resource conflict")
	ErrTooManyRequest = New(10006, "too many requests")
	ErrInternal       = New(10007, "internal server error")
	ErrTimeout        = New(10008, "operation timeout")
)

// =====================================================================
// core 模块（30000-39999）—— 资产 / 持仓 / 交易
// =====================================================================

var (
	// Asset
	ErrAssetNotFound      = New(30001, "asset not found")
	ErrAssetDuplicated    = New(30002, "asset already exists")
	ErrAssetTypeInvalid   = New(30003, "invalid asset type")
	ErrAssetCodeInvalid   = New(30004, "invalid asset code format")
	ErrAssetProbeNotFound = New(30005, "asset probe not found")
	ErrAssetProbeUpstream = New(30006, "asset probe upstream error")

	// Holding
	ErrHoldingNotFound   = New(30101, "holding not found")
	ErrHoldingDuplicated = New(30102, "holding already exists")
	ErrHoldingClosed     = New(30103, "holding already closed/matured")

	// Transaction
	ErrTxnNotFound          = New(30201, "transaction not found")
	ErrTxnTypeInvalid       = New(30202, "invalid txn_type")
	ErrTxnQuantityInvalid   = New(30203, "quantity must be positive")
	ErrTxnPriceInvalid      = New(30204, "price must be positive")
	ErrTxnAmountInvalid     = New(30205, "amount must be positive")
	ErrInsufficientQuantity = New(30206, "insufficient holding quantity for sell/withdraw")
	ErrTxnDuplicated        = New(30207, "duplicate transaction (external_id conflict)")
	ErrCashCodeInvalid      = New(30208, "invalid cash asset_code, must be CASH-{platform}-{currency}")

	// Platform / Wealth
	ErrPlatformNotFound       = New(30301, "platform not found")
	ErrPlatformCodeDuplicated = New(30302, "platform code already exists")
	ErrWealthDetailMissing    = New(30401, "wealth detail required for wealth asset")
	ErrWealthMatured          = New(30402, "wealth product already matured")
)

// =====================================================================
// quote 模块（40000-49999）—— 行情 / 汇率
// =====================================================================

var (
	ErrPriceQuoteNotFound   = New(40001, "price quote not found")
	ErrExchangeRateNotFound = New(40002, "exchange rate not found")
	ErrQuoteSourceUnsupport = New(40003, "unsupported quote source")
	ErrQuoteFetchFailed     = New(40004, "fetch quote failed")
)

// =====================================================================
// ai 模块（50000-59999）—— 对话 / Provider
// =====================================================================

var (
	ErrAISessionNotFound     = New(50001, "session not found")
	ErrAIRequestFailed       = New(50004, "llm request failed")
	ErrAIToolCallFailed      = New(50005, "llm tool call failed")
	ErrAIProviderRateLimited = New(50006, "llm provider rate limited")
	ErrAIToolNotFound        = New(50007, "tool not found")

	// AI 把脉
	ErrAIPulseUnavailable      = New(50010, "ai pulse diagnosis unavailable")
	ErrAIPulseDataInsufficient = New(50011, "ai pulse data insufficient")
	ErrAIPulseParseFailed      = New(50012, "ai pulse llm output parse failed")
)

// =====================================================================
// 系统级（90000-99999）
// =====================================================================

var (
	ErrDB          = New(90001, "database error")
	ErrCache       = New(90002, "cache error")
	ErrConfig      = New(90003, "config error")
	ErrTransaction = New(90004, "db transaction error")
)
