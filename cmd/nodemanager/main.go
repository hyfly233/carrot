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
		host        = flag.String("host", "localhost", "NodeManager host")
		port        = flag.Int("port", 8042, "NodeManager port")
		development = flag.Bool("dev", false, "Enable development mode")
		rmURL       = flag.String("rm-url", "http://localhost:8088", "ResourceManager URL")
		memory      = flag.Int64("memory", 8192, "Total memory in MB")
		vcores      = flag.Int("vcores", 8, "Total virtual cores")
	)
	flag.Parse()

	// 初始化日志系统
	if err := common.InitLogger(*development); err != nil {
		panic(err)
	}
	defer common.Sync()

	logger := common.GetLogger()
	logger.Info("NodeManager configuration",
		zap.String("host", *host),
		zap.Int("port", *port),
		zap.Bool("development", *development),
		zap.String("rm-url", *rmURL),
		zap.Int64("memory", *memory),
		zap.Int("vcores", *vcores))

	nodeID := common.NodeID{
		Host: *host,
		Port: int32(*port),
	}

	totalResource := common.Resource{
		Memory: *memory,
		VCores: int32(*vcores),
	}

	nm := nodemanager.NewNodeManager(nodeID, totalResource, *rmURL)

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
	if err := nm.Start(*port); err != nil {
		// 只有在不是正常关闭的情况下才记录错误
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start NodeManager: %v", err)
		}
	}

	logger.Info("NodeManager exited gracefully")
}
