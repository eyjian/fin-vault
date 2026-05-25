package agent

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// httpStatusErr 模拟 OpenAI 兼容客户端透传的"含 HTTP 状态码"错误，验证 errors.As 解包路径。
type httpStatusErr struct {
	code int
	msg  string
}

func (h *httpStatusErr) Error() string         { return h.msg }
func (h *httpStatusErr) HTTPStatusCode() int   { return h.code }

func assertErrCode(t *testing.T, err error, want *errs.Error) {
	t.Helper()
	require.Error(t, err)
	got := errs.As(err)
	require.NotNil(t, got, "expected *errs.Error, got %T: %v", err, err)
	assert.Equal(t, want.Code, got.Code, "error code mismatch (got=%d, want=%d, msg=%s)", got.Code, want.Code, got.Message)
}

// =====================================================================
// MapSDKError
// =====================================================================

func TestMapSDKError_Nil_ReturnsNil(t *testing.T) {
	assert.Nil(t, MapSDKError(nil))
}

func TestMapSDKError_HTTP429_ReturnsRateLimited(t *testing.T) {
	err := &httpStatusErr{code: http.StatusTooManyRequests, msg: "boom"}
	got := MapSDKError(err)
	assertErrCode(t, got, errs.ErrAIProviderRateLimited)
	// 保留 cause
	assert.True(t, errors.Is(got, got))
	wrapped := errs.As(got)
	assert.Equal(t, err, wrapped.Cause, "cause 应保留")
}

func TestMapSDKError_HTTP500_ReturnsRequestFailed(t *testing.T) {
	err := &httpStatusErr{code: http.StatusInternalServerError, msg: "oops"}
	assertErrCode(t, MapSDKError(err), errs.ErrAIRequestFailed)
}

func TestMapSDKError_Other4xx_ReturnsRequestFailed(t *testing.T) {
	err := &httpStatusErr{code: http.StatusBadRequest, msg: "bad request"}
	assertErrCode(t, MapSDKError(err), errs.ErrAIRequestFailed)
}

func TestMapSDKError_KeywordRateLimit_ReturnsRateLimited(t *testing.T) {
	cases := []string{
		"Status code: 429 too many requests",
		"server returned rate limit error",
		"Too Many Requests",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assertErrCode(t, MapSDKError(errors.New(msg)), errs.ErrAIProviderRateLimited)
		})
	}
}

func TestMapSDKError_KeywordUnknownTool_ReturnsToolNotFound(t *testing.T) {
	cases := []string{
		"unknown tool: search_fund",
		"tool not found",
		"no such tool registered",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assertErrCode(t, MapSDKError(errors.New(msg)), errs.ErrAIToolNotFound)
		})
	}
}

func TestMapSDKError_UnclassifiedFallback_ReturnsRequestFailed(t *testing.T) {
	assertErrCode(t, MapSDKError(errors.New("some random failure")), errs.ErrAIRequestFailed)
}

// =====================================================================
// MapResponseError
// =====================================================================

func TestMapResponseError_Nil_ReturnsNil(t *testing.T) {
	assert.Nil(t, MapResponseError(nil))
}

func TestMapResponseError_RateLimitMessage(t *testing.T) {
	got := MapResponseError(&sdkmodel.ResponseError{
		Type:    sdkmodel.ErrorTypeAPIError,
		Message: "Rate limit exceeded",
	})
	assertErrCode(t, got, errs.ErrAIProviderRateLimited)
}

func TestMapResponseError_UnknownToolMessage(t *testing.T) {
	got := MapResponseError(&sdkmodel.ResponseError{
		Type:    sdkmodel.ErrorTypeFlowError,
		Message: "unknown tool: foo",
	})
	assertErrCode(t, got, errs.ErrAIToolNotFound)
}

func TestMapResponseError_TypeBasedMapping(t *testing.T) {
	cases := []struct {
		name string
		t    string
	}{
		{"api_error", sdkmodel.ErrorTypeAPIError},
		{"stream_error", sdkmodel.ErrorTypeStreamError},
		{"flow_error", sdkmodel.ErrorTypeFlowError},
		{"run_error", sdkmodel.ErrorTypeRunError},
		{"cancelled", sdkmodel.ErrorTypeCancelled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MapResponseError(&sdkmodel.ResponseError{
				Type:    c.t,
				Message: "something happened",
			})
			assertErrCode(t, got, errs.ErrAIRequestFailed)
		})
	}
}

func TestMapResponseError_UnknownTypeFallback(t *testing.T) {
	got := MapResponseError(&sdkmodel.ResponseError{
		Type:    "weird_unknown_type",
		Message: "x",
	})
	assertErrCode(t, got, errs.ErrAIRequestFailed)
}

// =====================================================================
// MapToolPanic
// =====================================================================

func TestMapToolPanic_String(t *testing.T) {
	got := MapToolPanic("boom")
	assertErrCode(t, got, errs.ErrAIToolCallFailed)
	assert.Contains(t, got.Error(), "boom")
}

func TestMapToolPanic_Object(t *testing.T) {
	got := MapToolPanic(struct{ X int }{X: 42})
	assertErrCode(t, got, errs.ErrAIToolCallFailed)
	assert.Contains(t, got.Error(), "tool panic")
}
