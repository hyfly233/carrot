package nodemanager

import (
	"time"

	"carrot/internal/common"
)

// Node 节点信息
type Node struct {
	ID                 common.NodeID       `json:"id"`
	TotalResource      common.Resource     `json:"total_resource"`
	UsedResource       common.Resource     `json:"used_resource"`
	AvailableResource  common.Resource     `json:"available_resource"`
	State              string              `json:"state"`
	HTTPAddress        string              `json:"http_address"`
	RackName           string              `json:"rack_name"`
	LastHeartbeat      time.Time           `json:"last_heartbeat"`
	RegisterTime       time.Time           `json:"register_time"`
	HealthReport       string              `json:"health_report"`
	NodeLabels         []string            `json:"node_labels"`
	Containers         []*common.Container `json:"containers"`
	NodeManagerVersion string              `json:"node_manager_version"`

	// 性能指标
	NumContainers       int32 `json:"num_containers"`
	ContainersCompleted int64 `json:"containers_completed"`
	ContainersFailed    int64 `json:"containers_failed"`
}

// NodeBuilder 节点构建器
type NodeBuilder struct {
	node *Node
}

// NewNodeBuilder 创建节点构建器
func NewNodeBuilder() *NodeBuilder {
	return &NodeBuilder{
		node: &Node{
			NodeLabels:   make([]string, 0),
			Containers:   make([]*common.Container, 0),
			RegisterTime: time.Now(),
		},
	}
}

// WithID 设置节点ID
func (nb *NodeBuilder) WithID(id common.NodeID) *NodeBuilder {
	nb.node.ID = id
	return nb
}

// WithTotalResource 设置总资源
func (nb *NodeBuilder) WithTotalResource(resource common.Resource) *NodeBuilder {
	nb.node.TotalResource = resource
	nb.node.AvailableResource = resource // 初始时可用资源等于总资源
	return nb
}

// WithHTTPAddress 设置HTTP地址
func (nb *NodeBuilder) WithHTTPAddress(address string) *NodeBuilder {
	nb.node.HTTPAddress = address
	return nb
}

// WithRackName 设置机架名称
func (nb *NodeBuilder) WithRackName(rackName string) *NodeBuilder {
	nb.node.RackName = rackName
	return nb
}

// WithNodeLabels 设置节点标签
func (nb *NodeBuilder) WithNodeLabels(labels []string) *NodeBuilder {
	nb.node.NodeLabels = make([]string, len(labels))
	copy(nb.node.NodeLabels, labels)
	return nb
}

// WithNodeManagerVersion 设置NodeManager版本
func (nb *NodeBuilder) WithNodeManagerVersion(version string) *NodeBuilder {
	nb.node.NodeManagerVersion = version
	return nb
}

// Build 构建节点
func (nb *NodeBuilder) Build() *Node {
	// 设置默认值
	if nb.node.State == "" {
		nb.node.State = common.NodeStateNew
	}
	if nb.node.HealthReport == "" {
		nb.node.HealthReport = "Healthy"
	}
	if nb.node.LastHeartbeat.IsZero() {
		nb.node.LastHeartbeat = time.Now()
	}

	return nb.node
}

// GetResourceUtilization 获取资源利用率
func (n *Node) GetResourceUtilization() (memoryUtil, vcoreUtil float64) {
	if n.TotalResource.Memory > 0 {
		memoryUtil = float64(n.UsedResource.Memory) / float64(n.TotalResource.Memory)
	}
	if n.TotalResource.VCores > 0 {
		vcoreUtil = float64(n.UsedResource.VCores) / float64(n.TotalResource.VCores)
	}
	return
}

// IsHealthy 检查节点是否健康
func (n *Node) IsHealthy() bool {
	return n.State == common.NodeStateRunning &&
		(n.HealthReport == "Healthy" || n.HealthReport == "")
}

// IsAvailable 检查节点是否可用于调度
func (n *Node) IsAvailable() bool {
	return n.IsHealthy() &&
		n.AvailableResource.Memory > 0 &&
		n.AvailableResource.VCores > 0
}

// CanAllocate 检查是否可以分配指定资源
func (n *Node) CanAllocate(resource common.Resource) bool {
	return n.IsAvailable() &&
		n.AvailableResource.Memory >= resource.Memory &&
		n.AvailableResource.VCores >= resource.VCores
}

// HasLabel 检查节点是否有指定标签
func (n *Node) HasLabel(label string) bool {
	for _, l := range n.NodeLabels {
		if l == label {
			return true
		}
	}
	return false
}

// GetContainerByID 根据ID获取容器
func (n *Node) GetContainerByID(containerID common.ContainerID) *common.Container {
	for _, container := range n.Containers {
		if container.ID.ContainerID == containerID.ContainerID {
			return container
		}
	}
	return nil
}

// AddContainer 添加容器
func (n *Node) AddContainer(container *common.Container) {
	n.Containers = append(n.Containers, container)
	n.NumContainers = int32(len(n.Containers))
}

// RemoveContainer 移除容器
func (n *Node) RemoveContainer(containerID common.ContainerID) bool {
	for i, container := range n.Containers {
		if container.ID.ContainerID == containerID.ContainerID {
			// 移除容器
			n.Containers = append(n.Containers[:i], n.Containers[i+1:]...)
			n.NumContainers = int32(len(n.Containers))
			return true
		}
	}
	return false
}

// UpdateLastHeartbeat 更新最后心跳时间
func (n *Node) UpdateLastHeartbeat() {
	n.LastHeartbeat = time.Now()
}

// UpdateResourceUsage 更新资源使用情况
func (n *Node) UpdateResourceUsage(usedResource common.Resource) {
	n.UsedResource = usedResource
	n.AvailableResource = common.Resource{
		Memory: n.TotalResource.Memory - n.UsedResource.Memory,
		VCores: n.TotalResource.VCores - n.UsedResource.VCores,
	}
}

// GetAge 获取节点注册至今的时长
func (n *Node) GetAge() time.Duration {
	return time.Since(n.RegisterTime)
}

// GetHeartbeatAge 获取距离最后心跳的时长
func (n *Node) GetHeartbeatAge() time.Duration {
	return time.Since(n.LastHeartbeat)
}

// ToNodeReport 转换为节点报告
func (n *Node) ToNodeReport() *common.NodeReport {
	return &common.NodeReport{
		NodeID:           n.ID,
		HTTPAddress:      n.HTTPAddress,
		RackName:         n.RackName,
		UsedResource:     n.UsedResource,
		TotalResource:    n.TotalResource,
		NumContainers:    n.NumContainers,
		State:            n.State,
		HealthReport:     n.HealthReport,
		LastHealthUpdate: n.LastHeartbeat,
		NodeLabels:       n.NodeLabels,
		Containers:       []string{}, // 简化处理
	}
}

// Clone 克隆节点（用于快照）
func (n *Node) Clone() *Node {
	clone := &Node{
		ID:                  n.ID,
		TotalResource:       n.TotalResource,
		UsedResource:        n.UsedResource,
		AvailableResource:   n.AvailableResource,
		State:               n.State,
		HTTPAddress:         n.HTTPAddress,
		RackName:            n.RackName,
		LastHeartbeat:       n.LastHeartbeat,
		RegisterTime:        n.RegisterTime,
		HealthReport:        n.HealthReport,
		NodeManagerVersion:  n.NodeManagerVersion,
		NumContainers:       n.NumContainers,
		ContainersCompleted: n.ContainersCompleted,
		ContainersFailed:    n.ContainersFailed,
	}

	// 复制标签
	clone.NodeLabels = make([]string, len(n.NodeLabels))
	copy(clone.NodeLabels, n.NodeLabels)

	// 复制容器
	clone.Containers = make([]*common.Container, len(n.Containers))
	copy(clone.Containers, n.Containers)

	return clone
}
