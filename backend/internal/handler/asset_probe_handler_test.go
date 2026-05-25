// Package handler —— AssetHandler.probe 单元测试。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/platformapi"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// mock fetcher（与 service 单测用法一致）
// =====================================================================

type fakeMetaFetcher struct {
	fetch func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error)
}

func (f *fakeMetaFetcher) Source() string { return "api_eastmoney" }
func (f *fakeMetaFetcher) Supports(a platformapi.AssetKey) bool {
	return a.AssetType == "fund" || a.AssetType == "stock"
}
func (f *fakeMetaFetcher) FetchMeta(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
	return f.fetch(ctx, a)
}

// setupProbeRouter 构造一个仅注册 /assets/probe 的 gin engine。
//
// 不挂 middleware.Auth（handler 单测只覆盖业务路径；401 由全局中间件拦截，
// 在 spec 中作为契约约束，由 e2e/集成层覆盖）。
func setupProbeRouter(probeSvc *service.AssetProbeService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewAssetHandler(nil, probeSvc) // 仅测 probe，svc=nil 不影响
	r := gin.New()
	v1 := r.Group("/api/v1")
	// 仅注册 probe 路径（避免 nil svc 引发其他路由 panic）
	v1.GET("/assets/probe", h.probe)
	return r
}

func doProbeReq(r *gin.Engine, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/probe?"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func parseProbeBody(t *testing.T, w *httptest.ResponseRecorder) response.Body {
	t.Helper()
	var b response.Body
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &b))
	return b
}

// =====================================================================
// 200 happy path
// =====================================================================

func TestAssetHandler_Probe_Fund_Success(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			assert.Equal(t, "fund", a.AssetType)
			assert.Equal(t, "110022", a.AssetCode)
			return &platformapi.AssetMeta{
				Name: "易方达消费行业", Source: "api_eastmoney",
				Company: "易方达基金", Manager: "萧楠", FundType: "equity",
				LatestNAV: decimal.RequireFromString("2.6512"),
			}, nil
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=fund&asset_code=110022")
	require.Equal(t, http.StatusOK, w.Code)

	body := parseProbeBody(t, w)
	assert.Equal(t, 0, body.Code)

	// data 解析
	rawData, _ := json.Marshal(body.Data)
	var res service.ProbeResult
	require.NoError(t, json.Unmarshal(rawData, &res))
	assert.Equal(t, "易方达消费行业", res.Name)
	assert.Equal(t, "易方达基金", res.Company)
	assert.Equal(t, "萧楠", res.Manager)
	assert.Equal(t, "equity", res.FundType)
	assert.Equal(t, "2.6512", res.LatestNAV)
	assert.Equal(t, "api_eastmoney", res.Source)
	// 股票字段应在 JSON 中省略
	rawJSON := string(rawData)
	assert.NotContains(t, rawJSON, `"industry"`)
	assert.NotContains(t, rawJSON, `"sector"`)
	assert.NotContains(t, rawJSON, `"latest_price"`)
}

func TestAssetHandler_Probe_Stock_AShare_Success(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			assert.Equal(t, "stock", a.AssetType)
			assert.Equal(t, "SH", a.Market)
			return &platformapi.AssetMeta{
				Name: "贵州茅台", Source: "api_eastmoney",
				Market: "SH", Industry: "白酒", Sector: "消费",
				LatestPrice: decimal.RequireFromString("1728"),
			}, nil
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=stock&asset_code=600519&market=SH")
	require.Equal(t, http.StatusOK, w.Code)

	body := parseProbeBody(t, w)
	rawData, _ := json.Marshal(body.Data)
	var res service.ProbeResult
	require.NoError(t, json.Unmarshal(rawData, &res))
	assert.Equal(t, "贵州茅台", res.Name)
	assert.Equal(t, "SH", res.Market)
	assert.Equal(t, "白酒", res.Industry)
	assert.Equal(t, "消费", res.Sector)
	assert.Equal(t, "1728", res.LatestPrice)
}

// =====================================================================
// 422 / 400 invalid params
// =====================================================================

func TestAssetHandler_Probe_InvalidAssetType(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			t.Fatal("should not reach fetcher")
			return nil, nil
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=wealth&asset_code=LC001")
	assert.Equal(t, http.StatusBadRequest, w.Code) // ErrInvalidParam → 400

	body := parseProbeBody(t, w)
	assert.Equal(t, errs.ErrInvalidParam.Code, body.Code)
}

func TestAssetHandler_Probe_MissingAssetCode(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			t.Fatal("should not reach fetcher")
			return nil, nil
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=fund")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	body := parseProbeBody(t, w)
	assert.Equal(t, errs.ErrInvalidParam.Code, body.Code)
}

func TestAssetHandler_Probe_MissingMarketForStock(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			t.Fatal("should not reach fetcher")
			return nil, nil
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=stock&asset_code=600519")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =====================================================================
// 404 not found
// =====================================================================

func TestAssetHandler_Probe_NotFound(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrNoData
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=fund&asset_code=999999")
	assert.Equal(t, http.StatusNotFound, w.Code)

	body := parseProbeBody(t, w)
	assert.Equal(t, errs.ErrAssetProbeNotFound.Code, body.Code)
}

// =====================================================================
// 502 upstream error
// =====================================================================

func TestAssetHandler_Probe_UpstreamError(t *testing.T) {
	svc := service.NewAssetProbeService(&fakeMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, errors.New("network unreachable")
		},
	})
	r := setupProbeRouter(svc)
	w := doProbeReq(r, "asset_type=fund&asset_code=110022")
	assert.Equal(t, http.StatusBadGateway, w.Code)

	body := parseProbeBody(t, w)
	assert.Equal(t, errs.ErrAssetProbeUpstream.Code, body.Code)
}

// =====================================================================
// probeSvc 未注入：Handler 自身降级
// =====================================================================

func TestAssetHandler_Probe_NoServiceInjected(t *testing.T) {
	r := setupProbeRouter(nil) // probeSvc nil
	w := doProbeReq(r, "asset_type=fund&asset_code=110022")
	assert.Equal(t, http.StatusBadGateway, w.Code)

	body := parseProbeBody(t, w)
	assert.Equal(t, errs.ErrAssetProbeUpstream.Code, body.Code)
}
