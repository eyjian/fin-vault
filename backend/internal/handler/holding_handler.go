package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// HoldingHandler 持仓 HTTP 适配。
type HoldingHandler struct {
	svc *service.HoldingService
}

// NewHoldingHandler 构造。
func NewHoldingHandler(svc *service.HoldingService) *HoldingHandler {
	return &HoldingHandler{svc: svc}
}

// Register 挂载路由。
func (h *HoldingHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/holdings")
	g.GET("", h.list)
	g.GET("/summary", h.summary)
	g.GET("/:id", h.get)
	g.PATCH("/:id", h.patch)
}

func (h *HoldingHandler) list(c *gin.Context) {
	in := service.HoldingListInput{
		UserID:          userIDFromHeader(c),
		AssetID:         queryUint(c, "asset_id"),
		PlatformID:      queryUint(c, "platform_id"),
		PortfolioID:     queryUint(c, "portfolio_id"),
		Status:          domain.HoldingStatus(c.Query("status")),
		AssetType:       domain.AssetType(c.Query("asset_type")),
		Page:            queryInt(c, "page", 1),
		PageSize:        queryInt(c, "page_size", 20),
		DisplayCurrency: c.DefaultQuery("display_currency", "raw"),
	}
	list, total, err := h.svc.List(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, in.Page, in.PageSize)
}

func (h *HoldingHandler) get(c *gin.Context) {
	id := pathUint(c, "id")
	if id == 0 {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	view, err := h.svc.Get(c.Request.Context(), userIDFromHeader(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *HoldingHandler) summary(c *gin.Context) {
	currency := c.DefaultQuery("display_currency", "raw")
	out, err := h.svc.Summary(c.Request.Context(), userIDFromHeader(c), currency)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// HoldingPatchReq 持仓更新（一阶段仅支持切换 cost_method）。
type HoldingPatchReq struct {
	CostMethod string `json:"cost_method"`
}

func (h *HoldingHandler) patch(c *gin.Context) {
	id := pathUint(c, "id")
	if id == 0 {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	var req HoldingPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	if req.CostMethod != "" {
		if err := h.svc.SwitchCostMethod(c.Request.Context(), userIDFromHeader(c), id, domain.CostMethod(req.CostMethod)); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.OK(c, gin.H{"updated": true})
}
