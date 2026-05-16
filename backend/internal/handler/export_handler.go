package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/report"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// ExportHandler 数据导出接口（Excel / Markdown）。
type ExportHandler struct {
	svc *service.ExportService
}

// NewExportHandler 构造。
func NewExportHandler(svc *service.ExportService) *ExportHandler {
	return &ExportHandler{svc: svc}
}

// Register 挂在 /api/v1 下。
func (h *ExportHandler) Register(r *gin.RouterGroup) {
	r.GET("/export", h.Export)
}

// Export GET /api/v1/export?format=xlsx&scope=full&start=...&end=...
func (h *ExportHandler) Export(c *gin.Context) {
	format := report.Format(c.DefaultQuery("format", "xlsx"))
	scope := report.Scope(c.DefaultQuery("scope", "holdings"))
	uid := userIDFromHeader(c)

	in := service.ExportInput{
		UserID: uid,
		Format: format,
		Scope:  scope,
	}
	if v := c.Query("start"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			in.Start = t
		}
	}
	if v := c.Query("end"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			in.End = t
		}
	}

	// 设置文件下载头
	ts := time.Now().Format("20060102_150405")
	var contentType, filename string
	switch format {
	case report.FormatExcel:
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		filename = fmt.Sprintf("finvault_%s_%s.xlsx", scope, ts)
	case report.FormatMarkdown:
		contentType = "text/markdown; charset=utf-8"
		filename = fmt.Sprintf("finvault_%s_%s.md", scope, ts)
	default:
		response.Fail(c, errs.ErrInvalidParam.WithMsg("unsupported format"))
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Status(http.StatusOK)

	if err := h.svc.Export(c.Request.Context(), in, c.Writer); err != nil {
		// 此时部分字节已发送，无法再用 JSON 错误响应；只能写日志（gin recovery 会兜底）。
		_ = c.Error(err)
	}
}
