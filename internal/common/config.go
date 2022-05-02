package common

import (
	"os"
	"strconv"
	"time"
)

// Config 全局配置
type Config struct {
	ResourceManager ResourceManagerConfig `yaml:"resourcemanager"`
	NodeManager     NodeManagerConfig     `yaml:"nodemanager"`
	Scheduler       SchedulerConfig       `yaml:"scheduler"`
	Security        SecurityConfig        `yaml:"security"`
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

// getEnvIntOrDefault 获取环境变量整数值或使用默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
