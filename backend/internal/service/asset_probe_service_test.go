package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/platformapi"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// AssetProbeService.Probe —— 入参校验 + 错误归一化 + 字段映射
// =====================================================================

// mockMetaFetcher 测试桩。
type mockMetaFetcher struct {
	source   string
	supports func(platformapi.AssetKey) bool
	fetch    func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error)
}

func (m *mockMetaFetcher) Source() string { return m.source }
func (m *mockMetaFetcher) Supports(a platformapi.AssetKey) bool {
	if m.supports != nil {
		return m.supports(a)
	}
	return true
}
func (m *mockMetaFetcher) FetchMeta(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
	return m.fetch(ctx, a)
}

func TestAssetProbeService_Probe_Validation(t *testing.T) {
	svc := NewAssetProbeService(&mockMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			t.Fatal("should not reach fetcher when params invalid")
			return nil, nil
		},
	})

	// asset_type 非法
	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "wealth", AssetCode: "x"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)

	// asset_code 空
	_, err = svc.Probe(context.Background(), ProbeArgs{AssetType: "fund", AssetCode: "  "})
	require.Error(t, err)
	be = errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)

	// stock 缺 market
	_, err = svc.Probe(context.Background(), ProbeArgs{AssetType: "stock", AssetCode: "600519"})
	require.Error(t, err)
	be = errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)
}

func TestAssetProbeService_Probe_FetchNotFound(t *testing.T) {
	svc := NewAssetProbeService(&mockMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrNoData
		},
	})
	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "fund", AssetCode: "999999"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAssetProbeNotFound.Code, be.Code)
}

func TestAssetProbeService_Probe_FetchUpstreamError(t *testing.T) {
	svc := NewAssetProbeService(&mockMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, errors.New("network timeout")
		},
	})
	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "fund", AssetCode: "110022"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAssetProbeUpstream.Code, be.Code)
}

func TestAssetProbeService_Probe_FetcherUnsupported(t *testing.T) {
	svc := NewAssetProbeService(&mockMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrUnsupportedAsset
		},
	})
	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "stock", AssetCode: "AAPL", Market: "US"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)
}

func TestAssetProbeService_Probe_Fund_Success(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.Local)
	svc := NewAssetProbeService(&mockMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			assert.Equal(t, "fund", a.AssetType)
			assert.Equal(t, "110022", a.AssetCode)
			return &platformapi.AssetMeta{
				Name:      "易方达消费行业",
				Source:    "api_eastmoney",
				Company:   "易方达基金",
				Manager:   "萧楠",
				FundType:  "equity",
				LatestNAV: decimal.RequireFromString("2.6512"),
				NAVDate:   now,
			}, nil
		},
	})
	res, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "fund", AssetCode: "110022"})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "易方达消费行业", res.Name)
	assert.Equal(t, "易方达基金", res.Company)
	assert.Equal(t, "萧楠", res.Manager)
	assert.Equal(t, "equity", res.FundType)
	assert.Equal(t, "2.6512", res.LatestNAV)
	assert.Equal(t, "2026-05-15", res.NAVDate)
	assert.Equal(t, "api_eastmoney", res.Source)
	// 股票字段为空（omitempty 验证不在 unit test 而在 handler 层 JSON encode）
	assert.Empty(t, res.Industry)
	assert.Empty(t, res.Sector)
	assert.Empty(t, res.LatestPrice)
}

func TestAssetProbeService_Probe_Stock_Success(t *testing.T) {
	listing := time.Date(2001, 8, 27, 0, 0, 0, 0, time.Local)
	svc := NewAssetProbeService(&mockMetaFetcher{
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			assert.Equal(t, "stock", a.AssetType)
			assert.Equal(t, "600519", a.AssetCode)
			assert.Equal(t, "SH", a.Market)
			return &platformapi.AssetMeta{
				Name:        "贵州茅台",
				Source:      "api_eastmoney",
				Market:      "SH",
				Industry:    "白酒",
				Sector:      "消费",
				LatestPrice: decimal.RequireFromString("1728"),
				ListingDate: listing,
			}, nil
		},
	})
	res, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "stock", AssetCode: "600519", Market: "sh"})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "贵州茅台", res.Name)
	assert.Equal(t, "SH", res.Market)
	assert.Equal(t, "白酒", res.Industry)
	assert.Equal(t, "消费", res.Sector)
	assert.Equal(t, "1728", res.LatestPrice)
	assert.Equal(t, "2001-08-27", res.ListingDate)
}

func TestAssetProbeService_NilFetcher(t *testing.T) {
	svc := NewAssetProbeService(nil)
	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "fund", AssetCode: "110022"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAssetProbeUpstream.Code, be.Code)
}

func TestAssetProbeService_Fallback_SecondSourceSucceeds(t *testing.T) {
	// 主源东方财富失败（模拟网络错误），备用源新浪成功
	primary := &mockMetaFetcher{
		source:   "api_eastmoney",
		supports: func(a platformapi.AssetKey) bool { return true },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, errors.New("network timeout")
		},
	}
	fallback := &mockMetaFetcher{
		source:   "api_sina",
		supports: func(a platformapi.AssetKey) bool { return a.AssetType == "stock" },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return &platformapi.AssetMeta{
				Name:        "腾讯控股",
				Source:      "api_sina",
				Market:      "HK",
				LatestPrice: decimal.RequireFromString("380.20"),
			}, nil
		},
	}
	svc := NewAssetProbeService(primary, fallback)

	res, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "stock", AssetCode: "00700", Market: "HK"})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "腾讯控股", res.Name)
	assert.Equal(t, "api_sina", res.Source)
	assert.Equal(t, "HK", res.Market)
	assert.Equal(t, "380.2", res.LatestPrice)
}

func TestAssetProbeService_Fallback_AllSourcesFail(t *testing.T) {
	// 两个源都失败，应返回 upstream error（网络错误优先于 NoData）
	primary := &mockMetaFetcher{
		source:   "api_eastmoney",
		supports: func(a platformapi.AssetKey) bool { return true },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, errors.New("connection refused")
		},
	}
	fallback := &mockMetaFetcher{
		source:   "api_sina",
		supports: func(a platformapi.AssetKey) bool { return true },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrNoData
		},
	}
	svc := NewAssetProbeService(primary, fallback)

	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "stock", AssetCode: "99999", Market: "SH"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	// 网络错误优先级高于 NoData，但 lastErr 是最后一个错误(NoData)，
	// 而 allNoData=false（因为 primary 返回的不是 ErrNoData），走 default 分支 → ErrAssetProbeUpstream
	assert.Equal(t, errs.ErrAssetProbeUpstream.Code, be.Code)
}

func TestAssetProbeService_Fallback_AllNoData(t *testing.T) {
	// 两个源都返回 ErrNoData，应返回 ErrAssetProbeNotFound
	primary := &mockMetaFetcher{
		source:   "api_eastmoney",
		supports: func(a platformapi.AssetKey) bool { return true },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrNoData
		},
	}
	fallback := &mockMetaFetcher{
		source:   "api_sina",
		supports: func(a platformapi.AssetKey) bool { return true },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrNoData
		},
	}
	svc := NewAssetProbeService(primary, fallback)

	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "stock", AssetCode: "99999", Market: "SH"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrAssetProbeNotFound.Code, be.Code)
}

func TestAssetProbeService_Fallback_UnsupportedSkipped(t *testing.T) {
	// 主源不支持该资产（如基金），备用源也不支持，最终应返回 ErrInvalidParam
	primary := &mockMetaFetcher{
		source:   "api_eastmoney",
		supports: func(a platformapi.AssetKey) bool { return false },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrUnsupportedAsset
		},
	}
	fallback := &mockMetaFetcher{
		source:   "api_sina",
		supports: func(a platformapi.AssetKey) bool { return false },
		fetch: func(ctx context.Context, a platformapi.AssetKey) (*platformapi.AssetMeta, error) {
			return nil, platformapi.ErrUnsupportedAsset
		},
	}
	svc := NewAssetProbeService(primary, fallback)

	_, err := svc.Probe(context.Background(), ProbeArgs{AssetType: "wealth", AssetCode: "P001"})
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	// asset_type 非法在入参校验阶段就被拦截
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)
}
