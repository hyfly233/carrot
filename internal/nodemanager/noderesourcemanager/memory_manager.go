package noderesourcemanager

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hyfly233/carrot/internal/common"
	"go.uber.org/zap"
)

// MemoryManager 内存管理器
type MemoryManager struct {
	mu     sync.RWMutex
	config *MemoryManagerConfig
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// 内存分配状态
	allocatedMemory map[string]*MemoryAllocation
	totalMemory     int64
	availableMemory int64
	reservedMemory  int64

	// 内存池
	memoryPools map[string]*MemoryPool

	// 监控状态
	isRunning bool
	eventChan chan *MemoryEvent

	// 统计信息
	stats *MemoryManagerStats
}

// MemoryManagerConfig 内存管理器配置
type MemoryManagerConfig struct {
	CgroupsPath             string                       `json:"cgroups_path"`
	Policy                  MemoryPolicy                 `json:"policy"`
	ReservedMemory          int64                        `json:"reserved_memory"`
	SwapEnabled             bool                         `json:"swap_enabled"`
	OOMKillDisabled         bool                         `json:"oom_kill_disabled"`
	MemoryPressureThreshold float64                      `json:"memory_pressure_threshold"`
	MonitoringInterval      time.Duration                `json:"monitoring_interval"`
	EnableHugePages         bool                         `json:"enable_huge_pages"`
	HugePagesSize           string                       `json:"huge_pages_size"`
	MemoryQoSEnabled        bool                         `json:"memory_qos_enabled"`
	MemoryPools             map[string]*MemoryPoolConfig `json:"memory_pools"`
}

// MemoryPolicy 内存分配策略
type MemoryPolicy string

const (
	MemoryPolicyBestEffort MemoryPolicy = "best_effort"
	MemoryPolicyGuaranteed MemoryPolicy = "guaranteed"
	MemoryPolicyBurstable  MemoryPolicy = "burstable"
)

// MemoryAllocation 内存分配信息
type MemoryAllocation struct {
	ContainerID    string               `json:"container_id"`
	MemoryRequest  *common.ResourceSpec `json:"memory_request"`
	MemoryLimit    int64                `json:"memory_limit"`
	SwapLimit      int64                `json:"swap_limit"`
	OOMKillDisable bool                 `json:"oom_kill_disable"`
	MemoryUsage    int64                `json:"memory_usage"`
	SwapUsage      int64                `json:"swap_usage"`
	Timestamp      time.Time            `json:"timestamp"`
	Policy         MemoryPolicy         `json:"policy"`
	QoSClass       string               `json:"qos_class"`
	Priority       int                  `json:"priority"`
}

// MemoryPool 内存池
type MemoryPool struct {
	Name          string                       `json:"name"`
	TotalSize     int64                        `json:"total_size"`
	AllocatedSize int64                        `json:"allocated_size"`
	AvailableSize int64                        `json:"available_size"`
	Policy        MemoryPoolPolicy             `json:"policy"`
	Priority      int                          `json:"priority"`
	Allocations   map[string]*MemoryAllocation `json:"allocations"`
	CreatedAt     time.Time                    `json:"created_at"`
	mu            sync.RWMutex
}

// MemoryPoolConfig 内存池配置
type MemoryPoolConfig struct {
	Size     int64            `json:"size"`
	Policy   MemoryPoolPolicy `json:"policy"`
	Priority int              `json:"priority"`
}

// MemoryPoolPolicy 内存池策略
type MemoryPoolPolicy string

const (
	MemoryPoolPolicyShared    MemoryPoolPolicy = "shared"
	MemoryPoolPolicyDedicated MemoryPoolPolicy = "dedicated"
	MemoryPoolPolicyElastic   MemoryPoolPolicy = "elastic"
)

// MemoryEvent 内存事件
type MemoryEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
}

// MemoryEventType 内存事件类型
const (
	EventTypeMemoryAllocated    = "MEMORY_ALLOCATED"
	EventTypeMemoryDeallocated  = "MEMORY_DEALLOCATED"
	EventTypeMemoryPressure     = "MEMORY_PRESSURE"
	EventTypeOOMKilled          = "OOM_KILLED"
	EventTypeSwapUsageHigh      = "SWAP_USAGE_HIGH"
	EventTypeMemoryLimitReached = "MEMORY_LIMIT_REACHED"
	EventTypeMemoryPoolCreated  = "MEMORY_POOL_CREATED"
	EventTypeMemoryPoolDeleted  = "MEMORY_POOL_DELETED"
)

// MemoryManagerStats 内存管理器统计信息
type MemoryManagerStats struct {
	TotalMemory      int64                        `json:"total_memory"`
	AvailableMemory  int64                        `json:"available_memory"`
	AllocatedMemory  int64                        `json:"allocated_memory"`
	ReservedMemory   int64                        `json:"reserved_memory"`
	UsedMemory       int64                        `json:"used_memory"`
	FreeMemory       int64                        `json:"free_memory"`
	CachedMemory     int64                        `json:"cached_memory"`
	BufferedMemory   int64                        `json:"buffered_memory"`
	SwapTotal        int64                        `json:"swap_total"`
	SwapUsed         int64                        `json:"swap_used"`
	SwapFree         int64                        `json:"swap_free"`
	MemoryPressure   float64                      `json:"memory_pressure"`
	ActiveContainers int                          `json:"active_containers"`
	TotalAllocations int                          `json:"total_allocations"`
	Allocations      map[string]*MemoryAllocation `json:"allocations"`
	MemoryPools      map[string]*MemoryPoolStats  `json:"memory_pools"`
	LastUpdated      time.Time                    `json:"last_updated"`
}

// MemoryPoolStats 内存池统计信息
type MemoryPoolStats struct {
	Name              string  `json:"name"`
	TotalSize         int64   `json:"total_size"`
	AllocatedSize     int64   `json:"allocated_size"`
	AvailableSize     int64   `json:"available_size"`
	UtilizationRate   float64 `json:"utilization_rate"`
	ActiveAllocations int     `json:"active_allocations"`
}

// MemoryUsage 内存使用情况
type MemoryUsage struct {
	RSS          int64     `json:"rss"`
	Cache        int64     `json:"cache"`
	Swap         int64     `json:"swap"`
	Mapped       int64     `json:"mapped"`
	Total        int64     `json:"total"`
	Limit        int64     `json:"limit"`
	UsagePercent float64   `json:"usage_percent"`
	Timestamp    time.Time `json:"timestamp"`
}

// NewMemoryManager 创建内存管理器
func NewMemoryManager(config *MemoryManagerConfig) (*MemoryManager, error) {
	if config == nil {
		return nil, fmt.Errorf("memory manager config cannot be nil")
	}

	// 设置默认值
	if config.CgroupsPath == "" {
		config.CgroupsPath = "/sys/fs/cgroup"
	}
	if config.MonitoringInterval == 0 {
		config.MonitoringInterval = 30 * time.Second
	}
	if config.MemoryPressureThreshold == 0 {
		config.MemoryPressureThreshold = 80.0
	}
	if config.HugePagesSize == "" {
		config.HugePagesSize = "2MB"
	}

	ctx, cancel := context.WithCancel(context.Background())

	mm := &MemoryManager{
		config:          config,
		logger:          zap.NewNop(), // 在实际使用中应该注入正确的logger
		ctx:             ctx,
		cancel:          cancel,
		allocatedMemory: make(map[string]*MemoryAllocation),
		memoryPools:     make(map[string]*MemoryPool),
		eventChan:       make(chan *MemoryEvent, 1000),
		stats: &MemoryManagerStats{
			Allocations: make(map[string]*MemoryAllocation),
			MemoryPools: make(map[string]*MemoryPoolStats),
		},
	}

	// 初始化系统内存信息
	if err := mm.initSystemMemory(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize system memory: %v", err)
	}

	// 初始化内存池
	if err := mm.initMemoryPools(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize memory pools: %v", err)
	}

	return mm, nil
}

// Start 启动内存管理器
func (mm *MemoryManager) Start() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if mm.isRunning {
		return fmt.Errorf("memory manager is already running")
	}

	mm.logger.Info("Starting memory manager",
		zap.String("policy", string(mm.config.Policy)),
		zap.Int64("total_memory", mm.totalMemory),
		zap.Int64("available_memory", mm.availableMemory),
		zap.Int64("reserved_memory", mm.reservedMemory))

	mm.isRunning = true

	// 启动监控任务
	go mm.monitoringWorker()

	// 发送启动事件
	mm.sendEvent(&MemoryEvent{
		Type:      "MEMORY_MANAGER_STARTED",
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"policy":           string(mm.config.Policy),
			"total_memory":     mm.totalMemory,
			"available_memory": mm.availableMemory,
			"reserved_memory":  mm.reservedMemory,
		},
	})

	return nil
}

// Stop 停止内存管理器
func (mm *MemoryManager) Stop() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if !mm.isRunning {
		return nil
	}

	mm.logger.Info("Stopping memory manager")
	mm.isRunning = false
	mm.cancel()

	// 清理所有内存分配
	for containerID := range mm.allocatedMemory {
		if err := mm.releaseMemoryInternal(containerID); err != nil {
			mm.logger.Error("Failed to release memory during shutdown",
				zap.String("container_id", containerID),
				zap.Error(err))
		}
	}

	close(mm.eventChan)
	return nil
}

// AllocateMemory 为容器分配内存
func (mm *MemoryManager) AllocateMemory(containerID string, request *common.ResourceSpec) (*MemoryAllocation, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if !mm.isRunning {
		return nil, fmt.Errorf("memory manager is not running")
	}

	// 检查是否已经分配
	if existing, exists := mm.allocatedMemory[containerID]; exists {
		return existing, nil
	}

	// 验证内存请求
	if request.Memory <= 0 {
		return nil, fmt.Errorf("memory request must be greater than 0")
	}

	memoryLimit := request.Memory
	if request.MemoryLimit > 0 {
		memoryLimit = request.MemoryLimit
	}

	// 检查可用内存
	if memoryLimit > mm.availableMemory {
		return nil, fmt.Errorf("insufficient memory: requested %d, available %d",
			memoryLimit, mm.availableMemory)
	}

	// 确定QoS类别和策略
	qosClass := mm.determineQoSClass(request)
	policy := mm.determineMemoryPolicy(qosClass)

	// 分配内存
	allocation := &MemoryAllocation{
		ContainerID:    containerID,
		MemoryRequest:  request,
		MemoryLimit:    memoryLimit,
		SwapLimit:      mm.calculateSwapLimit(memoryLimit),
		OOMKillDisable: mm.config.OOMKillDisabled,
		Timestamp:      time.Now(),
		Policy:         policy,
		QoSClass:       qosClass,
		Priority:       mm.calculatePriority(qosClass),
	}

	// 从内存池分配
	if err := mm.allocateFromPool(allocation); err != nil {
		return nil, fmt.Errorf("failed to allocate from memory pool: %v", err)
	}

	// 应用内存限制
	if err := mm.applyMemoryLimits(containerID, allocation); err != nil {
		mm.releaseFromPool(allocation)
		return nil, fmt.Errorf("failed to apply memory limits: %v", err)
	}

	// 保存分配信息
	mm.allocatedMemory[containerID] = allocation
	mm.availableMemory -= memoryLimit

	// 更新统计信息
	mm.updateStats()

	// 发送分配事件
	mm.sendEvent(&MemoryEvent{
		Type:      EventTypeMemoryAllocated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"container_id":   containerID,
			"memory_request": request.Memory,
			"memory_limit":   memoryLimit,
			"qos_class":      qosClass,
			"policy":         string(policy),
		},
	})

	mm.logger.Info("Memory allocated",
		zap.String("container_id", containerID),
		zap.Int64("memory_limit", memoryLimit),
		zap.String("qos_class", qosClass),
		zap.String("policy", string(policy)))

	return allocation, nil
}

// ReleaseMemory 释放容器的内存
func (mm *MemoryManager) ReleaseMemory(containerID string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	return mm.releaseMemoryInternal(containerID)
}

// releaseMemoryInternal 内部释放内存方法（不加锁）
func (mm *MemoryManager) releaseMemoryInternal(containerID string) error {
	allocation, exists := mm.allocatedMemory[containerID]
	if !exists {
		return fmt.Errorf("container %s has no memory allocation", containerID)
	}

	// 移除cgroup限制
	if err := mm.removeMemoryLimits(containerID); err != nil {
		mm.logger.Error("Failed to remove memory limits",
			zap.String("container_id", containerID),
			zap.Error(err))
	}

	// 从内存池释放
	mm.releaseFromPool(allocation)

	// 归还内存
	mm.availableMemory += allocation.MemoryLimit

	// 删除分配记录
	delete(mm.allocatedMemory, containerID)

	// 更新统计信息
	mm.updateStats()

	// 发送释放事件
	mm.sendEvent(&MemoryEvent{
		Type:      EventTypeMemoryDeallocated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"container_id": containerID,
			"memory_limit": allocation.MemoryLimit,
		},
	})

	mm.logger.Info("Memory released",
		zap.String("container_id", containerID),
		zap.Int64("memory_limit", allocation.MemoryLimit))

	return nil
}

// GetMemoryAllocation 获取容器的内存分配信息
func (mm *MemoryManager) GetMemoryAllocation(containerID string) (*MemoryAllocation, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	allocation, exists := mm.allocatedMemory[containerID]
	if !exists {
		return nil, fmt.Errorf("container %s has no memory allocation", containerID)
	}

	// 返回副本
	return &MemoryAllocation{
		ContainerID:    allocation.ContainerID,
		MemoryRequest:  allocation.MemoryRequest,
		MemoryLimit:    allocation.MemoryLimit,
		SwapLimit:      allocation.SwapLimit,
		OOMKillDisable: allocation.OOMKillDisable,
		MemoryUsage:    allocation.MemoryUsage,
		SwapUsage:      allocation.SwapUsage,
		Timestamp:      allocation.Timestamp,
		Policy:         allocation.Policy,
		QoSClass:       allocation.QoSClass,
		Priority:       allocation.Priority,
	}, nil
}

// GetMemoryUsage 获取容器的内存使用情况
func (mm *MemoryManager) GetMemoryUsage(containerID string) (*MemoryUsage, error) {
	mm.mu.RLock()
	allocation, exists := mm.allocatedMemory[containerID]
	mm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("container %s has no memory allocation", containerID)
	}

	// 读取cgroup内存使用统计
	cgroupPath := filepath.Join(mm.config.CgroupsPath, "memory", containerID)
	usage, err := mm.readMemoryUsage(cgroupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory usage: %v", err)
	}

	usage.Limit = allocation.MemoryLimit
	if usage.Total > 0 && usage.Limit > 0 {
		usage.UsagePercent = float64(usage.Total) / float64(usage.Limit) * 100
	}

	return usage, nil
}

// GetStats 获取统计信息
func (mm *MemoryManager) GetStats() *MemoryManagerStats {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// 返回副本
	stats := &MemoryManagerStats{
		TotalMemory:      mm.stats.TotalMemory,
		AvailableMemory:  mm.stats.AvailableMemory,
		AllocatedMemory:  mm.stats.AllocatedMemory,
		ReservedMemory:   mm.stats.ReservedMemory,
		UsedMemory:       mm.stats.UsedMemory,
		FreeMemory:       mm.stats.FreeMemory,
		CachedMemory:     mm.stats.CachedMemory,
		BufferedMemory:   mm.stats.BufferedMemory,
		SwapTotal:        mm.stats.SwapTotal,
		SwapUsed:         mm.stats.SwapUsed,
		SwapFree:         mm.stats.SwapFree,
		MemoryPressure:   mm.stats.MemoryPressure,
		ActiveContainers: mm.stats.ActiveContainers,
		TotalAllocations: mm.stats.TotalAllocations,
		LastUpdated:      mm.stats.LastUpdated,
		Allocations:      make(map[string]*MemoryAllocation),
		MemoryPools:      make(map[string]*MemoryPoolStats),
	}

	// 复制分配信息
	for id, alloc := range mm.stats.Allocations {
		stats.Allocations[id] = &MemoryAllocation{
			ContainerID:    alloc.ContainerID,
			MemoryRequest:  alloc.MemoryRequest,
			MemoryLimit:    alloc.MemoryLimit,
			SwapLimit:      alloc.SwapLimit,
			OOMKillDisable: alloc.OOMKillDisable,
			MemoryUsage:    alloc.MemoryUsage,
			SwapUsage:      alloc.SwapUsage,
			Timestamp:      alloc.Timestamp,
			Policy:         alloc.Policy,
			QoSClass:       alloc.QoSClass,
			Priority:       alloc.Priority,
		}
	}

	// 复制内存池统计
	for name, pool := range mm.stats.MemoryPools {
		stats.MemoryPools[name] = &MemoryPoolStats{
			Name:              pool.Name,
			TotalSize:         pool.TotalSize,
			AllocatedSize:     pool.AllocatedSize,
			AvailableSize:     pool.AvailableSize,
			UtilizationRate:   pool.UtilizationRate,
			ActiveAllocations: pool.ActiveAllocations,
		}
	}

	return stats
}

// GetEvents 获取事件通道
func (mm *MemoryManager) GetEvents() <-chan *MemoryEvent {
	return mm.eventChan
}

// CreateMemoryPool 创建内存池
func (mm *MemoryManager) CreateMemoryPool(name string, config *MemoryPoolConfig) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if _, exists := mm.memoryPools[name]; exists {
		return fmt.Errorf("memory pool %s already exists", name)
	}

	if config.Size > mm.availableMemory {
		return fmt.Errorf("insufficient memory for pool: requested %d, available %d",
			config.Size, mm.availableMemory)
	}

	pool := &MemoryPool{
		Name:          name,
		TotalSize:     config.Size,
		AllocatedSize: 0,
		AvailableSize: config.Size,
		Policy:        config.Policy,
		Priority:      config.Priority,
		Allocations:   make(map[string]*MemoryAllocation),
		CreatedAt:     time.Now(),
	}

	mm.memoryPools[name] = pool
	mm.availableMemory -= config.Size

	// 发送事件
	mm.sendEvent(&MemoryEvent{
		Type:      EventTypeMemoryPoolCreated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"pool_name": name,
			"pool_size": config.Size,
			"policy":    string(config.Policy),
		},
	})

	mm.logger.Info("Memory pool created",
		zap.String("pool_name", name),
		zap.Int64("pool_size", config.Size))

	return nil
}

// DeleteMemoryPool 删除内存池
func (mm *MemoryManager) DeleteMemoryPool(name string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	pool, exists := mm.memoryPools[name]
	if !exists {
		return fmt.Errorf("memory pool %s does not exist", name)
	}

	if pool.AllocatedSize > 0 {
		return fmt.Errorf("cannot delete memory pool %s: still has allocations", name)
	}

	mm.availableMemory += pool.TotalSize
	delete(mm.memoryPools, name)

	// 发送事件
	mm.sendEvent(&MemoryEvent{
		Type:      EventTypeMemoryPoolDeleted,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"pool_name": name,
			"pool_size": pool.TotalSize,
		},
	})

	mm.logger.Info("Memory pool deleted", zap.String("pool_name", name))
	return nil
}

// initSystemMemory 初始化系统内存信息
func (mm *MemoryManager) initSystemMemory() error {
	// 读取系统内存信息
	meminfo, err := mm.readSystemMeminfo()
	if err != nil {
		return err
	}

	mm.totalMemory = meminfo["MemTotal"]
	mm.reservedMemory = mm.config.ReservedMemory
	mm.availableMemory = mm.totalMemory - mm.reservedMemory

	mm.logger.Info("System memory initialized",
		zap.Int64("total_memory", mm.totalMemory),
		zap.Int64("reserved_memory", mm.reservedMemory),
		zap.Int64("available_memory", mm.availableMemory))

	return nil
}

// initMemoryPools 初始化内存池
func (mm *MemoryManager) initMemoryPools() error {
	if mm.config.MemoryPools == nil {
		return nil
	}

	for name, config := range mm.config.MemoryPools {
		if err := mm.CreateMemoryPool(name, config); err != nil {
			return fmt.Errorf("failed to create memory pool %s: %v", name, err)
		}
	}

	return nil
}

// readSystemMeminfo 读取系统内存信息
func (mm *MemoryManager) readSystemMeminfo() (map[string]int64, error) {
	data, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, err
	}

	meminfo := make(map[string]int64)
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			value, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				meminfo[key] = value * 1024 // 转换为字节
			}
		}
	}

	return meminfo, nil
}

// determineQoSClass 确定QoS类别
func (mm *MemoryManager) determineQoSClass(request *common.ResourceSpec) string {
	if request.Memory == request.MemoryLimit && request.CPU == request.CPULimit {
		return "Guaranteed"
	} else if request.Memory > 0 || request.CPU > 0 {
		return "Burstable"
	} else {
		return "BestEffort"
	}
}

// determineMemoryPolicy 确定内存策略
func (mm *MemoryManager) determineMemoryPolicy(qosClass string) MemoryPolicy {
	switch qosClass {
	case "Guaranteed":
		return MemoryPolicyGuaranteed
	case "Burstable":
		return MemoryPolicyBurstable
	default:
		return MemoryPolicyBestEffort
	}
}

// calculateSwapLimit 计算Swap限制
func (mm *MemoryManager) calculateSwapLimit(memoryLimit int64) int64 {
	if !mm.config.SwapEnabled {
		return memoryLimit // 禁用swap时，swap限制等于内存限制
	}

	// 允许swap为内存的2倍
	return memoryLimit * 2
}

// calculatePriority 计算优先级
func (mm *MemoryManager) calculatePriority(qosClass string) int {
	switch qosClass {
	case "Guaranteed":
		return 1000
	case "Burstable":
		return 500
	default:
		return 100
	}
}

// allocateFromPool 从内存池分配
func (mm *MemoryManager) allocateFromPool(allocation *MemoryAllocation) error {
	// 如果没有配置内存池，直接返回
	if len(mm.memoryPools) == 0 {
		return nil
	}

	// 选择合适的内存池
	poolName := mm.selectMemoryPool(allocation)
	if poolName == "" {
		return fmt.Errorf("no suitable memory pool found")
	}

	pool := mm.memoryPools[poolName]
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if allocation.MemoryLimit > pool.AvailableSize {
		return fmt.Errorf("insufficient memory in pool %s: requested %d, available %d",
			poolName, allocation.MemoryLimit, pool.AvailableSize)
	}

	// 分配内存
	pool.AllocatedSize += allocation.MemoryLimit
	pool.AvailableSize -= allocation.MemoryLimit
	pool.Allocations[allocation.ContainerID] = allocation

	return nil
}

// releaseFromPool 从内存池释放
func (mm *MemoryManager) releaseFromPool(allocation *MemoryAllocation) {
	for _, pool := range mm.memoryPools {
		pool.mu.Lock()
		if _, exists := pool.Allocations[allocation.ContainerID]; exists {
			pool.AllocatedSize -= allocation.MemoryLimit
			pool.AvailableSize += allocation.MemoryLimit
			delete(pool.Allocations, allocation.ContainerID)
			pool.mu.Unlock()
			return
		}
		pool.mu.Unlock()
	}
}

// selectMemoryPool 选择内存池
func (mm *MemoryManager) selectMemoryPool(allocation *MemoryAllocation) string {
	var bestPool string
	var bestScore int

	for name, pool := range mm.memoryPools {
		if allocation.MemoryLimit > pool.AvailableSize {
			continue
		}

		score := pool.Priority
		if allocation.Policy == MemoryPolicyGuaranteed && pool.Policy == MemoryPoolPolicyDedicated {
			score += 1000
		}

		if score > bestScore {
			bestScore = score
			bestPool = name
		}
	}

	return bestPool
}

// applyMemoryLimits 应用内存限制
func (mm *MemoryManager) applyMemoryLimits(containerID string, allocation *MemoryAllocation) error {
	cgroupPath := filepath.Join(mm.config.CgroupsPath, "memory", containerID)

	// 创建cgroup目录
	if err := os.MkdirAll(cgroupPath, 0755); err != nil {
		return fmt.Errorf("failed to create memory cgroup directory: %v", err)
	}

	// 设置内存限制
	limitPath := filepath.Join(cgroupPath, "memory.limit_in_bytes")
	if err := ioutil.WriteFile(limitPath, []byte(fmt.Sprintf("%d", allocation.MemoryLimit)), 0644); err != nil {
		return fmt.Errorf("failed to set memory limit: %v", err)
	}

	// 设置swap限制
	if mm.config.SwapEnabled {
		swapLimitPath := filepath.Join(cgroupPath, "memory.memsw.limit_in_bytes")
		if err := ioutil.WriteFile(swapLimitPath, []byte(fmt.Sprintf("%d", allocation.SwapLimit)), 0644); err != nil {
			mm.logger.Warn("Failed to set swap limit", zap.Error(err))
		}
	}

	// 设置OOM控制
	if allocation.OOMKillDisable {
		oomControlPath := filepath.Join(cgroupPath, "memory.oom_control")
		if err := ioutil.WriteFile(oomControlPath, []byte("1"), 0644); err != nil {
			mm.logger.Warn("Failed to disable OOM killer", zap.Error(err))
		}
	}

	// 设置内存回收策略
	swappinessPath := filepath.Join(cgroupPath, "memory.swappiness")
	swappiness := "60" // 默认值
	if !mm.config.SwapEnabled {
		swappiness = "0"
	}
	if err := ioutil.WriteFile(swappinessPath, []byte(swappiness), 0644); err != nil {
		mm.logger.Warn("Failed to set memory swappiness", zap.Error(err))
	}

	return nil
}

// removeMemoryLimits 移除内存限制
func (mm *MemoryManager) removeMemoryLimits(containerID string) error {
	cgroupPath := filepath.Join(mm.config.CgroupsPath, "memory", containerID)
	if err := os.RemoveAll(cgroupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove memory cgroup: %v", err)
	}
	return nil
}

// readMemoryUsage 读取内存使用情况
func (mm *MemoryManager) readMemoryUsage(cgroupPath string) (*MemoryUsage, error) {
	usage := &MemoryUsage{
		Timestamp: time.Now(),
	}

	// 读取内存使用统计
	statPath := filepath.Join(cgroupPath, "memory.stat")
	if data, err := ioutil.ReadFile(statPath); err == nil {
		stats := mm.parseMemoryStats(string(data))
		usage.RSS = stats["rss"]
		usage.Cache = stats["cache"]
		usage.Mapped = stats["mapped_file"]
	}

	// 读取当前内存使用量
	usagePath := filepath.Join(cgroupPath, "memory.usage_in_bytes")
	if data, err := ioutil.ReadFile(usagePath); err == nil {
		if total, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			usage.Total = total
		}
	}

	// 读取swap使用量
	if mm.config.SwapEnabled {
		swapUsagePath := filepath.Join(cgroupPath, "memory.memsw.usage_in_bytes")
		if data, err := ioutil.ReadFile(swapUsagePath); err == nil {
			if swapTotal, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
				usage.Swap = swapTotal - usage.Total
			}
		}
	}

	return usage, nil
}

// parseMemoryStats 解析内存统计信息
func (mm *MemoryManager) parseMemoryStats(stats string) map[string]int64 {
	result := make(map[string]int64)
	lines := strings.Split(stats, "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := parts[0]
			value, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				result[key] = value
			}
		}
	}

	return result
}

// updateStats 更新统计信息
func (mm *MemoryManager) updateStats() {
	// 读取系统内存信息
	meminfo, err := mm.readSystemMeminfo()
	if err != nil {
		mm.logger.Error("Failed to read system memory info", zap.Error(err))
		return
	}

	mm.stats.TotalMemory = mm.totalMemory
	mm.stats.AvailableMemory = mm.availableMemory
	mm.stats.ReservedMemory = mm.reservedMemory
	mm.stats.UsedMemory = meminfo["MemTotal"] - meminfo["MemAvailable"]
	mm.stats.FreeMemory = meminfo["MemFree"]
	mm.stats.CachedMemory = meminfo["Cached"]
	mm.stats.BufferedMemory = meminfo["Buffers"]
	mm.stats.SwapTotal = meminfo["SwapTotal"]
	mm.stats.SwapUsed = meminfo["SwapTotal"] - meminfo["SwapFree"]
	mm.stats.SwapFree = meminfo["SwapFree"]

	// 计算内存压力
	if mm.stats.TotalMemory > 0 {
		usagePercent := float64(mm.stats.UsedMemory) / float64(mm.stats.TotalMemory) * 100
		mm.stats.MemoryPressure = usagePercent
	}

	// 计算已分配内存
	var allocatedMemory int64
	mm.stats.Allocations = make(map[string]*MemoryAllocation)

	for containerID, allocation := range mm.allocatedMemory {
		allocatedMemory += allocation.MemoryLimit

		// 更新使用情况
		if usage, err := mm.GetMemoryUsage(containerID); err == nil {
			allocation.MemoryUsage = usage.Total
			allocation.SwapUsage = usage.Swap
		}

		// 复制分配信息
		mm.stats.Allocations[containerID] = &MemoryAllocation{
			ContainerID:    allocation.ContainerID,
			MemoryRequest:  allocation.MemoryRequest,
			MemoryLimit:    allocation.MemoryLimit,
			SwapLimit:      allocation.SwapLimit,
			OOMKillDisable: allocation.OOMKillDisable,
			MemoryUsage:    allocation.MemoryUsage,
			SwapUsage:      allocation.SwapUsage,
			Timestamp:      allocation.Timestamp,
			Policy:         allocation.Policy,
			QoSClass:       allocation.QoSClass,
			Priority:       allocation.Priority,
		}
	}

	mm.stats.AllocatedMemory = allocatedMemory
	mm.stats.ActiveContainers = len(mm.allocatedMemory)
	mm.stats.TotalAllocations = len(mm.allocatedMemory)
	mm.stats.LastUpdated = time.Now()

	// 更新内存池统计
	mm.stats.MemoryPools = make(map[string]*MemoryPoolStats)
	for name, pool := range mm.memoryPools {
		pool.mu.RLock()
		var utilizationRate float64
		if pool.TotalSize > 0 {
			utilizationRate = float64(pool.AllocatedSize) / float64(pool.TotalSize) * 100
		}

		mm.stats.MemoryPools[name] = &MemoryPoolStats{
			Name:              name,
			TotalSize:         pool.TotalSize,
			AllocatedSize:     pool.AllocatedSize,
			AvailableSize:     pool.AvailableSize,
			UtilizationRate:   utilizationRate,
			ActiveAllocations: len(pool.Allocations),
		}
		pool.mu.RUnlock()
	}
}

// monitoringWorker 监控工作线程
func (mm *MemoryManager) monitoringWorker() {
	ticker := time.NewTicker(mm.config.MonitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.performMonitoring()
		case <-mm.ctx.Done():
			return
		}
	}
}

// performMonitoring 执行监控
func (mm *MemoryManager) performMonitoring() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 更新统计信息
	mm.updateStats()

	// 检查内存压力
	if mm.stats.MemoryPressure > mm.config.MemoryPressureThreshold {
		mm.sendEvent(&MemoryEvent{
			Type:      EventTypeMemoryPressure,
			Timestamp: time.Now(),
			Severity:  EventSeverityWarning,
			Data: map[string]interface{}{
				"memory_pressure": mm.stats.MemoryPressure,
				"threshold":       mm.config.MemoryPressureThreshold,
				"used_memory":     mm.stats.UsedMemory,
				"total_memory":    mm.stats.TotalMemory,
			},
		})
	}

	// 检查Swap使用
	if mm.config.SwapEnabled && mm.stats.SwapTotal > 0 {
		swapUsagePercent := float64(mm.stats.SwapUsed) / float64(mm.stats.SwapTotal) * 100
		if swapUsagePercent > 50.0 {
			mm.sendEvent(&MemoryEvent{
				Type:      EventTypeSwapUsageHigh,
				Timestamp: time.Now(),
				Severity:  EventSeverityWarning,
				Data: map[string]interface{}{
					"swap_usage_percent": swapUsagePercent,
					"swap_used":          mm.stats.SwapUsed,
					"swap_total":         mm.stats.SwapTotal,
				},
			})
		}
	}

	// 检查OOM事件
	mm.checkOOMEvents()

	// 发送监控事件
	mm.sendEvent(&MemoryEvent{
		Type:      "MEMORY_MONITORING",
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"total_memory":      mm.stats.TotalMemory,
			"available_memory":  mm.stats.AvailableMemory,
			"allocated_memory":  mm.stats.AllocatedMemory,
			"memory_pressure":   mm.stats.MemoryPressure,
			"active_containers": mm.stats.ActiveContainers,
		},
	})
}

// checkOOMEvents 检查OOM事件
func (mm *MemoryManager) checkOOMEvents() {
	for containerID := range mm.allocatedMemory {
		cgroupPath := filepath.Join(mm.config.CgroupsPath, "memory", containerID)
		oomControlPath := filepath.Join(cgroupPath, "memory.oom_control")

		if data, err := ioutil.ReadFile(oomControlPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "oom_kill_count") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						if count, err := strconv.ParseInt(parts[1], 10, 64); err == nil && count > 0 {
							mm.sendEvent(&MemoryEvent{
								Type:      EventTypeOOMKilled,
								Timestamp: time.Now(),
								Severity:  EventSeverityCritical,
								Data: map[string]interface{}{
									"container_id": containerID,
									"oom_count":    count,
								},
							})
						}
					}
				}
			}
		}
	}
}

// sendEvent 发送事件
func (mm *MemoryManager) sendEvent(event *MemoryEvent) {
	select {
	case mm.eventChan <- event:
	default:
		mm.logger.Warn("Memory event channel full, dropping event",
			zap.String("event_type", event.Type))
	}
}
