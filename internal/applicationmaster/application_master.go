package applicationmaster

import (
	"context"
	"fmt"
	"sync"
	"time"

	ampb "carrot/api/proto/applicationmaster"
	"carrot/internal/common"

	"go.uber.org/zap"
)

// ApplicationMaster 应用程序主控
type ApplicationMaster struct {
	mu                   sync.RWMutex
	applicationID        common.ApplicationID
	applicationAttemptID common.ApplicationAttemptID
	rmGRPCClient         *ApplicationMasterGRPCClient
	// nmClients            map[string]*ContainerManagerGRPCClient // TODO: 重新实现容器管理gRPC客户端
	containers          map[string]*common.Container
	pendingRequests     []*common.ContainerRequest
	allocatedContainers map[string]*common.Container
	completedContainers map[string]*common.Container
	failedContainers    map[string]*common.Container
	applicationState    string
	finalStatus         string
	progress            float32
	trackingURL         string
	config              *common.Config
	logger              *zap.Logger
	ctx                 context.Context
	cancel              context.CancelFunc
	heartbeatInterval   time.Duration
	maxContainerRetries int
	shutdownHook        chan struct{}
	useGRPC             bool
}

// ApplicationMasterConfig ApplicationMaster 配置
type ApplicationMasterConfig struct {
	ApplicationID        common.ApplicationID
	ApplicationAttemptID common.ApplicationAttemptID
	RMAddress            string
	RMGRPCAddress        string
	TrackingURL          string
	HeartbeatInterval    time.Duration
	MaxContainerRetries  int
	Port                 int
	UseGRPC              bool
}

// NewApplicationMaster 创建新的 ApplicationMaster
func NewApplicationMaster(config *ApplicationMasterConfig) *ApplicationMaster {
	ctx, cancel := context.WithCancel(context.Background())

	am := &ApplicationMaster{
		applicationID:        config.ApplicationID,
		applicationAttemptID: config.ApplicationAttemptID,
		// nmClients:            make(map[string]*ContainerManagerGRPCClient), // TODO: 重新实现
		containers:          make(map[string]*common.Container),
		pendingRequests:     make([]*common.ContainerRequest, 0),
		allocatedContainers: make(map[string]*common.Container),
		completedContainers: make(map[string]*common.Container),
		failedContainers:    make(map[string]*common.Container),
		applicationState:    common.ApplicationStateRunning,
		finalStatus:         common.FinalApplicationStatusUndefined,
		progress:            0.0,
		trackingURL:         config.TrackingURL,
		config:              common.GetDefaultConfig(),
		logger:              common.ComponentLogger("application-master"),
		ctx:                 ctx,
		cancel:              cancel,
		heartbeatInterval:   config.HeartbeatInterval,
		maxContainerRetries: config.MaxContainerRetries,
		shutdownHook:        make(chan struct{}, 1),
	}

	// 创建 gRPC 客户端
	if config.RMGRPCAddress != "" {
		am.rmGRPCClient = NewApplicationMasterGRPCClient(config.ApplicationID, config.RMGRPCAddress)
		am.useGRPC = true
	} else {
		am.logger.Error("ResourceManager gRPC address is required")
		am.useGRPC = false
	}

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

	// 断开gRPC连接
	if am.rmGRPCClient != nil {
		if err := am.rmGRPCClient.Disconnect(); err != nil {
			am.logger.Error("Failed to disconnect gRPC client", zap.Error(err))
		}
	}

	am.logger.Info("ApplicationMaster stopped")
	return nil
}

// registerWithRM 向 ResourceManager 注册
func (am *ApplicationMaster) registerWithRM() error {
	if am.useGRPC && am.rmGRPCClient != nil {
		// 使用gRPC注册
		if err := am.rmGRPCClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to ResourceManager gRPC: %w", err)
		}

		_, err := am.rmGRPCClient.RegisterApplicationMaster("localhost", 0, am.trackingURL)
		if err != nil {
			return fmt.Errorf("failed to register via gRPC: %w", err)
		}

		am.logger.Info("Registered with ResourceManager via gRPC")
		return nil
	}

	// 使用gRPC注册
	response, err := am.rmGRPCClient.RegisterApplicationMaster("localhost", 0, am.trackingURL)
	if err != nil {
		return err
	}

	am.logger.Info("Registered with ResourceManager",
		zap.String("queue", response.Queue),
		zap.Int64("max_memory", response.MaximumResourceCapability.MemoryMb),
		zap.Int32("max_vcores", response.MaximumResourceCapability.Vcores))

	return nil
}

// unregisterFromRM 从 ResourceManager 注销
func (am *ApplicationMaster) unregisterFromRM() error {
	// 使用gRPC注销
	_, err := am.rmGRPCClient.FinishApplicationMaster(am.finalStatus, "Application completed successfully", am.trackingURL)
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

	var response *ampb.AllocateResponse
	var err error

	// 使用gRPC发送心跳
	response, err = am.rmGRPCClient.Allocate(am.pendingRequests, []common.ContainerID{}, completedContainers, am.progress)

	if err != nil {
		return err
	}

	// 处理新分配的容器
	for _, pbContainer := range response.AllocatedContainers {
		// 转换 protobuf 容器为内部类型
		container := &common.Container{
			ID: common.ContainerID{
				ApplicationAttemptID: common.ApplicationAttemptID{
					ApplicationID: common.ApplicationID{
						ClusterTimestamp: am.applicationID.ClusterTimestamp,
					},
					AttemptID: am.applicationAttemptID.AttemptID,
				},
				ContainerID: pbContainer.Id.ContainerId,
			},
			NodeID: common.NodeID{
				Host: pbContainer.NodeId.Host,
				Port: pbContainer.NodeId.Port,
			},
			Resource: common.Resource{
				Memory: pbContainer.Resource.MemoryMb,
				VCores: pbContainer.Resource.Vcores,
			},
		}
		am.handleNewContainer(container)
	}

	// 处理已完成的容器
	for _, pbStatus := range response.CompletedContainers {
		// 转换 protobuf 容器状态为内部类型
		container := &common.Container{
			ID: common.ContainerID{
				ApplicationAttemptID: common.ApplicationAttemptID{
					ApplicationID: common.ApplicationID{
						ClusterTimestamp: am.applicationID.ClusterTimestamp,
					},
					AttemptID: am.applicationAttemptID.AttemptID,
				},
				ContainerID: pbStatus.ContainerId.ContainerId,
			},
			Status: pbStatus.Diagnostics,
			State:  fmt.Sprintf("%d", pbStatus.State),
		}
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

// launchContainer 启动容器 (TODO: 重新实现使用 gRPC)
func (am *ApplicationMaster) launchContainer(container *common.Container) {
	am.logger.Info("Container launch requested but not implemented in gRPC mode",
		zap.String("container_id", am.getContainerKey(container.ID)),
		zap.String("node_id", fmt.Sprintf("%s:%d", container.NodeID.Host, container.NodeID.Port)))

	// TODO: 实现 gRPC 容器启动
}

// RequestContainers 请求容器
func (am *ApplicationMaster) RequestContainers(requests []*common.ContainerRequest) {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.pendingRequests = append(am.pendingRequests, requests...)

	am.logger.Info("Added container requests",
		zap.Int("count", len(requests)))
}

// ReleaseContainer 释放容器 (TODO: 重新实现使用 gRPC)
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

	am.logger.Info("Container release requested but not implemented in gRPC mode",
		zap.String("container_id", containerKey))

	// TODO: 实现 gRPC 容器释放
	_ = container // 避免未使用变量警告
	return nil
}

// releaseAllContainers 释放所有容器 (TODO: 重新实现使用 gRPC)
func (am *ApplicationMaster) releaseAllContainers() {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.logger.Info("Release all containers requested but not implemented in gRPC mode",
		zap.Int("container_count", len(am.allocatedContainers)))

	// TODO: 实现 gRPC 容器批量释放
	am.allocatedContainers = make(map[string]*common.Container)
}

/* TODO: 重新实现容器管理功能，使用 gRPC
// getNMClient 获取 NodeManager 客户端
func (am *ApplicationMaster) getNMClient(nodeID common.NodeID) (*ContainerManagerGRPCClient, error) {
	nodeKey := am.getNodeKey(nodeID)

	am.mu.Lock()
	defer am.mu.Unlock()

	if client, exists := am.nmClients[nodeKey]; exists {
		return client, nil
	}

	// 创建新的 gRPC 客户端
	address := fmt.Sprintf("%s:%d", nodeID.Host, nodeID.Port+1000) // gRPC端口
	client, err := NewContainerManagerGRPCClient(address)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %v", err)
	}

	am.nmClients[nodeKey] = client
	return client, nil
}
*/

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

// checkContainerStatus 检查容器状态 (TODO: 重新实现使用 gRPC)
func (am *ApplicationMaster) checkContainerStatus() {
	am.mu.RLock()
	containerCount := len(am.allocatedContainers)
	am.mu.RUnlock()

	am.logger.Debug("Container status check requested but not implemented in gRPC mode",
		zap.Int("container_count", containerCount))

	// TODO: 实现 gRPC 容器状态检查
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
