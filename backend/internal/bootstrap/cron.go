package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/eyjian/fin-vault/backend/internal/service"
)

// CronManager 是 cron 调度器的轻量封装，便于优雅关闭。
type CronManager struct {
	c       *cron.Cron
	mature  *service.MatureService
	matureExpr string
}

// NewCronManager 构造调度器（不立即启动）。
func NewCronManager(mature *service.MatureService, matureExpr string) *CronManager {
	if matureExpr == "" {
		matureExpr = "30 0 * * *"
	}
	return &CronManager{
		c:          cron.New(cron.WithLogger(cron.DiscardLogger)),
		mature:     mature,
		matureExpr: matureExpr,
	}
}

// Start 注册任务并启动调度器。
func (m *CronManager) Start() error {
	if m.mature != nil {
		_, err := m.c.AddFunc(m.matureExpr, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			stat, err := m.mature.RunOnce(ctx)
			if err != nil {
				slog.Error("cron mature failed", "err", err)
				return
			}
			slog.Info("cron mature done",
				slog.Int("scanned", stat.Scanned),
				slog.Int("matured", stat.Matured),
				slog.Int("skipped", stat.Skipped),
			)
		})
		if err != nil {
			return err
		}
	}
	m.c.Start()
	slog.Info("cron started", "mature", m.matureExpr)
	return nil
}

// Stop 停止调度器，等待正在执行的任务完成。
func (m *CronManager) Stop() {
	if m.c != nil {
		<-m.c.Stop().Done()
	}
}

// RunMatureOnce 手工触发一次理财到期扫描（dev 模式下暴露给 admin 接口）。
func (m *CronManager) RunMatureOnce(ctx context.Context) (*service.MatureRunStat, error) {
	if m.mature == nil {
		return &service.MatureRunStat{}, nil
	}
	return m.mature.RunOnce(ctx)
}
