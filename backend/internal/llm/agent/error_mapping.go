package agent

import (
	"errors"
	"fmt"
	"strings"

	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// MapSDKError 把 SDK Runner.Run 直接返回的 error 映射为业务 errs.*Error（design.md D11）。
//
// 判别策略：
//
//  1. 优先尝试 errors.As 解包出 HTTP 状态接口（OpenAI 兼容客户端常见暴露
//     `interface{ HTTPStatusCode() int }`）；若命中，按 429 / 5xx / 其它 4xx 分类
//  2. 字符串关键字降级（429 / rate limit / too many requests → ErrAIProviderRateLimited；
//     unknown tool / tool not found / no such tool → ErrAIToolNotFound）
//  3. 兜底 ErrAIRequestFailed
//
// 注：SDK ResponseError 没有 HTTPStatus 字段，HTTP 状态码可能藏在 Error.Message 字符串里
// （如 OpenAI Go SDK "Status code: 429"），故第 2 步降级判别。
func MapSDKError(err error) error {
	if err == nil {
		return nil
	}

	// 1) HTTP status interface 解包
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		switch code := httpErr.HTTPStatusCode(); {
		case code == 429:
			return errs.ErrAIProviderRateLimited.WithCause(err)
		case code >= 500:
			return errs.ErrAIRequestFailed.WithCause(err)
		case code >= 400:
			return errs.ErrAIRequestFailed.WithCause(err)
		}
	}

	// 2) 字符串关键字降级
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "429"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "too many requests"):
		return errs.ErrAIProviderRateLimited.WithCause(err)
	case strings.Contains(msg, "unknown tool"),
		strings.Contains(msg, "tool not found"),
		strings.Contains(msg, "no such tool"):
		return errs.ErrAIToolNotFound.WithCause(err)
	}

	// 3) 兜底
	return errs.ErrAIRequestFailed.WithCause(err)
}

// MapResponseError 把 SDK 流中 event 携带的 *sdkmodel.ResponseError 映射为业务错误。
//
// SDK 没有直接的 HTTP status 字段，结合 Type 字段（stream_error / api_error / flow_error /
// run_error / cancelled）+ Message 关键字综合判别。
func MapResponseError(rerr *sdkmodel.ResponseError) error {
	if rerr == nil {
		return nil
	}
	msg := strings.ToLower(rerr.Message)
	switch {
	case strings.Contains(msg, "429"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "too many requests"):
		return errs.ErrAIProviderRateLimited.WithMsg(rerr.Message)
	case strings.Contains(msg, "unknown tool"),
		strings.Contains(msg, "tool not found"),
		strings.Contains(msg, "no such tool"):
		return errs.ErrAIToolNotFound.WithMsg(rerr.Message)
	}
	switch rerr.Type {
	case sdkmodel.ErrorTypeAPIError,
		sdkmodel.ErrorTypeStreamError,
		sdkmodel.ErrorTypeFlowError,
		sdkmodel.ErrorTypeRunError,
		sdkmodel.ErrorTypeCancelled:
		return errs.ErrAIRequestFailed.WithMsg(rerr.Message)
	}
	return errs.ErrAIRequestFailed.WithMsg(fmt.Sprintf("type=%s msg=%s", rerr.Type, rerr.Message))
}

// MapToolPanic 把工具 panic 转化为业务错误（design.md D11）。
//
// 注：SDK Runner 内部一般会 recover 工具 panic 并通过 tool.response 事件携带 Error 走"软失败"
// 路径（在 event_handler.AggregateEvents 里把 ToolCall.Status 设为 "failed"）。本函数留给
// 业务侧自管 Runner 的极端兜底场景与单测覆盖。
func MapToolPanic(panicVal any) error {
	return errs.ErrAIToolCallFailed.WithMsg(fmt.Sprintf("tool panic: %v", panicVal))
}
