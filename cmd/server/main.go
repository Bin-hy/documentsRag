// BinRag API 服务入口：加载配置、装配应用（internal/app）、启动 HTTP 服务并优雅关停。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bin-hy/bin-rag/internal/app"
	"github.com/Bin-hy/bin-rag/internal/config"
)

func main() {
	cfgPath := app.ParseConfigFlag(os.Args[1:])
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err, "path", cfgPath)
		os.Exit(1)
	}
	loadedPath := cfgPath
	if loadedPath == "" {
		loadedPath = "(环境变量或默认路径)"
	}
	slog.Info("配置加载完成", "path", loadedPath)

	a, err := app.New(cfg)
	if err != nil {
		slog.Error("应用装配失败", "err", err)
		os.Exit(1)
	}
	defer a.Close()

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: a.Router(),
	}

	go func() {
		slog.Info("HTTP 服务已启动",
			"addr", server.Addr,
			"worker", cfg.Server.WorkerCount,
			"storage", cfg.Server.FileStorageDir,
			"auth", cfg.Server.AuthEnabled,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务异常退出", "err", err)
			os.Exit(1)
		}
	}()

	// 优雅关停：先停 worker（等待当前任务），再关 HTTP
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("收到退出信号，开始优雅关停")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP 关停超时", "err", err)
	}
	slog.Info("服务已退出")
}
