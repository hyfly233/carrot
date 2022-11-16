package common

import (
	"fmt"
	"time"
)

type ServerType string

const (
	ServerTypeHTTP ServerType = "http"
	ServerTypeGRPC ServerType = "grpc"
	ServerTypeTCP  ServerType = "tcp"
	ServerTypeUDP  ServerType = "udp"
)

// Server 通用服务器接口
type Server interface {
	// Start 启动服务器
	Start(port int) error

	// Stop 停止服务器
	Stop() error

	// GetType 获取服务器类型
	GetType() ServerType

	// GetAddress 获取服务器地址
	GetAddress() string

	// IsRunning 检查服务器是否在运行
	IsRunning() bool
}

// Resource 表示资源配置
type Resource struct {
	Memory int64 `json:"memory"` // MB
	VCores int32 `json:"vcores"` // 虚拟核心数
}

// ResourceRequest 表示资源请求
type ResourceRequest struct {
	Priority            int32    `json:"priority"`
	Resource            Resource `json:"resource"`
	NumContainers       int32    `json:"num_containers"`
	RelaxLocality       bool     `json:"relax_locality"`
	NodeLabelExpression string   `json:"node_label_expression,omitempty"`
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

func (ni *NodeID) HostPortString() string {
	return fmt.Sprintf("%s:%d", ni.Host, ni.Port)
}

// ClusterID 集群标识
type ClusterID struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

func (ci *ClusterID) String() string {
	return fmt.Sprintf("%s-%s", ci.Name, ci.ID)
}

// ClusterNode 集群节点信息
type ClusterNode struct {
	ID            NodeID            `json:"id"`
	Type          NodeType          `json:"type"`
	State         NodeState         `json:"state"`
	Roles         []NodeRole        `json:"roles"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	JoinTime      time.Time         `json:"join_time"`
	Metadata      map[string]string `json:"metadata"`
	Capabilities  NodeCapabilities  `json:"capabilities"`
	Health        NodeHealth        `json:"health"`
	Version       string            `json:"version"`
}

// NodeType 节点类型
type NodeType string

const (
	NodeTypeResourceManager   NodeType = "resourcemanager"
	NodeTypeNodeManager       NodeType = "nodemanager"
	NodeTypeApplicationMaster NodeType = "applicationmaster"
)

// NodeState 节点状态
type NodeState string

const (
	NodeStateJoining  NodeState = "joining"
	NodeStateActive   NodeState = "active"
	NodeStateInactive NodeState = "inactive"
	NodeStateLeaving  NodeState = "leaving"
	NodeStateFailed   NodeState = "failed"
)

// NodeRole 节点角色
type NodeRole string

const (
	NodeRoleLeader   NodeRole = "leader"
	NodeRoleFollower NodeRole = "follower"
	NodeRoleWorker   NodeRole = "worker"
)

// NodeCapabilities 节点能力
type NodeCapabilities struct {
	MaxContainers     int32             `json:"max_containers"`
	SupportedFeatures []string          `json:"supported_features"`
	Resources         Resource          `json:"resources"`
	Labels            map[string]string `json:"labels"`
}

// NodeHealth 节点健康状态
type NodeHealth struct {
	Status    HealthStatus       `json:"status"`
	LastCheck time.Time          `json:"last_check"`
	Issues    []HealthIssue      `json:"issues"`
	Metrics   map[string]float64 `json:"metrics"`
}

// HealthStatus 健康状态
type HealthStatus string

const (
	HealthStatusHealthy  HealthStatus = "healthy"
	HealthStatusWarning  HealthStatus = "warning"
	HealthStatusCritical HealthStatus = "critical"
	HealthStatusUnknown  HealthStatus = "unknown"
)

// HealthIssue 健康问题
type HealthIssue struct {
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Severity  string    `json:"severity"`
	Timestamp time.Time `json:"timestamp"`
}

// ClusterInfo 集群信息
type ClusterInfo struct {
	ID         ClusterID              `json:"id"`
	State      ClusterState           `json:"state"`
	Leader     *NodeID                `json:"leader,omitempty"`
	Nodes      map[string]ClusterNode `json:"nodes"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Config     ClusterConfig          `json:"config"`
	Statistics ClusterStatistics      `json:"statistics"`
}

// ClusterState 集群状态
type ClusterState string

const (
	ClusterStateForming     ClusterState = "forming"
	ClusterStateActive      ClusterState = "active"
	ClusterStateDegraded    ClusterState = "degraded"
	ClusterStateMaintenance ClusterState = "maintenance"
	ClusterStateFailed      ClusterState = "failed"
)

// ClusterConfig 集群配置
type ClusterConfig struct {
	Name                   string                 `json:"name" yaml:"name"`
	ID                     string                 `json:"id" yaml:"id"`
	MinNodes               int                    `json:"min_nodes" yaml:"min_nodes"`
	MaxNodes               int                    `json:"max_nodes" yaml:"max_nodes"`
	ElectionTimeout        time.Duration          `json:"election_timeout" yaml:"election_timeout"`
	HeartbeatInterval      time.Duration          `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	FailureDetectionWindow time.Duration          `json:"failure_detection_window" yaml:"failure_detection_window"`
	SplitBrainProtection   bool                   `json:"split_brain_protection" yaml:"split_brain_protection"`
	AutoScaling            bool                   `json:"auto_scaling" yaml:"auto_scaling"`
	DiscoveryMethod        string                 `json:"discovery_method" yaml:"discovery_method"`
	DiscoveryConfig        map[string]interface{} `json:"discovery_config" yaml:"discovery_config"`
}

// ClusterStatistics 集群统计信息
type ClusterStatistics struct {
	TotalNodes            int32         `json:"total_nodes"`
	ActiveNodes           int32         `json:"active_nodes"`
	FailedNodes           int32         `json:"failed_nodes"`
	TotalResources        Resource      `json:"total_resources"`
	UsedResources         Resource      `json:"used_resources"`
	AvailableResources    Resource      `json:"available_resources"`
	RunningApplications   int32         `json:"running_applications"`
	PendingApplications   int32         `json:"pending_applications"`
	CompletedApplications int64         `json:"completed_applications"`
	FailedApplications    int64         `json:"failed_applications"`
	Uptime                time.Duration `json:"uptime"`
}

// ClusterEvent 集群事件
type ClusterEvent struct {
	ID        string                 `json:"id"`
	Type      ClusterEventType       `json:"type"`
	Source    NodeID                 `json:"source"`
	Target    *NodeID                `json:"target,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
}

// ClusterEventType 集群事件类型
type ClusterEventType string

const (
	ClusterEventNodeJoined    ClusterEventType = "node_joined"
	ClusterEventNodeLeft      ClusterEventType = "node_left"
	ClusterEventNodeFailed    ClusterEventType = "node_failed"
	ClusterEventLeaderElected ClusterEventType = "leader_elected"
	ClusterEventConfigChanged ClusterEventType = "config_changed"
	ClusterEventSplitBrain    ClusterEventType = "split_brain"
	ClusterEventHealing       ClusterEventType = "healing"
)

// EventSeverity 事件严重性
type EventSeverity string

const (
	EventSeverityInfo     EventSeverity = "info"
	EventSeverityWarning  EventSeverity = "warning"
	EventSeverityError    EventSeverity = "error"
	EventSeverityCritical EventSeverity = "critical"
)

// ApplicationID 应用程序标识
type ApplicationID struct {
	ClusterTimestamp int64 `json:"cluster_timestamp"`
	ID               int32 `json:"id"`
}

// String 返回 ApplicationID 的字符串表示
func (aid ApplicationID) String() string {
	return fmt.Sprintf("application_%d_%d", aid.ClusterTimestamp, aid.ID)
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
