package containermanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ContainerManager 容器管理器
type ContainerManager struct {
	mu                sync.RWMutex
	containers        map[string]*ContainerInfo
	containerExecutor ContainerExecutor
	resourceManager   ResourceManager
	eventDispatcher   *ContainerEventDispatcher
	logger            *zap.Logger
	ctx               context.Context
	cancel            context.CancelFunc

	// 配置
	config *ContainerManagerConfig

	// 状态
	isRunning bool

	// 统计信息
	stats *ContainerStats
}

// ContainerManagerConfig 容器管理器配置
type ContainerManagerConfig struct {
	MaxContainers         int           `json:"max_containers"`
	ContainerCleanupDelay time.Duration `json:"container_cleanup_delay"`
	ResourceCheckInterval time.Duration `json:"resource_check_interval"`
	ContainerLogDir       string        `json:"container_log_dir"`
	TempDir               string        `json:"temp_dir"`
	EnableResourceLimits  bool          `json:"enable_resource_limits"`
	EnableCgroupsV2       bool          `json:"enable_cgroups_v2"`
}

// ContainerInfo 容器信息
type ContainerInfo struct {
	Container        *common.Container
	LaunchContext    *common.ContainerLaunchContext
	State            ContainerState
	StartTime        time.Time
	FinishTime       time.Time
	ExitCode         int
	Diagnostics      string
	ResourceUsage    *ResourceUsage
	Process          ContainerProcess
	WorkingDirectory string
	LogFiles         map[string]string

	// 内部状态
	mu            sync.RWMutex
	lastHeartbeat time.Time
	restartCount  int
}

func (ci *ContainerInfo) Clone() *ContainerInfo {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	return &ContainerInfo{
		Container:        ci.Container,
		LaunchContext:    ci.LaunchContext,
		State:            ci.State,
		StartTime:        ci.StartTime,
		FinishTime:       ci.FinishTime,
		ExitCode:         ci.ExitCode,
		Diagnostics:      ci.Diagnostics,
		ResourceUsage:    ci.ResourceUsage,
		Process:          ci.Process,
		WorkingDirectory: ci.WorkingDirectory,
		LogFiles:         ci.LogFiles,
		lastHeartbeat:    ci.lastHeartbeat,
		restartCount:     ci.restartCount,
	}
}

// ContainerState 容器状态
type ContainerState int

const (
	ContainerStateNew ContainerState = iota
	ContainerStateLocalizing
	ContainerStateLocalized
	ContainerStateRunning
	ContainerStateExitedWithSuccess
	ContainerStateExitedWithFailure
	ContainerStateKilled
	ContainerStateCleaning
	ContainerStateDone
)

// String 返回容器状态字符串
func (cs ContainerState) String() string {
	switch cs {
	case ContainerStateNew:
		return "NEW"
	case ContainerStateLocalizing:
		return "LOCALIZING"
	case ContainerStateLocalized:
		return "LOCALIZED"
	case ContainerStateRunning:
		return "RUNNING"
	case ContainerStateExitedWithSuccess:
		return "EXITED_WITH_SUCCESS"
	case ContainerStateExitedWithFailure:
		return "EXITED_WITH_FAILURE"
	case ContainerStateKilled:
		return "KILLED"
	case ContainerStateCleaning:
		return "CLEANING"
	case ContainerStateDone:
		return "DONE"
	default:
		return "UNKNOWN"
	}
}

// ResourceUsage 资源使用情况
type ResourceUsage struct {
	Memory      int64     `json:"memory"`
	VCores      float64   `json:"vcores"`
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage int64     `json:"memory_usage"`
	NetworkRx   int64     `json:"network_rx"`
	NetworkTx   int64     `json:"network_tx"`
	DiskUsage   int64     `json:"disk_usage"`
	Timestamp   time.Time `json:"timestamp"`
}

// ContainerProcess 容器进程信息
type ContainerProcess interface {
	GetPID() int
	IsRunning() bool
	Kill() error
	Wait() error
	GetExitCode() int
}

// ContainerStats 容器统计信息
type ContainerStats struct {
	TotalLaunched     int64 `json:"total_launched"`
	TotalCompleted    int64 `json:"total_completed"`
	TotalFailed       int64 `json:"total_failed"`
	TotalKilled       int64 `json:"total_killed"`
	CurrentRunning    int   `json:"current_running"`
	CurrentLocalizing int   `json:"current_localizing"`
}

// ResourceManager 资源管理器接口
type ResourceManager interface {
	AllocateResources(containerID string, resource common.Resource) error
	ReleaseResources(containerID string) error
	GetResourceUsage(containerID string) (*ResourceUsage, error)
	CheckResourceLimits(containerID string) error
}

// ContainerEventDispatcher 容器事件分发器
type ContainerEventDispatcher struct {
	mu        sync.RWMutex
	listeners []ContainerEventListener
}

// ContainerEventListener 容器事件监听器
type ContainerEventListener interface {
	OnContainerStateChanged(containerID string, oldState, newState ContainerState)
	OnContainerStarted(containerID string)
	OnContainerFinished(containerID string, exitCode int)
}

// ContainerEvent 容器事件
type ContainerEvent struct {
	Type        string                 `json:"type"`
	ContainerID string                 `json:"container_id"`
	Timestamp   time.Time              `json:"timestamp"`
	Data        map[string]interface{} `json:"data"`
}

// NewContainerManager 创建新的容器管理器
func NewContainerManager(config *ContainerManagerConfig, executor ContainerExecutor, resourceManager ResourceManager) *ContainerManager {
	if config == nil {
		config = &ContainerManagerConfig{
			MaxContainers:         100,
			ContainerCleanupDelay: 30 * time.Second,
			ResourceCheckInterval: 5 * time.Second,
			ContainerLogDir:       "/tmp/carrot/logs",
			TempDir:               "/tmp/carrot/temp",
			EnableResourceLimits:  true,
			EnableCgroupsV2:       false,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	cm := &ContainerManager{
		containers:        make(map[string]*ContainerInfo),
		containerExecutor: executor,
		resourceManager:   resourceManager,
		eventDispatcher:   NewContainerEventDispatcher(),
		logger:            common.ComponentLogger("container-manager"),
		ctx:               ctx,
		cancel:            cancel,
		config:            config,
		stats:             &ContainerStats{},
	}

	return cm
}

// Start 启动容器管理器
func (cm *ContainerManager) Start() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.isRunning {
		return fmt.Errorf("container manager is already running")
	}

	cm.isRunning = true
	cm.logger.Info("Starting container manager")

	// 启动后台任务
	go cm.resourceMonitor()
	go cm.containerMonitor()
	go cm.cleanupWorker()

	return nil
}

// Stop 停止容器管理器
func (cm *ContainerManager) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.isRunning {
		return nil
	}

	cm.logger.Info("Stopping container manager")
	cm.isRunning = false
	cm.cancel()

	// 停止所有容器
	for _, containerInfo := range cm.containers {
		if containerInfo.State == ContainerStateRunning {
			cm.stopContainerInternal(containerInfo.Container.ID, "Container manager shutdown")
		}
	}

	return nil
}

// StartContainer 启动容器
func (cm *ContainerManager) StartContainer(container *common.Container, launchContext *common.ContainerLaunchContext) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	containerKey := cm.getContainerKey(container.ID)

	// 检查容器是否已存在
	if _, exists := cm.containers[containerKey]; exists {
		return fmt.Errorf("container %s already exists", containerKey)
	}

	// 检查容器数量限制
	if len(cm.containers) >= cm.config.MaxContainers {
		return fmt.Errorf("maximum number of containers (%d) reached", cm.config.MaxContainers)
	}

	// 创建容器信息
	containerInfo := &ContainerInfo{
		Container:        container,
		LaunchContext:    launchContext,
		State:            ContainerStateNew,
		StartTime:        time.Now(),
		ResourceUsage:    &ResourceUsage{},
		LogFiles:         make(map[string]string),
		WorkingDirectory: fmt.Sprintf("%s/%s", cm.config.TempDir, containerKey),
	}

	cm.containers[containerKey] = containerInfo
	cm.stats.TotalLaunched++

	cm.logger.Info("Starting container",
		zap.String("container_id", containerKey),
		zap.String("application_id", fmt.Sprintf("%d_%d",
			container.ID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
			container.ID.ApplicationAttemptID.ApplicationID.ID)))

	// 异步启动容器
	go cm.startContainerAsync(containerInfo)

	return nil
}

// startContainerAsync 异步启动容器
func (cm *ContainerManager) startContainerAsync(containerInfo *ContainerInfo) {
	containerKey := cm.getContainerKey(containerInfo.Container.ID)

	// 更新状态为 LOCALIZING
	cm.updateContainerState(containerInfo, ContainerStateLocalizing)

	// 资源分配
	if err := cm.resourceManager.AllocateResources(containerKey, containerInfo.Container.Resource); err != nil {
		cm.logger.Error("Failed to allocate resources",
			zap.String("container_id", containerKey),
			zap.Error(err))
		cm.updateContainerState(containerInfo, ContainerStateExitedWithFailure)
		containerInfo.Diagnostics = fmt.Sprintf("Resource allocation failed: %v", err)
		return
	}

	// 更新状态为 LOCALIZED
	cm.updateContainerState(containerInfo, ContainerStateLocalized)

	// 启动容器进程
	process, err := cm.containerExecutor.StartContainer(containerInfo.Container, containerInfo.LaunchContext)
	if err != nil {
		cm.logger.Error("Failed to start container",
			zap.String("container_id", containerKey),
			zap.Error(err))
		cm.resourceManager.ReleaseResources(containerKey)
		cm.updateContainerState(containerInfo, ContainerStateExitedWithFailure)
		containerInfo.Diagnostics = fmt.Sprintf("Container start failed: %v", err)
		return
	}

	containerInfo.Process = process
	cm.updateContainerState(containerInfo, ContainerStateRunning)
	cm.eventDispatcher.DispatchContainerStarted(containerKey)

	// 等待容器完成
	go cm.waitForContainer(containerInfo)
}

// waitForContainer 等待容器完成
func (cm *ContainerManager) waitForContainer(containerInfo *ContainerInfo) {
	containerKey := cm.getContainerKey(containerInfo.Container.ID)

	err := containerInfo.Process.Wait()
	exitCode := containerInfo.Process.GetExitCode()

	containerInfo.mu.Lock()
	containerInfo.FinishTime = time.Now()
	containerInfo.ExitCode = exitCode
	containerInfo.mu.Unlock()

	cm.logger.Info("Container finished",
		zap.String("container_id", containerKey),
		zap.Int("exit_code", exitCode))

	// 释放资源
	cm.resourceManager.ReleaseResources(containerKey)

	// 更新状态
	if err != nil || exitCode != 0 {
		cm.updateContainerState(containerInfo, ContainerStateExitedWithFailure)
		cm.stats.TotalFailed++
		containerInfo.Diagnostics = fmt.Sprintf("Container exited with code %d: %v", exitCode, err)
	} else {
		cm.updateContainerState(containerInfo, ContainerStateExitedWithSuccess)
		cm.stats.TotalCompleted++
	}

	cm.eventDispatcher.DispatchContainerFinished(containerKey, exitCode)

	// 标记为清理状态
	time.AfterFunc(cm.config.ContainerCleanupDelay, func() {
		cm.updateContainerState(containerInfo, ContainerStateCleaning)
	})
}

// StopContainer 停止容器
func (cm *ContainerManager) StopContainer(containerID common.ContainerID, reason string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.stopContainerInternal(containerID, reason)
}

// stopContainerInternal 内部停止容器方法
func (cm *ContainerManager) stopContainerInternal(containerID common.ContainerID, reason string) error {
	containerKey := cm.getContainerKey(containerID)
	containerInfo, exists := cm.containers[containerKey]
	if !exists {
		return fmt.Errorf("container not found: %s", containerKey)
	}

	if containerInfo.State != ContainerStateRunning {
		return fmt.Errorf("container %s is not running, current state: %s",
			containerKey, containerInfo.State.String())
	}

	cm.logger.Info("Stopping container",
		zap.String("container_id", containerKey),
		zap.String("reason", reason))

	// 杀死进程
	if containerInfo.Process != nil {
		if err := containerInfo.Process.Kill(); err != nil {
			cm.logger.Error("Failed to kill container process",
				zap.String("container_id", containerKey),
				zap.Error(err))
		}
	}

	// 释放资源
	cm.resourceManager.ReleaseResources(containerKey)

	// 更新状态
	cm.updateContainerState(containerInfo, ContainerStateKilled)
	cm.stats.TotalKilled++
	containerInfo.Diagnostics = reason

	return nil
}

// GetContainer 获取容器信息
func (cm *ContainerManager) GetContainer(containerID common.ContainerID) (*ContainerInfo, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	containerKey := cm.getContainerKey(containerID)
	containerInfo, exists := cm.containers[containerKey]
	if !exists {
		return nil, fmt.Errorf("container not found: %s", containerKey)
	}

	// 返回副本以防止外部修改
	return containerInfo.Clone(), nil
}

// GetAllContainers 获取所有容器信息
func (cm *ContainerManager) GetAllContainers() []*ContainerInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	containers := make([]*ContainerInfo, 0, len(cm.containers))
	for _, containerInfo := range cm.containers {
		containers = append(containers, containerInfo.Clone())
	}

	return containers
}

// GetContainerStats 获取容器统计信息
func (cm *ContainerManager) GetContainerStats() *ContainerStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 计算当前运行和本地化的容器数量
	currentRunning := 0
	currentLocalizing := 0
	for _, containerInfo := range cm.containers {
		if containerInfo.State == ContainerStateRunning {
			currentRunning++
		} else if containerInfo.State == ContainerStateLocalizing || containerInfo.State == ContainerStateLocalized {
			currentLocalizing++
		}
	}

	stats := *cm.stats
	stats.CurrentRunning = currentRunning
	stats.CurrentLocalizing = currentLocalizing

	return &stats
}

// updateContainerState 更新容器状态
func (cm *ContainerManager) updateContainerState(containerInfo *ContainerInfo, newState ContainerState) {
	containerInfo.mu.Lock()
	oldState := containerInfo.State
	containerInfo.State = newState
	containerInfo.mu.Unlock()

	containerKey := cm.getContainerKey(containerInfo.Container.ID)
	cm.eventDispatcher.DispatchStateChange(containerKey, oldState, newState)
}

// resourceMonitor 资源监控
func (cm *ContainerManager) resourceMonitor() {
	ticker := time.NewTicker(cm.config.ResourceCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.checkResourceUsage()
		case <-cm.ctx.Done():
			return
		}
	}
}

// checkResourceUsage 检查资源使用情况
func (cm *ContainerManager) checkResourceUsage() {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for containerKey, containerInfo := range cm.containers {
		if containerInfo.State == ContainerStateRunning {
			if usage, err := cm.resourceManager.GetResourceUsage(containerKey); err == nil {
				containerInfo.mu.Lock()
				containerInfo.ResourceUsage = usage
				containerInfo.lastHeartbeat = time.Now()
				containerInfo.mu.Unlock()

				// 检查资源限制
				if cm.config.EnableResourceLimits {
					if err := cm.resourceManager.CheckResourceLimits(containerKey); err != nil {
						cm.logger.Warn("Container exceeds resource limits",
							zap.String("container_id", containerKey),
							zap.Error(err))
					}
				}
			}
		}
	}
}

// containerMonitor 容器监控
func (cm *ContainerManager) containerMonitor() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.checkContainerHealth()
		case <-cm.ctx.Done():
			return
		}
	}
}

// checkContainerHealth 检查容器健康状态
func (cm *ContainerManager) checkContainerHealth() {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for containerKey, containerInfo := range cm.containers {
		if containerInfo.State == ContainerStateRunning {
			if containerInfo.Process != nil && !containerInfo.Process.IsRunning() {
				cm.logger.Warn("Container process is not running",
					zap.String("container_id", containerKey))

				// 异步处理容器停止
				go func(ci *ContainerInfo) {
					cm.waitForContainer(ci)
				}(containerInfo)
			}
		}
	}
}

// cleanupWorker 清理工作器
func (cm *ContainerManager) cleanupWorker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.cleanupFinishedContainers()
		case <-cm.ctx.Done():
			return
		}
	}
}

// cleanupFinishedContainers 清理已完成的容器
func (cm *ContainerManager) cleanupFinishedContainers() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for containerKey, containerInfo := range cm.containers {
		if containerInfo.State == ContainerStateCleaning {
			cm.logger.Debug("Cleaning up container", zap.String("container_id", containerKey))

			// 执行清理操作
			if err := cm.containerExecutor.CleanupContainer(containerInfo.Container.ID); err != nil {
				cm.logger.Error("Failed to cleanup container",
					zap.String("container_id", containerKey),
					zap.Error(err))
			}

			// 更新状态为 DONE
			containerInfo.State = ContainerStateDone

			// 从内存中移除（可选，或者保留一段时间用于查询）
			// delete(cm.containers, containerKey)
		}
	}
}

// getContainerKey 获取容器键
func (cm *ContainerManager) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ApplicationAttemptID.AttemptID,
		containerID.ContainerID)
}

// NewContainerEventDispatcher 创建容器事件分发器
func NewContainerEventDispatcher() *ContainerEventDispatcher {
	return &ContainerEventDispatcher{
		listeners: make([]ContainerEventListener, 0),
	}
}

// AddListener 添加事件监听器
func (ced *ContainerEventDispatcher) AddListener(listener ContainerEventListener) {
	ced.mu.Lock()
	defer ced.mu.Unlock()
	ced.listeners = append(ced.listeners, listener)
}

// DispatchStateChange 分发状态变更事件
func (ced *ContainerEventDispatcher) DispatchStateChange(containerID string, oldState, newState ContainerState) {
	ced.mu.RLock()
	defer ced.mu.RUnlock()

	for _, listener := range ced.listeners {
		go listener.OnContainerStateChanged(containerID, oldState, newState)
	}
}

// DispatchContainerStarted 分发容器启动事件
func (ced *ContainerEventDispatcher) DispatchContainerStarted(containerID string) {
	ced.mu.RLock()
	defer ced.mu.RUnlock()

	for _, listener := range ced.listeners {
		go listener.OnContainerStarted(containerID)
	}
}

// DispatchContainerFinished 分发容器完成事件
func (ced *ContainerEventDispatcher) DispatchContainerFinished(containerID string, exitCode int) {
	ced.mu.RLock()
	defer ced.mu.RUnlock()

	for _, listener := range ced.listeners {
		go listener.OnContainerFinished(containerID, exitCode)
	}
}
