package fifo

import "carrot/internal/common"

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
