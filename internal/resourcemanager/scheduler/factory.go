package scheduler

import (
	"carrot/internal/common"
	"carrot/internal/resourcemanager/scheduler/capacity"
	"carrot/internal/resourcemanager/scheduler/fair"
	"carrot/internal/resourcemanager/scheduler/fifo"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ApplicationInfo 统一的应用程序信息结构
type ApplicationInfo struct {
	ID         common.ApplicationID `json:"id"`
	Resource   common.Resource      `json:"resource"`
	SubmitTime time.Time            `json:"submit_time"`
	Queue      string               `json:"queue"`
	Priority   int32                `json:"priority"`
}

// ContainerAllocation 统一的容器分配信息结构
type ContainerAllocation struct {
	ID       common.ContainerID `json:"id"`
	NodeID   common.NodeID      `json:"node_id"`
	Resource common.Resource    `json:"resource"`
	Priority int32              `json:"priority"`
}

// Scheduler 统一的调度器接口
type Scheduler interface {
	Schedule(appInfo *ApplicationInfo) ([]*ContainerAllocation, error)
	SetResourceManager(rm ResourceManagerInterface)
}

// ResourceManagerInterface 资源管理器接口
type ResourceManagerInterface interface {
	GetAvailableNodes() []NodeInfo
	GetNodesForScheduler() map[string]*NodeInfo
	GetClusterTimestamp() int64
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID                common.NodeID   `json:"id"`
	Resource          common.Resource `json:"resource"`
	AvailableResource common.Resource `json:"available_resource"`
	State             string          `json:"state"`
	UsedResource      common.Resource `json:"used_resource"`
	LastHeartbeat     time.Time       `json:"last_heartbeat"`
}

// SchedulerAdapter 调度器适配器
type SchedulerAdapter struct {
	fifoScheduler     *fifo.FIFOScheduler
	capacityScheduler *capacity.CapacityScheduler
	fairScheduler     *fair.FairScheduler
	schedulerType     string
}

// SetResourceManager 设置资源管理器
func (sa *SchedulerAdapter) SetResourceManager(rm ResourceManagerInterface) {
	switch sa.schedulerType {
	case "fifo":
		if sa.fifoScheduler != nil {
			sa.fifoScheduler.SetResourceManager(&fifoAdapter{rm: rm})
		}
	case "capacity":
		if sa.capacityScheduler != nil {
			sa.capacityScheduler.SetResourceManager(&capacityAdapter{rm: rm})
		}
	case "fair":
		if sa.fairScheduler != nil {
			sa.fairScheduler.SetResourceManager(&fairAdapter{rm: rm})
		}
	}
}

// Schedule 调度应用程序
func (sa *SchedulerAdapter) Schedule(appInfo *ApplicationInfo) ([]*ContainerAllocation, error) {
	switch sa.schedulerType {
	case "fifo":
		if sa.fifoScheduler != nil {
			fifoApp := &fifo.ApplicationInfo{
				ID:         appInfo.ID,
				Resource:   appInfo.Resource,
				SubmitTime: appInfo.SubmitTime,
				Queue:      appInfo.Queue,
				Priority:   appInfo.Priority,
			}
			allocations, err := sa.fifoScheduler.Schedule(fifoApp)
			if err != nil {
				return nil, err
			}
			// 转换返回类型
			result := make([]*ContainerAllocation, len(allocations))
			for i, allocation := range allocations {
				result[i] = &ContainerAllocation{
					ID:       allocation.ID,
					NodeID:   allocation.NodeID,
					Resource: allocation.Resource,
					Priority: allocation.Priority,
				}
			}
			return result, nil
		}
	case "capacity":
		if sa.capacityScheduler != nil {
			capacityApp := &capacity.ApplicationInfo{
				ID:         appInfo.ID,
				Resource:   appInfo.Resource,
				SubmitTime: appInfo.SubmitTime,
				Queue:      appInfo.Queue,
				Priority:   appInfo.Priority,
			}
			allocations, err := sa.capacityScheduler.Schedule(capacityApp)
			if err != nil {
				return nil, err
			}
			// 转换返回类型
			result := make([]*ContainerAllocation, len(allocations))
			for i, allocation := range allocations {
				result[i] = &ContainerAllocation{
					ID:       allocation.ID,
					NodeID:   allocation.NodeID,
					Resource: allocation.Resource,
					Priority: allocation.Priority,
				}
			}
			return result, nil
		}
	case "fair":
		if sa.fairScheduler != nil {
			fairApp := &fair.ApplicationInfo{
				ID:         appInfo.ID,
				Resource:   appInfo.Resource,
				SubmitTime: appInfo.SubmitTime,
				Queue:      appInfo.Queue,
				Priority:   appInfo.Priority,
			}
			allocations, err := sa.fairScheduler.Schedule(fairApp)
			if err != nil {
				return nil, err
			}
			// 转换返回类型
			result := make([]*ContainerAllocation, len(allocations))
			for i, allocation := range allocations {
				result[i] = &ContainerAllocation{
					ID:       allocation.ID,
					NodeID:   allocation.NodeID,
					Resource: allocation.Resource,
					Priority: allocation.Priority,
				}
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("no scheduler available")
}

// 各个调度器的资源管理器适配器
type fifoAdapter struct {
	rm ResourceManagerInterface
}

func (fa *fifoAdapter) GetNodesForScheduler() map[string]*fifo.NodeInfo {
	nodes := fa.rm.GetNodesForScheduler()
	result := make(map[string]*fifo.NodeInfo)
	for key, node := range nodes {
		result[key] = &fifo.NodeInfo{
			ID:                node.ID,
			State:             node.State,
			AvailableResource: node.AvailableResource,
			UsedResource:      node.UsedResource,
			LastHeartbeat:     node.LastHeartbeat,
		}
	}
	return result
}

func (fa *fifoAdapter) GetClusterTimestamp() int64 {
	return fa.rm.GetClusterTimestamp()
}

type capacityAdapter struct {
	rm ResourceManagerInterface
}

func (ca *capacityAdapter) GetNodesForScheduler() map[string]*capacity.NodeInfo {
	nodes := ca.rm.GetNodesForScheduler()
	result := make(map[string]*capacity.NodeInfo)
	for key, node := range nodes {
		result[key] = &capacity.NodeInfo{
			ID:                node.ID,
			State:             node.State,
			AvailableResource: node.AvailableResource,
			UsedResource:      node.UsedResource,
			LastHeartbeat:     node.LastHeartbeat,
		}
	}
	return result
}

func (ca *capacityAdapter) GetClusterTimestamp() int64 {
	return ca.rm.GetClusterTimestamp()
}

type fairAdapter struct {
	rm ResourceManagerInterface
}

func (fa *fairAdapter) GetNodesForScheduler() map[string]*fair.NodeInfo {
	nodes := fa.rm.GetNodesForScheduler()
	result := make(map[string]*fair.NodeInfo)
	for key, node := range nodes {
		result[key] = &fair.NodeInfo{
			ID:                node.ID,
			State:             node.State,
			AvailableResource: node.AvailableResource,
			UsedResource:      node.UsedResource,
			LastHeartbeat:     node.LastHeartbeat,
		}
	}
	return result
}

func (fa *fairAdapter) GetClusterTimestamp() int64 {
	return fa.rm.GetClusterTimestamp()
}

// CreateScheduler 创建调度器
func CreateScheduler(config *common.Config) (Scheduler, error) {
	logger, _ := zap.NewProduction()

	var schedulerType string

	// 处理配置为空的情况
	if config == nil || config.Scheduler.Type == "" {
		schedulerType = "fifo" // 默认使用FIFO调度器
	} else {
		schedulerType = strings.ToLower(strings.TrimSpace(config.Scheduler.Type))
		if schedulerType == "" {
			schedulerType = "fifo" // 如果trim后为空，使用默认调度器
		}
	}

	adapter := &SchedulerAdapter{schedulerType: schedulerType}

	switch schedulerType {
	case "fifo":
		logger.Info("Creating FIFO scheduler")
		adapter.fifoScheduler = fifo.NewFIFOScheduler()
		return adapter, nil

	case "capacity":
		logger.Info("Creating Capacity scheduler")
		var capacityConfig *capacity.CapacitySchedulerConfig
		if config != nil && config.Scheduler.CapacityScheduler != nil {
			// 转换配置
			capacityConfig = convertCapacityConfig(config.Scheduler.CapacityScheduler)
		}
		adapter.capacityScheduler = capacity.NewCapacityScheduler(capacityConfig)
		return adapter, nil

	case "fair":
		logger.Info("Creating Fair scheduler")
		var fairConfig *fair.FairSchedulerConfig
		if config != nil && config.Scheduler.FairScheduler != nil {
			// 转换配置
			fairConfig = convertFairConfig(config.Scheduler.FairScheduler)
		}
		adapter.fairScheduler = fair.NewFairScheduler(fairConfig)
		return adapter, nil

	default:
		return nil, fmt.Errorf("unsupported scheduler type: %s", schedulerType)
	}
}

// convertCapacityConfig 转换容量调度器配置
func convertCapacityConfig(config *common.CapacitySchedulerConfig) *capacity.CapacitySchedulerConfig {
	capacityConfig := &capacity.CapacitySchedulerConfig{
		ResourceCalculator:   config.ResourceCalculator,
		MaxApplications:      config.MaxApplications,
		MaxAMResourcePercent: config.MaxAMResourcePercent,
		Queues:               make(map[string]*capacity.CapacityQueueConfig),
	}

	// 转换队列配置
	for name, queueConfig := range config.Queues {
		capacityConfig.Queues[name] = convertCapacityQueueConfig(queueConfig)
	}

	return capacityConfig
}

// convertCapacityQueueConfig 转换容量队列配置
func convertCapacityQueueConfig(config *common.CapacityQueueConfig) *capacity.CapacityQueueConfig {
	queueConfig := &capacity.CapacityQueueConfig{
		Name:                   config.Name,
		Type:                   config.Type,
		Capacity:               config.Capacity,
		MaxCapacity:            config.MaxCapacity,
		State:                  config.State,
		DefaultApplicationType: config.DefaultApplicationType,
		MaxApplications:        config.MaxApplications,
		MaxAMResourcePercent:   config.MaxAMResourcePercent,
		UserLimitFactor:        config.UserLimitFactor,
		OrderingPolicy:         config.OrderingPolicy,
	}

	// 转换子队列
	if config.Children != nil {
		queueConfig.Children = make(map[string]*capacity.CapacityQueueConfig)
		for name, childConfig := range config.Children {
			queueConfig.Children[name] = convertCapacityQueueConfig(childConfig)
		}
	}

	return queueConfig
}

// convertFairConfig 转换公平调度器配置
func convertFairConfig(config *common.FairSchedulerConfig) *fair.FairSchedulerConfig {
	fairConfig := &fair.FairSchedulerConfig{
		ContinuousSchedulingEnabled:  config.ContinuousSchedulingEnabled,
		PreemptionEnabled:            config.PreemptionEnabled,
		DefaultQueueSchedulingPolicy: config.DefaultQueueSchedulingPolicy,
		Queues:                       make(map[string]*fair.FairQueueConfig),
	}

	// 转换队列配置
	for name, queueConfig := range config.Queues {
		fairConfig.Queues[name] = convertFairQueueConfig(queueConfig)
	}

	return fairConfig
}

// convertFairQueueConfig 转换公平队列配置
func convertFairQueueConfig(config *common.FairQueueConfig) *fair.FairQueueConfig {
	queueConfig := &fair.FairQueueConfig{
		Name:             config.Name,
		Type:             config.Type,
		Weight:           config.Weight,
		MinResources:     config.MinResources,
		MaxResources:     config.MaxResources,
		MaxRunningApps:   config.MaxRunningApps,
		SchedulingPolicy: config.SchedulingPolicy,
	}

	// 转换子队列
	if config.Children != nil {
		queueConfig.Children = make(map[string]*fair.FairQueueConfig)
		for name, childConfig := range config.Children {
			queueConfig.Children[name] = convertFairQueueConfig(childConfig)
		}
	}

	return queueConfig
}
