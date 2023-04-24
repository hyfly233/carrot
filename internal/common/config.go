package common

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
type Config struct {
	Cluster           ClusterConfig           `yaml:"cluster"`
	ResourceManager   ResourceManagerConfig   `yaml:"resourcemanager"`
	NodeManager       NodeManagerConfig       `yaml:"rmnode"`
	ApplicationMaster ApplicationMasterConfig `yaml:"applicationmaster"`
	Scheduler         SchedulerConfig         `yaml:"scheduler"`
	Security          SecurityConfig          `yaml:"security"`
	Log               LogConfig               `yaml:"log"`
	HeartbeatTimeout  int                     `yaml:"heartbeat_timeout"` // 心跳超时时间（秒）
	MonitorInterval   int                     `yaml:"monitor_interval"`  // 监测间隔（秒）
}

// ResourceManagerConfig ResourceManager配置
type ResourceManagerConfig struct {
	Port                   int           `yaml:"port"`
	GRPCPort               int           `yaml:"grpc_port"`    // NodeManager gRPC 端口
	AMGRPCPort             int           `yaml:"am_grpc_port"` // ApplicationMaster gRPC 端口
	Address                string        `yaml:"address"`
	HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`
	NodeExpiryTimeout      time.Duration `yaml:"node_expiry_timeout"`
	ApplicationMaxAttempts int32         `yaml:"application_max_attempts"`
}

// NodeManagerConfig NodeManager配置
type NodeManagerConfig struct {
	Port                   int           `yaml:"port"`
	Address                string        `yaml:"address"`
	ResourceManagerURL     string        `yaml:"resourcemanager_url"`
	ResourceManagerGRPCURL string        `yaml:"resourcemanager_grpc_url"`
	HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`
	ContainerCleanupDelay  time.Duration `yaml:"container_cleanup_delay"`
	ContainerMemoryLimitMB int64         `yaml:"container_memory_limit_mb"`
	ContainerVCoresLimit   int32         `yaml:"container_vcores_limit"`
}

// ApplicationMasterConfig ApplicationMaster配置
type ApplicationMasterConfig struct {
	Port               int           `yaml:"port"`
	Address            string        `yaml:"address"`
	ResourceManagerURL string        `yaml:"resourcemanager_url"`
	HeartbeatInterval  time.Duration `yaml:"heartbeat_interval"`
	MaxRetries         int           `yaml:"max_retries"`
	AppType            string        `yaml:"app_type"`    // simple, distributed
	NumTasks           int           `yaml:"num_tasks"`   // for simple app
	NumWorkers         int           `yaml:"num_workers"` // for distributed app
	TrackingURL        string        `yaml:"tracking_url"`
	EnableDebug        bool          `yaml:"enable_debug"`
}

// SchedulerConfig 调度器配置
type SchedulerConfig struct {
	Type              string                   `yaml:"type"` // fifo, capacity, fair
	MaxApps           int32                    `yaml:"max_apps"`
	QueueConfigs      map[string]QueueConfig   `yaml:"queues"`
	CapacityScheduler *CapacitySchedulerConfig `yaml:"capacity_scheduler,omitempty"`
	FairScheduler     *FairSchedulerConfig     `yaml:"fair_scheduler,omitempty"`
}

// CapacitySchedulerConfig 容量调度器配置
type CapacitySchedulerConfig struct {
	ResourceCalculator   string                          `yaml:"resource_calculator"`
	MaxApplications      int                             `yaml:"max_applications"`
	MaxAMResourcePercent float64                         `yaml:"max_am_resource_percent"`
	Queues               map[string]*CapacityQueueConfig `yaml:"queues"`
}

// CapacityQueueConfig 容量队列配置
type CapacityQueueConfig struct {
	Name                   string                          `yaml:"name"`
	Type                   string                          `yaml:"type"` // "parent" or "leaf"
	Capacity               float64                         `yaml:"capacity"`
	MaxCapacity            float64                         `yaml:"max_capacity"`
	State                  string                          `yaml:"state"`
	DefaultApplicationType string                          `yaml:"default_application_type"`
	MaxApplications        int                             `yaml:"max_applications"`
	MaxAMResourcePercent   float64                         `yaml:"max_am_resource_percent"`
	UserLimitFactor        float64                         `yaml:"user_limit_factor"`
	OrderingPolicy         string                          `yaml:"ordering_policy"`
	Children               map[string]*CapacityQueueConfig `yaml:"children,omitempty"`
}

// FairSchedulerConfig 公平调度器配置
type FairSchedulerConfig struct {
	ContinuousSchedulingEnabled  bool                        `yaml:"continuous_scheduling_enabled"`
	PreemptionEnabled            bool                        `yaml:"preemption_enabled"`
	DefaultQueueSchedulingPolicy string                      `yaml:"default_queue_scheduling_policy"`
	Queues                       map[string]*FairQueueConfig `yaml:"queues"`
}

// FairQueueConfig 公平队列配置
type FairQueueConfig struct {
	Name             string                      `yaml:"name"`
	Type             string                      `yaml:"type"` // "parent" or "leaf"
	Weight           float64                     `yaml:"weight"`
	MinResources     Resource                    `yaml:"min_resources"`
	MaxResources     Resource                    `yaml:"max_resources"`
	MaxRunningApps   int                         `yaml:"max_running_apps"`
	SchedulingPolicy string                      `yaml:"scheduling_policy"`
	Children         map[string]*FairQueueConfig `yaml:"children,omitempty"`
}

// QueueConfig 队列配置
type QueueConfig struct {
	Name        string  `yaml:"name"`
	Capacity    float64 `yaml:"capacity"`
	MaxCapacity float64 `yaml:"max_capacity"`
	Priority    int32   `yaml:"priority"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	Enabled         bool          `yaml:"enabled"`
	TokenExpiry     time.Duration `yaml:"token_expiry"`
	SecretKey       string        `yaml:"secret_key"`
	ContainerSecure bool          `yaml:"container_secure"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level         string           `yaml:"level"`          // 日志级别: debug, info, warn, error
	Development   bool             `yaml:"development"`    // 开发模式
	FileOutput    LogFileConfig    `yaml:"file_output"`    // 文件输出配置
	KafkaOutput   LogKafkaConfig   `yaml:"kafka_output"`   // Kafka输出配置
	DorisOutput   LogDorisConfig   `yaml:"doris_output"`   // Doris输出配置
	ConsoleOutput LogConsoleConfig `yaml:"console_output"` // 控制台输出配置
}

// LogFileConfig 文件日志配置
type LogFileConfig struct {
	Enabled          bool   `yaml:"enabled"`           // 是否启用文件输出
	Directory        string `yaml:"directory"`         // 日志文件目录
	MaxFileSize      string `yaml:"max_file_size"`     // 单个文件最大大小
	MaxBackups       int    `yaml:"max_backups"`       // 保留的备份文件数量
	MaxAge           int    `yaml:"max_age"`           // 保留的天数
	Compress         bool   `yaml:"compress"`          // 是否压缩旧文件
	HourlyRotation   bool   `yaml:"hourly_rotation"`   // 是否按小时轮转
	DailyCompression bool   `yaml:"daily_compression"` // 每日压缩
}

// LogKafkaConfig Kafka日志配置
type LogKafkaConfig struct {
	Enabled   bool     `yaml:"enabled"`    // 是否启用Kafka输出
	Brokers   []string `yaml:"brokers"`    // Kafka broker地址列表
	Topic     string   `yaml:"topic"`      // 日志主题
	BatchSize int      `yaml:"batch_size"` // 批量发送大小
	Timeout   string   `yaml:"timeout"`    // 发送超时时间
	Retries   int      `yaml:"retries"`    // 重试次数
}

// LogDorisConfig Doris日志配置
type LogDorisConfig struct {
	Enabled       bool   `yaml:"enabled"`         // 是否启用Doris输出
	StreamLoadURL string `yaml:"stream_load_url"` // Stream Load URL
	Database      string `yaml:"database"`        // 数据库名
	Table         string `yaml:"table"`           // 表名
	Username      string `yaml:"username"`        // 用户名
	Password      string `yaml:"password"`        // 密码
	BatchSize     int    `yaml:"batch_size"`      // 批量大小
	FlushInterval string `yaml:"flush_interval"`  // 刷新间隔
}

// LogConsoleConfig 控制台日志配置
type LogConsoleConfig struct {
	Enabled   bool `yaml:"enabled"`   // 是否启用控制台输出
	Colorized bool `yaml:"colorized"` // 是否彩色输出
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		Cluster: ClusterConfig{
			Name:                   "carrot-cluster",
			ID:                     "",
			MinNodes:               1,
			MaxNodes:               100,
			ElectionTimeout:        5 * time.Second,
			HeartbeatInterval:      1 * time.Second,
			FailureDetectionWindow: 30 * time.Second,
			SplitBrainProtection:   true,
			AutoScaling:            false,
			DiscoveryMethod:        "static",
			DiscoveryConfig:        make(map[string]interface{}),
		},
		ResourceManager: ResourceManagerConfig{
			Port:                   8088,
			GRPCPort:               9088, // NodeManager gRPC 端口
			AMGRPCPort:             9089, // ApplicationMaster gRPC 端口
			Address:                "0.0.0.0",
			HeartbeatInterval:      3 * time.Second,
			NodeExpiryTimeout:      30 * time.Second,
			ApplicationMaxAttempts: 2,
		},
		NodeManager: NodeManagerConfig{
			Port:                   8042,
			Address:                "0.0.0.0",
			ResourceManagerURL:     "http://localhost:8088",
			ResourceManagerGRPCURL: "localhost:9088",
			HeartbeatInterval:      3 * time.Second,
			ContainerCleanupDelay:  5 * time.Second,
			ContainerMemoryLimitMB: 8192,
			ContainerVCoresLimit:   8,
		},
		ApplicationMaster: ApplicationMasterConfig{
			Port:               8088,
			Address:            "0.0.0.0",
			ResourceManagerURL: "http://localhost:8030",
			HeartbeatInterval:  10 * time.Second,
			MaxRetries:         3,
			AppType:            "simple",
			NumTasks:           3,
			NumWorkers:         2,
			TrackingURL:        "",
			EnableDebug:        false,
		},
		Scheduler: SchedulerConfig{
			Type:    "fifo",
			MaxApps: 1000,
			QueueConfigs: map[string]QueueConfig{
				"default": {
					Name:        "default",
					Capacity:    1.0,
					MaxCapacity: 1.0,
					Priority:    1,
				},
			},
		},
		Security: SecurityConfig{
			Enabled:         false,
			TokenExpiry:     24 * time.Hour,
			SecretKey:       getEnvOrDefault("CARROT_SECRET_KEY", "default-secret"),
			ContainerSecure: false,
		},
		Log: LogConfig{
			Level:       "info",
			Development: false,
			FileOutput: LogFileConfig{
				Enabled:          true,
				Directory:        "logs",
				MaxFileSize:      "100MB",
				MaxBackups:       168, // 保留一周的小时日志
				MaxAge:           7,   // 保留7天
				Compress:         true,
				HourlyRotation:   true,
				DailyCompression: true,
			},
			KafkaOutput: LogKafkaConfig{
				Enabled:   false,
				Brokers:   []string{"localhost:9092"},
				Topic:     "carrot-logs",
				BatchSize: 100,
				Timeout:   "10s",
				Retries:   3,
			},
			DorisOutput: LogDorisConfig{
				Enabled:       false,
				StreamLoadURL: "http://localhost:8030/api/carrot_logs/logs/_stream_load",
				Database:      "carrot_logs",
				Table:         "logs",
				Username:      "root",
				Password:      "",
				BatchSize:     1000,
				FlushInterval: "30s",
			},
			ConsoleOutput: LogConsoleConfig{
				Enabled:   true,
				Colorized: true,
			},
		},
	}
}

// getEnvOrDefault 获取环境变量或使用默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// LoadConfig 从配置文件加载配置
func LoadConfig(configPath string) (*Config, error) {
	// 如果配置文件不存在，使用默认配置
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return GetDefaultConfig(), nil
	}

	// 读取配置文件
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// 解析配置文件
	config := GetDefaultConfig() // 先获取默认配置作为基础
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	return config, nil
}

// LoadConfigFromDir 从指定目录加载配置文件
func LoadConfigFromDir(configDir, filename string) (*Config, error) {
	configPath := filepath.Join(configDir, filename)
	return LoadConfig(configPath)
}

// SaveConfig 保存配置到文件
func SaveConfig(config *Config, configPath string) error {
	// 确保目录存在
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	// 序列化配置
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 写入文件
	if err := ioutil.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", configPath, err)
	}

	return nil
}
