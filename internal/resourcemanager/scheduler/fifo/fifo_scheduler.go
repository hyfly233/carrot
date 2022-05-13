package fifo

import (
	"carrot/internal/common"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Scheduler 调度器接口
type Scheduler interface {
	// Schedule 调度应用程序，返回分配的容器列表
	Schedule(appInfo *ApplicationInfo) ([]*ContainerAllocation, error)

	// SetResourceManager 设置资源管理器引用
	SetResourceManager(rm ResourceManagerInterface)
}

// ResourceManagerInterface 资源管理器接口
type ResourceManagerInterface interface {
	GetNodesForScheduler() map[string]*NodeInfo
	GetClusterTimestamp() int64
}

// ApplicationInfo 应用程序信息
type ApplicationInfo struct {
	ID         common.ApplicationID `json:"id"`
	Resource   common.Resource      `json:"resource"`
	SubmitTime time.Time            `json:"submit_time"`
	Queue      string               `json:"queue"`
	Priority   int32                `json:"priority"`
}

// ContainerAllocation 容器分配信息
type ContainerAllocation struct {
	ID       common.ContainerID `json:"id"`
	NodeID   common.NodeID      `json:"node_id"`
	Resource common.Resource    `json:"resource"`
	Priority int32              `json:"priority"`
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID                common.NodeID   `json:"id"`
	State             string          `json:"state"`
	AvailableResource common.Resource `json:"available_resource"`
	UsedResource      common.Resource `json:"used_resource"`
	LastHeartbeat     time.Time       `json:"last_heartbeat"`
}

// HasSufficientResource 检查节点是否有足够的资源
func (n *NodeInfo) HasSufficientResource(required common.Resource) bool {
	return n.AvailableResource.Memory >= required.Memory &&
		n.AvailableResource.VCores >= required.VCores
}

// FIFOScheduler 先进先出调度器
type FIFOScheduler struct {
	mu                  sync.RWMutex
	rm                  ResourceManagerInterface
	pendingApplications []*ApplicationInfo
	logger              *zap.Logger
}

// NewFIFOScheduler 创建新的FIFO调度器
func NewFIFOScheduler() *FIFOScheduler {
	logger, _ := zap.NewProduction()
	return &FIFOScheduler{
		pendingApplications: make([]*ApplicationInfo, 0),
		logger:              logger,
	}
}

// SetResourceManager 设置资源管理器引用
func (s *FIFOScheduler) SetResourceManager(rm ResourceManagerInterface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rm = rm
}

// Schedule 调度应用程序
func (s *FIFOScheduler) Schedule(appInfo *ApplicationInfo) ([]*ContainerAllocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rm == nil {
		return nil, fmt.Errorf("resource manager not set")
	}

	// 添加到待处理队列
	s.pendingApplications = append(s.pendingApplications, appInfo)

	// 按提交时间排序（FIFO）
	sort.Slice(s.pendingApplications, func(i, j int) bool {
		return s.pendingApplications[i].SubmitTime.Before(s.pendingApplications[j].SubmitTime)
	})

	// 尝试分配容器
	containers := make([]*ContainerAllocation, 0)
	nodes := s.rm.GetNodesForScheduler()

	// 为当前应用程序查找合适的节点
	bestNode := s.selectBestNode(nodes, appInfo.Resource)
	if bestNode == nil {
		s.logger.Warn("No suitable node found",
			zap.String("app_id", fmt.Sprintf("%d", appInfo.ID.ID)),
			zap.Int64("memory", appInfo.Resource.Memory),
			zap.Int32("vcores", appInfo.Resource.VCores))
		return nil, fmt.Errorf("no suitable node found for application %d", appInfo.ID.ID)
	}

	// 创建容器分配
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: appInfo.ID,
			AttemptID:     1,
		},
		ContainerID: s.rm.GetClusterTimestamp(),
	}

	allocation := &ContainerAllocation{
		ID:       containerID,
		NodeID:   bestNode.ID,
		Resource: appInfo.Resource,
		Priority: appInfo.Priority,
	}

	containers = append(containers, allocation)

	s.logger.Info("Container allocated",
		zap.String("container_id", fmt.Sprintf("%d", containerID.ContainerID)),
		zap.String("node", bestNode.ID.Host),
		zap.Int64("memory", appInfo.Resource.Memory),
		zap.Int32("vcores", appInfo.Resource.VCores))

	return containers, nil
}

// selectBestNode 选择最佳节点（FIFO策略下选择第一个满足条件的节点）
func (s *FIFOScheduler) selectBestNode(nodes map[string]*NodeInfo, required common.Resource) *NodeInfo {
	// FIFO调度器选择第一个满足条件的节点
	for _, node := range nodes {
		if node.State == common.NodeStateRunning && node.HasSufficientResource(required) {
			return node
		}
	}
	return nil
}
