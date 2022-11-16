package cluster

import (
	"context"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ClusterManager 集群管理器
type ClusterManager struct {
	config      common.ClusterConfig
	logger      *zap.Logger
	localNode   *common.ClusterNode
	clusterInfo *common.ClusterInfo
	nodes       map[string]*common.ClusterNode
	nodesMutex  sync.RWMutex
	eventChan   chan common.ClusterEvent
	stopChan    chan struct{}

	// 组件
	discovery     Discovery
	election      Election
	healthChecker HealthChecker
	eventHandler  EventHandler

	// 回调函数
	onNodeJoined   []func(*common.ClusterNode)
	onNodeLeft     []func(*common.ClusterNode)
	onLeaderChange []func(*common.ClusterNode, *common.ClusterNode)
}

// NewClusterManager 创建新的集群管理器
func NewClusterManager(config common.ClusterConfig, localNode *common.ClusterNode, logger *zap.Logger) *ClusterManager {
	cm := &ClusterManager{
		config:         config,
		logger:         logger,
		localNode:      localNode,
		nodes:          make(map[string]*common.ClusterNode),
		eventChan:      make(chan common.ClusterEvent, 100),
		stopChan:       make(chan struct{}),
		onNodeJoined:   make([]func(*common.ClusterNode), 0),
		onNodeLeft:     make([]func(*common.ClusterNode), 0),
		onLeaderChange: make([]func(*common.ClusterNode, *common.ClusterNode), 0),
	}

	// 初始化集群信息
	cm.clusterInfo = &common.ClusterInfo{
		ID: common.ClusterID{
			Name: config.Name,
			ID:   config.ID,
		},
		State:      common.ClusterStateForming,
		Nodes:      make(map[string]common.ClusterNode),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Config:     config,
		Statistics: common.ClusterStatistics{},
	}

	// 注册本地节点
	cm.registerLocalNode()

	return cm
}

// Start 启动集群管理器
func (cm *ClusterManager) Start(ctx context.Context) error {
	cm.logger.Info("Starting cluster manager",
		zap.String("cluster", cm.config.Name),
		zap.String("node", cm.localNode.ID.String()))

	// 初始化组件
	if err := cm.initializeComponents(); err != nil {
		return err
	}

	// 启动服务发现
	if err := cm.discovery.Start(ctx); err != nil {
		return err
	}

	// 启动健康检查
	if err := cm.healthChecker.Start(ctx); err != nil {
		return err
	}

	// 启动事件处理
	go cm.handleEvents(ctx)

	// 启动定期任务
	go cm.periodicTasks(ctx)

	cm.logger.Info("Cluster manager started successfully")
	return nil
}

// Stop 停止集群管理器
func (cm *ClusterManager) Stop() error {
	cm.logger.Info("Stopping cluster manager")

	close(cm.stopChan)

	// 停止组件
	if cm.discovery != nil {
		cm.discovery.Stop()
	}
	if cm.healthChecker != nil {
		cm.healthChecker.Stop()
	}
	if cm.election != nil {
		cm.election.Stop()
	}

	// 通知集群节点离开
	cm.publishEvent(common.ClusterEvent{
		Type:      common.ClusterEventNodeLeft,
		Source:    cm.localNode.ID,
		Timestamp: time.Now(),
		Severity:  common.EventSeverityInfo,
	})

	cm.logger.Info("Cluster manager stopped")
	return nil
}

// GetClusterInfo 获取集群信息
func (cm *ClusterManager) GetClusterInfo() *common.ClusterInfo {
	cm.nodesMutex.RLock()
	defer cm.nodesMutex.RUnlock()

	// 更新统计信息
	cm.updateStatistics()

	// 复制当前状态
	info := *cm.clusterInfo
	info.Nodes = make(map[string]common.ClusterNode)
	for id, node := range cm.nodes {
		info.Nodes[id] = *node
	}

	return &info
}

// GetNodes 获取所有节点
func (cm *ClusterManager) GetNodes() map[string]*common.ClusterNode {
	cm.nodesMutex.RLock()
	defer cm.nodesMutex.RUnlock()

	nodes := make(map[string]*common.ClusterNode)
	for id, node := range cm.nodes {
		nodeCopy := *node
		nodes[id] = &nodeCopy
	}
	return nodes
}

// GetNode 获取指定节点
func (cm *ClusterManager) GetNode(nodeID string) (*common.ClusterNode, bool) {
	cm.nodesMutex.RLock()
	defer cm.nodesMutex.RUnlock()

	node, exists := cm.nodes[nodeID]
	if !exists {
		return nil, false
	}

	nodeCopy := *node
	return &nodeCopy, true
}

// GetLeader 获取领导者节点
func (cm *ClusterManager) GetLeader() (*common.ClusterNode, bool) {
	if cm.clusterInfo.Leader == nil {
		return nil, false
	}

	return cm.GetNode(cm.clusterInfo.Leader.String())
}

// IsLeader 检查本地节点是否为领导者
func (cm *ClusterManager) IsLeader() bool {
	leader, exists := cm.GetLeader()
	if !exists {
		return false
	}
	return leader.ID.String() == cm.localNode.ID.String()
}

// JoinCluster 加入集群
func (cm *ClusterManager) JoinCluster() error {
	cm.logger.Info("Joining cluster", zap.String("cluster", cm.config.Name))

	// 发现其他节点
	nodes, err := cm.discovery.DiscoverNodes()
	if err != nil {
		return err
	}

	// 注册已发现的节点
	for _, node := range nodes {
		cm.addNode(&node)
	}

	// 更新集群状态
	cm.updateClusterState()

	// 如果需要选举领导者
	if cm.shouldStartElection() {
		return cm.election.StartElection()
	}

	return nil
}

// LeaveCluster 离开集群
func (cm *ClusterManager) LeaveCluster() error {
	cm.logger.Info("Leaving cluster")

	// 如果是领导者，先放弃领导权
	if cm.IsLeader() {
		cm.election.StepDown()
	}

	// 通知其他节点
	cm.publishEvent(common.ClusterEvent{
		Type:      common.ClusterEventNodeLeft,
		Source:    cm.localNode.ID,
		Timestamp: time.Now(),
		Severity:  common.EventSeverityInfo,
	})

	return nil
}

// OnNodeJoined 注册节点加入回调
func (cm *ClusterManager) OnNodeJoined(callback func(*common.ClusterNode)) {
	cm.onNodeJoined = append(cm.onNodeJoined, callback)
}

// OnNodeLeft 注册节点离开回调
func (cm *ClusterManager) OnNodeLeft(callback func(*common.ClusterNode)) {
	cm.onNodeLeft = append(cm.onNodeLeft, callback)
}

// OnLeaderChange 注册领导者变更回调
func (cm *ClusterManager) OnLeaderChange(callback func(old, new *common.ClusterNode)) {
	cm.onLeaderChange = append(cm.onLeaderChange, callback)
}

// 私有方法

func (cm *ClusterManager) registerLocalNode() {
	cm.localNode.State = common.NodeStateJoining
	cm.localNode.JoinTime = time.Now()
	cm.localNode.LastHeartbeat = time.Now()

	cm.nodesMutex.Lock()
	cm.nodes[cm.localNode.ID.String()] = cm.localNode
	cm.nodesMutex.Unlock()
}

func (cm *ClusterManager) initializeComponents() error {
	var err error

	// 初始化服务发现
	cm.discovery, err = NewDiscovery(cm.config, cm.localNode, cm.logger)
	if err != nil {
		return err
	}

	// 初始化选举机制
	cm.election, err = NewElection(cm.config, cm.localNode, cm, cm.logger)
	if err != nil {
		return err
	}

	// 初始化健康检查
	cm.healthChecker, err = NewHealthChecker(cm.config, cm.localNode, cm, cm.logger)
	if err != nil {
		return err
	}

	// 初始化事件处理器
	cm.eventHandler, err = NewEventHandler(cm.config, cm.logger)
	if err != nil {
		return err
	}

	return nil
}

func (cm *ClusterManager) handleEvents(ctx context.Context) {
	for {
		select {
		case event := <-cm.eventChan:
			cm.processEvent(event)
		case <-ctx.Done():
			return
		case <-cm.stopChan:
			return
		}
	}
}

func (cm *ClusterManager) processEvent(event common.ClusterEvent) {
	cm.logger.Debug("Processing cluster event",
		zap.String("type", string(event.Type)),
		zap.String("source", event.Source.String()))

	switch event.Type {
	case common.ClusterEventNodeJoined:
		cm.handleNodeJoined(event)
	case common.ClusterEventNodeLeft:
		cm.handleNodeLeft(event)
	case common.ClusterEventNodeFailed:
		cm.handleNodeFailed(event)
	case common.ClusterEventLeaderElected:
		cm.handleLeaderElected(event)
	}

	// 委托给事件处理器
	if cm.eventHandler != nil {
		cm.eventHandler.HandleEvent(event)
	}
}

func (cm *ClusterManager) handleNodeJoined(event common.ClusterEvent) {
	// 实现节点加入逻辑
	for _, callback := range cm.onNodeJoined {
		if node, exists := cm.GetNode(event.Source.String()); exists {
			callback(node)
		}
	}
}

func (cm *ClusterManager) handleNodeLeft(event common.ClusterEvent) {
	nodeID := event.Source.String()

	cm.nodesMutex.Lock()
	node, exists := cm.nodes[nodeID]
	if exists {
		delete(cm.nodes, nodeID)
	}
	cm.nodesMutex.Unlock()

	if exists {
		for _, callback := range cm.onNodeLeft {
			callback(node)
		}
	}
}

func (cm *ClusterManager) handleNodeFailed(event common.ClusterEvent) {
	nodeID := event.Source.String()

	cm.nodesMutex.Lock()
	if node, exists := cm.nodes[nodeID]; exists {
		node.State = common.NodeStateFailed
		node.Health.Status = common.HealthStatusCritical
	}
	cm.nodesMutex.Unlock()

	// 如果失败的是领导者，触发重新选举
	if cm.clusterInfo.Leader != nil && cm.clusterInfo.Leader.String() == nodeID {
		cm.election.StartElection()
	}
}

func (cm *ClusterManager) handleLeaderElected(event common.ClusterEvent) {
	oldLeader := cm.clusterInfo.Leader
	cm.clusterInfo.Leader = &event.Source

	// 通知回调
	for _, callback := range cm.onLeaderChange {
		var oldNode, newNode *common.ClusterNode
		if oldLeader != nil {
			oldNode, _ = cm.GetNode(oldLeader.String())
		}
		newNode, _ = cm.GetNode(event.Source.String())
		callback(oldNode, newNode)
	}
}

func (cm *ClusterManager) addNode(node *common.ClusterNode) {
	nodeID := node.ID.String()

	cm.nodesMutex.Lock()
	cm.nodes[nodeID] = node
	cm.nodesMutex.Unlock()

	cm.publishEvent(common.ClusterEvent{
		Type:      common.ClusterEventNodeJoined,
		Source:    node.ID,
		Timestamp: time.Now(),
		Severity:  common.EventSeverityInfo,
	})
}

func (cm *ClusterManager) publishEvent(event common.ClusterEvent) {
	select {
	case cm.eventChan <- event:
	default:
		cm.logger.Warn("Event channel full, dropping event",
			zap.String("type", string(event.Type)))
	}
}

func (cm *ClusterManager) periodicTasks(ctx context.Context) {
	ticker := time.NewTicker(cm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.updateHeartbeat()
			cm.updateClusterState()
		case <-ctx.Done():
			return
		case <-cm.stopChan:
			return
		}
	}
}

func (cm *ClusterManager) updateHeartbeat() {
	cm.localNode.LastHeartbeat = time.Now()
}

func (cm *ClusterManager) updateClusterState() {
	cm.nodesMutex.Lock()
	defer cm.nodesMutex.Unlock()

	activeNodes := 0
	failedNodes := 0

	for _, node := range cm.nodes {
		switch node.State {
		case common.NodeStateActive:
			activeNodes++
		case common.NodeStateFailed:
			failedNodes++
		}
	}

	// 更新集群状态
	if activeNodes == 0 {
		cm.clusterInfo.State = common.ClusterStateFailed
	} else if failedNodes > 0 {
		cm.clusterInfo.State = common.ClusterStateDegraded
	} else {
		cm.clusterInfo.State = common.ClusterStateActive
	}

	cm.clusterInfo.UpdatedAt = time.Now()
}

func (cm *ClusterManager) updateStatistics() {
	totalNodes := int32(len(cm.nodes))
	activeNodes := int32(0)
	failedNodes := int32(0)

	for _, node := range cm.nodes {
		switch node.State {
		case common.NodeStateActive:
			activeNodes++
		case common.NodeStateFailed:
			failedNodes++
		}
	}

	cm.clusterInfo.Statistics.TotalNodes = totalNodes
	cm.clusterInfo.Statistics.ActiveNodes = activeNodes
	cm.clusterInfo.Statistics.FailedNodes = failedNodes
}

func (cm *ClusterManager) shouldStartElection() bool {
	return cm.clusterInfo.Leader == nil && len(cm.nodes) >= cm.config.MinNodes
}
