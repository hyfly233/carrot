package common

import (
	"fmt"
	"time"
)

// Resource 表示资源配置
type Resource struct {
	Memory int64 `json:"memory"` // MB
	VCores int32 `json:"vcores"` // 虚拟核心数
}

// ResourceSpec 表示详细的资源规格配置，支持限制和请求
type ResourceSpec struct {
	CPU         float64 `json:"cpu"`          // CPU 请求量（核心数）
	Memory      int64   `json:"memory"`       // 内存请求量（MB）
	CPULimit    float64 `json:"cpu_limit"`    // CPU 限制量（核心数）
	MemoryLimit int64   `json:"memory_limit"` // 内存限制量（MB）
}

// NodeID 节点标识
type NodeID struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
}

func (ni *NodeID) String() string {
	return fmt.Sprintf("%s:%d", ni.Host, ni.Port)
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

// ApplicationReport 应用程序报告
type ApplicationReport struct {
	ApplicationID   ApplicationID `json:"application_id"`
	ApplicationName string        `json:"application_name"`
	ApplicationType string        `json:"application_type"`
	User            string        `json:"user"`
	Queue           string        `json:"queue"`
	StartTime       time.Time     `json:"start_time"`
	FinishTime      time.Time     `json:"finish_time,omitempty"`
	FinalStatus     string        `json:"final_status"`
	State           string        `json:"state"`
	Progress        float32       `json:"progress"`
	TrackingURL     string        `json:"tracking_url"`
}

// NodeReport 节点报告
type NodeReport struct {
	NodeID           NodeID    `json:"node_id"`
	HTTPAddress      string    `json:"http_address"`
	RackName         string    `json:"rack_name"`
	Used             Resource  `json:"used"`
	Capability       Resource  `json:"capability"`
	NumContainers    int32     `json:"num_containers"`
	State            string    `json:"state"`
	HealthReport     string    `json:"health_report"`
	LastHealthUpdate time.Time `json:"last_health_update"`
	NodeLabels       []string  `json:"node_labels"`
}

// ApplicationSubmissionContext 应用程序提交上下文
type ApplicationSubmissionContext struct {
	ApplicationID   ApplicationID          `json:"application_id"`
	ApplicationName string                 `json:"application_name"`
	ApplicationType string                 `json:"application_type"`
	Queue           string                 `json:"queue"`
	Priority        int32                  `json:"priority"`
	AMContainerSpec ContainerLaunchContext `json:"am_container_spec"`
	Resource        Resource               `json:"resource"`
	MaxAppAttempts  int32                  `json:"max_app_attempts"`
}

// ContainerLaunchContext 容器启动上下文
type ContainerLaunchContext struct {
	Commands       []string                 `json:"commands"`
	Environment    map[string]string        `json:"environment"`
	LocalResources map[string]LocalResource `json:"local_resources"`
	ServiceData    map[string][]byte        `json:"service_data"`
	Tokens         []byte                   `json:"tokens,omitempty"`
}

// LocalResource 本地资源
type LocalResource struct {
	URL        string `json:"url"`
	Size       int64  `json:"size"`
	Timestamp  int64  `json:"timestamp"`
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
	Pattern    string `json:"pattern,omitempty"`
}

// Constants
const (
	// Application States
	ApplicationStateNew       = "NEW"
	ApplicationStateSubmitted = "SUBMITTED"
	ApplicationStateAccepted  = "ACCEPTED"
	ApplicationStateRunning   = "RUNNING"
	ApplicationStateFinished  = "FINISHED"
	ApplicationStateFailed    = "FAILED"
	ApplicationStateKilled    = "KILLED"

	// Container States
	ContainerStateNew      = "NEW"
	ContainerStateRunning  = "RUNNING"
	ContainerStateComplete = "COMPLETE"

	// Node States
	NodeStateNew            = "NEW"
	NodeStateRunning        = "RUNNING"
	NodeStateUnhealthy      = "UNHEALTHY"
	NodeStateDecommissioned = "DECOMMISSIONED"
	NodeStateLost           = "LOST"
	NodeStateRebooted       = "REBOOTED"

	// Final Application Status
	FinalApplicationStatusUndefined = "UNDEFINED"
	FinalApplicationStatusSucceeded = "SUCCEEDED"
	FinalApplicationStatusFailed    = "FAILED"
	FinalApplicationStatusKilled    = "KILLED"
)

// ParseContainerID 解析容器ID字符串
func ParseContainerID(containerIDStr string) (ContainerID, error) {
	// 简单实现：假设容器ID字符串是数字格式
	// 在真实实现中应该解析更复杂的格式
	var containerID ContainerID
	// 这里暂时返回一个基本的容器ID
	// 真实实现应该解析字符串并填充完整的 ContainerID 结构
	return containerID, nil
}
