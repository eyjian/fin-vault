package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// AssetHandler 资产 HTTP 适配。
type AssetHandler struct {
	svc      *service.AssetService
	probeSvc *service.AssetProbeService
}

// NewAssetHandler 构造。
//
// probeSvc 可为 nil；为 nil 时 GET /assets/probe 走 service 层 nil-fetcher 路径
// 返回 502 ErrAssetProbeUpstream（与 LLM 不可用 / 行情不可用的"降级失败"语义一致）。
func NewAssetHandler(svc *service.AssetService, probeSvc *service.AssetProbeService) *AssetHandler {
	return &AssetHandler{svc: svc, probeSvc: probeSvc}
}

// Register 挂载路由（v1 group）。
func (h *AssetHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/assets")
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/probe", h.probe)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
}

// AssetCreateReq POST /assets 请求体。
type AssetCreateReq struct {
	AssetCode        string               `json:"asset_code" binding:"required"`
	Name             string               `json:"name" binding:"required"`
	AssetType        string               `json:"asset_type" binding:"required"`
	Currency         string               `json:"currency"`
	IssuerPlatformID *uint                `json:"issuer_platform_id"`
	RiskLevel        string               `json:"risk_level"`
	Status           string               `json:"status"`
	Remark           string               `json:"remark"`
	FundDetail       *domain.FundDetail   `json:"fund_detail,omitempty"`
	StockDetail      *domain.StockDetail  `json:"stock_detail,omitempty"`
	WealthDetail     *domain.WealthDetail `json:"wealth_detail,omitempty"`
}

func (h *AssetHandler) create(c *gin.Context) {
	var req AssetCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	asset, err := h.svc.Create(c.Request.Context(), service.CreateAssetInput{
		UserID:           userIDFromHeader(c),
		AssetCode:        req.AssetCode,
		Name:             req.Name,
		AssetType:        domain.AssetType(req.AssetType),
		Currency:         req.Currency,
		IssuerPlatformID: req.IssuerPlatformID,
		RiskLevel:        req.RiskLevel,
		Status:           req.Status,
		Remark:           req.Remark,
		FundDetail:       req.FundDetail,
		StockDetail:      req.StockDetail,
		WealthDetail:     req.WealthDetail,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, response.Body{Code: 0, Message: "created", Data: asset})
}

// AssetUpdateReq PUT /assets/:id 请求体。
type AssetUpdateReq struct {
	Name             string               `json:"name"`
	Currency         string               `json:"currency"`
	IssuerPlatformID *uint                `json:"issuer_platform_id"`
	RiskLevel        string               `json:"risk_level"`
	Status           string               `json:"status"`
	Remark           string               `json:"remark"`
	FundDetail       *domain.FundDetail   `json:"fund_detail,omitempty"`
	StockDetail      *domain.StockDetail  `json:"stock_detail,omitempty"`
	WealthDetail     *domain.WealthDetail `json:"wealth_detail,omitempty"`
}

func (h *AssetHandler) update(c *gin.Context) {
	id := pathUint(c, "id")
	if id == 0 {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	var req AssetUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	asset, err := h.svc.Update(c.Request.Context(), service.UpdateAssetInput{
		UserID:           userIDFromHeader(c),
		ID:               id,
		Name:             req.Name,
		Currency:         req.Currency,
		IssuerPlatformID: req.IssuerPlatformID,
		RiskLevel:        req.RiskLevel,
		Status:           req.Status,
		Remark:           req.Remark,
		FundDetail:       req.FundDetail,
		StockDetail:      req.StockDetail,
		WealthDetail:     req.WealthDetail,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, asset)
}

func (h *AssetHandler) get(c *gin.Context) {
	id := pathUint(c, "id")
	if id == 0 {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	asset, err := h.svc.Get(c.Request.Context(), userIDFromHeader(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, asset)
}

func (h *AssetHandler) list(c *gin.Context) {
	in := service.AssetListInput{
		UserID:          userIDFromHeader(c),
		AssetType:       domain.AssetType(c.Query("asset_type")),
		Status:          c.Query("status"),
		Currency:        c.Query("currency"),
		Keyword:         c.Query("keyword"),
		Page:            queryInt(c, "page", 1),
		PageSize:        queryInt(c, "page_size", 20),
		IncludeHoldings: c.Query("include_holdings") == "true",
	}
	list, total, err := h.svc.List(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, in.Page, in.PageSize)
}

func (h *AssetHandler) delete(c *gin.Context) {
	id := pathUint(c, "id")
	if id == 0 {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userIDFromHeader(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// probe GET /assets/probe?asset_type=fund|stock&asset_code=xxx&market=SH
//
// 用于资产录入页"按代码自动填充"。仅查公开数据，不做用户隔离，
// 但仍要求经过 middleware.Auth（防止匿名滥用外部 API）。
func (h *AssetHandler) probe(c *gin.Context) {
	if h.probeSvc == nil {
		response.Fail(c, errs.ErrAssetProbeUpstream.WithMsg("asset probe not available"))
		return
	}
	args := service.ProbeArgs{
		AssetType: c.Query("asset_type"),
		AssetCode: c.Query("asset_code"),
		Market:    c.Query("market"),
	}
	res, err := h.probeSvc.Probe(c.Request.Context(), args)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
