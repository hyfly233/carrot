package fifo

import (
	"carrot/internal/common"
	"fmt"
	"log"
)

// ResourceManagerInterface 定义资源管理器接口，避免循环依赖
type ResourceManagerInterface interface {
	GetNodesForScheduler() map[string]*NodeInfo
	GetClusterTimestamp() int64
}

// NodeInfo 节点信息（简化版本，避免循环依赖）
type NodeInfo struct {
	ID                common.NodeID   `json:"id"`
	State             string          `json:"state"`
	AvailableResource common.Resource `json:"available_resource"`
	UsedResource      common.Resource `json:"used_resource"`
}

// ApplicationInfo 应用程序信息（简化版本）
type ApplicationInfo struct {
	ID       common.ApplicationID `json:"id"`
	Resource common.Resource      `json:"resource"`
}

// FIFOScheduler 先进先出调度器
type FIFOScheduler struct {
	rm ResourceManagerInterface
}

// NewFIFOScheduler 创建 FIFO 调度器
func NewFIFOScheduler() *FIFOScheduler {
	return &FIFOScheduler{}
}

// SetResourceManager 设置资源管理器引用
func (s *FIFOScheduler) SetResourceManager(rm ResourceManagerInterface) {
	s.rm = rm
}

// Schedule 为应用程序调度容器
func (s *FIFOScheduler) Schedule(app *ApplicationInfo) ([]*common.Container, error) {
	if s.rm == nil {
		return nil, fmt.Errorf("resource manager not set")
	}

	// 查找可用节点
	node := s.findAvailableNode(app.Resource)
	if node == nil {
		return nil, fmt.Errorf("no available node for resource requirement: %+v", app.Resource)
	}

	// 创建容器
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: app.ID,
			AttemptID:     1,
		},
		ContainerID: 1,
	}

	container := &common.Container{
		ID:       containerID,
		NodeID:   node.ID,
		Resource: app.Resource,
		Status:   "ALLOCATED",
		State:    common.ContainerStateNew,
	}

	log.Printf("Allocated container %v on node %s:%d", containerID, node.ID.Host, node.ID.Port)

	return []*common.Container{container}, nil
}
