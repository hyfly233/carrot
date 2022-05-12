package scheduler

import (
	"carrot/internal/common"
	"time"
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
