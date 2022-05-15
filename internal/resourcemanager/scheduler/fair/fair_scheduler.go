package fair

import (
	"carrot/internal/common"
	"fmt"
	"math"
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

// FairScheduler 公平调度器
type FairScheduler struct {
	mu     sync.RWMutex
	rm     ResourceManagerInterface
	queues map[string]*FairQueue
	logger *zap.Logger
	config *FairSchedulerConfig
}

// FairSchedulerConfig 公平调度器配置
type FairSchedulerConfig struct {
	ContinuousSchedulingEnabled  bool                        `yaml:"continuous_scheduling_enabled" json:"continuous_scheduling_enabled"`
	PreemptionEnabled            bool                        `yaml:"preemption_enabled" json:"preemption_enabled"`
	DefaultQueueSchedulingPolicy string                      `yaml:"default_queue_scheduling_policy" json:"default_queue_scheduling_policy"`
	Queues                       map[string]*FairQueueConfig `yaml:"queues" json:"queues"`
}

// FairQueueConfig 公平队列配置
type FairQueueConfig struct {
	Name             string                      `yaml:"name" json:"name"`
	Type             string                      `yaml:"type" json:"type"` // "parent" or "leaf"
	Weight           float64                     `yaml:"weight" json:"weight"`
	MinResources     common.Resource             `yaml:"min_resources" json:"min_resources"`
	MaxResources     common.Resource             `yaml:"max_resources" json:"max_resources"`
	MaxRunningApps   int                         `yaml:"max_running_apps" json:"max_running_apps"`
	SchedulingPolicy string                      `yaml:"scheduling_policy" json:"scheduling_policy"`
	Children         map[string]*FairQueueConfig `yaml:"children,omitempty" json:"children,omitempty"`
}

// FairQueue 公平队列
type FairQueue struct {
	Name                string
	Type                string // "parent" or "leaf"
	Parent              *FairQueue
	Children            map[string]*FairQueue
	Weight              float64
	MinResources        common.Resource
	MaxResources        common.Resource
	MaxRunningApps      int
	SchedulingPolicy    string
	FairShare           common.Resource
	UsedResources       common.Resource
	RunningApplications []*ApplicationInfo
	PendingApplications []*ApplicationInfo
}

// NewFairScheduler 创建新的公平调度器
func NewFairScheduler(config *FairSchedulerConfig) *FairScheduler {
	logger, _ := zap.NewProduction()

	if config == nil {
		config = &FairSchedulerConfig{
			ContinuousSchedulingEnabled:  true,
			PreemptionEnabled:            false,
			DefaultQueueSchedulingPolicy: "fair",
			Queues: map[string]*FairQueueConfig{
				"root": {
					Name:   "root",
					Type:   "parent",
					Weight: 1.0,
					Children: map[string]*FairQueueConfig{
						"default": {
							Name:   "default",
							Type:   "leaf",
							Weight: 1.0,
						},
					},
				},
			},
		}
	}

	fs := &FairScheduler{
		queues: make(map[string]*FairQueue),
		logger: logger,
		config: config,
	}

	fs.initializeQueues()
	return fs
}

// SetResourceManager 设置资源管理器
func (fs *FairScheduler) SetResourceManager(rm ResourceManagerInterface) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.rm = rm
}

// Schedule 调度应用程序
func (fs *FairScheduler) Schedule(app *ApplicationInfo) ([]*ContainerAllocation, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.rm == nil {
		return nil, fmt.Errorf("resource manager not set")
	}

	// 获取或使用默认队列
	queueName := app.Queue
	if queueName == "" {
		queueName = "root.default"
	}

	queue := fs.getQueue(queueName)
	if queue == nil {
		return nil, fmt.Errorf("queue %s not found", queueName)
	}

	// 更新公平共享
	fs.updateFairShares()

	// 检查队列是否可以接受新的应用程序
	if !fs.canAllocateInQueue(queue, app.Resource) {
		return nil, fmt.Errorf("queue %s cannot allocate resources", queueName)
	}

	// 查找最佳节点
	bestNode := fs.findBestNode(app.Resource, queue)
	if bestNode == nil {
		return nil, fmt.Errorf("no suitable node found")
	}

	// 创建容器分配
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: app.ID,
			AttemptID:     1,
		},
		ContainerID: fs.rm.GetClusterTimestamp(),
	}

	allocation := &ContainerAllocation{
		ID:       containerID,
		NodeID:   bestNode.ID,
		Resource: app.Resource,
		Priority: app.Priority,
	}

	// 更新队列使用情况
	fs.updateQueueUsage(queue, app.Resource, true)

	fs.logger.Info("Container allocated",
		zap.String("container_id", fmt.Sprintf("%d", containerID.ContainerID)),
		zap.String("queue", queueName),
		zap.String("node", bestNode.ID.Host))

	return []*ContainerAllocation{allocation}, nil
}

// initializeQueues 初始化队列
func (fs *FairScheduler) initializeQueues() {
	fs.buildQueueHierarchy("root", nil, fs.config.Queues["root"])
}

// buildQueueHierarchy 构建队列层次结构
func (fs *FairScheduler) buildQueueHierarchy(name string, parent *FairQueue, config *FairQueueConfig) *FairQueue {
	if config == nil {
		return nil
	}

	queue := &FairQueue{
		Name:                name,
		Type:                config.Type,
		Parent:              parent,
		Children:            make(map[string]*FairQueue),
		Weight:              config.Weight,
		MinResources:        config.MinResources,
		MaxResources:        config.MaxResources,
		MaxRunningApps:      config.MaxRunningApps,
		SchedulingPolicy:    config.SchedulingPolicy,
		RunningApplications: make([]*ApplicationInfo, 0),
		PendingApplications: make([]*ApplicationInfo, 0),
	}

	if queue.Weight == 0 {
		queue.Weight = 1.0
	}

	if queue.SchedulingPolicy == "" {
		queue.SchedulingPolicy = fs.config.DefaultQueueSchedulingPolicy
	}

	fs.queues[name] = queue

	// 递归构建子队列
	if config.Children != nil {
		for childName, childConfig := range config.Children {
			fullChildName := name + "." + childName
			childQueue := fs.buildQueueHierarchy(fullChildName, queue, childConfig)
			if childQueue != nil {
				queue.Children[childName] = childQueue
			}
		}
	}

	return queue
}

// getQueue 获取队列
func (fs *FairScheduler) getQueue(name string) *FairQueue {
	return fs.queues[name]
}

// updateFairShares 更新公平共享
func (fs *FairScheduler) updateFairShares() {
	if rootQueue := fs.queues["root"]; rootQueue != nil {
		// 获取集群总资源
		clusterResource := fs.getClusterResource()
		fs.updateFairSharesRecursive(rootQueue, clusterResource)
	}
}

// getClusterResource 获取集群总资源
func (fs *FairScheduler) getClusterResource() common.Resource {
	nodes := fs.rm.GetNodesForScheduler()
	totalResource := common.Resource{}

	for _, node := range nodes {
		if node.State == common.NodeStateRunning {
			totalResource.Memory += node.AvailableResource.Memory + node.UsedResource.Memory
			totalResource.VCores += node.AvailableResource.VCores + node.UsedResource.VCores
		}
	}

	return totalResource
}

// updateFairSharesRecursive 递归更新公平共享
func (fs *FairScheduler) updateFairSharesRecursive(queue *FairQueue, parentResource common.Resource) {
	if len(queue.Children) == 0 {
		// 叶子队列，直接分配公平共享
		queue.FairShare = parentResource
	} else {
		// 父队列，根据权重分配给子队列
		totalWeight := 0.0
		for _, child := range queue.Children {
			totalWeight += child.Weight
		}

		for _, child := range queue.Children {
			if totalWeight > 0 {
				ratio := child.Weight / totalWeight
				childResource := common.Resource{
					Memory: int64(float64(parentResource.Memory) * ratio),
					VCores: int32(float64(parentResource.VCores) * ratio),
				}
				fs.updateFairSharesRecursive(child, childResource)
			}
		}
	}
}

// canAllocateInQueue 检查队列是否可以分配资源
func (fs *FairScheduler) canAllocateInQueue(queue *FairQueue, resource common.Resource) bool {
	// 检查应用程序数量限制
	if queue.MaxRunningApps > 0 && len(queue.RunningApplications) >= queue.MaxRunningApps {
		return false
	}

	// 检查最大资源限制
	if queue.MaxResources.Memory > 0 {
		if queue.UsedResources.Memory+resource.Memory > queue.MaxResources.Memory {
			return false
		}
	}
	if queue.MaxResources.VCores > 0 {
		if queue.UsedResources.VCores+resource.VCores > queue.MaxResources.VCores {
			return false
		}
	}

	return true
}

// findBestNode 查找最佳节点
func (fs *FairScheduler) findBestNode(resource common.Resource, queue *FairQueue) *NodeInfo {
	nodes := fs.rm.GetNodesForScheduler()

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

	// 计算每个节点的评分
	bestNode := availableNodes[0]
	bestScore := fs.calculateNodeScore(bestNode, resource)

	for _, node := range availableNodes[1:] {
		score := fs.calculateNodeScore(node, resource)
		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}

	return bestNode
}

// calculateNodeScore 计算节点评分
func (fs *FairScheduler) calculateNodeScore(node *NodeInfo, resource common.Resource) float64 {
	// 计算资源利用率
	totalMemory := node.AvailableResource.Memory + node.UsedResource.Memory
	totalVCores := node.AvailableResource.VCores + node.UsedResource.VCores

	if totalMemory == 0 || totalVCores == 0 {
		return 0
	}

	memoryUtilization := float64(node.UsedResource.Memory) / float64(totalMemory)
	vcoreUtilization := float64(node.UsedResource.VCores) / float64(totalVCores)

	// 偏好适中的利用率
	idealUtilization := 0.7
	memoryScore := 1.0 - math.Abs(memoryUtilization-idealUtilization)
	vcoreScore := 1.0 - math.Abs(vcoreUtilization-idealUtilization)

	return (memoryScore + vcoreScore) / 2.0
}

// updateQueueUsage 更新队列使用情况
func (fs *FairScheduler) updateQueueUsage(queue *FairQueue, resource common.Resource, increase bool) {
	if increase {
		queue.UsedResources.Memory += resource.Memory
		queue.UsedResources.VCores += resource.VCores
	} else {
		queue.UsedResources.Memory -= resource.Memory
		queue.UsedResources.VCores -= resource.VCores
	}

	// 递归更新父队列
	if queue.Parent != nil {
		fs.updateQueueUsage(queue.Parent, resource, increase)
	}
}

// calculateResourceDeficit 计算资源缺口
func (fs *FairScheduler) calculateResourceDeficit(queue *FairQueue) float64 {
	if queue.FairShare.Memory == 0 || queue.FairShare.VCores == 0 {
		return 0
	}

	memoryRatio := float64(queue.UsedResources.Memory) / float64(queue.FairShare.Memory)
	vcoreRatio := float64(queue.UsedResources.VCores) / float64(queue.FairShare.VCores)

	// 返回与公平共享的差距
	averageRatio := (memoryRatio + vcoreRatio) / 2.0
	return 1.0 - averageRatio
}

// sortQueuesByFairness 按公平性排序队列
func (fs *FairScheduler) sortQueuesByFairness() []*FairQueue {
	queues := make([]*FairQueue, 0)
	for _, queue := range fs.queues {
		if queue.Type == "leaf" && len(queue.PendingApplications) > 0 {
			queues = append(queues, queue)
		}
	}

	sort.Slice(queues, func(i, j int) bool {
		deficitI := fs.calculateResourceDeficit(queues[i])
		deficitJ := fs.calculateResourceDeficit(queues[j])
		return deficitI > deficitJ // 缺口越大，优先级越高
	})

	return queues
}
