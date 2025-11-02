package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cyberbot/cve-auto-fix/src/internal/bot"
	"github.com/cyberbot/cve-auto-fix/src/internal/config"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化 Bot
	b, err := bot.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	log.Println("Security Bot started - scanning every", cfg.ScanIntervalHours, "hours")

	// 启动扫描循环
	ticker := time.NewTicker(time.Duration(cfg.ScanIntervalHours) * time.Hour)
	defer ticker.Stop()

	// 立即运行一次
	go func() {
		if err := b.Run(ctx); err != nil {
			log.Printf("Scan run failed: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("Bot shutting down")
			return
		case <-sigCh:
			log.Println("Received shutdown signal")
			cancel()
		case <-ticker.C:
			go func() {
				log.Println("Starting scheduled scan")
				if err := b.Run(ctx); err != nil {
					log.Printf("Scan run failed: %v", err)
				}
			}()
		}
	}
}

// version info for build
var (
	Version   = "dev"
	GitCommit = "none"
	BuildTime = "unknown"
)

func init() {
	log.Printf("Security Bot v%s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
}
