package main

import (
	"flag"
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
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		if err := rm.Stop(); err != nil {
			logger.Error("Error stopping ResourceManager", zap.Error(err))
		}
		os.Exit(0)
	}()

	// 启动服务
	if err := rm.Start(*port); err != nil {
		logger.Fatal("Failed to start ResourceManager", zap.Error(err))
	}
}
