package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// AdvisorHandler 买卖/配置建议接口（一次性，含 Tool Calling）。
type AdvisorHandler struct {
	svc *service.AdvisorService
}

// NewAdvisorHandler 构造。
func NewAdvisorHandler(svc *service.AdvisorService) *AdvisorHandler {
	return &AdvisorHandler{svc: svc}
}

// Register 挂在 /api/v1/ai 下。
func (h *AdvisorHandler) Register(r *gin.RouterGroup) {
	r.POST("/ai/advisor/recommend", h.Recommend)
}

type recommendReq struct {
	Target      string `json:"target" binding:"required"` // buy_sell / allocation
	AssetID     uint   `json:"asset_id"`
	LLMProvider string `json:"llm_provider"`
}

// Recommend POST /api/v1/ai/advisor/recommend
func (h *AdvisorHandler) Recommend(c *gin.Context) {
	var req recommendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	uid := userIDFromHeader(c)
	out, err := h.svc.Recommend(c.Request.Context(), service.RecommendInput{
		UserID:      uid,
		Target:      req.Target,
		AssetID:     req.AssetID,
		LLMProvider: req.LLMProvider,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}
