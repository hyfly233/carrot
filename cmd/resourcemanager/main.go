package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"carrot/internal/common"
	"carrot/internal/resourcemanager"

	"go.uber.org/zap"
)

func main() {
	var (
		port        = flag.Int("port", 8088, "ResourceManager port")
		development = flag.Bool("dev", false, "Enable development mode")
	)
	flag.Parse()

	// 初始化日志系统
	if err := common.InitLogger(*development); err != nil {
		panic(err)
	}
	defer common.Sync()

	logger := common.GetLogger()
	logger.Info("Starting YARN ResourceManager",
		zap.Int("port", *port),
		zap.Bool("development", *development))

	// 获取配置
	config := common.GetDefaultConfig()
	config.ResourceManager.Port = *port

	// 创建ResourceManager
	rm := resourcemanager.NewResourceManager(config)

	// 优雅关闭处理
	_, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel() // 取消context
		if err := rm.Stop(); err != nil {
			logger.Error("Error stopping ResourceManager", zap.Error(err))
		}
	}()

	// 启动服务
	if err := rm.Start(*port); err != nil {
		// 只有在不是正常关闭的情况下才记录错误
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("Failed to start ResourceManager", zap.Error(err))
		}
	}

	logger.Info("ResourceManager exited gracefully")
}
