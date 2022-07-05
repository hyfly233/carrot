package nodestatusmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"
	"go.uber.org/zap"
)

// HeartbeatManager 心跳管理器
type HeartbeatManager struct {
	mu     sync.RWMutex
	config *HeartbeatConfig
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// 心跳状态
	isRunning     bool
	lastHeartbeat time.Time
	heartbeatSeq  int64

	// ResourceManager 连接
	rmClient ResourceManagerClient

	// 状态信息
	nodeStatus     *NodeStatus
	statusProvider NodeStatusProvider

	// 事件通道
	eventChan chan *HeartbeatEvent

	// 统计信息
	stats *HeartbeatStats

	// 故障检测
	failureDetector *FailureDetector
}

// HeartbeatConfig 心跳配置
type HeartbeatConfig struct {
	Interval               time.Duration `json:"interval"`
	Timeout                time.Duration `json:"timeout"`
	RetryInterval          time.Duration `json:"retry_interval"`
	MaxRetries             int           `json:"max_retries"`
	ResourceManagerAddr    string        `json:"resource_manager_addr"`
	NodeID                 string        `json:"node_id"`
	EnableFailureDetection bool          `json:"enable_failure_detection"`
	FailureThreshold       int           `json:"failure_threshold"`
}

// ResourceManagerClient ResourceManager 客户端接口
type ResourceManagerClient interface {
	SendHeartbeat(ctx context.Context, heartbeat *Heartbeat) (*HeartbeatResponse, error)
	RegisterNode(ctx context.Context, registration *NodeRegistration) (*NodeRegistrationResponse, error)
	IsConnected() bool
	Connect() error
	Disconnect() error
}

// NodeStatusProvider 节点状态提供者接口
type NodeStatusProvider interface {
	GetNodeStatus() *NodeStatus
	GetResourceUsage() *ResourceUsage
	GetContainerStatuses() map[string]*ContainerStatus
	GetNodeHealth() *NodeHealth
}

// Heartbeat 心跳消息
type Heartbeat struct {
	NodeID              string                      `json:"node_id"`
	Sequence            int64                       `json:"sequence"`
	Timestamp           time.Time                   `json:"timestamp"`
	NodeStatus          *NodeStatus                 `json:"node_status"`
	ResourceUsage       *ResourceUsage              `json:"resource_usage"`
	ContainerStatuses   map[string]*ContainerStatus `json:"container_statuses"`
	NodeHealth          *NodeHealth                 `json:"node_health"`
	LastContainerUpdate time.Time                   `json:"last_container_update"`
}

// HeartbeatResponse 心跳响应
type HeartbeatResponse struct {
	NodeID       string         `json:"node_id"`
	Accepted     bool           `json:"accepted"`
	Commands     []*NodeCommand `json:"commands"`
	NewInterval  time.Duration  `json:"new_interval,omitempty"`
	Timestamp    time.Time      `json:"timestamp"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

// NodeRegistration 节点注册
type NodeRegistration struct {
	NodeID       string            `json:"node_id"`
	NodeInfo     *NodeInfo         `json:"node_info"`
	Capabilities *NodeCapabilities `json:"capabilities"`
	Resources    *NodeResources    `json:"resources"`
	Timestamp    time.Time         `json:"timestamp"`
}

// NodeRegistrationResponse 节点注册响应
type NodeRegistrationResponse struct {
	NodeID            string        `json:"node_id"`
	Accepted          bool          `json:"accepted"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	ErrorMessage      string        `json:"error_message,omitempty"`
}

// NodeCommand 节点命令
type NodeCommand struct {
	ID         string                 `json:"id"`
	Type       NodeCommandType        `json:"type"`
	Parameters map[string]interface{} `json:"parameters"`
	Timestamp  time.Time              `json:"timestamp"`
	Priority   int                    `json:"priority"`
}

// NodeCommandType 节点命令类型
type NodeCommandType string

const (
	NodeCommandTypeStartContainer   NodeCommandType = "START_CONTAINER"
	NodeCommandTypeStopContainer    NodeCommandType = "STOP_CONTAINER"
	NodeCommandTypeKillContainer    NodeCommandType = "KILL_CONTAINER"
	NodeCommandTypeCleanupContainer NodeCommandType = "CLEANUP_CONTAINER"
	NodeCommandTypeUpdateResources  NodeCommandType = "UPDATE_RESOURCES"
	NodeCommandTypeRestart          NodeCommandType = "RESTART"
	NodeCommandTypeShutdown         NodeCommandType = "SHUTDOWN"
	NodeCommandTypeCollectLogs      NodeCommandType = "COLLECT_LOGS"
)

// NodeStatus 节点状态
type NodeStatus struct {
	NodeID             string               `json:"node_id"`
	State              NodeState            `json:"state"`
	LastUpdate         time.Time            `json:"last_update"`
	RunningContainers  int                  `json:"running_containers"`
	AllocatedResources *common.ResourceSpec `json:"allocated_resources"`
	AvailableResources *common.ResourceSpec `json:"available_resources"`
	NodeLabels         map[string]string    `json:"node_labels"`
	NodeAttributes     map[string]string    `json:"node_attributes"`
}

// NodeState 节点状态
type NodeState string

const (
	NodeStateNew             NodeState = "NEW"
	NodeStateRunning         NodeState = "RUNNING"
	NodeStateUnhealthy       NodeState = "UNHEALTHY"
	NodeStateDecommissioning NodeState = "DECOMMISSIONING"
	NodeStateDecommissioned  NodeState = "DECOMMISSIONED"
	NodeStateLost            NodeState = "LOST"
	NodeStateRebooted        NodeState = "REBOOTED"
	NodeStateShutdown        NodeState = "SHUTDOWN"
)

// ResourceUsage 资源使用情况
type ResourceUsage struct {
	CPU               float64       `json:"cpu"`
	Memory            int64         `json:"memory"`
	Disk              int64         `json:"disk"`
	Network           *NetworkUsage `json:"network"`
	CPUUtilization    float64       `json:"cpu_utilization"`
	MemoryUtilization float64       `json:"memory_utilization"`
	DiskUtilization   float64       `json:"disk_utilization"`
	Timestamp         time.Time     `json:"timestamp"`
}

// NetworkUsage 网络使用情况
type NetworkUsage struct {
	RxBytes   int64 `json:"rx_bytes"`
	TxBytes   int64 `json:"tx_bytes"`
	RxPackets int64 `json:"rx_packets"`
	TxPackets int64 `json:"tx_packets"`
	RxErrors  int64 `json:"rx_errors"`
	TxErrors  int64 `json:"tx_errors"`
}

// ContainerStatus 容器状态
type ContainerStatus struct {
	ContainerID   string               `json:"container_id"`
	ApplicationID string               `json:"application_id"`
	State         ContainerState       `json:"state"`
	ExitCode      int                  `json:"exit_code,omitempty"`
	Diagnostics   string               `json:"diagnostics,omitempty"`
	UsedResources *common.ResourceSpec `json:"used_resources"`
	Progress      float64              `json:"progress"`
	LastUpdate    time.Time            `json:"last_update"`
}

// ContainerState 容器状态
type ContainerState string

const (
	ContainerStateNew        ContainerState = "NEW"
	ContainerStateLocalizing ContainerState = "LOCALIZING"
	ContainerStateRunning    ContainerState = "RUNNING"
	ContainerStateExited     ContainerState = "EXITED"
	ContainerStateFailed     ContainerState = "FAILED"
	ContainerStateKilled     ContainerState = "KILLED"
	ContainerStateDone       ContainerState = "DONE"
)

// NodeHealth 节点健康状态
type NodeHealth struct {
	Healthy         bool           `json:"healthy"`
	LastHealthCheck time.Time      `json:"last_health_check"`
	HealthReport    string         `json:"health_report"`
	DiskHealthy     bool           `json:"disk_healthy"`
	MemoryHealthy   bool           `json:"memory_healthy"`
	CPUHealthy      bool           `json:"cpu_healthy"`
	NetworkHealthy  bool           `json:"network_healthy"`
	HealthChecks    []*HealthCheck `json:"health_checks"`
}

// HealthCheck 健康检查
type HealthCheck struct {
	Name      string        `json:"name"`
	Status    HealthStatus  `json:"status"`
	Message   string        `json:"message"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
}

// HealthStatus 健康状态
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "HEALTHY"
	HealthStatusUnhealthy HealthStatus = "UNHEALTHY"
	HealthStatusUnknown   HealthStatus = "UNKNOWN"
)

// NodeInfo 节点信息
type NodeInfo struct {
	Hostname           string            `json:"hostname"`
	IPAddress          string            `json:"ip_address"`
	Port               int               `json:"port"`
	OSVersion          string            `json:"os_version"`
	KernelVersion      string            `json:"kernel_version"`
	Architecture       string            `json:"architecture"`
	StartTime          time.Time         `json:"start_time"`
	NodeManagerVersion string            `json:"node_manager_version"`
	Attributes         map[string]string `json:"attributes"`
}

// NodeCapabilities 节点能力
type NodeCapabilities struct {
	SupportedContainerTypes []string          `json:"supported_container_types"`
	MaxContainers           int               `json:"max_containers"`
	Features                []string          `json:"features"`
	Extensions              map[string]string `json:"extensions"`
}

// NodeResources 节点资源
type NodeResources struct {
	TotalCPU    float64 `json:"total_cpu"`
	TotalMemory int64   `json:"total_memory"`
	TotalDisk   int64   `json:"total_disk"`
	CPUCores    int     `json:"cpu_cores"`
	CPUSockets  int     `json:"cpu_sockets"`
	NUMANodes   int     `json:"numa_nodes"`
}

// HeartbeatEvent 心跳事件
type HeartbeatEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
}

// EventSeverity 事件严重级别
type EventSeverity string

const (
	EventSeverityInfo     EventSeverity = "INFO"
	EventSeverityWarning  EventSeverity = "WARNING"
	EventSeverityError    EventSeverity = "ERROR"
	EventSeverityCritical EventSeverity = "CRITICAL"
)

// HeartbeatEventType 心跳事件类型
const (
	EventTypeHeartbeatSent          = "HEARTBEAT_SENT"
	EventTypeHeartbeatFailed        = "HEARTBEAT_FAILED"
	EventTypeHeartbeatRestored      = "HEARTBEAT_RESTORED"
	EventTypeNodeRegistered         = "NODE_REGISTERED"
	EventTypeNodeRegistrationFailed = "NODE_REGISTRATION_FAILED"
	EventTypeCommandReceived        = "COMMAND_RECEIVED"
	EventTypeConnectionLost         = "CONNECTION_LOST"
	EventTypeConnectionRestored     = "CONNECTION_RESTORED"
)

// HeartbeatStats 心跳统计信息
type HeartbeatStats struct {
	TotalHeartbeats         int64         `json:"total_heartbeats"`
	SuccessfulHeartbeats    int64         `json:"successful_heartbeats"`
	FailedHeartbeats        int64         `json:"failed_heartbeats"`
	AverageLatency          time.Duration `json:"average_latency"`
	LastSuccessfulHeartbeat time.Time     `json:"last_successful_heartbeat"`
	LastFailedHeartbeat     time.Time     `json:"last_failed_heartbeat"`
	ConsecutiveFailures     int           `json:"consecutive_failures"`
	UpTime                  time.Duration `json:"uptime"`
	StartTime               time.Time     `json:"start_time"`
	IsConnected             bool          `json:"is_connected"`
}

// FailureDetector 故障检测器
type FailureDetector struct {
	threshold           int
	consecutiveFailures int
	lastFailureTime     time.Time
	isNodeHealthy       bool
	mu                  sync.RWMutex
}

// NewHeartbeatManager 创建心跳管理器
func NewHeartbeatManager(config *HeartbeatConfig, rmClient ResourceManagerClient, statusProvider NodeStatusProvider) (*HeartbeatManager, error) {
	if config == nil {
		return nil, fmt.Errorf("heartbeat config cannot be nil")
	}

	if rmClient == nil {
		return nil, fmt.Errorf("resource manager client cannot be nil")
	}

	if statusProvider == nil {
		return nil, fmt.Errorf("node status provider cannot be nil")
	}

	// 设置默认值
	if config.Interval == 0 {
		config.Interval = 3 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = 1 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}

	ctx, cancel := context.WithCancel(context.Background())

	hm := &HeartbeatManager{
		config:         config,
		logger:         zap.NewNop(), // 在实际使用中应该注入正确的logger
		ctx:            ctx,
		cancel:         cancel,
		rmClient:       rmClient,
		statusProvider: statusProvider,
		eventChan:      make(chan *HeartbeatEvent, 1000),
		stats: &HeartbeatStats{
			StartTime: time.Now(),
		},
	}

	// 初始化故障检测器
	if config.EnableFailureDetection {
		hm.failureDetector = &FailureDetector{
			threshold:     config.FailureThreshold,
			isNodeHealthy: true,
		}
	}

	return hm, nil
}

// Start 启动心跳管理器
func (hm *HeartbeatManager) Start() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if hm.isRunning {
		return fmt.Errorf("heartbeat manager is already running")
	}

	hm.logger.Info("Starting heartbeat manager",
		zap.String("node_id", hm.config.NodeID),
		zap.Duration("interval", hm.config.Interval))

	// 连接到ResourceManager
	if err := hm.rmClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to ResourceManager: %v", err)
	}

	// 注册节点
	if err := hm.registerNode(); err != nil {
		hm.rmClient.Disconnect()
		return fmt.Errorf("failed to register node: %v", err)
	}

	hm.isRunning = true
	hm.stats.IsConnected = true

	// 启动心跳循环
	go hm.heartbeatLoop()

	return nil
}

// Stop 停止心跳管理器
func (hm *HeartbeatManager) Stop() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if !hm.isRunning {
		return nil
	}

	hm.logger.Info("Stopping heartbeat manager")
	hm.isRunning = false
	hm.cancel()

	// 断开连接
	if err := hm.rmClient.Disconnect(); err != nil {
		hm.logger.Error("Failed to disconnect from ResourceManager", zap.Error(err))
	}

	hm.stats.IsConnected = false
	close(hm.eventChan)
	return nil
}

// GetStats 获取统计信息
func (hm *HeartbeatManager) GetStats() *HeartbeatStats {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	stats := *hm.stats
	stats.UpTime = time.Since(hm.stats.StartTime)
	return &stats
}

// GetEvents 获取事件通道
func (hm *HeartbeatManager) GetEvents() <-chan *HeartbeatEvent {
	return hm.eventChan
}

// IsRunning 检查是否运行中
func (hm *HeartbeatManager) IsRunning() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.isRunning
}

// IsConnected 检查是否已连接
func (hm *HeartbeatManager) IsConnected() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.stats.IsConnected && hm.rmClient.IsConnected()
}

// registerNode 注册节点
func (hm *HeartbeatManager) registerNode() error {
	nodeStatus := hm.statusProvider.GetNodeStatus()

	registration := &NodeRegistration{
		NodeID:    hm.config.NodeID,
		Timestamp: time.Now(),
		NodeInfo: &NodeInfo{
			Hostname:           nodeStatus.NodeLabels["hostname"],
			IPAddress:          nodeStatus.NodeLabels["ip_address"],
			NodeManagerVersion: "1.0.0", // 应该从配置或常量获取
			StartTime:          hm.stats.StartTime,
		},
		Capabilities: &NodeCapabilities{
			SupportedContainerTypes: []string{"application"},
			MaxContainers:           100, // 应该从配置获取
			Features:                []string{"resource_management", "monitoring"},
		},
		Resources: &NodeResources{
			TotalCPU:    nodeStatus.AvailableResources.CPU + nodeStatus.AllocatedResources.CPU,
			TotalMemory: nodeStatus.AvailableResources.Memory + nodeStatus.AllocatedResources.Memory,
		},
	}

	ctx, cancel := context.WithTimeout(hm.ctx, hm.config.Timeout)
	defer cancel()

	response, err := hm.rmClient.RegisterNode(ctx, registration)
	if err != nil {
		hm.sendEvent(&HeartbeatEvent{
			Type:      EventTypeNodeRegistrationFailed,
			Timestamp: time.Now(),
			Severity:  EventSeverityError,
			Data: map[string]interface{}{
				"node_id": hm.config.NodeID,
				"error":   err.Error(),
			},
		})
		return err
	}

	if !response.Accepted {
		err := fmt.Errorf("node registration rejected: %s", response.ErrorMessage)
		hm.sendEvent(&HeartbeatEvent{
			Type:      EventTypeNodeRegistrationFailed,
			Timestamp: time.Now(),
			Severity:  EventSeverityError,
			Data: map[string]interface{}{
				"node_id": hm.config.NodeID,
				"error":   err.Error(),
			},
		})
		return err
	}

	// 更新心跳间隔
	if response.HeartbeatInterval > 0 {
		hm.config.Interval = response.HeartbeatInterval
	}

	hm.sendEvent(&HeartbeatEvent{
		Type:      EventTypeNodeRegistered,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"node_id":            hm.config.NodeID,
			"heartbeat_interval": hm.config.Interval,
		},
	})

	hm.logger.Info("Node registered successfully",
		zap.String("node_id", hm.config.NodeID),
		zap.Duration("heartbeat_interval", hm.config.Interval))

	return nil
}

// heartbeatLoop 心跳循环
func (hm *HeartbeatManager) heartbeatLoop() {
	ticker := time.NewTicker(hm.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hm.sendHeartbeat()
		case <-hm.ctx.Done():
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (hm *HeartbeatManager) sendHeartbeat() {
	start := time.Now()

	// 构建心跳消息
	heartbeat := hm.buildHeartbeat()

	// 发送心跳
	ctx, cancel := context.WithTimeout(hm.ctx, hm.config.Timeout)
	defer cancel()

	response, err := hm.rmClient.SendHeartbeat(ctx, heartbeat)
	latency := time.Since(start)

	hm.mu.Lock()
	hm.heartbeatSeq++
	hm.stats.TotalHeartbeats++

	if err != nil {
		hm.handleHeartbeatFailure(err, latency)
	} else {
		hm.handleHeartbeatSuccess(response, latency)
	}
	hm.mu.Unlock()
}

// buildHeartbeat 构建心跳消息
func (hm *HeartbeatManager) buildHeartbeat() *Heartbeat {
	nodeStatus := hm.statusProvider.GetNodeStatus()
	resourceUsage := hm.statusProvider.GetResourceUsage()
	containerStatuses := hm.statusProvider.GetContainerStatuses()
	nodeHealth := hm.statusProvider.GetNodeHealth()

	return &Heartbeat{
		NodeID:              hm.config.NodeID,
		Sequence:            hm.heartbeatSeq,
		Timestamp:           time.Now(),
		NodeStatus:          nodeStatus,
		ResourceUsage:       resourceUsage,
		ContainerStatuses:   containerStatuses,
		NodeHealth:          nodeHealth,
		LastContainerUpdate: time.Now(), // 应该从容器管理器获取
	}
}

// handleHeartbeatSuccess 处理心跳成功
func (hm *HeartbeatManager) handleHeartbeatSuccess(response *HeartbeatResponse, latency time.Duration) {
	hm.stats.SuccessfulHeartbeats++
	hm.stats.LastSuccessfulHeartbeat = time.Now()
	hm.lastHeartbeat = time.Now()
	hm.updateAverageLatency(latency)

	// 重置连续失败计数
	if hm.stats.ConsecutiveFailures > 0 {
		hm.stats.ConsecutiveFailures = 0
		hm.stats.IsConnected = true

		hm.sendEvent(&HeartbeatEvent{
			Type:      EventTypeHeartbeatRestored,
			Timestamp: time.Now(),
			Severity:  EventSeverityInfo,
			Data: map[string]interface{}{
				"node_id": hm.config.NodeID,
				"latency": latency,
			},
		})
	}

	// 更新故障检测器
	if hm.failureDetector != nil {
		hm.failureDetector.recordSuccess()
	}

	// 处理接收到的命令
	if response.Accepted && len(response.Commands) > 0 {
		hm.handleCommands(response.Commands)
	}

	// 更新心跳间隔
	if response.NewInterval > 0 && response.NewInterval != hm.config.Interval {
		hm.logger.Info("Updating heartbeat interval",
			zap.Duration("old_interval", hm.config.Interval),
			zap.Duration("new_interval", response.NewInterval))
		hm.config.Interval = response.NewInterval
	}

	hm.sendEvent(&HeartbeatEvent{
		Type:      EventTypeHeartbeatSent,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"node_id":  hm.config.NodeID,
			"sequence": hm.heartbeatSeq,
			"latency":  latency,
			"accepted": response.Accepted,
			"commands": len(response.Commands),
		},
	})
}

// handleHeartbeatFailure 处理心跳失败
func (hm *HeartbeatManager) handleHeartbeatFailure(err error, latency time.Duration) {
	hm.stats.FailedHeartbeats++
	hm.stats.LastFailedHeartbeat = time.Now()
	hm.stats.ConsecutiveFailures++

	// 更新故障检测器
	if hm.failureDetector != nil {
		hm.failureDetector.recordFailure()
	}

	severity := EventSeverityWarning
	if hm.stats.ConsecutiveFailures >= hm.config.FailureThreshold {
		severity = EventSeverityError
		hm.stats.IsConnected = false
	}

	hm.sendEvent(&HeartbeatEvent{
		Type:      EventTypeHeartbeatFailed,
		Timestamp: time.Now(),
		Severity:  severity,
		Data: map[string]interface{}{
			"node_id":              hm.config.NodeID,
			"sequence":             hm.heartbeatSeq,
			"error":                err.Error(),
			"consecutive_failures": hm.stats.ConsecutiveFailures,
			"latency":              latency,
		},
	})

	hm.logger.Error("Heartbeat failed",
		zap.String("node_id", hm.config.NodeID),
		zap.Int64("sequence", hm.heartbeatSeq),
		zap.Int("consecutive_failures", hm.stats.ConsecutiveFailures),
		zap.Error(err))

	// 如果连续失败次数达到阈值，尝试重新连接
	if hm.stats.ConsecutiveFailures >= hm.config.FailureThreshold {
		go hm.attemptReconnect()
	}
}

// handleCommands 处理接收到的命令
func (hm *HeartbeatManager) handleCommands(commands []*NodeCommand) {
	for _, command := range commands {
		hm.sendEvent(&HeartbeatEvent{
			Type:      EventTypeCommandReceived,
			Timestamp: time.Now(),
			Severity:  EventSeverityInfo,
			Data: map[string]interface{}{
				"node_id":      hm.config.NodeID,
				"command_id":   command.ID,
				"command_type": string(command.Type),
				"priority":     command.Priority,
			},
		})

		hm.logger.Info("Received command",
			zap.String("command_id", command.ID),
			zap.String("command_type", string(command.Type)),
			zap.Int("priority", command.Priority))

		// 这里应该将命令转发给适当的处理器
		// 实际实现中需要有一个命令处理器接口
	}
}

// attemptReconnect 尝试重新连接
func (hm *HeartbeatManager) attemptReconnect() {
	hm.logger.Info("Attempting to reconnect to ResourceManager")

	for retry := 0; retry < hm.config.MaxRetries; retry++ {
		if hm.ctx.Err() != nil {
			return
		}

		time.Sleep(hm.config.RetryInterval)

		if err := hm.rmClient.Connect(); err != nil {
			hm.logger.Warn("Reconnection attempt failed",
				zap.Int("retry", retry+1),
				zap.Error(err))
			continue
		}

		// 重新注册节点
		if err := hm.registerNode(); err != nil {
			hm.logger.Warn("Node re-registration failed",
				zap.Int("retry", retry+1),
				zap.Error(err))
			hm.rmClient.Disconnect()
			continue
		}

		hm.mu.Lock()
		hm.stats.ConsecutiveFailures = 0
		hm.stats.IsConnected = true
		hm.mu.Unlock()

		hm.sendEvent(&HeartbeatEvent{
			Type:      EventTypeConnectionRestored,
			Timestamp: time.Now(),
			Severity:  EventSeverityInfo,
			Data: map[string]interface{}{
				"node_id": hm.config.NodeID,
				"retry":   retry + 1,
			},
		})

		hm.logger.Info("Successfully reconnected to ResourceManager")
		return
	}

	hm.sendEvent(&HeartbeatEvent{
		Type:      EventTypeConnectionLost,
		Timestamp: time.Now(),
		Severity:  EventSeverityCritical,
		Data: map[string]interface{}{
			"node_id":     hm.config.NodeID,
			"max_retries": hm.config.MaxRetries,
		},
	})

	hm.logger.Error("Failed to reconnect after maximum retries")
}

// updateAverageLatency 更新平均延迟
func (hm *HeartbeatManager) updateAverageLatency(latency time.Duration) {
	if hm.stats.SuccessfulHeartbeats == 1 {
		hm.stats.AverageLatency = latency
	} else {
		// 使用指数移动平均
		alpha := 0.1
		hm.stats.AverageLatency = time.Duration(
			float64(hm.stats.AverageLatency)*(1-alpha) + float64(latency)*alpha,
		)
	}
}

// sendEvent 发送事件
func (hm *HeartbeatManager) sendEvent(event *HeartbeatEvent) {
	select {
	case hm.eventChan <- event:
	default:
		hm.logger.Warn("Heartbeat event channel full, dropping event",
			zap.String("event_type", event.Type))
	}
}

// FailureDetector 方法

// recordSuccess 记录成功
func (fd *FailureDetector) recordSuccess() {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	fd.consecutiveFailures = 0
	fd.isNodeHealthy = true
}

// recordFailure 记录失败
func (fd *FailureDetector) recordFailure() {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	fd.consecutiveFailures++
	fd.lastFailureTime = time.Now()

	if fd.consecutiveFailures >= fd.threshold {
		fd.isNodeHealthy = false
	}
}

// IsHealthy 检查是否健康
func (fd *FailureDetector) IsHealthy() bool {
	fd.mu.RLock()
	defer fd.mu.RUnlock()
	return fd.isNodeHealthy
}

// GetConsecutiveFailures 获取连续失败次数
func (fd *FailureDetector) GetConsecutiveFailures() int {
	fd.mu.RLock()
	defer fd.mu.RUnlock()
	return fd.consecutiveFailures
}
