package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// AnalysisHandler 盈亏分析接口。
type AnalysisHandler struct {
	svc *service.AnalysisService
}

// NewAnalysisHandler 构造。
func NewAnalysisHandler(svc *service.AnalysisService) *AnalysisHandler {
	return &AnalysisHandler{svc: svc}
}

// Register 挂在 /api/v1/ai 下。
func (h *AnalysisHandler) Register(r *gin.RouterGroup) {
	r.POST("/ai/analysis/profit", h.Profit)
}

type profitReq struct {
	Period          string `json:"period"`
	DisplayCurrency string `json:"display_currency"`
	LLMProvider     string `json:"llm_provider"`
}

// Profit POST /api/v1/ai/analysis/profit
func (h *AnalysisHandler) Profit(c *gin.Context) {
	var req profitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	uid := userIDFromHeader(c)
	out, err := h.svc.AnalyzeProfit(c.Request.Context(), service.ProfitInput{
		UserID:          uid,
		Period:          req.Period,
		DisplayCurrency: req.DisplayCurrency,
		LLMProvider:     req.LLMProvider,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}
