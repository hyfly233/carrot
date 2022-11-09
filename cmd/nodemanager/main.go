package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"carrot/internal/common"
	"carrot/internal/nodemanager"

	"go.uber.org/zap"
)

func main() {
	var (
		configFile  = flag.String("config", "configs/nodemanager.yaml", "Configuration file path")
		development = flag.Bool("dev", false, "Enable development mode")
	)
	flag.Parse()

	// 初始化日志系统
	if err := common.InitLogger(*development); err != nil {
		panic(err)
	}
	defer common.Sync()

	logger := common.GetLogger()
	logger.Info("Starting YARN NodeManager",
		zap.String("config_file", *configFile),
		zap.Bool("development", *development))

	// 加载配置文件
	config, err := common.LoadConfig(*configFile)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	logger.Info("Configuration loaded",
		zap.String("cluster_name", config.Cluster.Name),
		zap.Int("port", config.NodeManager.Port),
		zap.String("rm_url", config.NodeManager.ResourceManagerURL),
		zap.Int64("memory", config.NodeManager.ContainerMemoryLimitMB),
		zap.Int32("vcores", config.NodeManager.ContainerVCoresLimit))

	nodeID := common.NodeID{
		Host: config.NodeManager.Address,
		Port: int32(config.NodeManager.Port),
	}

	totalResource := common.Resource{
		Memory: config.NodeManager.ContainerMemoryLimitMB,
		VCores: config.NodeManager.ContainerVCoresLimit,
	}

	nm := nodemanager.NewNodeManager(nodeID, totalResource, config.NodeManager.ResourceManagerURL)

	// 优雅关闭处理
	_, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel() // 取消context
		if err := nm.Stop(); err != nil {
			logger.Error("Error stopping NodeManager", zap.Error(err))
		}
	}()

	// 启动服务
	if err := nm.Start(config.NodeManager.Port); err != nil {
		// 只有在不是正常关闭的情况下才记录错误
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start NodeManager: %v", err)
		}
	}

	logger.Info("NodeManager exited gracefully")
}
