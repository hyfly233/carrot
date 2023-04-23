package nodemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// NodeTracker 节点跟踪器
type NodeTracker struct {
	mu               sync.RWMutex
	nodes            map[string]*Node
	expiredNodes     map[string]*Node
	heartbeatTimeout time.Duration
	cleanupInterval  time.Duration
	logger           *zap.Logger
	ctx              context.Context
	cancel           context.CancelFunc

	// 事件通道
	nodeEvents chan *NodeEvent

	// 统计信息
	stats *NodeStatistics
}

// NodeEvent 节点事件
type NodeEvent struct {
	Type      string                 `json:"type"`
	NodeID    common.NodeID          `json:"node_id"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// NodeEventType 节点事件类型
const (
	NodeEventRegistered     = "NODE_REGISTERED"
	NodeEventUnregistered   = "NODE_UNREGISTERED"
	NodeEventHeartbeat      = "NODE_HEARTBEAT"
	NodeEventLost           = "NODE_LOST"
	NodeEventUnhealthy      = "NODE_UNHEALTHY"
	NodeEventHealthy        = "NODE_HEALTHY"
	NodeEventDecommissioned = "NODE_DECOMMISSIONED"
)

// NodeStatistics 节点统计信息
type NodeStatistics struct {
	TotalNodes          int   `json:"total_nodes"`
	ActiveNodes         int   `json:"active_nodes"`
	LostNodes           int   `json:"lost_nodes"`
	UnhealthyNodes      int   `json:"unhealthy_nodes"`
	DecommissionedNodes int   `json:"decommissioned_nodes"`
	TotalMemory         int64 `json:"total_memory"`
	TotalVCores         int32 `json:"total_vcores"`
	UsedMemory          int64 `json:"used_memory"`
	UsedVCores          int32 `json:"used_vcores"`
	AvailableMemory     int64 `json:"available_memory"`
	AvailableVCores     int32 `json:"available_vcores"`
}

// NewNodeTracker 创建新的节点跟踪器
func NewNodeTracker(heartbeatTimeout time.Duration) *NodeTracker {
	ctx, cancel := context.WithCancel(context.Background())

	nt := &NodeTracker{
		nodes:            make(map[string]*Node),
		expiredNodes:     make(map[string]*Node),
		heartbeatTimeout: heartbeatTimeout,
		cleanupInterval:  5 * time.Minute,
		logger:           common.ComponentLogger("node-tracker"),
		ctx:              ctx,
		cancel:           cancel,
		nodeEvents:       make(chan *NodeEvent, 1000),
		stats:            &NodeStatistics{},
	}

	// 启动后台任务
	go nt.nodeHealthChecker()
	go nt.eventProcessor()
	go nt.statisticsUpdater()

	return nt
}

// RegisterNode 注册节点
func (nt *NodeTracker) RegisterNode(nodeID common.NodeID, resource common.Resource, httpAddress string) (*Node, error) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nodeKey := nt.getNodeKey(nodeID)

	// 检查节点是否已注册
	if existingNode, exists := nt.nodes[nodeKey]; exists {
		nt.logger.Warn("Node already registered",
			zap.String("node_id", nodeKey),
			zap.String("existing_state", existingNode.State))
		return existingNode, nil
	}

	// 创建新节点
	node := &Node{
		ID:                 nodeID,
		TotalResource:      resource,
		UsedResource:       common.Resource{Memory: 0, VCores: 0},
		State:              common.NodeStateRunning,
		HTTPAddress:        httpAddress,
		LastHeartbeat:      time.Now(),
		HealthReport:       "Healthy",
		NodeLabels:         make([]string, 0),
		Containers:         make([]*common.Container, 0),
		RegisterTime:       time.Now(),
		NodeManagerVersion: "1.0.0",
	}

	nt.nodes[nodeKey] = node

	nt.logger.Info("节点已注册",
		zap.String("node_id", nodeKey),
		zap.String("http_address", httpAddress),
		zap.Int64("memory", resource.Memory),
		zap.Int32("vcores", resource.VCores))

	// 发送事件
	nt.sendEvent(&NodeEvent{
		Type:      NodeEventRegistered,
		NodeID:    nodeID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"http_address": httpAddress,
			"resource":     resource,
		},
	})

	return node, nil
}

// UnregisterNode 注销节点
func (nt *NodeTracker) UnregisterNode(nodeID common.NodeID, reason string) error {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nodeKey := nt.getNodeKey(nodeID)
	node, exists := nt.nodes[nodeKey]
	if !exists {
		return fmt.Errorf("node %s not found", nodeKey)
	}

	node.State = common.NodeStateDecommissioned
	nt.expiredNodes[nodeKey] = node
	delete(nt.nodes, nodeKey)

	nt.logger.Info("Node unregistered",
		zap.String("node_id", nodeKey),
		zap.String("reason", reason))

	// 发送事件
	nt.sendEvent(&NodeEvent{
		Type:      NodeEventUnregistered,
		NodeID:    nodeID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"reason": reason,
		},
	})

	return nil
}

// NodeHeartbeat 处理节点心跳
func (nt *NodeTracker) NodeHeartbeat(nodeID common.NodeID, usedResource common.Resource, containers []*common.Container) error {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nodeKey := nt.getNodeKey(nodeID)
	node, exists := nt.nodes[nodeKey]
	if !exists {
		return fmt.Errorf("node %s not registered", nodeKey)
	}

	// 更新节点信息
	node.LastHeartbeat = time.Now()
	node.UsedResource = usedResource
	node.Containers = containers

	// 计算可用资源
	node.AvailableResource = common.Resource{
		Memory: node.TotalResource.Memory - node.UsedResource.Memory,
		VCores: node.TotalResource.VCores - node.UsedResource.VCores,
	}

	// 更新健康状态
	if node.State == common.NodeStateUnhealthy && nt.isNodeHealthy(node) {
		node.State = common.NodeStateRunning
		node.HealthReport = "Healthy"

		nt.logger.Info("Node recovered to healthy state",
			zap.String("node_id", nodeKey))

		nt.sendEvent(&NodeEvent{
			Type:      NodeEventHealthy,
			NodeID:    nodeID,
			Timestamp: time.Now(),
		})
	}

	nt.logger.Debug("Node heartbeat received",
		zap.String("node_id", nodeKey),
		zap.Int64("used_memory", usedResource.Memory),
		zap.Int32("used_vcores", usedResource.VCores),
		zap.Int("num_containers", len(containers)))

	// 发送心跳事件
	nt.sendEvent(&NodeEvent{
		Type:      NodeEventHeartbeat,
		NodeID:    nodeID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"used_resource":      usedResource,
			"available_resource": node.AvailableResource,
			"num_containers":     len(containers),
		},
	})

	return nil
}

// GetNode 获取节点
func (nt *NodeTracker) GetNode(nodeID common.NodeID) (*Node, error) {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	nodeKey := nt.getNodeKey(nodeID)
	if node, exists := nt.nodes[nodeKey]; exists {
		return node, nil
	}

	if node, exists := nt.expiredNodes[nodeKey]; exists {
		return node, nil
	}

	return nil, fmt.Errorf("node %s not found", nodeKey)
}

// GetNodes 获取所有节点
func (nt *NodeTracker) GetNodes() []*Node {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	nodes := make([]*Node, 0, len(nt.nodes))
	for _, node := range nt.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetActiveNodes 获取活跃节点
func (nt *NodeTracker) GetActiveNodes() []*Node {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	nodes := make([]*Node, 0)
	for _, node := range nt.nodes {
		if node.State == common.NodeStateRunning {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// GetAvailableNodes 获取可用节点（用于调度）
func (nt *NodeTracker) GetAvailableNodes() []*Node {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	nodes := make([]*Node, 0)
	for _, node := range nt.nodes {
		if node.State == common.NodeStateRunning &&
			node.AvailableResource.Memory > 0 &&
			node.AvailableResource.VCores > 0 {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// UpdateNodeHealth 更新节点健康状态
func (nt *NodeTracker) UpdateNodeHealth(nodeID common.NodeID, isHealthy bool, healthReport string) error {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nodeKey := nt.getNodeKey(nodeID)
	node, exists := nt.nodes[nodeKey]
	if !exists {
		return fmt.Errorf("node %s not found", nodeKey)
	}

	oldState := node.State
	if isHealthy && node.State == common.NodeStateUnhealthy {
		node.State = common.NodeStateRunning
		node.HealthReport = healthReport

		nt.logger.Info("Node marked as healthy",
			zap.String("node_id", nodeKey))

		nt.sendEvent(&NodeEvent{
			Type:      NodeEventHealthy,
			NodeID:    nodeID,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"health_report": healthReport,
				"old_state":     oldState,
			},
		})
	} else if !isHealthy && node.State == common.NodeStateRunning {
		node.State = common.NodeStateUnhealthy
		node.HealthReport = healthReport

		nt.logger.Warn("Node marked as unhealthy",
			zap.String("node_id", nodeKey),
			zap.String("health_report", healthReport))

		nt.sendEvent(&NodeEvent{
			Type:      NodeEventUnhealthy,
			NodeID:    nodeID,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"health_report": healthReport,
				"old_state":     oldState,
			},
		})
	}

	return nil
}

// nodeHealthChecker 节点健康检查器
func (nt *NodeTracker) nodeHealthChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			nt.checkNodeHealth()
		case <-nt.ctx.Done():
			return
		}
	}
}

// checkNodeHealth 检查节点健康状态
func (nt *NodeTracker) checkNodeHealth() {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	now := time.Now()
	lostNodes := make([]*Node, 0)

	for nodeKey, node := range nt.nodes {
		// 检查心跳超时
		if now.Sub(node.LastHeartbeat) > nt.heartbeatTimeout {
			nt.logger.Warn("Node heartbeat timeout",
				zap.String("node_id", nodeKey),
				zap.Duration("timeout", now.Sub(node.LastHeartbeat)))

			node.State = common.NodeStateLost
			lostNodes = append(lostNodes, node)

			nt.sendEvent(&NodeEvent{
				Type:      NodeEventLost,
				NodeID:    node.ID,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"last_heartbeat":   node.LastHeartbeat,
					"timeout_duration": now.Sub(node.LastHeartbeat).String(),
				},
			})
		}
	}

	// 移动丢失的节点到过期列表
	for _, node := range lostNodes {
		nodeKey := nt.getNodeKey(node.ID)
		nt.expiredNodes[nodeKey] = node
		delete(nt.nodes, nodeKey)
	}

	if len(lostNodes) > 0 {
		nt.logger.Info("Moved lost nodes to expired list",
			zap.Int("count", len(lostNodes)))
	}
}

// isNodeHealthy 检查节点是否健康
func (nt *NodeTracker) isNodeHealthy(node *Node) bool {
	// 简单的健康检查逻辑
	// 可以根据实际需要扩展
	return node.HealthReport == "Healthy" || node.HealthReport == ""
}

// eventProcessor 事件处理器
func (nt *NodeTracker) eventProcessor() {
	for {
		select {
		case event := <-nt.nodeEvents:
			nt.logger.Debug("Processing node event",
				zap.String("event_type", event.Type),
				zap.Any("node_id", event.NodeID))
			// 这里可以添加事件处理逻辑
		case <-nt.ctx.Done():
			return
		}
	}
}

// statisticsUpdater 统计信息更新器
func (nt *NodeTracker) statisticsUpdater() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			nt.updateStatistics()
		case <-nt.ctx.Done():
			return
		}
	}
}

// updateStatistics 更新统计信息
func (nt *NodeTracker) updateStatistics() {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	stats := &NodeStatistics{}

	for _, node := range nt.nodes {
		stats.TotalNodes++
		stats.TotalMemory += node.TotalResource.Memory
		stats.TotalVCores += node.TotalResource.VCores
		stats.UsedMemory += node.UsedResource.Memory
		stats.UsedVCores += node.UsedResource.VCores

		switch node.State {
		case common.NodeStateRunning:
			stats.ActiveNodes++
		case common.NodeStateUnhealthy:
			stats.UnhealthyNodes++
		case common.NodeStateLost:
			stats.LostNodes++
		case common.NodeStateDecommissioned:
			stats.DecommissionedNodes++
		}
	}

	stats.AvailableMemory = stats.TotalMemory - stats.UsedMemory
	stats.AvailableVCores = stats.TotalVCores - stats.UsedVCores

	nt.stats = stats
}

// GetStatistics 获取统计信息
func (nt *NodeTracker) GetStatistics() *NodeStatistics {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	// 返回副本
	statsCopy := *nt.stats
	return &statsCopy
}

// sendEvent 发送事件
func (nt *NodeTracker) sendEvent(event *NodeEvent) {
	select {
	case nt.nodeEvents <- event:
	default:
		nt.logger.Warn("Node event channel full, dropping event",
			zap.String("event_type", event.Type),
			zap.Any("node_id", event.NodeID))
	}
}

// getNodeKey 获取节点键
func (nt *NodeTracker) getNodeKey(nodeID common.NodeID) string {
	return fmt.Sprintf("%s:%d", nodeID.Host, nodeID.Port)
}

// Stop 停止节点跟踪器
func (nt *NodeTracker) Stop() error {
	nt.logger.Info("Stopping node tracker")
	nt.cancel()
	close(nt.nodeEvents)
	return nil
}
