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
		configFile  = flag.String("config", "configs/resourcemanager.yaml", "Configuration file path")
		development = flag.Bool("dev", false, "Enable development mode")
	)
	flag.Parse()

	// 加载配置文件
	config, err := common.LoadConfig(*configFile)
	if err != nil {
		panic(err)
	}

	// 初始化日志系统
	if err := common.InitLoggerFromConfig(config); err != nil {
		panic(err)
	}
	defer common.Sync()

	logger := common.ComponentLogger("resourcemanager")
	logger.Info("Starting YARN ResourceManager",
		zap.String("config_file", *configFile),
		zap.Bool("development", *development))

	logger.Info("Configuration loaded",
		zap.String("cluster_name", config.Cluster.Name),
		zap.Int("port", config.ResourceManager.Port),
		zap.String("scheduler_type", config.Scheduler.Type))

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
	if err := rm.Start(config.ResourceManager.Port); err != nil {
		// 只有在不是正常关闭的情况下才记录错误
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("Failed to start ResourceManager", zap.Error(err))
		}
	}

	logger.Info("ResourceManager exited gracefully")
}
