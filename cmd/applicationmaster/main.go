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
	"time"

	"carrot/internal/applicationmaster"
	"carrot/internal/common"

	"go.uber.org/zap"
)

func main() {
	// 命令行参数
	var (
		appIDStr          = flag.String("application_id", "", "Application ID (format: timestamp_id)")
		attemptIDStr      = flag.String("application_attempt_id", "", "Application Attempt ID (format: timestamp_id_attempt)")
		rmAddress         = flag.String("rm_address", "http://localhost:8030", "ResourceManager address")
		trackingURL       = flag.String("tracking_url", "", "Application tracking URL")
		port              = flag.Int("port", 8088, "ApplicationMaster HTTP server port")
		appType           = flag.String("app_type", "simple", "Application type: simple, distributed")
		numTasks          = flag.Int("num_tasks", 3, "Number of tasks for simple application")
		numWorkers        = flag.Int("num_workers", 2, "Number of workers for distributed application")
		heartbeatInterval = flag.Duration("heartbeat_interval", 10*time.Second, "Heartbeat interval")
		maxRetries        = flag.Int("max_retries", 3, "Maximum container retries")
		debug             = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	// 配置日志
	var logger *zap.Logger
	var err error
	if *debug {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		fmt.Printf("Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting ApplicationMaster",
		zap.String("app_id", *appIDStr),
		zap.String("attempt_id", *attemptIDStr),
		zap.String("rm_address", *rmAddress),
		zap.String("app_type", *appType))

	// 解析应用程序 ID
	appID, err := parseApplicationID(*appIDStr)
	if err != nil {
		logger.Fatal("Invalid application ID", zap.Error(err))
	}

	// 解析应用程序尝试 ID
	attemptID, err := parseApplicationAttemptID(*attemptIDStr)
	if err != nil {
		logger.Fatal("Invalid application attempt ID", zap.Error(err))
	}

	// 生成跟踪 URL（如果未提供）
	if *trackingURL == "" {
		*trackingURL = fmt.Sprintf("http://localhost:%d", *port)
	}

	// 创建 ApplicationMaster 配置
	config := &applicationmaster.ApplicationMasterConfig{
		ApplicationID:        *appID,
		ApplicationAttemptID: *attemptID,
		RMAddress:            *rmAddress,
		TrackingURL:          *trackingURL,
		HeartbeatInterval:    *heartbeatInterval,
		MaxContainerRetries:  *maxRetries,
		Port:                 *port,
	}

	// 创建 ApplicationMaster
	am := applicationmaster.NewApplicationMaster(config)

	// 启动 ApplicationMaster
	if err := am.Start(); err != nil {
		logger.Fatal("Failed to start ApplicationMaster", zap.Error(err))
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
		switch *appType {
		case "simple":
			app := applicationmaster.NewSimpleApplication(am, *numTasks)
			appErr = app.Run(ctx)
		case "distributed":
			app := applicationmaster.NewDistributedApplication(am, *numWorkers)
			appErr = app.Run(ctx)
		default:
			logger.Error("Unknown application type", zap.String("type", *appType))
			appErr = fmt.Errorf("unknown application type: %s", *appType)
		}

		if appErr != nil {
			logger.Error("Application failed", zap.Error(appErr))
		} else {
			logger.Info("Application completed successfully")
		}

		// 应用程序完成后发送信号
		sigChan <- syscall.SIGTERM
	}()

	// 等待信号
	sig := <-sigChan
	logger.Info("Received signal", zap.String("signal", sig.String()))

	// 停止 ApplicationMaster
	if err := am.Stop(); err != nil {
		logger.Error("Failed to stop ApplicationMaster", zap.Error(err))
	}

	logger.Info("ApplicationMaster stopped")
}

// parseApplicationID 解析应用程序 ID
func parseApplicationID(idStr string) (*common.ApplicationID, error) {
	if idStr == "" {
		return nil, fmt.Errorf("application ID is required")
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
		return nil, fmt.Errorf("application attempt ID is required")
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
