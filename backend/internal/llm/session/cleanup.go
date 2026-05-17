package session

import (
	"context"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
)

// cleanupBatchSize 一次删除多少行 step，避免单条 DELETE 锁表过久。
const cleanupBatchSize = 500

// CleanupSteps 当数据库估算占用超过 maxBytes 时，按 f_created_at ASC 分批删除最旧的
// agent step，直到占用 ≤ maxBytes 或表已清空。
//
// 边界（与 spec ai-session "Scenario: 配置为 0 表示不清理" 对齐）：
//   - maxBytes <= 0：立即返回 (0, nil)，对应配置 max_steps_size_mb=0
//     （也兼容防御性传入负值的场景：service 层正常会校验，store 层兜底跳过）。
//   - 仅清理 t_fv_ai_agent_steps，不影响 t_fv_ai_messages / t_fv_ai_sessions
//     （spec "Scenario: 清理不影响用户消息" 严格保留）。
//
// 与 EstimateStepsSize 的关系：依赖 SessionStore.EstimateStepsSize（design Risks
// 表整库估算策略：PRAGMA page_count * page_size），保守估算偏大，触发清理偏早，
// 但仅删 step，不会误伤 messages/sessions。
//
// 实现策略：
//  1. SQLite 不支持 `DELETE ... ORDER BY ... LIMIT`，因此先 SELECT IDs（按
//     f_created_at ASC）拿一批最旧的 step ID，再 DELETE WHERE IN(...)；
//  2. 每批 cleanupBatchSize 行；批与批之间重新 EstimateStepsSize 判断是否继续；
//  3. 如果一轮 SELECT 拿到 0 行（表已空但 size 仍超阈值，可能 SQLite WAL/freelist
//     未回收），直接返回当前已删除数。
//
// 返回：deleted = 已删除的 step 行数总和；err = SQL 错误（取消、连接失败等）。
func CleanupSteps(ctx context.Context, db *gorm.DB, store SessionStore, maxBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return 0, nil
	}
	var totalDeleted int64
	for {
		size, err := store.EstimateStepsSize(ctx)
		if err != nil {
			return totalDeleted, err
		}
		if size <= maxBytes {
			return totalDeleted, nil
		}
		// 第一步：取最旧一批的 IDs（绕开 SQLite DELETE LIMIT 限制）
		var ids []string
		if err := db.WithContext(ctx).
			Model(&domain.AgentStep{}).
			Order("f_created_at ASC").
			Limit(cleanupBatchSize).
			Pluck("f_id", &ids).Error; err != nil {
			return totalDeleted, err
		}
		if len(ids) == 0 {
			// 表已空但估算仍未下来——SQLite 页/freelist 还未回收，停止避免死循环
			return totalDeleted, nil
		}
		// 第二步：硬删这一批
		res := db.WithContext(ctx).
			Where("f_id IN ?", ids).
			Delete(&domain.AgentStep{})
		if res.Error != nil {
			return totalDeleted, res.Error
		}
		totalDeleted += res.RowsAffected
	}
}
