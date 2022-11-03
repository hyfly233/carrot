package common

import (
	"context"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type loggerKeyType string

const loggerKey loggerKeyType = "logger"

var (
	logger *zap.Logger
	sugar  *zap.SugaredLogger
)

// InitLogger 初始化日志系统
func InitLogger(development bool) error {
	var config zap.Config

	if development {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
	}

	// 设置日志级别
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		var level zapcore.Level
		if err := level.UnmarshalText([]byte(logLevel)); err == nil {
			config.Level = zap.NewAtomicLevelAt(level)
		}
	}

	var err error
	logger, err = config.Build(zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return err
	}

	sugar = logger.Sugar()
	return nil
}

// GetLogger 获取结构化日志记录器
func GetLogger() *zap.Logger {
	if logger == nil {
		// 如果未初始化，使用默认配置
		logger, _ = zap.NewDevelopment()
	}
	return logger
}

// GetSugaredLogger 获取更便于使用的日志记录器
func GetSugaredLogger() *zap.SugaredLogger {
	if sugar == nil {
		sugar = GetLogger().Sugar()
	}
	return sugar
}

// LoggerFromContext 从上下文中获取日志记录器
func LoggerFromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerKey).(*zap.Logger); ok {
		return l
	}
	return GetLogger()
}

// ContextWithLogger 将日志记录器添加到上下文
func ContextWithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// ComponentLogger 为特定组件创建带有组件信息的日志记录器
func ComponentLogger(component string) *zap.Logger {
	return GetLogger().With(zap.String("component", component))
}

// Sync 同步日志缓冲区
func Sync() {
	if logger != nil {
		_ = logger.Sync()
	}
}
