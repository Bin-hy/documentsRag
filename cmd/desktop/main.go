// BinRag 桌面版入口（Wails v3）：
// 内嵌完整后端（internal/app）监听 127.0.0.1 随机端口，窗口直接加载该地址，
// 前端与 Web 版完全同源，无代理层、不依赖 Wails 专有 API。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Bin-hy/bin-rag/internal/app"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	// 解析配置文件路径（与 cmd/server 一致：-c / BINRAG_CONFIG / 默认路径）
	cfgPath := app.ParseConfigFlag(os.Args[1:])
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err, "path", cfgPath)
		os.Exit(1)
	}

	// 装配完整后端
	a, err := app.New(cfg)
	if err != nil {
		slog.Error("应用装配失败", "err", err)
		os.Exit(1)
	}

	// 内嵌 HTTP 服务：仅监听本机随机端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("监听本地端口失败", "err", err)
		os.Exit(1)
	}
	server := &http.Server{Handler: a.Router()}
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("内嵌 HTTP 服务异常退出", "err", err)
			os.Exit(1)
		}
	}()
	addr := "http://" + ln.Addr().String() + "/"
	slog.Info("内嵌服务已启动", "addr", addr)

	// Wails 应用：窗口加载内嵌服务地址
	wailsApp := application.New(application.Options{
		Name:        "BinRag",
		Description: "BinRag 企业级文档知识库问答系统",
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: func() {
			// 窗口关闭：优雅关停内嵌服务与后端资源
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				slog.Warn("内嵌 HTTP 关停超时", "err", err)
			}
			if err := a.Close(); err != nil {
				slog.Warn("后端资源释放异常", "err", err)
			}
			slog.Info("桌面应用已退出")
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "BinRag 知识库问答",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: application.NewRGB(245, 247, 250),
		URL:              addr,
	})

	if err := wailsApp.Run(); err != nil {
		slog.Error("桌面应用运行失败", "err", err)
		os.Exit(1)
	}
}
