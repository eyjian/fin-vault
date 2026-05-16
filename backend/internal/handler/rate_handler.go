package handler

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// RateHandler 汇率接口。
type RateHandler struct {
	svc *service.RateService
}

// NewRateHandler 构造。
func NewRateHandler(svc *service.RateService) *RateHandler {
	return &RateHandler{svc: svc}
}

// Register 挂在 /api/v1 下。
func (h *RateHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/rates")
	g.GET("", h.GetLatest)
	g.POST("", h.Save)
	g.GET("/list", h.List)
}

// GetLatest GET /api/v1/rates?from=USD&to=CNY&as_of=2026-05-15
func (h *RateHandler) GetLatest(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")
	asOf := time.Now()
	if v := c.Query("as_of"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			asOf = t
		}
	}
	r, err := h.svc.GetLatest(c.Request.Context(), from, to, asOf)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, r)
}

type rateSaveReq struct {
	FromCurrency string  `json:"from_currency" binding:"required"`
	ToCurrency   string  `json:"to_currency" binding:"required"`
	Rate         string  `json:"rate" binding:"required"`
	QuoteDate    *string `json:"quote_date"`
	Source       string  `json:"source"`
}

// Save POST /api/v1/rates
func (h *RateHandler) Save(c *gin.Context) {
	var req rateSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	rate, err := decimal.NewFromString(req.Rate)
	if err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid rate"))
		return
	}
	r := &domain.ExchangeRate{
		FromCurrency: req.FromCurrency,
		ToCurrency:   req.ToCurrency,
		Rate:         rate,
		Source:       req.Source,
	}
	if req.QuoteDate != nil && *req.QuoteDate != "" {
		if t, err := time.Parse("2006-01-02", *req.QuoteDate); err == nil {
			r.QuoteDate = t
		}
	}
	if err := h.svc.Save(c.Request.Context(), r); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, r)
}

// List GET /api/v1/rates/list?from=USD&to=CNY&start=2026-01-01&end=2026-05-15
func (h *RateHandler) List(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")
	var start, end time.Time
	if v := c.Query("start"); v != "" {
		start, _ = time.Parse("2006-01-02", v)
	}
	if v := c.Query("end"); v != "" {
		end, _ = time.Parse("2006-01-02", v)
	}
	list, err := h.svc.List(c.Request.Context(), from, to, start, end)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}
