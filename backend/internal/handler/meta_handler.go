package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// MetaHandler 提供 healthz/version/platforms 等元信息接口。
type MetaHandler struct {
	assetSvc *service.AssetService
	version  string
}

// NewMetaHandler 构造。
func NewMetaHandler(assetSvc *service.AssetService, version string) *MetaHandler {
	return &MetaHandler{assetSvc: assetSvc, version: version}
}

// Register 挂载路由。
func (h *MetaHandler) Register(r *gin.RouterGroup) {
	r.GET("/healthz", h.healthz)
	r.GET("/version", h.version_)
	r.GET("/platforms", h.platforms)
}

func (h *MetaHandler) healthz(c *gin.Context) {
	response.OK(c, gin.H{"status": "ok"})
}

func (h *MetaHandler) version_(c *gin.Context) {
	response.OK(c, gin.H{"version": h.version})
}

func (h *MetaHandler) platforms(c *gin.Context) {
	list, err := h.assetSvc.ListPlatforms(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// pathUint 从 :id 路径参数取无符号整数，失败返回 0。
func pathUint(c *gin.Context, key string) uint {
	v := c.Param(key)
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil || id == 0 {
		return 0
	}
	return uint(id)
}
