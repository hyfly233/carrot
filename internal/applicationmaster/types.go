package applicationmaster

import (
	"carrot/internal/common"
)

// RegisterApplicationMasterResponse 注册 ApplicationMaster 响应
type RegisterApplicationMasterResponse struct {
	MaximumResourceCapability *common.Resource `json:"maximum_resource_capability"`
	ApplicationACLs           []string         `json:"application_acls"`
	Queue                     string           `json:"queue"`
	ClientToAMTokenMasterKey  []string         `json:"client_to_am_token_master_key"`
}

// AllocateRequest 分配请求
type AllocateRequest struct {
	Ask                 []*common.ContainerRequest `json:"ask"`
	Release             []common.ContainerID       `json:"release"`
	CompletedContainers []*ContainerStatus         `json:"completed_containers"`
	Progress            float32                    `json:"progress"`
	ResponseID          int32                      `json:"response_id"`
}

// AllocateResponse 分配响应
type AllocateResponse struct {
	AllocatedContainers []*common.Container  `json:"allocated_containers"`
	CompletedContainers []*ContainerStatus   `json:"completed_containers"`
	Limit               int32                `json:"limit"`
	UpdatedNodes        []*common.NodeReport `json:"updated_nodes"`
	NumClusterNodes     int32                `json:"num_cluster_nodes"`
	AvailableResources  *common.Resource     `json:"available_resources"`
	ResponseID          int32                `json:"response_id"`
}

// FinishApplicationMasterResponse 完成 ApplicationMaster 响应
type FinishApplicationMasterResponse struct {
	IsUnregistered bool `json:"is_unregistered"`
}

// ClusterMetrics 集群指标
type ClusterMetrics struct {
	NumActiveNodes         int32 `json:"num_active_nodes"`
	NumDecommissionedNodes int32 `json:"num_decommissioned_nodes"`
	NumLostNodes           int32 `json:"num_lost_nodes"`
	NumUnhealthyNodes      int32 `json:"num_unhealthy_nodes"`
	NumRebootedNodes       int32 `json:"num_rebooted_nodes"`
	TotalMB                int64 `json:"total_mb"`
	TotalVirtualCores      int32 `json:"total_virtual_cores"`
	TotalNodes             int32 `json:"total_nodes"`
	AvailableMB            int64 `json:"available_mb"`
	AvailableVirtualCores  int32 `json:"available_virtual_cores"`
	AllocatedMB            int64 `json:"allocated_mb"`
	AllocatedVirtualCores  int32 `json:"allocated_virtual_cores"`
	ContainersAllocated    int32 `json:"containers_allocated"`
	ContainersPending      int32 `json:"containers_pending"`
	ContainersReserved     int32 `json:"containers_reserved"`
}

// ContainerStatus 容器状态
type ContainerStatus struct {
	ContainerID common.ContainerID `json:"container_id"`
	State       string             `json:"state"`
	ExitCode    int32              `json:"exit_code"`
	Diagnostics string             `json:"diagnostics"`
}
