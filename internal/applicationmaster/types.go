package applicationmaster

import (
	"carrot/internal/common"
)

// ContainerRequest 容器请求
type ContainerRequest struct {
	Resource  common.Resource
	Priority  int32
	NodeLabel string
}

// AllocateRequest 资源分配请求
type AllocateRequest struct {
	Ask                 []*ContainerRequest
	Release             []common.ContainerID
	CompletedContainers []*common.Container
	Progress            float32
}

// AllocateResponse 资源分配响应
type AllocateResponse struct {
	AllocatedContainers []*common.Container
	CompletedContainers []*common.Container
	Limit               int32
	UpdatedNodes        []common.NodeReport
	NumClusterNodes     int32
}

// RegisterApplicationMasterResponse 注册AM响应
type RegisterApplicationMasterResponse struct {
	MaximumResourceCapability common.Resource
	ApplicationACLs           map[string]string
	Queue                     string
}

// FinishApplicationMasterResponse 完成AM响应
type FinishApplicationMasterResponse struct {
	IsUnregistered bool
}

// ClusterMetrics 集群指标
type ClusterMetrics struct {
	AppsSubmitted         int32
	AppsCompleted         int32
	AppsPending           int32
	AppsRunning           int32
	AppsFailed            int32
	AppsKilled            int32
	ActiveNodes           int32
	LostNodes             int32
	UnhealthyNodes        int32
	DecommissionedNodes   int32
	TotalNodes            int32
	ReservedVirtualCores  int32
	AvailableVirtualCores int32
	AllocatedVirtualCores int32
}
