package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// QuoteHandler 行情接口。
type QuoteHandler struct {
	svc *service.QuoteService
}

// NewQuoteHandler 构造。
func NewQuoteHandler(svc *service.QuoteService) *QuoteHandler {
	return &QuoteHandler{svc: svc}
}

// Register 路由注册。挂在 /api/v1 下。
func (h *QuoteHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/quotes")
	g.GET("/latest", h.GetLatest)
	g.POST("/refresh", h.Refresh)
	g.POST("", h.SaveManual)
}

// GetLatest GET /api/v1/quotes/latest?asset_ids=1,2,3
func (h *QuoteHandler) GetLatest(c *gin.Context) {
	idsRaw := c.Query("asset_ids")
	if idsRaw == "" {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("asset_ids required"))
		return
	}
	parts := strings.Split(idsRaw, ",")
	ids := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, uint(id))
	}
	out, err := h.svc.GetLatest(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// RefreshReq 请求体。
type refreshReq struct {
	AssetIDs []uint `json:"asset_ids"`
	Source   string `json:"source"` // auto / eastmoney / sina / tencent
}

// Refresh POST /api/v1/quotes/refresh
func (h *QuoteHandler) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	if req.Source == "" {
		req.Source = "auto"
	}
	uid := userIDFromHeader(c)
	res, err := h.svc.Refresh(c.Request.Context(), uid, req.AssetIDs, req.Source)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// quoteCreateReq 手动写入行情。
type quoteCreateReq struct {
	AssetID   uint    `json:"asset_id" binding:"required"`
	Price     string  `json:"price" binding:"required"`
	ChangePct string  `json:"change_pct"`
	Volume    string  `json:"volume"`
	QuoteTime *string `json:"quote_time"`
	Source    string  `json:"source"`
}

// SaveManual POST /api/v1/quotes
func (h *QuoteHandler) SaveManual(c *gin.Context) {
	var req quoteCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	price, err := decimal.NewFromString(req.Price)
	if err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid price"))
		return
	}
	q := &domain.PriceQuote{
		AssetID: req.AssetID,
		Price:   price,
		Source:  req.Source,
	}
	if req.ChangePct != "" {
		if v, err := decimal.NewFromString(req.ChangePct); err == nil {
			q.ChangePct = v
		}
	}
	if req.Volume != "" {
		if v, err := decimal.NewFromString(req.Volume); err == nil {
			q.Volume = v
		}
	}
	if req.QuoteTime != nil && *req.QuoteTime != "" {
		if t, err := time.Parse(time.RFC3339, *req.QuoteTime); err == nil {
			q.QuoteTime = t
		}
	}
	if err := h.svc.SaveManual(c.Request.Context(), q); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, q)
}
