package applicationmaster

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ApplicationMaster 应用程序主控
type ApplicationMaster struct {
	mu                   sync.RWMutex
	applicationID        common.ApplicationID
	applicationAttemptID common.ApplicationAttemptID
	rmClient             *ResourceManagerClient
	nmClients            map[string]*NodeManagerClient
	containers           map[string]*common.Container
	pendingRequests      []*common.ContainerRequest
	allocatedContainers  map[string]*common.Container
	completedContainers  map[string]*common.Container
	failedContainers     map[string]*common.Container
	applicationState     string
	finalStatus          string
	progress             float32
	trackingURL          string
	httpServer           *http.Server
	config               *common.Config
	logger               *zap.Logger
	ctx                  context.Context
	cancel               context.CancelFunc
	heartbeatInterval    time.Duration
	maxContainerRetries  int
	shutdownHook         chan struct{}
}

// ApplicationMasterConfig ApplicationMaster 配置
type ApplicationMasterConfig struct {
	ApplicationID        common.ApplicationID
	ApplicationAttemptID common.ApplicationAttemptID
	RMAddress            string
	TrackingURL          string
	HeartbeatInterval    time.Duration
	MaxContainerRetries  int
	Port                 int
}

// NewApplicationMaster 创建新的 ApplicationMaster
func NewApplicationMaster(config *ApplicationMasterConfig) *ApplicationMaster {
	ctx, cancel := context.WithCancel(context.Background())

	am := &ApplicationMaster{
		applicationID:        config.ApplicationID,
		applicationAttemptID: config.ApplicationAttemptID,
		nmClients:            make(map[string]*NodeManagerClient),
		containers:           make(map[string]*common.Container),
		pendingRequests:      make([]*common.ContainerRequest, 0),
		allocatedContainers:  make(map[string]*common.Container),
		completedContainers:  make(map[string]*common.Container),
		failedContainers:     make(map[string]*common.Container),
		applicationState:     common.ApplicationStateRunning,
		finalStatus:          common.FinalApplicationStatusUndefined,
		progress:             0.0,
		trackingURL:          config.TrackingURL,
		config:               common.GetDefaultConfig(),
		logger:               common.ComponentLogger("application-master"),
		ctx:                  ctx,
		cancel:               cancel,
		heartbeatInterval:    config.HeartbeatInterval,
		maxContainerRetries:  config.MaxContainerRetries,
		shutdownHook:         make(chan struct{}, 1),
	}

	// 创建 ResourceManager 客户端
	am.rmClient = NewResourceManagerClient(config.RMAddress, am.logger)

	return am
}

// Start 启动 ApplicationMaster
func (am *ApplicationMaster) Start() error {
	am.logger.Info("Starting ApplicationMaster",
		zap.Any("app_id", am.applicationID),
		zap.Any("attempt_id", am.applicationAttemptID))

	// 注册到 ResourceManager
	if err := am.registerWithRM(); err != nil {
		return fmt.Errorf("failed to register with ResourceManager: %w", err)
	}

	// 启动 HTTP 服务器
	if err := am.startHTTPServer(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// 启动心跳
	go am.startHeartbeat()

	// 启动容器监控
	go am.monitorContainers()

	// 启动应用程序逻辑
	go am.runApplicationLogic()

	am.logger.Info("ApplicationMaster started successfully")
	return nil
}

// Stop 停止 ApplicationMaster
func (am *ApplicationMaster) Stop() error {
	am.logger.Info("Stopping ApplicationMaster")

	// 发送停止信号
	close(am.shutdownHook)

	// 取消上下文
	am.cancel()

	// 释放所有容器
	am.releaseAllContainers()

	// 注销 ResourceManager
	if err := am.unregisterFromRM(); err != nil {
		am.logger.Error("Failed to unregister from ResourceManager", zap.Error(err))
	}

	// 停止 HTTP 服务器
	if am.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := am.httpServer.Shutdown(ctx); err != nil {
			am.logger.Error("Failed to shutdown HTTP server", zap.Error(err))
		}
	}

	am.logger.Info("ApplicationMaster stopped")
	return nil
}

// registerWithRM 向 ResourceManager 注册
func (am *ApplicationMaster) registerWithRM() error {
	request := &RegisterApplicationMasterRequest{
		Host:        "localhost", // TODO: 获取实际主机名
		RPCPort:     0,           // 不使用 RPC
		TrackingURL: am.trackingURL,
	}

	response, err := am.rmClient.RegisterApplicationMaster(request)
	if err != nil {
		return err
	}

	am.logger.Info("Registered with ResourceManager",
		zap.String("queue", response.Queue),
		zap.Int64("max_memory", response.MaximumResourceCapability.Memory),
		zap.Int32("max_vcores", response.MaximumResourceCapability.VCores))

	return nil
}

// unregisterFromRM 从 ResourceManager 注销
func (am *ApplicationMaster) unregisterFromRM() error {
	request := &FinishApplicationMasterRequest{
		FinalApplicationStatus: am.finalStatus,
		Diagnostics:            "Application completed successfully",
		TrackingURL:            am.trackingURL,
	}

	_, err := am.rmClient.FinishApplicationMaster(request)
	return err
}

// startHeartbeat 启动心跳
func (am *ApplicationMaster) startHeartbeat() {
	ticker := time.NewTicker(am.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := am.sendHeartbeat(); err != nil {
				am.logger.Error("Failed to send heartbeat", zap.Error(err))
			}
		case <-am.ctx.Done():
			return
		case <-am.shutdownHook:
			return
		}
	}
}

// sendHeartbeat 发送心跳到 ResourceManager
func (am *ApplicationMaster) sendHeartbeat() error {
	am.mu.Lock()
	defer am.mu.Unlock()

	// 收集已完成的容器
	completedContainers := make([]*common.Container, 0, len(am.completedContainers))
	for _, container := range am.completedContainers {
		completedContainers = append(completedContainers, container)
	}
	am.completedContainers = make(map[string]*common.Container) // 清空已完成容器

	request := &AllocateRequest{
		Ask:                 am.pendingRequests,
		Release:             []common.ContainerID{}, // TODO: 实现容器释放
		CompletedContainers: completedContainers,
		Progress:            am.progress,
	}

	response, err := am.rmClient.Allocate(request)
	if err != nil {
		return err
	}

	// 处理新分配的容器
	for _, container := range response.AllocatedContainers {
		am.handleNewContainer(container)
	}

	// 处理已完成的容器
	for _, container := range response.CompletedContainers {
		am.handleCompletedContainer(container)
	}

	return nil
}

// handleNewContainer 处理新分配的容器
func (am *ApplicationMaster) handleNewContainer(container *common.Container) {
	containerKey := am.getContainerKey(container.ID)
	am.allocatedContainers[containerKey] = container

	am.logger.Info("Received new container",
		zap.String("container_id", containerKey),
		zap.String("node", container.NodeID.Host))

	// 启动容器
	go am.launchContainer(container)
}

// handleCompletedContainer 处理已完成的容器
func (am *ApplicationMaster) handleCompletedContainer(container *common.Container) {
	containerKey := am.getContainerKey(container.ID)

	if container.Status == "COMPLETE" {
		am.completedContainers[containerKey] = container
		am.logger.Info("Container completed successfully",
			zap.String("container_id", containerKey))
	} else {
		am.failedContainers[containerKey] = container
		am.logger.Error("Container failed",
			zap.String("container_id", containerKey),
			zap.String("status", container.Status))
	}

	// 从分配的容器中移除
	delete(am.allocatedContainers, containerKey)
}

// launchContainer 启动容器
func (am *ApplicationMaster) launchContainer(container *common.Container) {
	nodeKey := am.getNodeKey(container.NodeID)

	// 获取或创建 NodeManager 客户端
	nmClient, err := am.getNMClient(container.NodeID)
	if err != nil {
		am.logger.Error("Failed to get NodeManager client", zap.Error(err))
		return
	}

	// 创建容器启动上下文
	launchContext := &common.ContainerLaunchContext{
		Commands:    []string{"echo 'Hello from container'", "sleep 30"},
		Environment: make(map[string]string),
	}

	// 启动容器
	err = nmClient.StartContainer(container.ID, launchContext)
	if err != nil {
		am.logger.Error("Failed to start container",
			zap.String("container_id", am.getContainerKey(container.ID)),
			zap.Error(err))
		return
	}

	am.logger.Info("Container started successfully",
		zap.String("container_id", am.getContainerKey(container.ID)),
		zap.String("node", nodeKey))
}

// RequestContainers 请求容器
func (am *ApplicationMaster) RequestContainers(requests []*common.ContainerRequest) {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.pendingRequests = append(am.pendingRequests, requests...)

	am.logger.Info("Added container requests",
		zap.Int("count", len(requests)))
}

// ReleaseContainer 释放容器
func (am *ApplicationMaster) ReleaseContainer(containerID common.ContainerID) error {
	containerKey := am.getContainerKey(containerID)

	am.mu.Lock()
	container, exists := am.allocatedContainers[containerKey]
	if !exists {
		am.mu.Unlock()
		return fmt.Errorf("container %s not found", containerKey)
	}
	delete(am.allocatedContainers, containerKey)
	am.mu.Unlock()

	// 停止容器
	nmClient, err := am.getNMClient(container.NodeID)
	if err != nil {
		return err
	}

	return nmClient.StopContainer(containerID)
}

// releaseAllContainers 释放所有容器
func (am *ApplicationMaster) releaseAllContainers() {
	am.mu.Lock()
	defer am.mu.Unlock()

	for containerKey, container := range am.allocatedContainers {
		nmClient, err := am.getNMClient(container.NodeID)
		if err != nil {
			am.logger.Error("Failed to get NodeManager client for container release",
				zap.String("container_id", containerKey),
				zap.Error(err))
			continue
		}

		if err := nmClient.StopContainer(container.ID); err != nil {
			am.logger.Error("Failed to stop container",
				zap.String("container_id", containerKey),
				zap.Error(err))
		}
	}

	am.allocatedContainers = make(map[string]*common.Container)
}

// getNMClient 获取 NodeManager 客户端
func (am *ApplicationMaster) getNMClient(nodeID common.NodeID) (*NodeManagerClient, error) {
	nodeKey := am.getNodeKey(nodeID)

	am.mu.Lock()
	defer am.mu.Unlock()

	if client, exists := am.nmClients[nodeKey]; exists {
		return client, nil
	}

	// 创建新的客户端
	address := fmt.Sprintf("http://%s:%d", nodeID.Host, nodeID.Port)
	client := NewNodeManagerClient(address, am.logger)
	am.nmClients[nodeKey] = client

	return client, nil
}

// monitorContainers 监控容器状态
func (am *ApplicationMaster) monitorContainers() {
	ticker := time.NewTicker(10 * time.Second) // 每10秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			am.checkContainerStatus()
		case <-am.ctx.Done():
			return
		case <-am.shutdownHook:
			return
		}
	}
}

// checkContainerStatus 检查容器状态
func (am *ApplicationMaster) checkContainerStatus() {
	am.mu.RLock()
	containers := make([]*common.Container, 0, len(am.allocatedContainers))
	for _, container := range am.allocatedContainers {
		containers = append(containers, container)
	}
	am.mu.RUnlock()

	for _, container := range containers {
		nmClient, err := am.getNMClient(container.NodeID)
		if err != nil {
			continue
		}

		status, err := nmClient.GetContainerStatus(container.ID)
		if err != nil {
			am.logger.Error("Failed to get container status",
				zap.String("container_id", am.getContainerKey(container.ID)),
				zap.Error(err))
			continue
		}

		// 更新容器状态
		if status.State != container.State {
			am.updateContainerState(container.ID, status.State)
		}
	}
}

// updateContainerState 更新容器状态
func (am *ApplicationMaster) updateContainerState(containerID common.ContainerID, newState string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	containerKey := am.getContainerKey(containerID)
	if container, exists := am.allocatedContainers[containerKey]; exists {
		container.State = newState

		// 如果容器完成，移动到完成列表
		if newState == common.ContainerStateComplete {
			am.completedContainers[containerKey] = container
			delete(am.allocatedContainers, containerKey)
		}
	}
}

// runApplicationLogic 运行应用程序逻辑
func (am *ApplicationMaster) runApplicationLogic() {
	am.logger.Info("Starting application logic")

	// 示例：请求一些容器
	requests := []*common.ContainerRequest{
		{
			Resource: common.Resource{Memory: 1024, VCores: 1},
			Priority: 1,
		},
		{
			Resource: common.Resource{Memory: 2048, VCores: 2},
			Priority: 1,
		},
	}

	am.RequestContainers(requests)

	// 等待应用程序完成
	am.waitForCompletion()

	// 设置最终状态
	am.mu.Lock()
	am.applicationState = common.ApplicationStateFinished
	am.finalStatus = common.FinalApplicationStatusSucceeded
	am.progress = 1.0
	am.mu.Unlock()

	// 发送停止信号
	am.shutdownHook <- struct{}{}
}

// waitForCompletion 等待应用程序完成
func (am *ApplicationMaster) waitForCompletion() {
	// 简单的完成逻辑：等待所有容器完成
	timeout := time.After(5 * time.Minute) // 5分钟超时
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			am.logger.Warn("Application timeout")
			return
		case <-ticker.C:
			am.mu.RLock()
			totalContainers := len(am.allocatedContainers) + len(am.completedContainers) + len(am.failedContainers)
			completedContainers := len(am.completedContainers)
			am.progress = float32(completedContainers) / float32(max(totalContainers, 1))
			am.mu.RUnlock()

			if len(am.allocatedContainers) == 0 && totalContainers > 0 {
				am.logger.Info("All containers completed")
				return
			}
		case <-am.ctx.Done():
			return
		}
	}
}

// getContainerKey 获取容器键
func (am *ApplicationMaster) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d", containerID.ContainerID)
}

// getNodeKey 获取节点键
func (am *ApplicationMaster) getNodeKey(nodeID common.NodeID) string {
	return fmt.Sprintf("%s:%d", nodeID.Host, nodeID.Port)
}

// GetProgress 获取应用程序进度
func (am *ApplicationMaster) GetProgress() float32 {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.progress
}

// GetState 获取应用程序状态
func (am *ApplicationMaster) GetState() string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.applicationState
}

// GetContainerStatistics 获取容器统计信息
func (am *ApplicationMaster) GetContainerStatistics() map[string]int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return map[string]int{
		"allocated": len(am.allocatedContainers),
		"completed": len(am.completedContainers),
		"failed":    len(am.failedContainers),
		"pending":   len(am.pendingRequests),
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
