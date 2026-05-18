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
	svc *service.AssetService
}

// NewAssetHandler 构造。
func NewAssetHandler(svc *service.AssetService) *AssetHandler {
	return &AssetHandler{svc: svc}
}

// Register 挂载路由（v1 group）。
func (h *AssetHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/assets")
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
}

// AssetCreateReq POST /assets 请求体。
type AssetCreateReq struct {
	AssetCode        string                `json:"asset_code" binding:"required"`
	Name             string                `json:"name" binding:"required"`
	AssetType        string                `json:"asset_type" binding:"required"`
	Currency         string                `json:"currency"`
	IssuerPlatformID *uint                 `json:"issuer_platform_id"`
	RiskLevel        string                `json:"risk_level"`
	Status           string                `json:"status"`
	Remark           string                `json:"remark"`
	FundDetail       *domain.FundDetail    `json:"fund_detail,omitempty"`
	StockDetail      *domain.StockDetail   `json:"stock_detail,omitempty"`
	WealthDetail     *domain.WealthDetail  `json:"wealth_detail,omitempty"`
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
	Name             string                `json:"name"`
	Currency         string                `json:"currency"`
	IssuerPlatformID *uint                 `json:"issuer_platform_id"`
	RiskLevel        string                `json:"risk_level"`
	Status           string                `json:"status"`
	Remark           string                `json:"remark"`
	FundDetail       *domain.FundDetail    `json:"fund_detail,omitempty"`
	StockDetail      *domain.StockDetail   `json:"stock_detail,omitempty"`
	WealthDetail     *domain.WealthDetail  `json:"wealth_detail,omitempty"`
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
		UserID:         userIDFromHeader(c),
		AssetType:      domain.AssetType(c.Query("asset_type")),
		Status:         c.Query("status"),
		Currency:       c.Query("currency"),
		Keyword:        c.Query("keyword"),
		Page:           queryInt(c, "page", 1),
		PageSize:       queryInt(c, "page_size", 20),
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
