package capacity

import (
	"carrot/internal/common"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
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

// CapacityScheduler 容量调度器
type CapacityScheduler struct {
	mu     sync.RWMutex
	rm     ResourceManagerInterface
	queues map[string]*Queue
	logger *zap.Logger
	config *CapacitySchedulerConfig
}

// CapacitySchedulerConfig 容量调度器配置
type CapacitySchedulerConfig struct {
	ResourceCalculator   string                          `yaml:"resource_calculator" json:"resource_calculator"`
	MaxApplications      int                             `yaml:"max_applications" json:"max_applications"`
	MaxAMResourcePercent float64                         `yaml:"max_am_resource_percent" json:"max_am_resource_percent"`
	Queues               map[string]*CapacityQueueConfig `yaml:"queues" json:"queues"`
}

// CapacityQueueConfig 容量队列配置
type CapacityQueueConfig struct {
	Name                   string                          `yaml:"name" json:"name"`
	Type                   string                          `yaml:"type" json:"type"` // "parent" or "leaf"
	Capacity               float64                         `yaml:"capacity" json:"capacity"`
	MaxCapacity            float64                         `yaml:"max_capacity" json:"max_capacity"`
	State                  string                          `yaml:"state" json:"state"`
	DefaultApplicationType string                          `yaml:"default_application_type" json:"default_application_type"`
	MaxApplications        int                             `yaml:"max_applications" json:"max_applications"`
	MaxAMResourcePercent   float64                         `yaml:"max_am_resource_percent" json:"max_am_resource_percent"`
	UserLimitFactor        float64                         `yaml:"user_limit_factor" json:"user_limit_factor"`
	OrderingPolicy         string                          `yaml:"ordering_policy" json:"ordering_policy"`
	Children               map[string]*CapacityQueueConfig `yaml:"children,omitempty" json:"children,omitempty"`
}

// Queue 队列
type Queue struct {
	Name                 string
	Type                 string // "parent" or "leaf"
	Parent               *Queue
	Children             map[string]*Queue
	Capacity             float64
	MaxCapacity          float64
	AbsoluteCapacity     float64
	AbsoluteMaxCapacity  float64
	State                string
	Applications         map[string]*ApplicationInfo
	PendingApplications  []*ApplicationInfo
	RunningApplications  []*ApplicationInfo
	UsedResources        common.Resource
	MaxApplications      int
	MaxAMResourcePercent float64
	UserLimitFactor      float64
	OrderingPolicy       string
}

// NewCapacityScheduler 创建新的容量调度器
func NewCapacityScheduler(config *CapacitySchedulerConfig) *CapacityScheduler {
	logger, _ := zap.NewProduction()

	if config == nil {
		config = &CapacitySchedulerConfig{
			ResourceCalculator:   "DefaultResourceCalculator",
			MaxApplications:      10000,
			MaxAMResourcePercent: 0.1,
			Queues: map[string]*CapacityQueueConfig{
				"root": {
					Name:     "root",
					Type:     "parent",
					Capacity: 100.0,
					Children: map[string]*CapacityQueueConfig{
						"default": {
							Name:     "default",
							Type:     "leaf",
							Capacity: 100.0,
						},
					},
				},
			},
		}
	}

	cs := &CapacityScheduler{
		queues: make(map[string]*Queue),
		logger: logger,
		config: config,
	}

	cs.initializeQueues()
	return cs
}

// SetResourceManager 设置资源管理器
func (cs *CapacityScheduler) SetResourceManager(rm ResourceManagerInterface) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.rm = rm
}

// Schedule 调度应用程序
func (cs *CapacityScheduler) Schedule(app *ApplicationInfo) ([]*ContainerAllocation, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.rm == nil {
		return nil, fmt.Errorf("resource manager not set")
	}

	// 获取或使用默认队列
	queueName := app.Queue
	if queueName == "" {
		queueName = "root.default"
	}

	queue := cs.getQueue(queueName)
	if queue == nil {
		return nil, fmt.Errorf("queue %s not found", queueName)
	}

	// 检查队列是否可以接受新的应用程序
	if !cs.canAllocateInQueue(queue, app.Resource) {
		return nil, fmt.Errorf("queue %s cannot allocate resources", queueName)
	}

	// 查找最佳节点
	bestNode := cs.findBestNode(app.Resource, queue)
	if bestNode == nil {
		return nil, fmt.Errorf("no suitable node found")
	}

	// 创建容器分配
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: app.ID,
			AttemptID:     1,
		},
		ContainerID: cs.rm.GetClusterTimestamp(),
	}

	allocation := &ContainerAllocation{
		ID:       containerID,
		NodeID:   bestNode.ID,
		Resource: app.Resource,
		Priority: app.Priority,
	}

	// 更新队列使用情况
	cs.updateQueueUsage(queue, app.Resource, true)

	cs.logger.Info("Container allocated",
		zap.String("container_id", fmt.Sprintf("%d", containerID.ContainerID)),
		zap.String("queue", queueName),
		zap.String("node", bestNode.ID.Host))

	return []*ContainerAllocation{allocation}, nil
}

// initializeQueues 初始化队列
func (cs *CapacityScheduler) initializeQueues() {
	cs.buildQueueHierarchy("root", nil, cs.config.Queues["root"])
	cs.calculateAbsoluteCapacities()
}

// buildQueueHierarchy 构建队列层次结构
func (cs *CapacityScheduler) buildQueueHierarchy(name string, parent *Queue, config *CapacityQueueConfig) *Queue {
	if config == nil {
		return nil
	}

	queue := &Queue{
		Name:                 name,
		Type:                 config.Type,
		Parent:               parent,
		Children:             make(map[string]*Queue),
		Capacity:             config.Capacity,
		MaxCapacity:          config.MaxCapacity,
		State:                config.State,
		Applications:         make(map[string]*ApplicationInfo),
		PendingApplications:  make([]*ApplicationInfo, 0),
		RunningApplications:  make([]*ApplicationInfo, 0),
		MaxApplications:      config.MaxApplications,
		MaxAMResourcePercent: config.MaxAMResourcePercent,
		UserLimitFactor:      config.UserLimitFactor,
		OrderingPolicy:       config.OrderingPolicy,
	}

	if queue.MaxCapacity == 0 {
		queue.MaxCapacity = 100.0
	}

	cs.queues[name] = queue

	// 递归构建子队列
	if config.Children != nil {
		for childName, childConfig := range config.Children {
			fullChildName := name + "." + childName
			childQueue := cs.buildQueueHierarchy(fullChildName, queue, childConfig)
			if childQueue != nil {
				queue.Children[childName] = childQueue
			}
		}
	}

	return queue
}

// calculateAbsoluteCapacities 计算绝对容量
func (cs *CapacityScheduler) calculateAbsoluteCapacities() {
	if rootQueue := cs.queues["root"]; rootQueue != nil {
		cs.calculateAbsoluteCapacitiesRecursive(rootQueue, 100.0)
	}
}

// calculateAbsoluteCapacitiesRecursive 递归计算绝对容量
func (cs *CapacityScheduler) calculateAbsoluteCapacitiesRecursive(queue *Queue, parentAbsoluteCapacity float64) {
	queue.AbsoluteCapacity = parentAbsoluteCapacity * queue.Capacity / 100.0
	queue.AbsoluteMaxCapacity = parentAbsoluteCapacity * queue.MaxCapacity / 100.0

	for _, child := range queue.Children {
		cs.calculateAbsoluteCapacitiesRecursive(child, queue.AbsoluteCapacity)
	}
}

// getQueue 获取队列
func (cs *CapacityScheduler) getQueue(name string) *Queue {
	return cs.queues[name]
}

// canAllocateInQueue 检查队列是否可以分配资源
func (cs *CapacityScheduler) canAllocateInQueue(queue *Queue, resource common.Resource) bool {
	if queue.State != "" && queue.State != "RUNNING" {
		return false
	}

	// 检查应用程序数量限制
	if queue.MaxApplications > 0 && len(queue.RunningApplications) >= queue.MaxApplications {
		return false
	}

	// 对于叶子队列，暂时总是允许分配（实际实现需要考虑容量限制）
	// 这里简化处理，在实际生产环境中需要考虑更多因素
	return true
}

// findBestNode 查找最佳节点
func (cs *CapacityScheduler) findBestNode(resource common.Resource, queue *Queue) *NodeInfo {
	nodes := cs.rm.GetNodesForScheduler()

	// 过滤可用节点
	var availableNodes []*NodeInfo
	for _, node := range nodes {
		if node.State == common.NodeStateRunning && node.HasSufficientResource(resource) {
			availableNodes = append(availableNodes, node)
		}
	}

	if len(availableNodes) == 0 {
		return nil
	}

	// 按可用资源排序，选择资源最多的节点
	sort.Slice(availableNodes, func(i, j int) bool {
		return availableNodes[i].AvailableResource.Memory > availableNodes[j].AvailableResource.Memory
	})

	return availableNodes[0]
}

// updateQueueUsage 更新队列使用情况
func (cs *CapacityScheduler) updateQueueUsage(queue *Queue, resource common.Resource, increase bool) {
	if increase {
		queue.UsedResources.Memory += resource.Memory
		queue.UsedResources.VCores += resource.VCores
	} else {
		queue.UsedResources.Memory -= resource.Memory
		queue.UsedResources.VCores -= resource.VCores
	}

	// 递归更新父队列
	if queue.Parent != nil {
		cs.updateQueueUsage(queue.Parent, resource, increase)
	}
}
