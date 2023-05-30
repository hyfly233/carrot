package resourcemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"
	"carrot/internal/resourcemanager/scheduler"

	"go.uber.org/zap"
)

// RefactoredResourceManager 重构后的资源管理器，使用继承式设计
type RefactoredResourceManager struct {
	mu               sync.RWMutex
	applications     map[string]*common.ExtendedApplication
	nodes            map[string]*common.ExtendedNode
	scheduler        scheduler.Scheduler
	converter        *common.TypeConverter
	appIDCounter     int32
	clusterTimestamp int64
	logger           *zap.Logger
	ctx              context.Context
	cancel           context.CancelFunc

	// 资源统计
	totalResources     common.ResourceInterface
	usedResources      common.ResourceInterface
	availableResources common.ResourceInterface

	// 事件处理
	eventHandlers map[string]func(interface{})
}

// NewRefactoredResourceManager 创建重构后的资源管理器
func NewRefactoredResourceManager(logger *zap.Logger) *RefactoredResourceManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &RefactoredResourceManager{
		applications:       make(map[string]*common.ExtendedApplication),
		nodes:              make(map[string]*common.ExtendedNode),
		converter:          common.NewTypeConverter(),
		logger:             logger,
		ctx:                ctx,
		cancel:             cancel,
		clusterTimestamp:   time.Now().Unix(),
		totalResources:     common.NewResource(0, 0),
		usedResources:      common.NewResource(0, 0),
		availableResources: common.NewResource(0, 0),
		eventHandlers:      make(map[string]func(interface{})),
	}
}

// RegisterNode 注册节点（使用新的继承式设计）
func (rm *RefactoredResourceManager) RegisterNode(nodeID common.NodeID, totalResource common.ResourceInterface, httpAddress string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 使用构建器模式创建节点
	node := common.BuildNode().
		WithID(nodeID.Host, nodeID.Port).
		WithTotalResource(totalResource).
		WithCapability("http_address", httpAddress).
		Build()

	nodeKey := rm.getNodeKey(nodeID)
	rm.nodes[nodeKey] = node

	// 更新集群资源统计
	rm.updateClusterResources()

	rm.logger.Info("节点注册成功",
		zap.String("node_id", nodeID.HostPortString()),
		zap.String("resource", totalResource.String()),
		zap.String("http_address", httpAddress))

	// 触发节点注册事件
	rm.triggerEvent("node_registered", node)

	return nil
}

// SubmitApplication 提交应用程序（使用新的继承式设计）
func (rm *RefactoredResourceManager) SubmitApplication(ctx common.ApplicationSubmissionContext) (*common.ApplicationID, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 生成新的应用程序ID
	rm.appIDCounter++
	appID := common.ApplicationID{
		ClusterTimestamp: rm.clusterTimestamp,
		ID:               rm.appIDCounter,
	}

	// 创建扩展应用程序
	app := common.NewTypeConverter().ConvertToExtendedApplication(
		common.NewTypeConverter().ConvertToBaseApplication(appID, ctx.ApplicationName, common.ApplicationStateSubmitted, rm.getUserFromContext(ctx)),
		ctx.ApplicationType,
		ctx.Queue,
		ctx.Priority,
		rm.converter.ConvertFromOldResource(ctx.Resource.Memory, ctx.Resource.VCores),
	)

	rm.applications[appID.String()] = app

	rm.logger.Info("应用程序提交成功",
		zap.String("application_id", appID.String()),
		zap.String("name", ctx.ApplicationName),
		zap.String("type", ctx.ApplicationType),
		zap.String("user", app.GetUser()))

	// 触发应用程序提交事件
	rm.triggerEvent("application_submitted", app)

	// 异步调度应用程序
	go rm.scheduleApplicationAsync(app)

	return &appID, nil
}

// AllocateContainers 分配容器（使用新的继承式设计）
func (rm *RefactoredResourceManager) AllocateContainers(
	appID common.ApplicationID,
	asks []*common.ContainerRequest,
	releases []common.ContainerID,
	completed []*common.Container,
	progress float32) ([]*common.Container, []common.NodeReport, error) {

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 查找应用程序
	app, exists := rm.applications[appID.String()]
	if !exists {
		return nil, nil, fmt.Errorf("应用未找到: %s", appID.String())
	}

	// 更新应用程序进度
	app.Progress = progress

	var allocatedContainers []*common.Container
	var updatedNodes []common.NodeReport

	// 处理容器请求
	for _, ask := range asks {
		requestedResource := rm.converter.ConvertFromOldResource(ask.Resource.Memory, ask.Resource.VCores)

		// 查找合适的节点
		selectedNode := rm.findSuitableNode(requestedResource, ask.NodeLabel)
		if selectedNode == nil {
			rm.logger.Warn("没有找到合适的节点",
				zap.String("resource", requestedResource.String()),
				zap.String("node_label", ask.NodeLabel))
			continue
		}

		// 创建容器
		container := rm.createContainer(appID, selectedNode, requestedResource)
		allocatedContainers = append(allocatedContainers, container)

		// 更新节点资源
		selectedNode.AvailableResource = selectedNode.AvailableResource.Subtract(requestedResource)

		rm.logger.Info("容器分配成功",
			zap.String("container_id", fmt.Sprintf("%d", container.ID.ContainerID)),
			zap.String("node_id", selectedNode.GetAddress()),
			zap.String("resource", requestedResource.String()))
	}

	// 处理释放的容器
	for _, releaseID := range releases {
		rm.releaseContainer(releaseID)
	}

	// 生成节点报告
	for _, node := range rm.nodes {
		nodeReport := rm.converter.ConvertToNodeReport(node)
		updatedNodes = append(updatedNodes, *nodeReport)
	}

	// 更新集群资源统计
	rm.updateClusterResources()

	// 触发容器分配事件
	rm.triggerEvent("containers_allocated", map[string]interface{}{
		"app_id":    appID,
		"allocated": len(allocatedContainers),
		"released":  len(releases),
	})

	return allocatedContainers, updatedNodes, nil
}

// NodeHeartbeat 处理节点心跳（使用新的继承式设计）
func (rm *RefactoredResourceManager) NodeHeartbeat(nodeID common.NodeID, usedResource common.Resource, containers []*common.Container) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	nodeKey := rm.getNodeKey(nodeID)
	node, exists := rm.nodes[nodeKey]
	if !exists {
		return fmt.Errorf("节点未注册: %s", nodeID.HostPortString())
	}

	// 更新节点状态
	node.LastHeartbeat = time.Now()
	usedResourceInterface := rm.converter.ConvertFromOldResource(usedResource.Memory, usedResource.VCores)
	node.AvailableResource = node.TotalResource.Subtract(usedResourceInterface)

	// 更新容器信息（这里可以进一步优化）
	// node.Containers = containers

	rm.logger.Debug("收到节点心跳",
		zap.String("node_id", nodeID.HostPortString()),
		zap.String("used_resource", usedResourceInterface.String()),
		zap.Int("containers", len(containers)))

	// 检查节点健康状态
	if !node.IsHealthy() {
		rm.logger.Warn("节点不健康", zap.String("node_id", nodeID.HostPortString()))
		rm.triggerEvent("node_unhealthy", node)
	}

	return nil
}

// GetApplications 获取应用程序列表（转换为报告格式）
func (rm *RefactoredResourceManager) GetApplications() []*common.ApplicationReport {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	reports := make([]*common.ApplicationReport, 0, len(rm.applications))
	for _, app := range rm.applications {
		report := rm.converter.ConvertToApplicationReport(app)
		reports = append(reports, report)
	}

	return reports
}

// GetNodes 获取节点列表（转换为报告格式）
func (rm *RefactoredResourceManager) GetNodes() []*common.NodeReport {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	reports := make([]*common.NodeReport, 0, len(rm.nodes))
	for _, node := range rm.nodes {
		report := rm.converter.ConvertToNodeReport(node)
		reports = append(reports, report)
	}

	return reports
}

// GetClusterMetrics 获取集群指标（使用新的资源统计）
func (rm *RefactoredResourceManager) GetClusterMetrics() common.ClusterMetrics {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	runningApps := 0
	pendingApps := 0
	completedApps := 0
	failedApps := 0

	for _, app := range rm.applications {
		switch app.GetState() {
		case common.ApplicationStateRunning:
			runningApps++
		case common.ApplicationStateSubmitted:
			pendingApps++
		case common.ApplicationStateFinished:
			completedApps++
		case common.ApplicationStateFailed, common.ApplicationStateKilled:
			failedApps++
		}
	}

	healthyNodes := 0
	for _, node := range rm.nodes {
		if node.IsHealthy() {
			healthyNodes++
		}
	}

	return common.ClusterMetrics{
		AppsRunning:           runningApps,
		AppsPending:           pendingApps,
		AppsCompleted:         completedApps,
		AppsFailed:            failedApps,
		ActiveNodes:           healthyNodes,
		TotalNodes:            len(rm.nodes),
		AvailableVirtualCores: int(rm.availableResources.GetVCores()),
		AllocatedVirtualCores: int(rm.usedResources.GetVCores()),
	}
}

// 私有方法

func (rm *RefactoredResourceManager) findSuitableNode(resource common.ResourceInterface, nodeLabel string) *common.ExtendedNode {
	for _, node := range rm.nodes {
		if !node.IsHealthy() {
			continue
		}

		// 检查资源是否足够
		if node.AvailableResource.GetMemory() < resource.GetMemory() ||
			node.AvailableResource.GetVCores() < resource.GetVCores() {
			continue
		}

		// 检查标签匹配
		if nodeLabel != "" && !node.HasLabel(nodeLabel) {
			continue
		}

		return node
	}
	return nil
}

func (rm *RefactoredResourceManager) createContainer(appID common.ApplicationID, node *common.ExtendedNode, resource common.ResourceInterface) *common.Container {
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: appID,
			AttemptID:     1,
		},
		ContainerID: time.Now().UnixNano(),
	}

	extendedContainer := rm.converter.ConvertToExtendedContainer(
		rm.converter.ConvertToBaseContainer(containerID, node.GetID(), resource, common.ContainerStateNew),
		"ALLOCATED",
		nil,
	)

	return rm.converter.ConvertToContainer(extendedContainer)
}

func (rm *RefactoredResourceManager) releaseContainer(containerID common.ContainerID) {
	// 这里应该找到容器并释放其资源
	rm.logger.Info("容器已释放", zap.String("container_id", fmt.Sprintf("%d", containerID.ContainerID)))
}

func (rm *RefactoredResourceManager) scheduleApplicationAsync(app *common.ExtendedApplication) {
	// 异步调度逻辑
	time.Sleep(100 * time.Millisecond) // 模拟调度延迟

	rm.mu.Lock()
	app.State = common.ApplicationStateAccepted
	rm.mu.Unlock()

	rm.logger.Info("应用程序调度成功", zap.String("app_id", app.GetID().String()))
	rm.triggerEvent("application_scheduled", app)
}

func (rm *RefactoredResourceManager) updateClusterResources() {
	totalMem := int64(0)
	totalCores := int32(0)
	usedMem := int64(0)
	usedCores := int32(0)

	for _, node := range rm.nodes {
		totalMem += node.TotalResource.GetMemory()
		totalCores += node.TotalResource.GetVCores()
		usedMem += node.TotalResource.GetMemory() - node.AvailableResource.GetMemory()
		usedCores += node.TotalResource.GetVCores() - node.AvailableResource.GetVCores()
	}

	rm.totalResources = common.NewResource(totalMem, totalCores)
	rm.usedResources = common.NewResource(usedMem, usedCores)
	rm.availableResources = common.NewResource(totalMem-usedMem, totalCores-usedCores)
}

func (rm *RefactoredResourceManager) getUserFromContext(ctx common.ApplicationSubmissionContext) string {
	// 从上下文中提取用户信息，这里简化为返回默认用户
	return "default"
}

func (rm *RefactoredResourceManager) getNodeKey(nodeID common.NodeID) string {
	return fmt.Sprintf("%s:%d", nodeID.Host, nodeID.Port)
}

func (rm *RefactoredResourceManager) triggerEvent(eventType string, data interface{}) {
	if handler, exists := rm.eventHandlers[eventType]; exists {
		go handler(data) // 异步处理事件
	}
}

// RegisterEventHandler 注册事件处理器
func (rm *RefactoredResourceManager) RegisterEventHandler(eventType string, handler func(interface{})) {
	rm.eventHandlers[eventType] = handler
}

// Stop 停止资源管理器
func (rm *RefactoredResourceManager) Stop() error {
	rm.cancel()
	rm.logger.Info("资源管理器已停止")
	return nil
}
