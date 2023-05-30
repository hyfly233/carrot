package common

import (
	"time"
)

// TypeConverter 提供类型转换功能，用于在不同的结构体之间转换
type TypeConverter struct{}

// NewTypeConverter 创建类型转换器
func NewTypeConverter() *TypeConverter {
	return &TypeConverter{}
}

// ConvertToBaseResource 将任意资源类型转换为基础资源
func (tc *TypeConverter) ConvertToBaseResource(src ResourceInterface) *BaseResource {
	if src == nil {
		return &BaseResource{}
	}
	return &BaseResource{
		Memory: src.GetMemory(),
		VCores: src.GetVCores(),
	}
}

// ConvertFromOldResource 从旧的Resource结构体转换
func (tc *TypeConverter) ConvertFromOldResource(memory int64, vcores int32) ResourceInterface {
	return &BaseResource{
		Memory: memory,
		VCores: vcores,
	}
}

// ConvertToExtendedResource 转换为扩展资源
func (tc *TypeConverter) ConvertToExtendedResource(src ResourceInterface, gpu int32, disk int64, network int64) *ExtendedResource {
	base := tc.ConvertToBaseResource(src)
	return &ExtendedResource{
		BaseResource: *base,
		GPU:          gpu,
		Disk:         disk,
		Network:      network,
		Attributes:   make(map[string]string),
	}
}

// ConvertToBaseNode 转换为基础节点
func (tc *TypeConverter) ConvertToBaseNode(host string, port int32) *BaseNode {
	return &BaseNode{
		ID: NodeID{
			Host: host,
			Port: port,
		},
		Host: host,
		Port: port,
	}
}

// ConvertToExtendedNode 转换为扩展节点
func (tc *TypeConverter) ConvertToExtendedNode(base *BaseNode, totalResource ResourceInterface, rackName string, labels []string) *ExtendedNode {
	return &ExtendedNode{
		BaseNode:          *base,
		RackName:          rackName,
		Labels:            labels,
		LastHeartbeat:     time.Now(),
		TotalResource:     totalResource,
		AvailableResource: totalResource,
		Capabilities:      make(map[string]interface{}),
	}
}

// ConvertToBaseContainer 转换为基础容器
func (tc *TypeConverter) ConvertToBaseContainer(id ContainerID, nodeID NodeID, resource ResourceInterface, state string) *BaseContainer {
	return &BaseContainer{
		ID:       id,
		NodeID:   nodeID,
		Resource: resource,
		State:    state,
	}
}

// ConvertToExtendedContainer 转换为扩展容器
func (tc *TypeConverter) ConvertToExtendedContainer(base *BaseContainer, status string, launchContext *ContainerLaunchContext) *ExtendedContainer {
	return &ExtendedContainer{
		BaseContainer: *base,
		Status:        status,
		StartTime:     time.Now(),
		LaunchContext: launchContext,
	}
}

// ConvertToBaseApplication 转换为基础应用程序
func (tc *TypeConverter) ConvertToBaseApplication(id ApplicationID, name, state, user string) *BaseApplication {
	return &BaseApplication{
		ID:    id,
		Name:  name,
		State: state,
		User:  user,
	}
}

// ConvertToExtendedApplication 转换为扩展应用程序
func (tc *TypeConverter) ConvertToExtendedApplication(base *BaseApplication, appType, queue string, priority int32, resource ResourceInterface) *ExtendedApplication {
	return &ExtendedApplication{
		BaseApplication: *base,
		Type:            appType,
		Queue:           queue,
		Priority:        priority,
		Resource:        resource,
		StartTime:       time.Now(),
		Tags:            make(map[string]string),
	}
}

// ConvertFromNodeReport 从NodeReport转换为ExtendedNode
func (tc *TypeConverter) ConvertFromNodeReport(report *NodeReport) *ExtendedNode {
	baseNode := tc.ConvertToBaseNode(report.NodeID.Host, report.NodeID.Port)
	totalResource := tc.ConvertFromOldResource(report.TotalResource.Memory, report.TotalResource.VCores)

	return &ExtendedNode{
		BaseNode:      *baseNode,
		RackName:      report.RackName,
		Labels:        report.NodeLabels,
		LastHeartbeat: report.LastHealthUpdate,
		TotalResource: totalResource,
		AvailableResource: tc.ConvertFromOldResource(
			report.TotalResource.Memory-report.UsedResource.Memory,
			report.TotalResource.VCores-report.UsedResource.VCores,
		),
		Capabilities: make(map[string]interface{}),
	}
}

// ConvertToNodeReport 从ExtendedNode转换为NodeReport
func (tc *TypeConverter) ConvertToNodeReport(node *ExtendedNode) *NodeReport {
	usedResource := node.TotalResource.Subtract(node.AvailableResource)
	usedBaseResource := tc.ConvertToBaseResource(usedResource)
	totalBaseResource := tc.ConvertToBaseResource(node.TotalResource)
	return &NodeReport{
		NodeID:           node.GetID(),
		HTTPAddress:      node.GetAddress(),
		RackName:         node.RackName,
		UsedResource:     Resource{Memory: usedBaseResource.Memory, VCores: usedBaseResource.VCores},
		TotalResource:    Resource{Memory: totalBaseResource.Memory, VCores: totalBaseResource.VCores},
		NumContainers:    0, // 需要从外部设置
		State:            "RUNNING",
		HealthReport:     "HEALTHY",
		LastHealthUpdate: node.LastHeartbeat,
		NodeLabels:       node.Labels,
		Containers:       []string{},
	}
}

// ConvertFromApplicationReport 从ApplicationReport转换为ExtendedApplication
func (tc *TypeConverter) ConvertFromApplicationReport(report *ApplicationReport) *ExtendedApplication {
	baseApp := tc.ConvertToBaseApplication(report.ApplicationID, report.ApplicationName, report.State, report.User)

	return &ExtendedApplication{
		BaseApplication: *baseApp,
		Type:            report.ApplicationType,
		Queue:           report.Queue,
		Progress:        report.Progress,
		StartTime:       report.StartTime,
		FinishTime:      report.FinishTime,
		TrackingURL:     report.TrackingURL,
		Tags:            make(map[string]string),
	}
}

// ConvertToApplicationReport 从ExtendedApplication转换为ApplicationReport
func (tc *TypeConverter) ConvertToApplicationReport(app *ExtendedApplication) *ApplicationReport {
	return &ApplicationReport{
		ApplicationID:   app.GetID(),
		ApplicationName: app.GetName(),
		ApplicationType: app.Type,
		User:            app.GetUser(),
		Queue:           app.Queue,
		Host:            "", // 需要从外部设置
		RPCPort:         0,  // 需要从外部设置
		TrackingURL:     app.TrackingURL,
		StartTime:       app.StartTime,
		FinishTime:      app.FinishTime,
		FinalStatus:     "", // 需要根据状态计算
		State:           app.GetState(),
		Progress:        app.Progress,
		Diagnostics:     "", // 需要从外部设置
	}
}

// ConvertFromContainer 从Container转换为ExtendedContainer
func (tc *TypeConverter) ConvertFromContainer(container *Container) *ExtendedContainer {
	baseContainer := tc.ConvertToBaseContainer(
		container.ID,
		container.NodeID,
		tc.ConvertFromOldResource(container.Resource.Memory, container.Resource.VCores),
		container.State,
	)

	return &ExtendedContainer{
		BaseContainer: *baseContainer,
		Status:        container.Status,
		StartTime:     time.Now(),
	}
}

// ConvertToContainer 从ExtendedContainer转换为Container
func (tc *TypeConverter) ConvertToContainer(container *ExtendedContainer) *Container {
	baseResource := tc.ConvertToBaseResource(container.GetResource())
	return &Container{
		ID:       container.GetID(),
		NodeID:   container.GetNodeID(),
		Resource: Resource{Memory: baseResource.Memory, VCores: baseResource.VCores},
		Status:   container.Status,
		State:    container.GetState(),
	}
}

// ResourceBuilder 资源构建器，支持流式API
type ResourceBuilder struct {
	resource *ExtendedResource
}

// NewResourceBuilder 创建资源构建器
func NewResourceBuilder() *ResourceBuilder {
	return &ResourceBuilder{
		resource: &ExtendedResource{
			BaseResource: BaseResource{},
			Attributes:   make(map[string]string),
		},
	}
}

// WithMemory 设置内存
func (rb *ResourceBuilder) WithMemory(memory int64) *ResourceBuilder {
	rb.resource.Memory = memory
	return rb
}

// WithVCores 设置虚拟核心数
func (rb *ResourceBuilder) WithVCores(vcores int32) *ResourceBuilder {
	rb.resource.VCores = vcores
	return rb
}

// WithGPU 设置GPU数量
func (rb *ResourceBuilder) WithGPU(gpu int32) *ResourceBuilder {
	rb.resource.GPU = gpu
	return rb
}

// WithDisk 设置磁盘空间
func (rb *ResourceBuilder) WithDisk(disk int64) *ResourceBuilder {
	rb.resource.Disk = disk
	return rb
}

// WithNetwork 设置网络带宽
func (rb *ResourceBuilder) WithNetwork(network int64) *ResourceBuilder {
	rb.resource.Network = network
	return rb
}

// WithAttribute 添加自定义属性
func (rb *ResourceBuilder) WithAttribute(key, value string) *ResourceBuilder {
	rb.resource.Attributes[key] = value
	return rb
}

// Build 构建资源
func (rb *ResourceBuilder) Build() ResourceInterface {
	return rb.resource
}

// NodeBuilder 节点构建器
type NodeBuilder struct {
	node *ExtendedNode
}

// NewNodeBuilder 创建节点构建器
func NewNodeBuilder() *NodeBuilder {
	return &NodeBuilder{
		node: &ExtendedNode{
			BaseNode:     BaseNode{},
			Capabilities: make(map[string]interface{}),
		},
	}
}

// WithID 设置节点ID
func (nb *NodeBuilder) WithID(host string, port int32) *NodeBuilder {
	nb.node.ID = NodeID{Host: host, Port: port}
	nb.node.Host = host
	nb.node.Port = port
	return nb
}

// WithRackName 设置机架名
func (nb *NodeBuilder) WithRackName(rackName string) *NodeBuilder {
	nb.node.RackName = rackName
	return nb
}

// WithLabels 设置标签
func (nb *NodeBuilder) WithLabels(labels []string) *NodeBuilder {
	nb.node.Labels = labels
	return nb
}

// WithTotalResource 设置总资源
func (nb *NodeBuilder) WithTotalResource(resource ResourceInterface) *NodeBuilder {
	nb.node.TotalResource = resource
	nb.node.AvailableResource = resource
	return nb
}

// WithCapability 添加能力
func (nb *NodeBuilder) WithCapability(key string, value interface{}) *NodeBuilder {
	nb.node.Capabilities[key] = value
	return nb
}

// Build 构建节点
func (nb *NodeBuilder) Build() *ExtendedNode {
	nb.node.LastHeartbeat = time.Now()
	return nb.node
}

// 全局类型转换器实例
var DefaultConverter = NewTypeConverter()

// 便捷函数
func ConvertResource(memory int64, vcores int32) ResourceInterface {
	return DefaultConverter.ConvertFromOldResource(memory, vcores)
}

func BuildResource() *ResourceBuilder {
	return NewResourceBuilder()
}

func BuildNode() *NodeBuilder {
	return NewNodeBuilder()
}
