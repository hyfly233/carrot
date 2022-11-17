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

// @title Carrot YARN ResourceManager API
// @version 1.0
// @description Carrot YARN 资源管理器 REST API 服务
// @termsOfService http://swagger.io/terms/
// @contact.name API Support
// @contact.email support@carrot.io
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @host localhost:8088
// @BasePath /ws/v1
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
		zap.Int("http_port", config.ResourceManager.Port),
		zap.Int("grpc_port", config.ResourceManager.GRPCPort),
		zap.String("scheduler_type", config.Scheduler.Type))

	// 创建ResourceManager
	rm := resourcemanager.NewResourceManager(config)

	logger.Info("Starting ResourceManager server",
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
	logger.Info("Received shutdown signal")
	cancel() // 取消context
	if err := rm.Stop(); err != nil {
		logger.Error("Error stopping ResourceManager", zap.Error(err))
	}

	logger.Info("ResourceManager exited gracefully")
}
