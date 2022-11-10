package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/natefinch/lumberjack"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type loggerKeyType string

const loggerKey loggerKeyType = "logger"

var (
	logger       *zap.Logger
	sugar        *zap.SugaredLogger
	kafkaWriter  *kafka.Writer
	dorisBuffer  []LogEntry
	dorisMutex   sync.Mutex
	dorisConfig  LogDorisConfig
	logConfig    LogConfig
	loggerOnce   sync.Once
)

// LogEntry 日志条目结构
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Component string `json:"component"`
	Message   string `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// HourlyRotateWriter 按小时轮转的写入器
type HourlyRotateWriter struct {
	*lumberjack.Logger
	currentHour string
	directory   string
}

// NewHourlyRotateWriter 创建按小时轮转的写入器
func NewHourlyRotateWriter(config LogFileConfig) *HourlyRotateWriter {
	// 确保日志目录存在
	if err := os.MkdirAll(config.Directory, 0755); err != nil {
		fmt.Printf("Failed to create log directory: %v\n", err)
	}

	currentHour := time.Now().Format("2006-01-02-15")
	filename := filepath.Join(config.Directory, fmt.Sprintf("carrot-%s.log", currentHour))

	return &HourlyRotateWriter{
		Logger: &lumberjack.Logger{
			Filename: filename,
			MaxSize:  parseSize(config.MaxFileSize),
			MaxAge:   config.MaxAge,
			Compress: config.Compress,
		},
		currentHour: currentHour,
		directory:   config.Directory,
	}
}

// Write 实现 io.Writer 接口，支持按小时轮转
func (w *HourlyRotateWriter) Write(p []byte) (n int, err error) {
	currentHour := time.Now().Format("2006-01-02-15")
	
	// 如果小时发生变化，更新文件名
	if currentHour != w.currentHour {
		w.currentHour = currentHour
		newFilename := filepath.Join(w.directory, fmt.Sprintf("carrot-%s.log", currentHour))
		
		// 更新文件名
		w.Logger.Filename = newFilename
		
		// 启动每日压缩任务（异步）
		if logConfig.FileOutput.DailyCompression {
			go w.compressPreviousDayLogs()
		}
	}
	
	return w.Logger.Write(p)
}

// compressPreviousDayLogs 压缩前一天的日志文件
func (w *HourlyRotateWriter) compressPreviousDayLogs() {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	
	// 查找前一天的日志文件
	files, err := filepath.Glob(filepath.Join(w.directory, fmt.Sprintf("carrot-%s-*.log", yesterday)))
	if err != nil {
		return
	}
	
	// 压缩每个文件
	for _, file := range files {
		// 使用 lumberjack 的压缩功能
		if err := w.compressFile(file); err != nil {
			fmt.Printf("Failed to compress log file %s: %v\n", file, err)
		}
	}
}

// compressFile 压缩单个文件 - 简化实现
func (w *HourlyRotateWriter) compressFile(filename string) error {
	// lumberjack 会在文件轮转时自动压缩，这里可以实现额外的压缩逻辑
	return nil
}

// parseSize 解析文件大小字符串
func parseSize(sizeStr string) int {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))
	
	var multiplier int = 1
	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	}
	
	// 解析数字部分
	var size int
	fmt.Sscanf(sizeStr, "%d", &size)
	return size * multiplier / (1024 * 1024) // lumberjack 期望 MB
}

// InitLogger 初始化日志系统
func InitLogger(config LogConfig) error {
	var err error
	loggerOnce.Do(func() {
		logConfig = config
		
		// 设置日志级别
		var level zapcore.Level
		switch strings.ToLower(config.Level) {
		case "debug":
			level = zapcore.DebugLevel
		case "info":
			level = zapcore.InfoLevel
		case "warn", "warning":
			level = zapcore.WarnLevel
		case "error":
			level = zapcore.ErrorLevel
		default:
			level = zapcore.InfoLevel
		}
		
		// 创建编码器配置
		encoderConfig := zap.NewProductionEncoderConfig()
		if config.Development {
			encoderConfig = zap.NewDevelopmentEncoderConfig()
		}
		encoderConfig.TimeKey = "timestamp"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		
		// 创建核心写入器列表
		var cores []zapcore.Core
		
		// 控制台输出
		if config.ConsoleOutput.Enabled {
			consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
			if config.ConsoleOutput.Colorized && config.Development {
				encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
				consoleEncoder = zapcore.NewConsoleEncoder(encoderConfig)
			}
			cores = append(cores, zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level))
		}
		
		// 文件输出
		if config.FileOutput.Enabled {
			fileWriter := NewHourlyRotateWriter(config.FileOutput)
			fileEncoder := zapcore.NewJSONEncoder(encoderConfig)
			cores = append(cores, zapcore.NewCore(fileEncoder, zapcore.AddSync(fileWriter), level))
		}
		
		// Kafka输出
		if config.KafkaOutput.Enabled {
			kafkaCore, kafkaErr := createKafkaCore(config.KafkaOutput, encoderConfig, level)
			if kafkaErr == nil {
				cores = append(cores, kafkaCore)
			} else {
				fmt.Printf("Failed to initialize Kafka logging: %v\n", kafkaErr)
			}
		}
		
		// Doris输出
		if config.DorisOutput.Enabled {
			dorisConfig = config.DorisOutput
			dorisCore := createDorisCore(config.DorisOutput, encoderConfig, level)
			cores = append(cores, dorisCore)
			
			// 启动定期刷新Doris缓冲区
			go startDorisFlushScheduler()
		}
		
		// 创建复合核心
		core := zapcore.NewTee(cores...)
		
		// 创建日志器
		logger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
		sugar = logger.Sugar()
	})
	
	return err
}

// createKafkaCore 创建Kafka日志核心
func createKafkaCore(config LogKafkaConfig, encoderConfig zapcore.EncoderConfig, level zapcore.Level) (zapcore.Core, error) {
	// 解析超时时间
	timeout, err := time.ParseDuration(config.Timeout)
	if err != nil {
		timeout = 10 * time.Second
	}
	
	// 创建Kafka写入器
	kafkaWriter = &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers...),
		Topic:        config.Topic,
		BatchSize:    config.BatchSize,
		BatchTimeout: timeout,
		RequiredAcks: kafka.RequireOne,
		Async:        true,
	}
	
	// 创建Kafka写入器适配器
	kafkaWriterAdapter := &KafkaWriterAdapter{writer: kafkaWriter}
	
	// 创建编码器和核心
	encoder := zapcore.NewJSONEncoder(encoderConfig)
	return zapcore.NewCore(encoder, zapcore.AddSync(kafkaWriterAdapter), level), nil
}

// KafkaWriterAdapter Kafka写入器适配器
type KafkaWriterAdapter struct {
	writer *kafka.Writer
}

// Write 实现io.Writer接口
func (w *KafkaWriterAdapter) Write(p []byte) (n int, err error) {
	message := kafka.Message{
		Key:   []byte(fmt.Sprintf("carrot-%d", time.Now().UnixNano())),
		Value: p,
		Time:  time.Now(),
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	err = w.writer.WriteMessages(ctx, message)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// createDorisCore 创建Doris日志核心
func createDorisCore(config LogDorisConfig, encoderConfig zapcore.EncoderConfig, level zapcore.Level) zapcore.Core {
	dorisWriter := &DorisWriterAdapter{config: config}
	encoder := zapcore.NewJSONEncoder(encoderConfig)
	return zapcore.NewCore(encoder, zapcore.AddSync(dorisWriter), level)
}

// DorisWriterAdapter Doris写入器适配器
type DorisWriterAdapter struct {
	config LogDorisConfig
}

// Write 实现io.Writer接口
func (w *DorisWriterAdapter) Write(p []byte) (n int, err error) {
	// 解析日志条目
	var entry LogEntry
	if err := json.Unmarshal(p, &entry); err != nil {
		return 0, err
	}
	
	// 添加到缓冲区
	dorisMutex.Lock()
	dorisBuffer = append(dorisBuffer, entry)
	shouldFlush := len(dorisBuffer) >= w.config.BatchSize
	dorisMutex.Unlock()
	
	// 如果达到批量大小，立即刷新
	if shouldFlush {
		go flushDorisBuffer()
	}
	
	return len(p), nil
}

// startDorisFlushScheduler 启动Doris刷新调度器
func startDorisFlushScheduler() {
	interval, err := time.ParseDuration(dorisConfig.FlushInterval)
	if err != nil {
		interval = 30 * time.Second
	}
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			flushDorisBuffer()
		}
	}
}

// flushDorisBuffer 刷新Doris缓冲区
func flushDorisBuffer() {
	dorisMutex.Lock()
	if len(dorisBuffer) == 0 {
		dorisMutex.Unlock()
		return
	}
	
	// 复制缓冲区并清空
	entries := make([]LogEntry, len(dorisBuffer))
	copy(entries, dorisBuffer)
	dorisBuffer = dorisBuffer[:0]
	dorisMutex.Unlock()
	
	// 发送到Doris
	if err := sendToDoris(entries); err != nil {
		fmt.Printf("Failed to send logs to Doris: %v\n", err)
		// 可以实现重试逻辑
	}
}

// sendToDoris 发送日志到Doris
func sendToDoris(entries []LogEntry) error {
	// 构建批量数据
	var lines []string
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		lines = append(lines, string(data))
	}
	
	if len(lines) == 0 {
		return nil
	}
	
	// 发送Stream Load请求
	payload := strings.Join(lines, "\n")
	req, err := http.NewRequest("PUT", dorisConfig.StreamLoadURL, strings.NewReader(payload))
	if err != nil {
		return err
	}
	
	// 设置认证
	req.SetBasicAuth(dorisConfig.Username, dorisConfig.Password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("format", "json")
	req.Header.Set("strip_outer_array", "true")
	
	// 发送请求
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// 检查响应
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Doris Stream Load failed with status: %d", resp.StatusCode)
	}
	
	return nil
}

// GetLogger 获取结构化日志记录器
func GetLogger() *zap.Logger {
	if logger == nil {
		// 如果未初始化，使用默认配置
		defaultConfig := LogConfig{
			Level:       "info",
			Development: true,
			ConsoleOutput: LogConsoleConfig{
				Enabled:   true,
				Colorized: true,
			},
		}
		InitLogger(defaultConfig)
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
	
	// 刷新Kafka写入器
	if kafkaWriter != nil {
		kafkaWriter.Close()
	}
	
	// 刷新Doris缓冲区
	flushDorisBuffer()
}

// InitLoggerFromConfig 从配置初始化日志系统
func InitLoggerFromConfig(config *Config) error {
	return InitLogger(config.Log)
}
