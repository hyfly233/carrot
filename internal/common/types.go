package common

// Resource 表示资源配置
type Resource struct {
	Memory int64 `json:"memory"` // MB
	VCores int32 `json:"vcores"` // 虚拟核心数
}

// NodeID 节点标识
type NodeID struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
}

// ApplicationID 应用程序标识
type ApplicationID struct {
	ClusterTimestamp int64 `json:"cluster_timestamp"`
	ID               int32 `json:"id"`
}

// ContainerID 容器标识
type ContainerID struct {
	ApplicationAttemptID ApplicationAttemptID `json:"application_attempt_id"`
	ContainerID          int64                `json:"container_id"`
}

// ApplicationAttemptID 应用程序尝试标识
type ApplicationAttemptID struct {
	ApplicationID ApplicationID `json:"application_id"`
	AttemptID     int32         `json:"attempt_id"`
}

// ContainerRequest 容器请求
type ContainerRequest struct {
	Resource  Resource `json:"resource"`
	Priority  int32    `json:"priority"`
	Locality  string   `json:"locality,omitempty"`
	NodeLabel string   `json:"node_label,omitempty"`
}

// Container 容器信息
type Container struct {
	ID       ContainerID `json:"id"`
	NodeID   NodeID      `json:"node_id"`
	Resource Resource    `json:"resource"`
	Status   string      `json:"status"`
	State    string      `json:"state"`
}
