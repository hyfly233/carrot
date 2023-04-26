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
		configFile  = flag.String("config", "configs/node.yaml", "Configuration file path")
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

	logger := common.ComponentLogger("node")
	logger.Info("正在启动 YARN NodeManager",
		zap.String("config_file", *configFile),
		zap.Bool("development", *development))

	// 调试：直接打印 gRPC URL
	logger.Info("Debug: gRPC URL", zap.String("value", config.NodeManager.ResourceManagerGRPCURL), zap.Int("length", len(config.NodeManager.ResourceManagerGRPCURL)))

	logger.Info("配置已加载",
		zap.String("cluster_name", config.Cluster.Name),
		zap.Int("port", config.NodeManager.Port),
		zap.String("rm_url", config.NodeManager.ResourceManagerURL),
		zap.String("rm_grpc_url", config.NodeManager.ResourceManagerGRPCURL),
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

	nm := nodemanager.NewNodeManager(nodeID, totalResource, "", config.NodeManager.ResourceManagerGRPCURL)

	// 优雅关闭处理
	_, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("收到关闭信号")
		cancel() // 取消context
		if err := nm.Stop(); err != nil {
			logger.Error("停止 NodeManager 出错", zap.Error(err))
		}
	}()

	// 启动服务
	if err := nm.Start(config.NodeManager.Port); err != nil {
		// 只有在不是正常关闭的情况下才记录错误
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("启动 NodeManager 失败: %v", err)
		}
	}

	logger.Info("NodeManager 已优雅退出")
}
