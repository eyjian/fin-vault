// Package handler —— AI 把脉 HTTP 适配（spec ai-pulse-diagnosis）。
//
// 设计要点（与 design.md D5/D7/D10/D15 + spec ai-pulse-diagnosis 对齐）：
//   - D15 强校验：用 requireUserIDFromHeader 取 X-User-Id；缺失/非法/0 → 401
//   - D5 并行化：POST 批量把脉用 errgroup + 信号量控制并发（默认 3，配置可调）
//   - 单资产失败不阻塞其他资产；返回项含 status / error_message 字段
//   - GET 接口仅查数据库，不触发新把脉（spec "把脉结果缓存"）；支持单查 / 批量查
//   - D16 降级：service.IsAvailable() 返 false 时整体不暴露路由（router.go 处理）
package handler

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/service"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
	"github.com/eyjian/fin-vault/backend/pkg/utils/response"
)

// =====================================================================
// DTO
// =====================================================================

// PulseDiagnoseReq POST /api/v1/ai/pulse-diagnosis 请求体。
type PulseDiagnoseReq struct {
	AssetIDs []uint `json:"asset_ids" binding:"required,min=1,dive,gt=0"`
}

// PulseDiagnoseItemDTO 单个资产的把脉结果（HTTP 视图）。
//
// Status：
//   - "success"：本次把脉成功完成
//   - "failed" ：把脉失败，详见 ErrorMessage
type PulseDiagnoseItemDTO struct {
	AssetID        uint   `json:"asset_id"`
	Recommendation string `json:"recommendation,omitempty"` // sell / reduce / hold / add
	Confidence     string `json:"confidence,omitempty"`     // high / medium / low
	Summary        string `json:"summary,omitempty"`
	Detail         string `json:"detail,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	TriggerSource  string `json:"trigger_source,omitempty"`
	DiagnosedAt    string `json:"diagnosed_at,omitempty"` // RFC3339
	Status         string `json:"status"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// PulseDiagnoseResp POST/GET 接口响应体。
type PulseDiagnoseResp struct {
	Items []PulseDiagnoseItemDTO `json:"items"`
}

// =====================================================================
// Handler
// =====================================================================

// PulseDiagnosisHandler 暴露 /api/v1/ai/pulse-diagnosis 路由。
type PulseDiagnosisHandler struct {
	svc         *service.PulseDiagnosisService
	concurrency int
}

// NewPulseDiagnosisHandler 构造。concurrency<=0 时默认 3。
func NewPulseDiagnosisHandler(svc *service.PulseDiagnosisService, concurrency int) *PulseDiagnosisHandler {
	if concurrency <= 0 {
		concurrency = 3
	}
	return &PulseDiagnosisHandler{svc: svc, concurrency: concurrency}
}

// Register 挂到 /api/v1。
//
// POST /ai/pulse-diagnosis：批量触发新把脉（消耗 token）
// GET  /ai/pulse-diagnosis：仅查数据库已有把脉结果（不触发新把脉）
func (h *PulseDiagnosisHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/ai/pulse-diagnosis")
	g.POST("", h.create)
	g.GET("", h.get)
}

// =====================================================================
// 路由实现
// =====================================================================

// create POST /api/v1/ai/pulse-diagnosis
//
// 请求：PulseDiagnoseReq{ asset_ids: number[] }
// 响应：200 + PulseDiagnoseResp{ items: [...] }（即使部分失败也返 200，每项含 status）
// 错误：401 (无 user_id) / 400 (asset_ids 空) / 500 (LLM 不可用 50010)
func (h *PulseDiagnosisHandler) create(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	if h.svc == nil || !h.svc.IsAvailable() {
		response.Fail(c, errs.ErrAIPulseUnavailable)
		return
	}
	var req PulseDiagnoseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.ErrInvalidParam.WithCause(err))
		return
	}

	ctx := c.Request.Context()
	ids := dedupAssetIDs(req.AssetIDs)
	items := make([]PulseDiagnoseItemDTO, len(ids))

	// errgroup + 信号量限流并发
	g, gctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, h.concurrency)
	var mu sync.Mutex // 保护 items 写入（虽然每个 goroutine 写不同 idx，加锁更稳）

	for i, assetID := range ids {
		i, assetID := i, assetID
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-gctx.Done():
				return gctx.Err()
			}
			res, err := h.svc.Diagnose(gctx, service.PulseDiagnoseInput{
				UserID:        uid,
				AssetID:       assetID,
				TriggerSource: domain.PulseTriggerManual,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				items[i] = PulseDiagnoseItemDTO{
					AssetID:      assetID,
					Status:       "failed",
					ErrorMessage: shortErrMessage(err),
				}
				// 单资产失败不向 errgroup 报错（避免 cancel 其他 goroutine）
				return nil
			}
			items[i] = pulseResultToDTO(res, "success", "")
			return nil
		})
	}
	if err := g.Wait(); err != nil && err != context.Canceled {
		// 仅在 ctx 取消等系统级错误时回到这里；业务错误已在 items 中
		response.Fail(c, errs.ErrInternal.WithCause(err))
		return
	}
	response.OK(c, PulseDiagnoseResp{Items: items})
}

// get GET /api/v1/ai/pulse-diagnosis?asset_id=N 或 ?asset_ids=N1,N2,...
//
// 仅读数据库中最近一次把脉结果（不触发新把脉）。
// 未把脉过的资产不出现在返回项中（前端按 asset_id 字典断空）。
func (h *PulseDiagnosisHandler) get(c *gin.Context) {
	uid, ok := requireUserIDFromHeader(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	if h.svc == nil {
		response.OK(c, PulseDiagnoseResp{Items: []PulseDiagnoseItemDTO{}})
		return
	}
	ids := parseAssetIDsQuery(c)

	// 用 ListCached 一次性批量取（asset_ids 为空时返回当前用户全部）
	results, err := h.svc.ListCached(c.Request.Context(), uid, ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]PulseDiagnoseItemDTO, 0, len(results))
	for i := range results {
		items = append(items, pulseResultToDTO(&results[i], "success", ""))
	}
	response.OK(c, PulseDiagnoseResp{Items: items})
}

// =====================================================================
// 辅助
// =====================================================================

// pulseResultToDTO 把 service.PulseDiagnoseResult 转为 HTTP DTO。
func pulseResultToDTO(r *service.PulseDiagnoseResult, status, errMsg string) PulseDiagnoseItemDTO {
	dto := PulseDiagnoseItemDTO{
		AssetID:        r.AssetID,
		Recommendation: string(r.Recommendation),
		Confidence:     string(r.Confidence),
		Summary:        r.Summary,
		Detail:         r.Detail,
		SessionID:      r.SessionID,
		TriggerSource:  string(r.TriggerSource),
		Status:         status,
		ErrorMessage:   errMsg,
	}
	if !r.DiagnosedAt.IsZero() {
		dto.DiagnosedAt = r.DiagnosedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

// shortErrMessage 提取业务错误的简短消息（避免把堆栈/底层错误透给前端）。
func shortErrMessage(err error) string {
	if err == nil {
		return ""
	}
	if biz := errs.As(err); biz != nil {
		return biz.Message
	}
	return err.Error()
}

// dedupAssetIDs 去重 + 过滤 0 值。
func dedupAssetIDs(in []uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(in))
	for _, id := range in {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// parseAssetIDsQuery 解析 GET 查询参数：
//   - asset_id=N    → []uint{N}
//   - asset_ids=N1,N2,...
//   - 都没有 → 空切片（service 层语义为"返回当前用户全部"）
func parseAssetIDsQuery(c *gin.Context) []uint {
	if v := c.Query("asset_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil && id > 0 {
			return []uint{uint(id)}
		}
	}
	v := c.Query("asset_ids")
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	ids := make([]uint, 0, len(parts))
	seen := map[uint]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseUint(p, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		if seen[uint(id)] {
			continue
		}
		seen[uint(id)] = true
		ids = append(ids, uint(id))
	}
	return ids
}
