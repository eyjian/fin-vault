// Package main 是 FinVault 后端 API 服务入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/bootstrap"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "config file path")
	flag.Parse()

	cfg, err := bootstrap.LoadConfig(*cfgPath)
	if err != nil {
		// 配置加载失败时无法用 slog 默认 Logger 之外的，直接 panic
		fmt.Fprintf(os.Stderr, "load config failed: %v\n", err)
		os.Exit(1)
	}

	bootstrap.InitLogger(cfg.Log)

	app, err := bootstrap.Wire(cfg)
	if err != nil {
		slog.Error("wire app failed", "err", err)
		os.Exit(1)
	}
	defer app.Close()

	if cfg.Database.AutoMigrate {
		if err := bootstrap.Migrate(app.DB); err != nil {
			slog.Error("auto migrate failed", "err", err)
			os.Exit(1)
		}
		if err := bootstrap.SeedInitialData(app.DB); err != nil {
			slog.Error("seed initial data failed", "err", err)
			os.Exit(1)
		}
	}

	if err := app.Cron.Start(); err != nil {
		slog.Error("cron start failed", "err", err)
		os.Exit(1)
	}

	router := bootstrap.RegisterRoutes(app)

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		slog.Info("fin-vault api start", "addr", srv.Addr, "mode", cfg.Server.Mode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server listen failed", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("server shutting down ...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced shutdown", "err", err)
	}
	slog.Info("server exited")
}
