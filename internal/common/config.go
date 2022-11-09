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
	NodeManager       NodeManagerConfig       `yaml:"nodemanager"`
	ApplicationMaster ApplicationMasterConfig `yaml:"applicationmaster"`
	Scheduler         SchedulerConfig         `yaml:"scheduler"`
	Security          SecurityConfig          `yaml:"security"`
	HeartbeatTimeout  int                     `yaml:"heartbeat_timeout"` // 心跳超时时间（秒）
	MonitorInterval   int                     `yaml:"monitor_interval"`  // 监测间隔（秒）
}

// ResourceManagerConfig ResourceManager配置
type ResourceManagerConfig struct {
	Port                   int           `yaml:"port"`
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
	HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`
	ContainerCleanupDelay  time.Duration `yaml:"container_cleanup_delay"`
	ContainerMemoryLimitMB int64         `yaml:"container_memory_limit_mb"`
	ContainerVCoresLimit   int32         `yaml:"container_vcores_limit"`
}

// ApplicationMasterConfig ApplicationMaster配置
type ApplicationMasterConfig struct {
	Port                int           `yaml:"port"`
	Address             string        `yaml:"address"`
	ResourceManagerURL  string        `yaml:"resourcemanager_url"`
	HeartbeatInterval   time.Duration `yaml:"heartbeat_interval"`
	MaxRetries          int           `yaml:"max_retries"`
	AppType             string        `yaml:"app_type"`     // simple, distributed
	NumTasks            int           `yaml:"num_tasks"`    // for simple app
	NumWorkers          int           `yaml:"num_workers"`  // for distributed app
	TrackingURL         string        `yaml:"tracking_url"`
	EnableDebug         bool          `yaml:"enable_debug"`
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

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		Cluster: ClusterConfig{
			Name:                    "carrot-cluster",
			ID:                      "",
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
			Address:                "0.0.0.0",
			HeartbeatInterval:      3 * time.Second,
			NodeExpiryTimeout:      30 * time.Second,
			ApplicationMaxAttempts: 2,
		},
		NodeManager: NodeManagerConfig{
			Port:                   8042,
			Address:                "0.0.0.0",
			ResourceManagerURL:     "http://localhost:8088",
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
