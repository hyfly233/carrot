package common

import (
	"fmt"
	"time"
)

// 基础接口定义，用于实现类似继承的功能

// ResourceInterface 资源接口
type ResourceInterface interface {
	GetMemory() int64
	GetVCores() int32
	IsEmpty() bool
	Equals(other ResourceInterface) bool
	Add(other ResourceInterface) ResourceInterface
	Subtract(other ResourceInterface) ResourceInterface
	String() string
}

// NodeInterface 节点接口
type NodeInterface interface {
	GetID() NodeID
	GetHost() string
	GetPort() int32
	GetAddress() string
	String() string
}

// ContainerInterface 容器接口
type ContainerInterface interface {
	GetID() ContainerID
	GetNodeID() NodeID
	GetResource() ResourceInterface
	GetState() string
	String() string
}

// ApplicationInterface 应用程序接口
type ApplicationInterface interface {
	GetID() ApplicationID
	GetName() string
	GetState() string
	GetUser() string
	String() string
}

// 基础资源类型，作为所有资源类型的基础
type BaseResource struct {
	Memory int64 `json:"memory" yaml:"memory"` // 内存 MB
	VCores int32 `json:"vcores" yaml:"vcores"` // 虚拟核心数
}

// 实现 ResourceInterface
func (r *BaseResource) GetMemory() int64 {
	return r.Memory
}

func (r *BaseResource) GetVCores() int32 {
	return r.VCores
}

func (r *BaseResource) IsEmpty() bool {
	return r.Memory == 0 && r.VCores == 0
}

func (r *BaseResource) Equals(other ResourceInterface) bool {
	return r.Memory == other.GetMemory() && r.VCores == other.GetVCores()
}

func (r *BaseResource) Add(other ResourceInterface) ResourceInterface {
	return &BaseResource{
		Memory: r.Memory + other.GetMemory(),
		VCores: r.VCores + other.GetVCores(),
	}
}

func (r *BaseResource) Subtract(other ResourceInterface) ResourceInterface {
	return &BaseResource{
		Memory: r.Memory - other.GetMemory(),
		VCores: r.VCores - other.GetVCores(),
	}
}

func (r *BaseResource) String() string {
	return fmt.Sprintf("Resource{Memory: %d MB, VCores: %d}", r.Memory, r.VCores)
}

// 基础节点类型
type BaseNode struct {
	ID   NodeID `json:"id" yaml:"id"`
	Host string `json:"host" yaml:"host"`
	Port int32  `json:"port" yaml:"port"`
}

// 实现 NodeInterface
func (n *BaseNode) GetID() NodeID {
	return n.ID
}

func (n *BaseNode) GetHost() string {
	return n.Host
}

func (n *BaseNode) GetPort() int32 {
	return n.Port
}

func (n *BaseNode) GetAddress() string {
	return fmt.Sprintf("%s:%d", n.Host, n.Port)
}

func (n *BaseNode) String() string {
	return fmt.Sprintf("Node{Host: %s, Port: %d}", n.Host, n.Port)
}

// 基础容器类型
type BaseContainer struct {
	ID       ContainerID       `json:"id" yaml:"id"`
	NodeID   NodeID            `json:"node_id" yaml:"node_id"`
	Resource ResourceInterface `json:"resource" yaml:"resource"`
	State    string            `json:"state" yaml:"state"`
}

// 实现 ContainerInterface
func (c *BaseContainer) GetID() ContainerID {
	return c.ID
}

func (c *BaseContainer) GetNodeID() NodeID {
	return c.NodeID
}

func (c *BaseContainer) GetResource() ResourceInterface {
	return c.Resource
}

func (c *BaseContainer) GetState() string {
	return c.State
}

func (c *BaseContainer) String() string {
	return fmt.Sprintf("Container{ID: %v, NodeID: %v, State: %s}", c.ID, c.NodeID, c.State)
}

// 基础应用程序类型
type BaseApplication struct {
	ID    ApplicationID `json:"id" yaml:"id"`
	Name  string        `json:"name" yaml:"name"`
	State string        `json:"state" yaml:"state"`
	User  string        `json:"user" yaml:"user"`
}

// 实现 ApplicationInterface
func (a *BaseApplication) GetID() ApplicationID {
	return a.ID
}

func (a *BaseApplication) GetName() string {
	return a.Name
}

func (a *BaseApplication) GetState() string {
	return a.State
}

func (a *BaseApplication) GetUser() string {
	return a.User
}

func (a *BaseApplication) String() string {
	return fmt.Sprintf("Application{ID: %s, Name: %s, State: %s, User: %s}",
		a.ID.String(), a.Name, a.State, a.User)
}

// 扩展的资源类型，继承自 BaseResource
type ExtendedResource struct {
	BaseResource
	// 额外字段
	GPU        int32             `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	Disk       int64             `json:"disk,omitempty" yaml:"disk,omitempty"`             // 磁盘空间 MB
	Network    int64             `json:"network,omitempty" yaml:"network,omitempty"`       // 网络带宽 Mbps
	Attributes map[string]string `json:"attributes,omitempty" yaml:"attributes,omitempty"` // 自定义属性
}

// 重写方法以支持扩展字段
func (r *ExtendedResource) Add(other ResourceInterface) ResourceInterface {
	base := r.BaseResource.Add(other).(*BaseResource)
	result := &ExtendedResource{
		BaseResource: *base,
		GPU:          r.GPU,
		Disk:         r.Disk,
		Network:      r.Network,
		Attributes:   make(map[string]string),
	}

	// 复制属性
	for k, v := range r.Attributes {
		result.Attributes[k] = v
	}

	// 如果 other 也是 ExtendedResource，合并扩展字段
	if ext, ok := other.(*ExtendedResource); ok {
		result.GPU += ext.GPU
		result.Disk += ext.Disk
		result.Network += ext.Network
		for k, v := range ext.Attributes {
			result.Attributes[k] = v
		}
	}

	return result
}

func (r *ExtendedResource) String() string {
	return fmt.Sprintf("ExtendedResource{Memory: %d MB, VCores: %d, GPU: %d, Disk: %d MB, Network: %d Mbps}",
		r.Memory, r.VCores, r.GPU, r.Disk, r.Network)
}

// 扩展的节点类型，继承自 BaseNode
type ExtendedNode struct {
	BaseNode
	// 额外字段
	RackName          string                 `json:"rack_name,omitempty" yaml:"rack_name,omitempty"`
	Labels            []string               `json:"labels,omitempty" yaml:"labels,omitempty"`
	LastHeartbeat     time.Time              `json:"last_heartbeat" yaml:"last_heartbeat"`
	TotalResource     ResourceInterface      `json:"total_resource" yaml:"total_resource"`
	AvailableResource ResourceInterface      `json:"available_resource" yaml:"available_resource"`
	Capabilities      map[string]interface{} `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
}

func (n *ExtendedNode) GetTotalResource() ResourceInterface {
	return n.TotalResource
}

func (n *ExtendedNode) GetAvailableResource() ResourceInterface {
	return n.AvailableResource
}

func (n *ExtendedNode) HasLabel(label string) bool {
	for _, l := range n.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func (n *ExtendedNode) IsHealthy() bool {
	return time.Since(n.LastHeartbeat) < 30*time.Second
}

// 扩展的容器类型，继承自 BaseContainer
type ExtendedContainer struct {
	BaseContainer
	// 额外字段
	Status        string                  `json:"status,omitempty" yaml:"status,omitempty"`
	StartTime     time.Time               `json:"start_time,omitempty" yaml:"start_time,omitempty"`
	FinishTime    time.Time               `json:"finish_time,omitempty" yaml:"finish_time,omitempty"`
	ExitCode      int                     `json:"exit_code,omitempty" yaml:"exit_code,omitempty"`
	Diagnostics   string                  `json:"diagnostics,omitempty" yaml:"diagnostics,omitempty"`
	LaunchContext *ContainerLaunchContext `json:"launch_context,omitempty" yaml:"launch_context,omitempty"`
}

func (c *ExtendedContainer) GetStatus() string {
	return c.Status
}

func (c *ExtendedContainer) IsRunning() bool {
	return c.State == ContainerStateRunning
}

func (c *ExtendedContainer) IsCompleted() bool {
	return c.State == ContainerStateComplete
}

// 扩展的应用程序类型，继承自 BaseApplication
type ExtendedApplication struct {
	BaseApplication
	// 额外字段
	Type        string                 `json:"type,omitempty" yaml:"type,omitempty"`
	Queue       string                 `json:"queue,omitempty" yaml:"queue,omitempty"`
	Priority    int32                  `json:"priority,omitempty" yaml:"priority,omitempty"`
	StartTime   time.Time              `json:"start_time,omitempty" yaml:"start_time,omitempty"`
	FinishTime  time.Time              `json:"finish_time,omitempty" yaml:"finish_time,omitempty"`
	Progress    float32                `json:"progress,omitempty" yaml:"progress,omitempty"`
	Resource    ResourceInterface      `json:"resource,omitempty" yaml:"resource,omitempty"`
	TrackingURL string                 `json:"tracking_url,omitempty" yaml:"tracking_url,omitempty"`
	Attempts    []ApplicationAttemptID `json:"attempts,omitempty" yaml:"attempts,omitempty"`
	Tags        map[string]string      `json:"tags,omitempty" yaml:"tags,omitempty"`
}

func (a *ExtendedApplication) GetType() string {
	return a.Type
}

func (a *ExtendedApplication) GetQueue() string {
	return a.Queue
}

func (a *ExtendedApplication) GetPriority() int32 {
	return a.Priority
}

func (a *ExtendedApplication) GetProgress() float32 {
	return a.Progress
}

func (a *ExtendedApplication) IsRunning() bool {
	return a.State == ApplicationStateRunning
}

func (a *ExtendedApplication) IsCompleted() bool {
	return a.State == ApplicationStateFinished ||
		a.State == ApplicationStateFailed ||
		a.State == ApplicationStateKilled
}

// 工厂函数，用于创建不同类型的资源
func NewResource(memory int64, vcores int32) ResourceInterface {
	return &BaseResource{
		Memory: memory,
		VCores: vcores,
	}
}

func NewExtendedResource(memory int64, vcores int32, gpu int32, disk int64, network int64) ResourceInterface {
	return &ExtendedResource{
		BaseResource: BaseResource{
			Memory: memory,
			VCores: vcores,
		},
		GPU:        gpu,
		Disk:       disk,
		Network:    network,
		Attributes: make(map[string]string),
	}
}

func NewNode(host string, port int32) NodeInterface {
	return &BaseNode{
		ID: NodeID{
			Host: host,
			Port: port,
		},
		Host: host,
		Port: port,
	}
}

func NewExtendedNode(host string, port int32, totalResource ResourceInterface) *ExtendedNode {
	return &ExtendedNode{
		BaseNode: BaseNode{
			ID: NodeID{
				Host: host,
				Port: port,
			},
			Host: host,
			Port: port,
		},
		TotalResource:     totalResource,
		AvailableResource: totalResource,
		LastHeartbeat:     time.Now(),
		Capabilities:      make(map[string]interface{}),
	}
}
