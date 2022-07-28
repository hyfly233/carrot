package noderesourcemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"
	"go.uber.org/zap"
)

// ResourceManager 资源管理器
type ResourceManager struct {
	mu     sync.RWMutex
	config *ResourceManagerConfig
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// 子管理器
	cpuManager    *CPUManager
	memoryManager *MemoryManager

	// 资源分配状态
	allocations map[string]*ResourceAllocation

	// 监控状态
	isRunning bool
	eventChan chan *ResourceEvent

	// 统计信息
	stats *ResourceManagerStats
}

// ResourceManagerConfig 资源管理器配置
type ResourceManagerConfig struct {
	CPU                *CPUManagerConfig    `json:"cpu"`
	Memory             *MemoryManagerConfig `json:"memory"`
	Policy             ResourcePolicy       `json:"policy"`
	QoSEnabled         bool                 `json:"qos_enabled"`
	MonitoringInterval time.Duration        `json:"monitoring_interval"`
	EventBufferSize    int                  `json:"event_buffer_size"`
}

// ResourcePolicy 资源分配策略
type ResourcePolicy string

const (
	ResourcePolicyBestEffort    ResourcePolicy = "best_effort"
	ResourcePolicyGuaranteed    ResourcePolicy = "guaranteed"
	ResourcePolicyBurstable     ResourcePolicy = "burstable"
	ResourcePolicyTopologyAware ResourcePolicy = "topology_aware"
)

// ResourceAllocation 资源分配信息
type ResourceAllocation struct {
	ContainerID      string               `json:"container_id"`
	Request          *common.ResourceSpec `json:"request"`
	CPUAllocation    *CPUAllocation       `json:"cpu_allocation"`
	MemoryAllocation *MemoryAllocation    `json:"memory_allocation"`
	QoSClass         string               `json:"qos_class"`
	Priority         int                  `json:"priority"`
	Timestamp        time.Time            `json:"timestamp"`
	Status           AllocationStatus     `json:"status"`
}

// AllocationStatus 分配状态
type AllocationStatus string

const (
	AllocationStatusPending   AllocationStatus = "PENDING"
	AllocationStatusAllocated AllocationStatus = "ALLOCATED"
	AllocationStatusFailed    AllocationStatus = "FAILED"
	AllocationStatusReleased  AllocationStatus = "RELEASED"
)

// ResourceEvent 资源事件
type ResourceEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
	Source    string                 `json:"source"`
}

// ResourceEventType 资源事件类型
const (
	EventTypeResourceAllocated        = "RESOURCE_ALLOCATED"
	EventTypeResourceDeallocated      = "RESOURCE_DEALLOCATED"
	EventTypeResourceAllocationFailed = "RESOURCE_ALLOCATION_FAILED"
	EventTypeResourcePressure         = "RESOURCE_PRESSURE"
	EventTypeResourceManagerStarted   = "RESOURCE_MANAGER_STARTED"
	EventTypeResourceManagerStopped   = "RESOURCE_MANAGER_STOPPED"
	EventTypeQoSViolation             = "QOS_VIOLATION"
)

// ResourceManagerStats 资源管理器统计信息
type ResourceManagerStats struct {
	CPU                 *CPUManagerStats     `json:"cpu"`
	Memory              *MemoryManagerStats  `json:"memory"`
	TotalAllocations    int                  `json:"total_allocations"`
	ActiveContainers    int                  `json:"active_containers"`
	FailedAllocations   int                  `json:"failed_allocations"`
	ResourceUtilization *ResourceUtilization `json:"resource_utilization"`
	QoSStats            *QoSStats            `json:"qos_stats"`
	LastUpdated         time.Time            `json:"last_updated"`
}

// ResourceUtilization 资源利用率
type ResourceUtilization struct {
	CPUUtilization     float64 `json:"cpu_utilization"`
	MemoryUtilization  float64 `json:"memory_utilization"`
	OverallUtilization float64 `json:"overall_utilization"`
}

// QoSStats QoS统计信息
type QoSStats struct {
	GuaranteedContainers int `json:"guaranteed_containers"`
	BurstableContainers  int `json:"burstable_containers"`
	BestEffortContainers int `json:"best_effort_containers"`
	QoSViolations        int `json:"qos_violations"`
}

// NewResourceManager 创建资源管理器
func NewResourceManager(config *ResourceManagerConfig) (*ResourceManager, error) {
	if config == nil {
		return nil, fmt.Errorf("resource manager config cannot be nil")
	}

	// 设置默认值
	if config.MonitoringInterval == 0 {
		config.MonitoringInterval = 30 * time.Second
	}
	if config.EventBufferSize == 0 {
		config.EventBufferSize = 1000
	}

	ctx, cancel := context.WithCancel(context.Background())

	rm := &ResourceManager{
		config:      config,
		logger:      zap.NewNop(), // 在实际使用中应该注入正确的logger
		ctx:         ctx,
		cancel:      cancel,
		allocations: make(map[string]*ResourceAllocation),
		eventChan:   make(chan *ResourceEvent, config.EventBufferSize),
		stats: &ResourceManagerStats{
			ResourceUtilization: &ResourceUtilization{},
			QoSStats:            &QoSStats{},
		},
	}

	// 初始化CPU管理器
	if config.CPU != nil {
		cpuManager, err := NewCPUManager(config.CPU)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create CPU manager: %v", err)
		}
		rm.cpuManager = cpuManager
	}

	// 初始化内存管理器
	if config.Memory != nil {
		memoryManager, err := NewMemoryManager(config.Memory)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create memory manager: %v", err)
		}
		rm.memoryManager = memoryManager
	}

	return rm, nil
}

// Start 启动资源管理器
func (rm *ResourceManager) Start() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.isRunning {
		return fmt.Errorf("resource manager is already running")
	}

	rm.logger.Info("Starting resource manager", zap.String("policy", string(rm.config.Policy)))

	// 启动CPU管理器
	if rm.cpuManager != nil {
		if err := rm.cpuManager.Start(); err != nil {
			return fmt.Errorf("failed to start CPU manager: %v", err)
		}
	}

	// 启动内存管理器
	if rm.memoryManager != nil {
		if err := rm.memoryManager.Start(); err != nil {
			// 如果内存管理器启动失败，停止CPU管理器
			if rm.cpuManager != nil {
				rm.cpuManager.Stop()
			}
			return fmt.Errorf("failed to start memory manager: %v", err)
		}
	}

	rm.isRunning = true

	// 启动监控任务
	go rm.monitoringWorker()
	go rm.eventForwarder()

	// 发送启动事件
	rm.sendEvent(&ResourceEvent{
		Type:      EventTypeResourceManagerStarted,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Source:    "ResourceManager",
		Data: map[string]interface{}{
			"policy":      string(rm.config.Policy),
			"qos_enabled": rm.config.QoSEnabled,
		},
	})

	return nil
}

// Stop 停止资源管理器
func (rm *ResourceManager) Stop() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.isRunning {
		return nil
	}

	rm.logger.Info("Stopping resource manager")
	rm.isRunning = false
	rm.cancel()

	// 释放所有资源分配
	for containerID := range rm.allocations {
		if err := rm.releaseResourcesInternal(containerID); err != nil {
			rm.logger.Error("Failed to release resources during shutdown",
				zap.String("container_id", containerID),
				zap.Error(err))
		}
	}

	// 停止子管理器
	if rm.memoryManager != nil {
		if err := rm.memoryManager.Stop(); err != nil {
			rm.logger.Error("Failed to stop memory manager", zap.Error(err))
		}
	}

	if rm.cpuManager != nil {
		if err := rm.cpuManager.Stop(); err != nil {
			rm.logger.Error("Failed to stop CPU manager", zap.Error(err))
		}
	}

	// 发送停止事件
	rm.sendEvent(&ResourceEvent{
		Type:      EventTypeResourceManagerStopped,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Source:    "ResourceManager",
		Data:      map[string]interface{}{},
	})

	close(rm.eventChan)
	return nil
}

// AllocateResources 为容器分配资源
func (rm *ResourceManager) AllocateResources(containerID string, request *common.ResourceSpec) (*ResourceAllocation, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.isRunning {
		return nil, fmt.Errorf("resource manager is not running")
	}

	// 检查是否已经分配
	if existing, exists := rm.allocations[containerID]; exists {
		return existing, nil
	}

	// 创建资源分配对象
	allocation := &ResourceAllocation{
		ContainerID: containerID,
		Request:     request,
		Timestamp:   time.Now(),
		Status:      AllocationStatusPending,
	}

	// 确定QoS类别
	if rm.config.QoSEnabled {
		allocation.QoSClass = rm.determineQoSClass(request)
		allocation.Priority = rm.calculatePriority(allocation.QoSClass)
	}

	// 分配CPU资源
	if rm.cpuManager != nil && request.CPU > 0 {
		cpuAllocation, err := rm.cpuManager.AllocateCPUs(containerID, request)
		if err != nil {
			allocation.Status = AllocationStatusFailed
			rm.sendAllocationFailedEvent(containerID, "CPU", err)
			return nil, fmt.Errorf("failed to allocate CPU: %v", err)
		}
		allocation.CPUAllocation = cpuAllocation
	}

	// 分配内存资源
	if rm.memoryManager != nil && request.Memory > 0 {
		memoryAllocation, err := rm.memoryManager.AllocateMemory(containerID, request)
		if err != nil {
			// 如果内存分配失败，回滚CPU分配
			if rm.cpuManager != nil && allocation.CPUAllocation != nil {
				rm.cpuManager.ReleaseCPUs(containerID)
			}
			allocation.Status = AllocationStatusFailed
			rm.sendAllocationFailedEvent(containerID, "Memory", err)
			return nil, fmt.Errorf("failed to allocate memory: %v", err)
		}
		allocation.MemoryAllocation = memoryAllocation
	}

	// 标记分配成功
	allocation.Status = AllocationStatusAllocated
	rm.allocations[containerID] = allocation

	// 更新统计信息
	rm.updateStats()

	// 发送分配事件
	rm.sendEvent(&ResourceEvent{
		Type:      EventTypeResourceAllocated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Source:    "ResourceManager",
		Data: map[string]interface{}{
			"container_id":   containerID,
			"cpu_request":    request.CPU,
			"memory_request": request.Memory,
			"qos_class":      allocation.QoSClass,
		},
	})

	rm.logger.Info("Resources allocated",
		zap.String("container_id", containerID),
		zap.Float64("cpu_request", request.CPU),
		zap.Int64("memory_request", request.Memory),
		zap.String("qos_class", allocation.QoSClass))

	return allocation, nil
}

// ReleaseResources 释放容器的资源
func (rm *ResourceManager) ReleaseResources(containerID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	return rm.releaseResourcesInternal(containerID)
}

// releaseResourcesInternal 内部释放资源方法（不加锁）
func (rm *ResourceManager) releaseResourcesInternal(containerID string) error {
	allocation, exists := rm.allocations[containerID]
	if !exists {
		return fmt.Errorf("container %s has no resource allocation", containerID)
	}

	var errors []string

	// 释放CPU资源
	if rm.cpuManager != nil && allocation.CPUAllocation != nil {
		if err := rm.cpuManager.ReleaseCPUs(containerID); err != nil {
			errors = append(errors, fmt.Sprintf("CPU: %v", err))
		}
	}

	// 释放内存资源
	if rm.memoryManager != nil && allocation.MemoryAllocation != nil {
		if err := rm.memoryManager.ReleaseMemory(containerID); err != nil {
			errors = append(errors, fmt.Sprintf("Memory: %v", err))
		}
	}

	// 更新分配状态
	allocation.Status = AllocationStatusReleased
	delete(rm.allocations, containerID)

	// 更新统计信息
	rm.updateStats()

	// 发送释放事件
	rm.sendEvent(&ResourceEvent{
		Type:      EventTypeResourceDeallocated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Source:    "ResourceManager",
		Data: map[string]interface{}{
			"container_id": containerID,
		},
	})

	rm.logger.Info("Resources released", zap.String("container_id", containerID))

	if len(errors) > 0 {
		return fmt.Errorf("partial release failure: %v", errors)
	}

	return nil
}

// GetResourceAllocation 获取容器的资源分配信息
func (rm *ResourceManager) GetResourceAllocation(containerID string) (*ResourceAllocation, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	allocation, exists := rm.allocations[containerID]
	if !exists {
		return nil, fmt.Errorf("container %s has no resource allocation", containerID)
	}

	// 返回副本
	return &ResourceAllocation{
		ContainerID:      allocation.ContainerID,
		Request:          allocation.Request,
		CPUAllocation:    allocation.CPUAllocation,
		MemoryAllocation: allocation.MemoryAllocation,
		QoSClass:         allocation.QoSClass,
		Priority:         allocation.Priority,
		Timestamp:        allocation.Timestamp,
		Status:           allocation.Status,
	}, nil
}

// GetStats 获取统计信息
func (rm *ResourceManager) GetStats() *ResourceManagerStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// 返回副本
	stats := &ResourceManagerStats{
		TotalAllocations:  rm.stats.TotalAllocations,
		ActiveContainers:  rm.stats.ActiveContainers,
		FailedAllocations: rm.stats.FailedAllocations,
		LastUpdated:       rm.stats.LastUpdated,
		ResourceUtilization: &ResourceUtilization{
			CPUUtilization:     rm.stats.ResourceUtilization.CPUUtilization,
			MemoryUtilization:  rm.stats.ResourceUtilization.MemoryUtilization,
			OverallUtilization: rm.stats.ResourceUtilization.OverallUtilization,
		},
		QoSStats: &QoSStats{
			GuaranteedContainers: rm.stats.QoSStats.GuaranteedContainers,
			BurstableContainers:  rm.stats.QoSStats.BurstableContainers,
			BestEffortContainers: rm.stats.QoSStats.BestEffortContainers,
			QoSViolations:        rm.stats.QoSStats.QoSViolations,
		},
	}

	// 获取子管理器统计信息
	if rm.cpuManager != nil {
		stats.CPU = rm.cpuManager.GetStats()
	}

	if rm.memoryManager != nil {
		stats.Memory = rm.memoryManager.GetStats()
	}

	return stats
}

// GetEvents 获取事件通道
func (rm *ResourceManager) GetEvents() <-chan *ResourceEvent {
	return rm.eventChan
}

// GetCPUManager 获取CPU管理器
func (rm *ResourceManager) GetCPUManager() *CPUManager {
	return rm.cpuManager
}

// GetMemoryManager 获取内存管理器
func (rm *ResourceManager) GetMemoryManager() *MemoryManager {
	return rm.memoryManager
}

// UpdateResourceLimits 更新容器的资源限制
func (rm *ResourceManager) UpdateResourceLimits(containerID string, newSpec *common.ResourceSpec) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	_, exists := rm.allocations[containerID]
	if !exists {
		return fmt.Errorf("container %s has no resource allocation", containerID)
	}

	// 目前简化实现，重新分配资源
	if err := rm.releaseResourcesInternal(containerID); err != nil {
		return fmt.Errorf("failed to release existing resources: %v", err)
	}

	_, err := rm.AllocateResources(containerID, newSpec)
	return err
}

// determineQoSClass 确定QoS类别
func (rm *ResourceManager) determineQoSClass(request *common.ResourceSpec) string {
	hasLimits := request.CPULimit > 0 || request.MemoryLimit > 0
	hasRequests := request.CPU > 0 || request.Memory > 0

	if hasRequests && hasLimits {
		// 检查requests是否等于limits
		cpuEqual := (request.CPU == 0 && request.CPULimit == 0) || request.CPU == request.CPULimit
		memoryEqual := (request.Memory == 0 && request.MemoryLimit == 0) || request.Memory == request.MemoryLimit

		if cpuEqual && memoryEqual {
			return "Guaranteed"
		}
		return "Burstable"
	} else if hasRequests {
		return "Burstable"
	} else {
		return "BestEffort"
	}
}

// calculatePriority 计算优先级
func (rm *ResourceManager) calculatePriority(qosClass string) int {
	switch qosClass {
	case "Guaranteed":
		return 1000
	case "Burstable":
		return 500
	default: // BestEffort
		return 100
	}
}

// updateStats 更新统计信息
func (rm *ResourceManager) updateStats() {
	rm.stats.TotalAllocations = len(rm.allocations)
	rm.stats.ActiveContainers = len(rm.allocations)
	rm.stats.LastUpdated = time.Now()

	// 统计QoS类别
	rm.stats.QoSStats.GuaranteedContainers = 0
	rm.stats.QoSStats.BurstableContainers = 0
	rm.stats.QoSStats.BestEffortContainers = 0

	for _, allocation := range rm.allocations {
		switch allocation.QoSClass {
		case "Guaranteed":
			rm.stats.QoSStats.GuaranteedContainers++
		case "Burstable":
			rm.stats.QoSStats.BurstableContainers++
		case "BestEffort":
			rm.stats.QoSStats.BestEffortContainers++
		}
	}

	// 计算资源利用率
	if rm.cpuManager != nil {
		cpuStats := rm.cpuManager.GetStats()
		rm.stats.ResourceUtilization.CPUUtilization = cpuStats.CPUUtilization
	}

	if rm.memoryManager != nil {
		memoryStats := rm.memoryManager.GetStats()
		if memoryStats.TotalMemory > 0 {
			rm.stats.ResourceUtilization.MemoryUtilization =
				float64(memoryStats.AllocatedMemory) / float64(memoryStats.TotalMemory) * 100
		}
	}

	// 计算整体利用率
	rm.stats.ResourceUtilization.OverallUtilization =
		(rm.stats.ResourceUtilization.CPUUtilization + rm.stats.ResourceUtilization.MemoryUtilization) / 2
}

// monitoringWorker 监控工作线程
func (rm *ResourceManager) monitoringWorker() {
	ticker := time.NewTicker(rm.config.MonitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.performMonitoring()
		case <-rm.ctx.Done():
			return
		}
	}
}

// performMonitoring 执行监控
func (rm *ResourceManager) performMonitoring() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 更新统计信息
	rm.updateStats()

	// 检查资源压力
	rm.checkResourcePressure()

	// 检查QoS违规
	if rm.config.QoSEnabled {
		rm.checkQoSViolations()
	}

	// 发送监控事件
	rm.sendEvent(&ResourceEvent{
		Type:      "RESOURCE_MONITORING",
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Source:    "ResourceManager",
		Data: map[string]interface{}{
			"active_containers":   rm.stats.ActiveContainers,
			"cpu_utilization":     rm.stats.ResourceUtilization.CPUUtilization,
			"memory_utilization":  rm.stats.ResourceUtilization.MemoryUtilization,
			"overall_utilization": rm.stats.ResourceUtilization.OverallUtilization,
		},
	})
}

// checkResourcePressure 检查资源压力
func (rm *ResourceManager) checkResourcePressure() {
	cpuPressure := rm.stats.ResourceUtilization.CPUUtilization > 80.0
	memoryPressure := rm.stats.ResourceUtilization.MemoryUtilization > 85.0

	if cpuPressure || memoryPressure {
		severity := EventSeverityWarning
		if rm.stats.ResourceUtilization.OverallUtilization > 90.0 {
			severity = EventSeverityCritical
		}

		rm.sendEvent(&ResourceEvent{
			Type:      EventTypeResourcePressure,
			Timestamp: time.Now(),
			Severity:  severity,
			Source:    "ResourceManager",
			Data: map[string]interface{}{
				"cpu_pressure":       cpuPressure,
				"memory_pressure":    memoryPressure,
				"cpu_utilization":    rm.stats.ResourceUtilization.CPUUtilization,
				"memory_utilization": rm.stats.ResourceUtilization.MemoryUtilization,
			},
		})
	}
}

// checkQoSViolations 检查QoS违规
func (rm *ResourceManager) checkQoSViolations() {
	for containerID, allocation := range rm.allocations {
		if allocation.QoSClass == "Guaranteed" {
			// 检查保证类型容器的资源使用
			violation := rm.checkGuaranteedQoS(containerID, allocation)
			if violation {
				rm.stats.QoSStats.QoSViolations++
				rm.sendEvent(&ResourceEvent{
					Type:      EventTypeQoSViolation,
					Timestamp: time.Now(),
					Severity:  EventSeverityError,
					Source:    "ResourceManager",
					Data: map[string]interface{}{
						"container_id":   containerID,
						"qos_class":      allocation.QoSClass,
						"violation_type": "guaranteed_resources_not_available",
					},
				})
			}
		}
	}
}

// checkGuaranteedQoS 检查保证类型QoS
func (rm *ResourceManager) checkGuaranteedQoS(containerID string, allocation *ResourceAllocation) bool {
	// 简化实现，检查资源是否仍然可用
	if allocation.CPUAllocation != nil && rm.cpuManager != nil {
		currentAllocation, err := rm.cpuManager.GetCPUAllocation(containerID)
		if err != nil || len(currentAllocation.AllocatedCPUs) == 0 {
			return true
		}
	}

	if allocation.MemoryAllocation != nil && rm.memoryManager != nil {
		currentAllocation, err := rm.memoryManager.GetMemoryAllocation(containerID)
		if err != nil || currentAllocation.MemoryLimit <= 0 {
			return true
		}
	}

	return false
}

// eventForwarder 事件转发器
func (rm *ResourceManager) eventForwarder() {
	var cpuEvents <-chan *CPUEvent
	var memoryEvents <-chan *MemoryEvent

	if rm.cpuManager != nil {
		cpuEvents = rm.cpuManager.GetEvents()
	}

	if rm.memoryManager != nil {
		memoryEvents = rm.memoryManager.GetEvents()
	}

	for {
		select {
		case event := <-cpuEvents:
			if event != nil {
				rm.sendEvent(&ResourceEvent{
					Type:      event.Type,
					Timestamp: event.Timestamp,
					Severity:  event.Severity,
					Source:    "CPUManager",
					Data:      event.Data,
				})
			}

		case event := <-memoryEvents:
			if event != nil {
				rm.sendEvent(&ResourceEvent{
					Type:      event.Type,
					Timestamp: event.Timestamp,
					Severity:  event.Severity,
					Source:    "MemoryManager",
					Data:      event.Data,
				})
			}

		case <-rm.ctx.Done():
			return
		}
	}
}

// sendAllocationFailedEvent 发送分配失败事件
func (rm *ResourceManager) sendAllocationFailedEvent(containerID, resourceType string, err error) {
	rm.sendEvent(&ResourceEvent{
		Type:      EventTypeResourceAllocationFailed,
		Timestamp: time.Now(),
		Severity:  EventSeverityError,
		Source:    "ResourceManager",
		Data: map[string]interface{}{
			"container_id":  containerID,
			"resource_type": resourceType,
			"error_message": err.Error(),
		},
	})
}

// sendEvent 发送事件
func (rm *ResourceManager) sendEvent(event *ResourceEvent) {
	select {
	case rm.eventChan <- event:
	default:
		rm.logger.Warn("Resource event channel full, dropping event",
			zap.String("event_type", event.Type),
			zap.String("source", event.Source))
	}
}
