package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// TransactionHandler 交易流水 HTTP 适配。
type TransactionHandler struct {
	svc *service.TransactionService
}

// NewTransactionHandler 构造。
func NewTransactionHandler(svc *service.TransactionService) *TransactionHandler {
	return &TransactionHandler{svc: svc}
}

// Register 挂载路由。
func (h *TransactionHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/transactions")
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/:id", h.get)
	g.POST("/import", h.batchImport)
}

// TxnCreateReq POST /transactions 请求体。
//
// 金额字段统一用字符串传输，避免 JSON 浮点精度丢失。
type TxnCreateReq struct {
	AssetID    uint   `json:"asset_id" binding:"required"`
	PlatformID uint   `json:"platform_id" binding:"required"`
	TxnType    string `json:"txn_type" binding:"required"`
	TxnTime    string `json:"txn_time"`
	Quantity   string `json:"quantity"`
	Price      string `json:"price"`
	Amount     string `json:"amount"`
	Fee        string `json:"fee"`
	Tax        string `json:"tax"`
	Currency   string `json:"currency"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Note       string `json:"note"`
}

func (h *TransactionHandler) create(c *gin.Context) {
	var req TxnCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	in, err := buildCreateTxnInput(userIDFromHeader(c), req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	t, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, response.Body{Code: 0, Message: "created", Data: t})
}

func (h *TransactionHandler) get(c *gin.Context) {
	id := pathUint(c, "id")
	if id == 0 {
		response.Fail(c, errs.ErrInvalidParam.WithMsg("invalid id"))
		return
	}
	t, err := h.svc.Get(c.Request.Context(), userIDFromHeader(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, t)
}

func (h *TransactionHandler) list(c *gin.Context) {
	in := service.TxnListInput{
		UserID:     userIDFromHeader(c),
		HoldingID:  queryUint(c, "holding_id"),
		AssetID:    queryUint(c, "asset_id"),
		PlatformID: queryUint(c, "platform_id"),
		TxnType:    domain.TxnType(c.Query("txn_type")),
		Page:       queryInt(c, "page", 1),
		PageSize:   queryInt(c, "page_size", 20),
	}
	if v := c.Query("start_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			in.StartTime = &t
		}
	}
	if v := c.Query("end_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			in.EndTime = &t
		}
	}
	list, total, err := h.svc.List(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, in.Page, in.PageSize)
}

// TxnImportReq POST /transactions/import 请求体。
type TxnImportReq struct {
	DryRun bool           `json:"dry_run"`
	Rows   []TxnCreateReq `json:"rows" binding:"required"`
}

func (h *TransactionHandler) batchImport(c *gin.Context) {
	var req TxnImportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}
	uid := userIDFromHeader(c)
	inputs := make([]service.CreateTxnInput, 0, len(req.Rows))
	for _, r := range req.Rows {
		in, err := buildCreateTxnInput(uid, r)
		if err != nil {
			// 让 service.BatchImport 集中收集 row 错误更稳；这里把错误塞到 input 让 validate 失败
			in.UserID = uid
			in.AssetID = r.AssetID
			in.PlatformID = r.PlatformID
			in.TxnType = domain.TxnType(r.TxnType)
		}
		inputs = append(inputs, in)
	}
	res, err := h.svc.BatchImport(c.Request.Context(), inputs, req.DryRun)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// =====================================================================
// helpers
// =====================================================================

func buildCreateTxnInput(userID uint, r TxnCreateReq) (service.CreateTxnInput, error) {
	in := service.CreateTxnInput{
		UserID:     userID,
		AssetID:    r.AssetID,
		PlatformID: r.PlatformID,
		TxnType:    domain.TxnType(r.TxnType),
		Currency:   r.Currency,
		Source:     r.Source,
		ExternalID: r.ExternalID,
		Note:       r.Note,
	}
	if r.TxnTime != "" {
		t, err := time.Parse(time.RFC3339, r.TxnTime)
		if err != nil {
			return in, errs.ErrInvalidParam.WithMsg("invalid txn_time, want RFC3339")
		}
		in.TxnTime = t
	}
	var err error
	if in.Quantity, err = parseDecimal(r.Quantity); err != nil {
		return in, errs.ErrInvalidParam.WithMsg("invalid quantity")
	}
	if in.Price, err = parseDecimal(r.Price); err != nil {
		return in, errs.ErrInvalidParam.WithMsg("invalid price")
	}
	if in.Amount, err = parseDecimal(r.Amount); err != nil {
		return in, errs.ErrInvalidParam.WithMsg("invalid amount")
	}
	if in.Fee, err = parseDecimal(r.Fee); err != nil {
		return in, errs.ErrInvalidParam.WithMsg("invalid fee")
	}
	if in.Tax, err = parseDecimal(r.Tax); err != nil {
		return in, errs.ErrInvalidParam.WithMsg("invalid tax")
	}
	return in, nil
}

func parseDecimal(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}
