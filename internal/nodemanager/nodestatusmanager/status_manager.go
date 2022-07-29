package nodestatusmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// StatusManager 状态管理器
type StatusManager struct {
	mu     sync.RWMutex
	config *StatusManagerConfig
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// 心跳管理器
	heartbeatManager *HeartbeatManager

	// 状态信息
	nodeStatus        *NodeStatus
	resourceUsage     *ResourceUsage
	containerStatuses map[string]*ContainerStatus
	nodeHealth        *NodeHealth

	// 状态提供者
	resourceProvider  ResourceStatusProvider
	containerProvider ContainerStatusProvider
	healthProvider    HealthStatusProvider

	// 监控状态
	isRunning bool
	eventChan chan *StatusEvent

	// 统计信息
	stats *StatusManagerStats

	// 更新通知
	updateNotifiers []StatusUpdateNotifier
}

// StatusManagerConfig 状态管理器配置
type StatusManagerConfig struct {
	NodeID              string            `json:"node_id"`
	UpdateInterval      time.Duration     `json:"update_interval"`
	HealthCheckInterval time.Duration     `json:"health_check_interval"`
	StatusCacheSize     int               `json:"status_cache_size"`
	EnableHealthChecks  bool              `json:"enable_health_checks"`
	HeartbeatConfig     *HeartbeatConfig  `json:"heartbeat_config"`
	ResourceManagerAddr string            `json:"resource_manager_addr"`
	NodeLabels          map[string]string `json:"node_labels"`
	NodeAttributes      map[string]string `json:"node_attributes"`
}

// ResourceStatusProvider 资源状态提供者接口
type ResourceStatusProvider interface {
	GetTotalResources() *common.ResourceSpec
	GetAllocatedResources() *common.ResourceSpec
	GetAvailableResources() *common.ResourceSpec
	GetResourceUsage() *ResourceUsage
}

// ContainerStatusProvider 容器状态提供者接口
type ContainerStatusProvider interface {
	GetContainerStatuses() map[string]*ContainerStatus
	GetContainerCount() int
	GetRunningContainerCount() int
}

// HealthStatusProvider 健康状态提供者接口
type HealthStatusProvider interface {
	PerformHealthChecks() []*HealthCheck
	GetNodeHealth() *NodeHealth
	IsHealthy() bool
}

// StatusUpdateNotifier 状态更新通知器接口
type StatusUpdateNotifier interface {
	OnStatusUpdate(status *NodeStatus)
	OnResourceUsageUpdate(usage *ResourceUsage)
	OnContainerStatusUpdate(containerID string, status *ContainerStatus)
	OnHealthStatusUpdate(health *NodeHealth)
}

// StatusEvent 状态事件
type StatusEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
}

// StatusEventType 状态事件类型
const (
	EventTypeStatusUpdated          = "STATUS_UPDATED"
	EventTypeResourceUsageUpdated   = "RESOURCE_USAGE_UPDATED"
	EventTypeContainerStatusUpdated = "CONTAINER_STATUS_UPDATED"
	EventTypeHealthStatusUpdated    = "HEALTH_STATUS_UPDATED"
	EventTypeNodeStateChanged       = "NODE_STATE_CHANGED"
	EventTypeHealthCheckFailed      = "HEALTH_CHECK_FAILED"
	EventTypeStatusManagerStarted   = "STATUS_MANAGER_STARTED"
	EventTypeStatusManagerStopped   = "STATUS_MANAGER_STOPPED"
)

// StatusManagerStats 状态管理器统计信息
type StatusManagerStats struct {
	TotalUpdates           int64           `json:"total_updates"`
	StatusUpdates          int64           `json:"status_updates"`
	ResourceUsageUpdates   int64           `json:"resource_usage_updates"`
	ContainerStatusUpdates int64           `json:"container_status_updates"`
	HealthCheckUpdates     int64           `json:"health_check_updates"`
	LastUpdate             time.Time       `json:"last_update"`
	AverageUpdateInterval  time.Duration   `json:"average_update_interval"`
	StartTime              time.Time       `json:"start_time"`
	UpTime                 time.Duration   `json:"uptime"`
	IsRunning              bool            `json:"is_running"`
	HeartbeatStats         *HeartbeatStats `json:"heartbeat_stats,omitempty"`
}

// DefaultResourceStatusProvider 默认资源状态提供者
type DefaultResourceStatusProvider struct {
	totalResources     *common.ResourceSpec
	allocatedResources *common.ResourceSpec
	mu                 sync.RWMutex
}

// DefaultContainerStatusProvider 默认容器状态提供者
type DefaultContainerStatusProvider struct {
	containerStatuses map[string]*ContainerStatus
	mu                sync.RWMutex
}

// DefaultHealthStatusProvider 默认健康状态提供者
type DefaultHealthStatusProvider struct {
	nodeHealth *NodeHealth
	mu         sync.RWMutex
}

// NewStatusManager 创建状态管理器
func NewStatusManager(config *StatusManagerConfig, rmClient ResourceManagerClient) (*StatusManager, error) {
	if config == nil {
		return nil, fmt.Errorf("status manager config cannot be nil")
	}

	// 设置默认值
	if config.UpdateInterval == 0 {
		config.UpdateInterval = 10 * time.Second
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.StatusCacheSize == 0 {
		config.StatusCacheSize = 1000
	}

	ctx, cancel := context.WithCancel(context.Background())

	sm := &StatusManager{
		config:            config,
		logger:            zap.NewNop(), // 在实际使用中应该注入正确的logger
		ctx:               ctx,
		cancel:            cancel,
		containerStatuses: make(map[string]*ContainerStatus),
		eventChan:         make(chan *StatusEvent, config.StatusCacheSize),
		updateNotifiers:   make([]StatusUpdateNotifier, 0),
		stats: &StatusManagerStats{
			StartTime: time.Now(),
		},
	}

	// 初始化默认状态提供者
	sm.resourceProvider = &DefaultResourceStatusProvider{
		totalResources:     &common.ResourceSpec{},
		allocatedResources: &common.ResourceSpec{},
	}
	sm.containerProvider = &DefaultContainerStatusProvider{
		containerStatuses: make(map[string]*ContainerStatus),
	}
	sm.healthProvider = &DefaultHealthStatusProvider{
		nodeHealth: &NodeHealth{
			Healthy:         true,
			LastHealthCheck: time.Now(),
			HealthReport:    "Node is healthy",
			DiskHealthy:     true,
			MemoryHealthy:   true,
			CPUHealthy:      true,
			NetworkHealthy:  true,
			HealthChecks:    make([]*HealthCheck, 0),
		},
	}

	// 初始化节点状态
	sm.initializeNodeStatus()

	// 创建心跳管理器
	if config.HeartbeatConfig != nil && rmClient != nil {
		heartbeatManager, err := NewHeartbeatManager(config.HeartbeatConfig, rmClient, sm)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create heartbeat manager: %v", err)
		}
		sm.heartbeatManager = heartbeatManager
	}

	return sm, nil
}

// Start 启动状态管理器
func (sm *StatusManager) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.isRunning {
		return fmt.Errorf("status manager is already running")
	}

	sm.logger.Info("Starting status manager", zap.String("node_id", sm.config.NodeID))

	sm.isRunning = true
	sm.stats.IsRunning = true

	// 启动状态更新循环
	go sm.statusUpdateLoop()

	// 启动健康检查循环
	if sm.config.EnableHealthChecks {
		go sm.healthCheckLoop()
	}

	// 启动心跳管理器
	if sm.heartbeatManager != nil {
		if err := sm.heartbeatManager.Start(); err != nil {
			sm.isRunning = false
			sm.stats.IsRunning = false
			return fmt.Errorf("failed to start heartbeat manager: %v", err)
		}
	}

	// 发送启动事件
	sm.sendEvent(&StatusEvent{
		Type:      EventTypeStatusManagerStarted,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"node_id":         sm.config.NodeID,
			"update_interval": sm.config.UpdateInterval,
		},
	})

	return nil
}

// Stop 停止状态管理器
func (sm *StatusManager) Stop() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.isRunning {
		return nil
	}

	sm.logger.Info("Stopping status manager")
	sm.isRunning = false
	sm.stats.IsRunning = false
	sm.cancel()

	// 停止心跳管理器
	if sm.heartbeatManager != nil {
		if err := sm.heartbeatManager.Stop(); err != nil {
			sm.logger.Error("Failed to stop heartbeat manager", zap.Error(err))
		}
	}

	// 发送停止事件
	sm.sendEvent(&StatusEvent{
		Type:      EventTypeStatusManagerStopped,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"node_id": sm.config.NodeID,
		},
	})

	close(sm.eventChan)
	return nil
}

// GetNodeStatus 获取节点状态（实现NodeStatusProvider接口）
func (sm *StatusManager) GetNodeStatus() *NodeStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 返回副本
	return &NodeStatus{
		NodeID:             sm.nodeStatus.NodeID,
		State:              sm.nodeStatus.State,
		LastUpdate:         sm.nodeStatus.LastUpdate,
		RunningContainers:  sm.nodeStatus.RunningContainers,
		AllocatedResources: sm.nodeStatus.AllocatedResources,
		AvailableResources: sm.nodeStatus.AvailableResources,
		NodeLabels:         copyStringMap(sm.nodeStatus.NodeLabels),
		NodeAttributes:     copyStringMap(sm.nodeStatus.NodeAttributes),
	}
}

// GetResourceUsage 获取资源使用情况（实现NodeStatusProvider接口）
func (sm *StatusManager) GetResourceUsage() *ResourceUsage {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.resourceUsage == nil {
		return &ResourceUsage{Timestamp: time.Now()}
	}

	// 返回副本
	return &ResourceUsage{
		CPU:               sm.resourceUsage.CPU,
		Memory:            sm.resourceUsage.Memory,
		Disk:              sm.resourceUsage.Disk,
		Network:           sm.resourceUsage.Network,
		CPUUtilization:    sm.resourceUsage.CPUUtilization,
		MemoryUtilization: sm.resourceUsage.MemoryUtilization,
		DiskUtilization:   sm.resourceUsage.DiskUtilization,
		Timestamp:         sm.resourceUsage.Timestamp,
	}
}

// GetContainerStatuses 获取容器状态（实现NodeStatusProvider接口）
func (sm *StatusManager) GetContainerStatuses() map[string]*ContainerStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 返回副本
	result := make(map[string]*ContainerStatus)
	for id, status := range sm.containerStatuses {
		result[id] = &ContainerStatus{
			ContainerID:   status.ContainerID,
			ApplicationID: status.ApplicationID,
			State:         status.State,
			ExitCode:      status.ExitCode,
			Diagnostics:   status.Diagnostics,
			UsedResources: status.UsedResources,
			Progress:      status.Progress,
			LastUpdate:    status.LastUpdate,
		}
	}
	return result
}

// GetNodeHealth 获取节点健康状态（实现NodeStatusProvider接口）
func (sm *StatusManager) GetNodeHealth() *NodeHealth {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.nodeHealth == nil {
		return &NodeHealth{
			Healthy:         true,
			LastHealthCheck: time.Now(),
			HealthReport:    "Node is healthy",
		}
	}

	// 返回副本
	healthChecks := make([]*HealthCheck, len(sm.nodeHealth.HealthChecks))
	for i, check := range sm.nodeHealth.HealthChecks {
		healthChecks[i] = &HealthCheck{
			Name:      check.Name,
			Status:    check.Status,
			Message:   check.Message,
			Timestamp: check.Timestamp,
			Duration:  check.Duration,
		}
	}

	return &NodeHealth{
		Healthy:         sm.nodeHealth.Healthy,
		LastHealthCheck: sm.nodeHealth.LastHealthCheck,
		HealthReport:    sm.nodeHealth.HealthReport,
		DiskHealthy:     sm.nodeHealth.DiskHealthy,
		MemoryHealthy:   sm.nodeHealth.MemoryHealthy,
		CPUHealthy:      sm.nodeHealth.CPUHealthy,
		NetworkHealthy:  sm.nodeHealth.NetworkHealthy,
		HealthChecks:    healthChecks,
	}
}

// GetStats 获取统计信息
func (sm *StatusManager) GetStats() *StatusManagerStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := &StatusManagerStats{
		TotalUpdates:           sm.stats.TotalUpdates,
		StatusUpdates:          sm.stats.StatusUpdates,
		ResourceUsageUpdates:   sm.stats.ResourceUsageUpdates,
		ContainerStatusUpdates: sm.stats.ContainerStatusUpdates,
		HealthCheckUpdates:     sm.stats.HealthCheckUpdates,
		LastUpdate:             sm.stats.LastUpdate,
		AverageUpdateInterval:  sm.stats.AverageUpdateInterval,
		StartTime:              sm.stats.StartTime,
		IsRunning:              sm.stats.IsRunning,
	}

	stats.UpTime = time.Since(sm.stats.StartTime)

	if sm.heartbeatManager != nil {
		stats.HeartbeatStats = sm.heartbeatManager.GetStats()
	}

	return stats
}

// GetEvents 获取事件通道
func (sm *StatusManager) GetEvents() <-chan *StatusEvent {
	return sm.eventChan
}

// SetResourceProvider 设置资源状态提供者
func (sm *StatusManager) SetResourceProvider(provider ResourceStatusProvider) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.resourceProvider = provider
}

// SetContainerProvider 设置容器状态提供者
func (sm *StatusManager) SetContainerProvider(provider ContainerStatusProvider) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.containerProvider = provider
}

// SetHealthProvider 设置健康状态提供者
func (sm *StatusManager) SetHealthProvider(provider HealthStatusProvider) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.healthProvider = provider
}

// AddStatusUpdateNotifier 添加状态更新通知器
func (sm *StatusManager) AddStatusUpdateNotifier(notifier StatusUpdateNotifier) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.updateNotifiers = append(sm.updateNotifiers, notifier)
}

// UpdateContainerStatus 更新容器状态
func (sm *StatusManager) UpdateContainerStatus(containerID string, status *ContainerStatus) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldStatus, exists := sm.containerStatuses[containerID]
	sm.containerStatuses[containerID] = status

	// 更新统计信息
	sm.stats.ContainerStatusUpdates++
	sm.stats.TotalUpdates++
	sm.stats.LastUpdate = time.Now()

	// 更新节点状态中的运行容器数
	sm.updateRunningContainerCount()

	// 通知状态更新
	for _, notifier := range sm.updateNotifiers {
		notifier.OnContainerStatusUpdate(containerID, status)
	}

	// 发送事件
	eventData := map[string]interface{}{
		"container_id": containerID,
		"new_state":    string(status.State),
	}

	if exists {
		eventData["old_state"] = string(oldStatus.State)
	}

	sm.sendEvent(&StatusEvent{
		Type:      EventTypeContainerStatusUpdated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data:      eventData,
	})

	sm.logger.Debug("Container status updated",
		zap.String("container_id", containerID),
		zap.String("state", string(status.State)))
}

// UpdateNodeState 更新节点状态
func (sm *StatusManager) UpdateNodeState(newState NodeState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldState := sm.nodeStatus.State
	if oldState == newState {
		return
	}

	sm.nodeStatus.State = newState
	sm.nodeStatus.LastUpdate = time.Now()

	// 发送状态变更事件
	sm.sendEvent(&StatusEvent{
		Type:      EventTypeNodeStateChanged,
		Timestamp: time.Now(),
		Severity:  sm.getStateChangeSeverity(oldState, newState),
		Data: map[string]interface{}{
			"node_id":   sm.config.NodeID,
			"old_state": string(oldState),
			"new_state": string(newState),
		},
	})

	sm.logger.Info("Node state changed",
		zap.String("node_id", sm.config.NodeID),
		zap.String("old_state", string(oldState)),
		zap.String("new_state", string(newState)))
}

// initializeNodeStatus 初始化节点状态
func (sm *StatusManager) initializeNodeStatus() {
	sm.nodeStatus = &NodeStatus{
		NodeID:             sm.config.NodeID,
		State:              NodeStateNew,
		LastUpdate:         time.Now(),
		RunningContainers:  0,
		AllocatedResources: &common.ResourceSpec{},
		AvailableResources: &common.ResourceSpec{},
		NodeLabels:         copyStringMap(sm.config.NodeLabels),
		NodeAttributes:     copyStringMap(sm.config.NodeAttributes),
	}

	// 如果没有设置标签，添加默认标签
	if sm.nodeStatus.NodeLabels == nil {
		sm.nodeStatus.NodeLabels = make(map[string]string)
	}
	if sm.nodeStatus.NodeAttributes == nil {
		sm.nodeStatus.NodeAttributes = make(map[string]string)
	}
}

// statusUpdateLoop 状态更新循环
func (sm *StatusManager) statusUpdateLoop() {
	ticker := time.NewTicker(sm.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.updateStatus()
		case <-sm.ctx.Done():
			return
		}
	}
}

// updateStatus 更新状态
func (sm *StatusManager) updateStatus() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 更新资源使用情况
	if sm.resourceProvider != nil {
		sm.resourceUsage = sm.resourceProvider.GetResourceUsage()
		sm.nodeStatus.AllocatedResources = sm.resourceProvider.GetAllocatedResources()
		sm.nodeStatus.AvailableResources = sm.resourceProvider.GetAvailableResources()

		sm.stats.ResourceUsageUpdates++

		// 通知资源使用更新
		for _, notifier := range sm.updateNotifiers {
			notifier.OnResourceUsageUpdate(sm.resourceUsage)
		}

		sm.sendEvent(&StatusEvent{
			Type:      EventTypeResourceUsageUpdated,
			Timestamp: time.Now(),
			Severity:  EventSeverityInfo,
			Data: map[string]interface{}{
				"cpu_utilization":    sm.resourceUsage.CPUUtilization,
				"memory_utilization": sm.resourceUsage.MemoryUtilization,
				"disk_utilization":   sm.resourceUsage.DiskUtilization,
			},
		})
	}

	// 更新容器状态
	if sm.containerProvider != nil {
		newContainerStatuses := sm.containerProvider.GetContainerStatuses()

		// 检查状态变化
		for containerID, newStatus := range newContainerStatuses {
			oldStatus, exists := sm.containerStatuses[containerID]
			if !exists || oldStatus.State != newStatus.State {
				sm.containerStatuses[containerID] = newStatus

				// 通知容器状态更新
				for _, notifier := range sm.updateNotifiers {
					notifier.OnContainerStatusUpdate(containerID, newStatus)
				}
			} else {
				// 更新其他字段但状态未变
				sm.containerStatuses[containerID] = newStatus
			}
		}

		// 删除已不存在的容器
		for containerID := range sm.containerStatuses {
			if _, exists := newContainerStatuses[containerID]; !exists {
				delete(sm.containerStatuses, containerID)
			}
		}

		sm.updateRunningContainerCount()
	}

	// 更新节点状态
	sm.nodeStatus.LastUpdate = time.Now()
	sm.stats.StatusUpdates++
	sm.stats.TotalUpdates++
	sm.stats.LastUpdate = time.Now()

	// 通知节点状态更新
	for _, notifier := range sm.updateNotifiers {
		notifier.OnStatusUpdate(sm.nodeStatus)
	}

	sm.sendEvent(&StatusEvent{
		Type:      EventTypeStatusUpdated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"node_id":            sm.config.NodeID,
			"running_containers": sm.nodeStatus.RunningContainers,
		},
	})
}

// healthCheckLoop 健康检查循环
func (sm *StatusManager) healthCheckLoop() {
	ticker := time.NewTicker(sm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.performHealthCheck()
		case <-sm.ctx.Done():
			return
		}
	}
}

// performHealthCheck 执行健康检查
func (sm *StatusManager) performHealthCheck() {
	if sm.healthProvider == nil {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldHealth := sm.nodeHealth
	sm.nodeHealth = sm.healthProvider.GetNodeHealth()

	sm.stats.HealthCheckUpdates++
	sm.stats.TotalUpdates++
	sm.stats.LastUpdate = time.Now()

	// 检查健康状态变化
	if oldHealth == nil || oldHealth.Healthy != sm.nodeHealth.Healthy {
		severity := EventSeverityInfo
		if !sm.nodeHealth.Healthy {
			severity = EventSeverityError
		}

		sm.sendEvent(&StatusEvent{
			Type:      EventTypeHealthStatusUpdated,
			Timestamp: time.Now(),
			Severity:  severity,
			Data: map[string]interface{}{
				"node_id":       sm.config.NodeID,
				"healthy":       sm.nodeHealth.Healthy,
				"health_report": sm.nodeHealth.HealthReport,
			},
		})

		// 更新节点状态
		if !sm.nodeHealth.Healthy && sm.nodeStatus.State == NodeStateRunning {
			sm.nodeStatus.State = NodeStateUnhealthy
		} else if sm.nodeHealth.Healthy && sm.nodeStatus.State == NodeStateUnhealthy {
			sm.nodeStatus.State = NodeStateRunning
		}
	}

	// 检查个别健康检查失败
	for _, check := range sm.nodeHealth.HealthChecks {
		if check.Status == HealthStatusUnhealthy {
			sm.sendEvent(&StatusEvent{
				Type:      EventTypeHealthCheckFailed,
				Timestamp: time.Now(),
				Severity:  EventSeverityWarning,
				Data: map[string]interface{}{
					"node_id":    sm.config.NodeID,
					"check_name": check.Name,
					"message":    check.Message,
				},
			})
		}
	}

	// 通知健康状态更新
	for _, notifier := range sm.updateNotifiers {
		notifier.OnHealthStatusUpdate(sm.nodeHealth)
	}
}

// updateRunningContainerCount 更新运行中的容器数量
func (sm *StatusManager) updateRunningContainerCount() {
	count := 0
	for _, status := range sm.containerStatuses {
		if status.State == ContainerStateRunning {
			count++
		}
	}
	sm.nodeStatus.RunningContainers = count
}

// getStateChangeSeverity 获取状态变更的严重级别
func (sm *StatusManager) getStateChangeSeverity(oldState, newState NodeState) EventSeverity {
	// 从健康状态到不健康状态
	if oldState == NodeStateRunning && newState == NodeStateUnhealthy {
		return EventSeverityWarning
	}

	// 到失联状态
	if newState == NodeStateLost {
		return EventSeverityError
	}

	// 到关闭状态
	if newState == NodeStateShutdown || newState == NodeStateDecommissioned {
		return EventSeverityInfo
	}

	// 其他状态变更
	return EventSeverityInfo
}

// sendEvent 发送事件
func (sm *StatusManager) sendEvent(event *StatusEvent) {
	select {
	case sm.eventChan <- event:
	default:
		sm.logger.Warn("Status event channel full, dropping event",
			zap.String("event_type", event.Type))
	}
}

// copyStringMap 复制字符串映射
func copyStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}

	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// 默认状态提供者实现

// DefaultResourceStatusProvider 方法

// GetTotalResources 获取总资源
func (p *DefaultResourceStatusProvider) GetTotalResources() *common.ResourceSpec {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &common.ResourceSpec{
		CPU:    p.totalResources.CPU,
		Memory: p.totalResources.Memory,
	}
}

// GetAllocatedResources 获取已分配资源
func (p *DefaultResourceStatusProvider) GetAllocatedResources() *common.ResourceSpec {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &common.ResourceSpec{
		CPU:    p.allocatedResources.CPU,
		Memory: p.allocatedResources.Memory,
	}
}

// GetAvailableResources 获取可用资源
func (p *DefaultResourceStatusProvider) GetAvailableResources() *common.ResourceSpec {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &common.ResourceSpec{
		CPU:    p.totalResources.CPU - p.allocatedResources.CPU,
		Memory: p.totalResources.Memory - p.allocatedResources.Memory,
	}
}

// GetResourceUsage 获取资源使用情况
func (p *DefaultResourceStatusProvider) GetResourceUsage() *ResourceUsage {
	return &ResourceUsage{
		CPU:               p.allocatedResources.CPU,
		Memory:            p.allocatedResources.Memory,
		CPUUtilization:    50.0, // 模拟数据
		MemoryUtilization: 60.0, // 模拟数据
		Timestamp:         time.Now(),
	}
}

// DefaultContainerStatusProvider 方法

// GetContainerStatuses 获取容器状态
func (p *DefaultContainerStatusProvider) GetContainerStatuses() map[string]*ContainerStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*ContainerStatus)
	for id, status := range p.containerStatuses {
		result[id] = &ContainerStatus{
			ContainerID:   status.ContainerID,
			ApplicationID: status.ApplicationID,
			State:         status.State,
			ExitCode:      status.ExitCode,
			Diagnostics:   status.Diagnostics,
			UsedResources: status.UsedResources,
			Progress:      status.Progress,
			LastUpdate:    status.LastUpdate,
		}
	}
	return result
}

// GetContainerCount 获取容器总数
func (p *DefaultContainerStatusProvider) GetContainerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.containerStatuses)
}

// GetRunningContainerCount 获取运行中容器数
func (p *DefaultContainerStatusProvider) GetRunningContainerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, status := range p.containerStatuses {
		if status.State == ContainerStateRunning {
			count++
		}
	}
	return count
}

// DefaultHealthStatusProvider 方法

// PerformHealthChecks 执行健康检查
func (p *DefaultHealthStatusProvider) PerformHealthChecks() []*HealthCheck {
	checks := []*HealthCheck{
		{
			Name:      "disk_space",
			Status:    HealthStatusHealthy,
			Message:   "Disk space is sufficient",
			Timestamp: time.Now(),
			Duration:  time.Millisecond * 10,
		},
		{
			Name:      "memory",
			Status:    HealthStatusHealthy,
			Message:   "Memory usage is normal",
			Timestamp: time.Now(),
			Duration:  time.Millisecond * 5,
		},
		{
			Name:      "cpu",
			Status:    HealthStatusHealthy,
			Message:   "CPU usage is normal",
			Timestamp: time.Now(),
			Duration:  time.Millisecond * 8,
		},
		{
			Name:      "network",
			Status:    HealthStatusHealthy,
			Message:   "Network connectivity is good",
			Timestamp: time.Now(),
			Duration:  time.Millisecond * 15,
		},
	}

	p.mu.Lock()
	p.nodeHealth.HealthChecks = checks
	p.nodeHealth.LastHealthCheck = time.Now()

	// 更新健康状态
	allHealthy := true
	for _, check := range checks {
		if check.Status != HealthStatusHealthy {
			allHealthy = false
			break
		}
	}

	p.nodeHealth.Healthy = allHealthy
	p.nodeHealth.DiskHealthy = checks[0].Status == HealthStatusHealthy
	p.nodeHealth.MemoryHealthy = checks[1].Status == HealthStatusHealthy
	p.nodeHealth.CPUHealthy = checks[2].Status == HealthStatusHealthy
	p.nodeHealth.NetworkHealthy = checks[3].Status == HealthStatusHealthy

	if allHealthy {
		p.nodeHealth.HealthReport = "All health checks passed"
	} else {
		p.nodeHealth.HealthReport = "Some health checks failed"
	}
	p.mu.Unlock()

	return checks
}

// GetNodeHealth 获取节点健康状态
func (p *DefaultHealthStatusProvider) GetNodeHealth() *NodeHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 返回副本
	healthChecks := make([]*HealthCheck, len(p.nodeHealth.HealthChecks))
	for i, check := range p.nodeHealth.HealthChecks {
		healthChecks[i] = &HealthCheck{
			Name:      check.Name,
			Status:    check.Status,
			Message:   check.Message,
			Timestamp: check.Timestamp,
			Duration:  check.Duration,
		}
	}

	return &NodeHealth{
		Healthy:         p.nodeHealth.Healthy,
		LastHealthCheck: p.nodeHealth.LastHealthCheck,
		HealthReport:    p.nodeHealth.HealthReport,
		DiskHealthy:     p.nodeHealth.DiskHealthy,
		MemoryHealthy:   p.nodeHealth.MemoryHealthy,
		CPUHealthy:      p.nodeHealth.CPUHealthy,
		NetworkHealthy:  p.nodeHealth.NetworkHealthy,
		HealthChecks:    healthChecks,
	}
}

// IsHealthy 检查是否健康
func (p *DefaultHealthStatusProvider) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nodeHealth.Healthy
}
