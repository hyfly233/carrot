package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ContainerMonitor 容器监控器
type ContainerMonitor struct {
	mu         sync.RWMutex
	containers map[string]*ContainerMetrics
	config     *ContainerMonitorConfig
	logger     *zap.Logger
	ctx        context.Context
	cancel     context.CancelFunc

	// 监控状态
	isRunning bool

	// 事件通道
	eventChan chan *ContainerEvent

	// 监控器
	resourceCollector *ResourceCollector
	healthChecker     *HealthChecker
}

// ContainerMonitorConfig 容器监控器配置
type ContainerMonitorConfig struct {
	CollectionInterval      time.Duration    `json:"collection_interval"`
	HealthCheckInterval     time.Duration    `json:"health_check_interval"`
	MetricsRetentionTime    time.Duration    `json:"metrics_retention_time"`
	AlertThresholds         *AlertThresholds `json:"alert_thresholds"`
	EnableCgroupsMonitoring bool             `json:"enable_cgroups_monitoring"`
	CgroupsPath             string           `json:"cgroups_path"`
	EnableNetworkMonitoring bool             `json:"enable_network_monitoring"`
	EnableDiskMonitoring    bool             `json:"enable_disk_monitoring"`
}

// ContainerMetrics 容器指标
type ContainerMetrics struct {
	ContainerID string    `json:"container_id"`
	Timestamp   time.Time `json:"timestamp"`

	// CPU 指标
	CPUUsagePercent float64 `json:"cpu_usage_percent"`
	CPUUserTime     float64 `json:"cpu_user_time"`
	CPUSystemTime   float64 `json:"cpu_system_time"`
	CPUThrottled    int64   `json:"cpu_throttled"`

	// 内存指标
	MemoryUsage        int64   `json:"memory_usage"`
	MemoryLimit        int64   `json:"memory_limit"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	MemoryCache        int64   `json:"memory_cache"`
	MemorySwap         int64   `json:"memory_swap"`

	// 网络指标
	NetworkRxBytes   int64 `json:"network_rx_bytes"`
	NetworkTxBytes   int64 `json:"network_tx_bytes"`
	NetworkRxPackets int64 `json:"network_rx_packets"`
	NetworkTxPackets int64 `json:"network_tx_packets"`
	NetworkRxErrors  int64 `json:"network_rx_errors"`
	NetworkTxErrors  int64 `json:"network_tx_errors"`

	// 磁盘指标
	DiskReadBytes  int64 `json:"disk_read_bytes"`
	DiskWriteBytes int64 `json:"disk_write_bytes"`
	DiskReadOps    int64 `json:"disk_read_ops"`
	DiskWriteOps   int64 `json:"disk_write_ops"`

	// 进程指标
	PID        int `json:"pid"`
	NumThreads int `json:"num_threads"`
	NumFDs     int `json:"num_fds"`

	// 健康状态
	HealthStatus HealthStatus        `json:"health_status"`
	HealthChecks []HealthCheckResult `json:"health_checks"`
}

// HealthStatus 健康状态
type HealthStatus int

const (
	HealthStatusUnknown HealthStatus = iota
	HealthStatusHealthy
	HealthStatusUnhealthy
	HealthStatusDegraded
)

// String 返回健康状态字符串
func (hs HealthStatus) String() string {
	switch hs {
	case HealthStatusHealthy:
		return "HEALTHY"
	case HealthStatusUnhealthy:
		return "UNHEALTHY"
	case HealthStatusDegraded:
		return "DEGRADED"
	default:
		return "UNKNOWN"
	}
}

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	CheckType string        `json:"check_type"`
	Status    string        `json:"status"`
	Message   string        `json:"message"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
}

// AlertThresholds 告警阈值
type AlertThresholds struct {
	CPUUsagePercent     float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent  float64 `json:"memory_usage_percent"`
	DiskUsagePercent    float64 `json:"disk_usage_percent"`
	NetworkErrorRate    float64 `json:"network_error_rate"`
	HealthCheckFailures int     `json:"health_check_failures"`
}

// ContainerEvent 容器事件
type ContainerEvent struct {
	Type        string                 `json:"type"`
	ContainerID string                 `json:"container_id"`
	Timestamp   time.Time              `json:"timestamp"`
	Data        map[string]interface{} `json:"data"`
	Severity    EventSeverity          `json:"severity"`
}

// EventSeverity 事件严重性
type EventSeverity int

const (
	EventSeverityInfo EventSeverity = iota
	EventSeverityWarning
	EventSeverityError
	EventSeverityCritical
)

// String 返回事件严重性字符串
func (es EventSeverity) String() string {
	switch es {
	case EventSeverityInfo:
		return "INFO"
	case EventSeverityWarning:
		return "WARNING"
	case EventSeverityError:
		return "ERROR"
	case EventSeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// ContainerEventType 容器事件类型
const (
	EventTypeContainerAdded    = "CONTAINER_ADDED"
	EventTypeContainerRemoved  = "CONTAINER_REMOVED"
	EventTypeMetricsCollected  = "METRICS_COLLECTED"
	EventTypeHealthCheckFailed = "HEALTH_CHECK_FAILED"
	EventTypeResourceAlert     = "RESOURCE_ALERT"
	EventTypeContainerOOM      = "CONTAINER_OOM"
	EventTypeContainerExit     = "CONTAINER_EXIT"
)

// NewContainerMonitor 创建容器监控器
func NewContainerMonitor(config *ContainerMonitorConfig) *ContainerMonitor {
	if config == nil {
		config = &ContainerMonitorConfig{
			CollectionInterval:      10 * time.Second,
			HealthCheckInterval:     30 * time.Second,
			MetricsRetentionTime:    24 * time.Hour,
			EnableCgroupsMonitoring: true,
			CgroupsPath:             "/sys/fs/cgroup",
			EnableNetworkMonitoring: true,
			EnableDiskMonitoring:    true,
			AlertThresholds: &AlertThresholds{
				CPUUsagePercent:     80.0,
				MemoryUsagePercent:  85.0,
				DiskUsagePercent:    90.0,
				NetworkErrorRate:    5.0,
				HealthCheckFailures: 3,
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	cm := &ContainerMonitor{
		containers:        make(map[string]*ContainerMetrics),
		config:            config,
		logger:            common.ComponentLogger("container-monitor"),
		ctx:               ctx,
		cancel:            cancel,
		eventChan:         make(chan *ContainerEvent, 1000),
		resourceCollector: NewResourceCollector(config),
		healthChecker:     NewHealthChecker(config),
	}

	return cm
}

// Start 启动容器监控器
func (cm *ContainerMonitor) Start() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.isRunning {
		return fmt.Errorf("container monitor is already running")
	}

	cm.isRunning = true
	cm.logger.Info("Starting container monitor")

	// 启动后台任务
	go cm.metricsCollectionWorker()
	go cm.healthCheckWorker()
	go cm.eventProcessor()
	go cm.cleanupWorker()

	return nil
}

// Stop 停止容器监控器
func (cm *ContainerMonitor) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.isRunning {
		return nil
	}

	cm.logger.Info("Stopping container monitor")
	cm.isRunning = false
	cm.cancel()
	close(cm.eventChan)

	return nil
}

// AddContainer 添加容器监控
func (cm *ContainerMonitor) AddContainer(containerID string, pid int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.containers[containerID]; exists {
		return fmt.Errorf("container %s is already being monitored", containerID)
	}

	metrics := &ContainerMetrics{
		ContainerID:  containerID,
		PID:          pid,
		Timestamp:    time.Now(),
		HealthStatus: HealthStatusUnknown,
	}

	cm.containers[containerID] = metrics

	cm.logger.Info("Added container to monitoring",
		zap.String("container_id", containerID),
		zap.Int("pid", pid))

	// 发送事件
	cm.sendEvent(&ContainerEvent{
		Type:        EventTypeContainerAdded,
		ContainerID: containerID,
		Timestamp:   time.Now(),
		Severity:    EventSeverityInfo,
		Data: map[string]interface{}{
			"pid": pid,
		},
	})

	return nil
}

// RemoveContainer 移除容器监控
func (cm *ContainerMonitor) RemoveContainer(containerID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.containers[containerID]; !exists {
		return fmt.Errorf("container %s is not being monitored", containerID)
	}

	delete(cm.containers, containerID)

	cm.logger.Info("Removed container from monitoring",
		zap.String("container_id", containerID))

	// 发送事件
	cm.sendEvent(&ContainerEvent{
		Type:        EventTypeContainerRemoved,
		ContainerID: containerID,
		Timestamp:   time.Now(),
		Severity:    EventSeverityInfo,
	})

	return nil
}

// GetContainerMetrics 获取容器指标
func (cm *ContainerMonitor) GetContainerMetrics(containerID string) (*ContainerMetrics, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	metrics, exists := cm.containers[containerID]
	if !exists {
		return nil, fmt.Errorf("container %s is not being monitored", containerID)
	}

	// 返回副本
	copy := *metrics
	return &copy, nil
}

// GetAllContainerMetrics 获取所有容器指标
func (cm *ContainerMonitor) GetAllContainerMetrics() map[string]*ContainerMetrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]*ContainerMetrics)
	for id, metrics := range cm.containers {
		copy := *metrics
		result[id] = &copy
	}

	return result
}

// GetEvents 获取事件
func (cm *ContainerMonitor) GetEvents() <-chan *ContainerEvent {
	return cm.eventChan
}

// metricsCollectionWorker 指标收集工作线程
func (cm *ContainerMonitor) metricsCollectionWorker() {
	ticker := time.NewTicker(cm.config.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.collectMetrics()
		case <-cm.ctx.Done():
			return
		}
	}
}

// collectMetrics 收集指标
func (cm *ContainerMonitor) collectMetrics() {
	cm.mu.Lock()
	containerIDs := make([]string, 0, len(cm.containers))
	for id := range cm.containers {
		containerIDs = append(containerIDs, id)
	}
	cm.mu.Unlock()

	for _, containerID := range containerIDs {
		if metrics, err := cm.collectContainerMetrics(containerID); err == nil {
			cm.mu.Lock()
			if existing, exists := cm.containers[containerID]; exists {
				*existing = *metrics
			}
			cm.mu.Unlock()

			// 检查告警阈值
			cm.checkAlertThresholds(metrics)

			// 发送指标收集事件
			cm.sendEvent(&ContainerEvent{
				Type:        EventTypeMetricsCollected,
				ContainerID: containerID,
				Timestamp:   time.Now(),
				Severity:    EventSeverityInfo,
				Data: map[string]interface{}{
					"cpu_usage":    metrics.CPUUsagePercent,
					"memory_usage": metrics.MemoryUsagePercent,
				},
			})
		} else {
			cm.logger.Error("Failed to collect metrics",
				zap.String("container_id", containerID),
				zap.Error(err))
		}
	}
}

// collectContainerMetrics 收集单个容器指标
func (cm *ContainerMonitor) collectContainerMetrics(containerID string) (*ContainerMetrics, error) {
	cm.mu.RLock()
	existing, exists := cm.containers[containerID]
	if !exists {
		cm.mu.RUnlock()
		return nil, fmt.Errorf("container not found: %s", containerID)
	}
	pid := existing.PID
	cm.mu.RUnlock()

	metrics := &ContainerMetrics{
		ContainerID: containerID,
		PID:         pid,
		Timestamp:   time.Now(),
	}

	// 收集 CPU 指标
	if cpuMetrics, err := cm.resourceCollector.CollectCPUMetrics(containerID, pid); err == nil {
		metrics.CPUUsagePercent = cpuMetrics.UsagePercent
		metrics.CPUUserTime = float64(cpuMetrics.UserTime)
		metrics.CPUSystemTime = float64(cpuMetrics.SystemTime)
		metrics.CPUThrottled = cpuMetrics.Throttled
	}

	// 收集内存指标
	if memMetrics, err := cm.resourceCollector.CollectMemoryMetrics(containerID, pid); err == nil {
		metrics.MemoryUsage = memMetrics.Usage
		metrics.MemoryLimit = memMetrics.Limit
		metrics.MemoryUsagePercent = memMetrics.UsagePercent
		metrics.MemoryCache = memMetrics.Cache
		metrics.MemorySwap = memMetrics.Swap
	}

	// 收集网络指标
	if cm.config.EnableNetworkMonitoring {
		if netMetrics, err := cm.resourceCollector.CollectNetworkMetrics(containerID, pid); err == nil {
			metrics.NetworkRxBytes = netMetrics.RxBytes
			metrics.NetworkTxBytes = netMetrics.TxBytes
			metrics.NetworkRxPackets = netMetrics.RxPackets
			metrics.NetworkTxPackets = netMetrics.TxPackets
			metrics.NetworkRxErrors = netMetrics.RxErrors
			metrics.NetworkTxErrors = netMetrics.TxErrors
		}
	}

	// 收集磁盘指标
	if cm.config.EnableDiskMonitoring {
		if diskMetrics, err := cm.resourceCollector.CollectDiskMetrics(containerID, pid); err == nil {
			metrics.DiskReadBytes = diskMetrics.ReadBytes
			metrics.DiskWriteBytes = diskMetrics.WriteBytes
			metrics.DiskReadOps = diskMetrics.ReadOps
			metrics.DiskWriteOps = diskMetrics.WriteOps
		}
	}

	// 收集进程指标
	if procMetrics, err := cm.resourceCollector.CollectProcessMetrics(containerID, pid); err == nil {
		metrics.NumThreads = procMetrics.NumThreads
		metrics.NumFDs = procMetrics.NumFDs
	}

	return metrics, nil
}

// checkAlertThresholds 检查告警阈值
func (cm *ContainerMonitor) checkAlertThresholds(metrics *ContainerMetrics) {
	thresholds := cm.config.AlertThresholds
	if thresholds == nil {
		return
	}

	// 检查 CPU 使用率
	if metrics.CPUUsagePercent > thresholds.CPUUsagePercent {
		cm.sendAlert(metrics.ContainerID, "CPU usage too high",
			map[string]interface{}{
				"current_usage": metrics.CPUUsagePercent,
				"threshold":     thresholds.CPUUsagePercent,
			}, EventSeverityWarning)
	}

	// 检查内存使用率
	if metrics.MemoryUsagePercent > thresholds.MemoryUsagePercent {
		severity := EventSeverityWarning
		if metrics.MemoryUsagePercent > 95.0 {
			severity = EventSeverityCritical
		}

		cm.sendAlert(metrics.ContainerID, "Memory usage too high",
			map[string]interface{}{
				"current_usage": metrics.MemoryUsagePercent,
				"threshold":     thresholds.MemoryUsagePercent,
			}, severity)
	}

	// 检查网络错误率
	if cm.config.EnableNetworkMonitoring {
		totalPackets := metrics.NetworkRxPackets + metrics.NetworkTxPackets
		totalErrors := metrics.NetworkRxErrors + metrics.NetworkTxErrors

		if totalPackets > 0 {
			errorRate := float64(totalErrors) / float64(totalPackets) * 100
			if errorRate > thresholds.NetworkErrorRate {
				cm.sendAlert(metrics.ContainerID, "Network error rate too high",
					map[string]interface{}{
						"error_rate": errorRate,
						"threshold":  thresholds.NetworkErrorRate,
					}, EventSeverityWarning)
			}
		}
	}
}

// sendAlert 发送告警
func (cm *ContainerMonitor) sendAlert(containerID, message string, data map[string]interface{}, severity EventSeverity) {
	cm.sendEvent(&ContainerEvent{
		Type:        EventTypeResourceAlert,
		ContainerID: containerID,
		Timestamp:   time.Now(),
		Severity:    severity,
		Data: map[string]interface{}{
			"message": message,
			"details": data,
		},
	})
}

// healthCheckWorker 健康检查工作线程
func (cm *ContainerMonitor) healthCheckWorker() {
	ticker := time.NewTicker(cm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.performHealthChecks()
		case <-cm.ctx.Done():
			return
		}
	}
}

// performHealthChecks 执行健康检查
func (cm *ContainerMonitor) performHealthChecks() {
	cm.mu.RLock()
	containerIDs := make([]string, 0, len(cm.containers))
	for id := range cm.containers {
		containerIDs = append(containerIDs, id)
	}
	cm.mu.RUnlock()

	for _, containerID := range containerIDs {
		results := cm.healthChecker.CheckContainer(containerID)

		cm.mu.Lock()
		if metrics, exists := cm.containers[containerID]; exists {
			metrics.HealthChecks = results

			// 更新健康状态
			healthyCount := 0
			for _, result := range results {
				if result.Status == "HEALTHY" {
					healthyCount++
				}
			}

			if len(results) == 0 {
				metrics.HealthStatus = HealthStatusUnknown
			} else if healthyCount == len(results) {
				metrics.HealthStatus = HealthStatusHealthy
			} else if healthyCount > 0 {
				metrics.HealthStatus = HealthStatusDegraded
			} else {
				metrics.HealthStatus = HealthStatusUnhealthy

				// 发送健康检查失败事件
				cm.sendEvent(&ContainerEvent{
					Type:        EventTypeHealthCheckFailed,
					ContainerID: containerID,
					Timestamp:   time.Now(),
					Severity:    EventSeverityError,
					Data: map[string]interface{}{
						"failed_checks": len(results) - healthyCount,
						"total_checks":  len(results),
					},
				})
			}
		}
		cm.mu.Unlock()
	}
}

// eventProcessor 事件处理器
func (cm *ContainerMonitor) eventProcessor() {
	for {
		select {
		case event, ok := <-cm.eventChan:
			if !ok {
				return
			}
			cm.processEvent(event)
		case <-cm.ctx.Done():
			return
		}
	}
}

// processEvent 处理事件
func (cm *ContainerMonitor) processEvent(event *ContainerEvent) {
	cm.logger.Debug("Processing container event",
		zap.String("event_type", event.Type),
		zap.String("container_id", event.ContainerID),
		zap.String("severity", event.Severity.String()))

	// 这里可以添加事件处理逻辑
	// 例如：发送到外部监控系统、记录到数据库等
}

// cleanupWorker 清理工作线程
func (cm *ContainerMonitor) cleanupWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.cleanupOldMetrics()
		case <-cm.ctx.Done():
			return
		}
	}
}

// cleanupOldMetrics 清理旧指标
func (cm *ContainerMonitor) cleanupOldMetrics() {
	cutoff := time.Now().Add(-cm.config.MetricsRetentionTime)

	cm.mu.Lock()
	defer cm.mu.Unlock()

	for containerID, metrics := range cm.containers {
		if metrics.Timestamp.Before(cutoff) {
			cm.logger.Debug("Cleaning up old metrics",
				zap.String("container_id", containerID),
				zap.Time("last_update", metrics.Timestamp))

			delete(cm.containers, containerID)
		}
	}
}

// sendEvent 发送事件
func (cm *ContainerMonitor) sendEvent(event *ContainerEvent) {
	select {
	case cm.eventChan <- event:
	default:
		cm.logger.Warn("Event channel full, dropping event",
			zap.String("event_type", event.Type),
			zap.String("container_id", event.ContainerID))
	}
}
