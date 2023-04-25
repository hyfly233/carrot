package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"carrot/internal/applicationmaster"
	"carrot/internal/common"

	"go.uber.org/zap"
)

func main() {
	// 命令行参数
	var (
		configFile   = flag.String("config", "configs/applicationmaster.yaml", "Configuration file path")
		appIDStr     = flag.String("application_id", "", "Application ID (format: timestamp_id)")
		attemptIDStr = flag.String("application_attempt_id", "", "Application Attempt ID (format: timestamp_id_attempt)")
	)
	flag.Parse()

	// 加载配置文件
	config, err := common.LoadConfig(*configFile)
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志系统
	if err := common.InitLoggerFromConfig(config); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer common.Sync()

	logger := common.ComponentLogger("applicationmaster")
	logger.Info("正在启动 ApplicationMaster",
		zap.String("config_file", *configFile),
		zap.String("app_id", *appIDStr),
		zap.String("attempt_id", *attemptIDStr),
		zap.String("rm_address", config.ApplicationMaster.ResourceManagerURL),
		zap.String("app_type", config.ApplicationMaster.AppType))

	// 解析应用程序 ID
	appID, err := parseApplicationID(*appIDStr)
	if err != nil {
		logger.Fatal("无效的应用程序 ID", zap.Error(err))
	}

	// 解析应用程序尝试 ID
	attemptID, err := parseApplicationAttemptID(*attemptIDStr)
	if err != nil {
		logger.Fatal("无效的应用程序尝试 ID", zap.Error(err))
	}

	// 生成跟踪 URL（如果未提供）
	trackingURL := config.ApplicationMaster.TrackingURL
	if trackingURL == "" {
		trackingURL = fmt.Sprintf("http://localhost:%d", config.ApplicationMaster.Port)
	}

	// 创建 ApplicationMaster 配置
	amConfig := &applicationmaster.ApplicationMasterConfig{
		ApplicationID:        *appID,
		ApplicationAttemptID: *attemptID,
		RMAddress:            config.ApplicationMaster.ResourceManagerURL,
		TrackingURL:          trackingURL,
		HeartbeatInterval:    config.ApplicationMaster.HeartbeatInterval,
		MaxContainerRetries:  config.ApplicationMaster.MaxRetries,
		Port:                 config.ApplicationMaster.Port,
	}

	// 创建 ApplicationMaster
	am := applicationmaster.NewApplicationMaster(amConfig)

	// 启动 ApplicationMaster
	if err := am.Start(); err != nil {
		logger.Fatal("启动 ApplicationMaster 失败", zap.Error(err))
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 运行应用程序逻辑
	go func() {
		var appErr error
		switch config.ApplicationMaster.AppType {
		case "simple":
			app := applicationmaster.NewSimpleApplication(am, config.ApplicationMaster.NumTasks)
			appErr = app.Run(ctx)
		case "distributed":
			app := applicationmaster.NewDistributedApplication(am, config.ApplicationMaster.NumWorkers)
			appErr = app.Run(ctx)
		default:
			logger.Error("未知的应用程序类型", zap.String("type", config.ApplicationMaster.AppType))
			appErr = fmt.Errorf("unknown application type: %s", config.ApplicationMaster.AppType)
		}

		if appErr != nil {
			logger.Error("应用程序运行失败", zap.Error(appErr))
		} else {
			logger.Info("应用程序成功完成")
		}

		// 应用程序完成后发送信号
		sigChan <- syscall.SIGTERM
	}()

	// 等待信号
	sig := <-sigChan
	logger.Info("收到信号", zap.String("signal", sig.String()))

	// 停止 ApplicationMaster
	if err := am.Stop(); err != nil {
		logger.Error("停止 ApplicationMaster 失败", zap.Error(err))
	}

	logger.Info("ApplicationMaster 已停止")
}

// parseApplicationID 解析应用程序 ID
func parseApplicationID(idStr string) (*common.ApplicationID, error) {
	if idStr == "" {
		return nil, fmt.Errorf("application ID 是必需的")
	}

	parts := strings.Split(idStr, "_")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid application ID format, expected timestamp_id")
	}

	timestamp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp in application ID: %w", err)
	}

	id, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid ID in application ID: %w", err)
	}

	return &common.ApplicationID{
		ClusterTimestamp: timestamp,
		ID:               int32(id),
	}, nil
}

// parseApplicationAttemptID 解析应用程序尝试 ID
func parseApplicationAttemptID(idStr string) (*common.ApplicationAttemptID, error) {
	if idStr == "" {
		return nil, fmt.Errorf("application attempt ID 是必需的")
	}

	parts := strings.Split(idStr, "_")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid application attempt ID format, expected timestamp_id_attempt")
	}

	appID, err := parseApplicationID(strings.Join(parts[:2], "_"))
	if err != nil {
		return nil, fmt.Errorf("invalid application ID in attempt ID: %w", err)
	}

	attemptID, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid attempt ID: %w", err)
	}

	return &common.ApplicationAttemptID{
		ApplicationID: *appID,
		AttemptID:     int32(attemptID),
	}, nil
}
