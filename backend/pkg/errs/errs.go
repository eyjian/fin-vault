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
	ErrInvalidParam   = New(10001, "参数不合法")
	ErrUnauthorized   = New(10002, "未登录或登录已失效")
	ErrForbidden      = New(10003, "没有访问权限")
	ErrNotFound       = New(10004, "资源不存在")
	ErrConflict       = New(10005, "资源冲突")
	ErrTooManyRequest = New(10006, "请求太频繁，请稍后重试")
	ErrInternal       = New(10007, "服务器内部错误")
	ErrTimeout        = New(10008, "操作超时")
)

// =====================================================================
// core 模块（30000-39999）—— 资产 / 持仓 / 交易
// =====================================================================

var (
	// Asset
	ErrAssetNotFound      = New(30001, "资产不存在")
	ErrAssetDuplicated    = New(30002, "该资产已存在")
	ErrAssetTypeInvalid   = New(30003, "资产类型不合法")
	ErrAssetCodeInvalid   = New(30004, "资产代码格式不合法")
	ErrAssetProbeNotFound = New(30005, "未查到该资产信息，请核对代码与市场")
	ErrAssetProbeUpstream = New(30006, "行情源暂不可用，请稍后重试或手动填写")

	// Holding
	ErrHoldingNotFound   = New(30101, "持仓不存在")
	ErrHoldingDuplicated = New(30102, "持仓已存在")
	ErrHoldingClosed     = New(30103, "持仓已平仓或到期")

	// Transaction
	ErrTxnNotFound          = New(30201, "交易流水不存在")
	ErrTxnTypeInvalid       = New(30202, "交易类型不合法")
	ErrTxnQuantityInvalid   = New(30203, "数量必须为正数")
	ErrTxnPriceInvalid      = New(30204, "价格必须为正数")
	ErrTxnAmountInvalid     = New(30205, "金额必须为正数")
	ErrInsufficientQuantity = New(30206, "可用持仓不足，无法卖出/赎回")
	ErrTxnDuplicated        = New(30207, "交易重复（external_id 冲突）")
	ErrCashCodeInvalid      = New(30208, "现金资产代码不合法，须为 CASH-{platform}-{currency}")

	// Platform / Wealth
	ErrPlatformNotFound       = New(30301, "平台不存在")
	ErrPlatformCodeDuplicated = New(30302, "平台代码已存在")
	ErrWealthDetailMissing    = New(30401, "理财产品必须填写详情")
	ErrWealthMatured          = New(30402, "理财产品已到期")
)

// =====================================================================
// quote 模块（40000-49999）—— 行情 / 汇率
// =====================================================================

var (
	ErrPriceQuoteNotFound   = New(40001, "行情不存在")
	ErrExchangeRateNotFound = New(40002, "汇率不存在")
	ErrQuoteSourceUnsupport = New(40003, "不支持的行情来源")
	ErrQuoteFetchFailed     = New(40004, "拉取行情失败")
)

// =====================================================================
// ai 模块（50000-59999）—— 对话 / Provider
// =====================================================================

var (
	ErrAISessionNotFound     = New(50001, "会话不存在")
	ErrAIRequestFailed       = New(50004, "AI 请求失败")
	ErrAIToolCallFailed      = New(50005, "AI 工具调用失败")
	ErrAIProviderRateLimited = New(50006, "AI 服务提供商限流")
	ErrAIToolNotFound        = New(50007, "AI 工具不存在")

	// AI 把脉
	ErrAIPulseUnavailable      = New(50010, "AI 把脉服务不可用")
	ErrAIPulseDataInsufficient = New(50011, "AI 把脉数据不足")
	ErrAIPulseParseFailed      = New(50012, "AI 把脉输出解析失败")
)

// =====================================================================
// 系统级（90000-99999）
// =====================================================================

var (
	ErrDB          = New(90001, "数据库错误")
	ErrCache       = New(90002, "缓存错误")
	ErrConfig      = New(90003, "配置错误")
	ErrTransaction = New(90004, "数据库事务错误")
)
