package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"carrot/internal/common"
	"carrot/internal/resourcemanager"

	_ "carrot/docs" // 导入生成的 swagger 文档

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
	logger.Info("启动 Carrot ResourceManager",
		zap.String("config_file", *configFile),
		zap.Bool("development", *development))

	logger.Info("配置加载完成",
		zap.String("cluster_name", config.Cluster.Name),
		zap.Int("http_port", config.ResourceManager.Port),
		zap.Int("grpc_port", config.ResourceManager.GRPCPort),
		zap.String("scheduler_type", config.Scheduler.Type))

	// 创建ResourceManager
	rm := resourcemanager.NewResourceManager(config)

	logger.Info("启动 ResourceManager 服务",
		zap.Int("http_port", config.ResourceManager.Port),
		zap.Int("nm_grpc_port", config.ResourceManager.GRPCPort),
		zap.Int("am_grpc_port", config.ResourceManager.AMGRPCPort),
		zap.String("swagger_url", fmt.Sprintf("http://localhost:%d/swagger/index.html", config.ResourceManager.Port)))

	// 优雅关闭处理
	_, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动服务
	go func() {
		if err := rm.Start(config.ResourceManager.Port, config.ResourceManager.GRPCPort, config.ResourceManager.AMGRPCPort); err != nil {
			// 只有在不是正常关闭的情况下才记录错误
			if !errors.Is(err, http.ErrServerClosed) {
				logger.Fatal("Failed to start ResourceManager", zap.Error(err))
			}
		}
	}()

	// 等待关闭信号
	<-sigChan
	logger.Info("接收到关闭信号，正在关闭 ResourceManager...")
	cancel() // 取消context
	if err := rm.Stop(); err != nil {
		logger.Error("关闭 ResourceManager 失败", zap.Error(err))
	}

	logger.Info("ResourceManager 正常退出...")
}
