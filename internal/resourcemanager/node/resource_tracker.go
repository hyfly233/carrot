package node

import (
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ResourceTracker 资源跟踪器
type ResourceTracker struct {
	mu                 sync.RWMutex
	totalResources     common.Resource
	usedResources      common.Resource
	availableResources common.Resource
	reservedResources  common.Resource

	// 资源分配记录
	allocations map[string]*ResourceAllocation

	// 资源使用历史
	usageHistory   []*ResourceUsageSnapshot
	maxHistorySize int

	logger *zap.Logger
}

// ResourceAllocation 资源分配记录
type ResourceAllocation struct {
	ContainerID   common.ContainerID `json:"container_id"`
	Resource      common.Resource    `json:"resource"`
	NodeID        common.NodeID      `json:"node_id"`
	AllocatedTime time.Time          `json:"allocated_time"`
	Status        string             `json:"status"`
}

// ResourceUsageSnapshot 资源使用快照
type ResourceUsageSnapshot struct {
	Timestamp          time.Time       `json:"timestamp"`
	TotalResources     common.Resource `json:"total_resources"`
	UsedResources      common.Resource `json:"used_resources"`
	AvailableResources common.Resource `json:"available_resources"`
	ReservedResources  common.Resource `json:"reserved_resources"`
	UtilizationRate    float64         `json:"utilization_rate"`
}

// ResourceMetrics 资源指标
type ResourceMetrics struct {
	TotalMemory       int64   `json:"total_memory"`
	UsedMemory        int64   `json:"used_memory"`
	AvailableMemory   int64   `json:"available_memory"`
	ReservedMemory    int64   `json:"reserved_memory"`
	MemoryUtilization float64 `json:"memory_utilization"`

	TotalVCores      int32   `json:"total_vcores"`
	UsedVCores       int32   `json:"used_vcores"`
	AvailableVCores  int32   `json:"available_vcores"`
	ReservedVCores   int32   `json:"reserved_vcores"`
	VCoreUtilization float64 `json:"vcore_utilization"`

	ActiveAllocations int `json:"active_allocations"`
	TotalAllocations  int `json:"total_allocations"`
}

// NewResourceTracker 创建新的资源跟踪器
func NewResourceTracker() *ResourceTracker {
	return &ResourceTracker{
		allocations:    make(map[string]*ResourceAllocation),
		usageHistory:   make([]*ResourceUsageSnapshot, 0),
		maxHistorySize: 100, // 保留最近100个快照
		logger:         common.ComponentLogger("resource-tracker"),
	}
}

// SetTotalResources 设置总资源
func (rt *ResourceTracker) SetTotalResources(resources common.Resource) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.totalResources = resources
	rt.updateAvailableResources()

	rt.logger.Info("Total resources updated",
		zap.Int64("memory", resources.Memory),
		zap.Int32("vcores", resources.VCores))
}

// AllocateResource 分配资源
func (rt *ResourceTracker) AllocateResource(containerID common.ContainerID, resource common.Resource, nodeID common.NodeID) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// 检查资源是否足够
	if !rt.canAllocateUnlocked(resource) {
		return &InsufficientResourceError{
			Requested: resource,
			Available: rt.availableResources,
		}
	}

	// 创建分配记录
	allocationKey := rt.getAllocationKey(containerID)
	allocation := &ResourceAllocation{
		ContainerID:   containerID,
		Resource:      resource,
		NodeID:        nodeID,
		AllocatedTime: time.Now(),
		Status:        "ALLOCATED",
	}

	rt.allocations[allocationKey] = allocation
	rt.usedResources.Memory += resource.Memory
	rt.usedResources.VCores += resource.VCores
	rt.updateAvailableResources()

	rt.logger.Info("Resource allocated",
		zap.String("container_id", allocationKey),
		zap.Int64("memory", resource.Memory),
		zap.Int32("vcores", resource.VCores))

	return nil
}

// DeallocateResource 释放资源
func (rt *ResourceTracker) DeallocateResource(containerID common.ContainerID) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	allocationKey := rt.getAllocationKey(containerID)
	allocation, exists := rt.allocations[allocationKey]
	if !exists {
		return &AllocationNotFoundError{ContainerID: containerID}
	}

	// 释放资源
	rt.usedResources.Memory -= allocation.Resource.Memory
	rt.usedResources.VCores -= allocation.Resource.VCores
	rt.updateAvailableResources()

	// 更新分配状态
	allocation.Status = "DEALLOCATED"
	delete(rt.allocations, allocationKey)

	rt.logger.Info("Resource deallocated",
		zap.String("container_id", allocationKey),
		zap.Int64("memory", allocation.Resource.Memory),
		zap.Int32("vcores", allocation.Resource.VCores))

	return nil
}

// ReserveResource 预留资源
func (rt *ResourceTracker) ReserveResource(resource common.Resource) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// 检查是否有足够的可用资源
	if rt.availableResources.Memory < resource.Memory ||
		rt.availableResources.VCores < resource.VCores {
		return &InsufficientResourceError{
			Requested: resource,
			Available: rt.availableResources,
		}
	}

	rt.reservedResources.Memory += resource.Memory
	rt.reservedResources.VCores += resource.VCores
	rt.updateAvailableResources()

	rt.logger.Debug("Resource reserved",
		zap.Int64("memory", resource.Memory),
		zap.Int32("vcores", resource.VCores))

	return nil
}

// UnreserveResource 取消预留资源
func (rt *ResourceTracker) UnreserveResource(resource common.Resource) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.reservedResources.Memory -= resource.Memory
	rt.reservedResources.VCores -= resource.VCores

	// 确保不会变成负数
	if rt.reservedResources.Memory < 0 {
		rt.reservedResources.Memory = 0
	}
	if rt.reservedResources.VCores < 0 {
		rt.reservedResources.VCores = 0
	}

	rt.updateAvailableResources()

	rt.logger.Debug("Resource unreserved",
		zap.Int64("memory", resource.Memory),
		zap.Int32("vcores", resource.VCores))
}

// CanAllocate 检查是否可以分配指定资源
func (rt *ResourceTracker) CanAllocate(resource common.Resource) bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return rt.canAllocateUnlocked(resource)
}

// canAllocateUnlocked 内部无锁版本的检查方法
func (rt *ResourceTracker) canAllocateUnlocked(resource common.Resource) bool {
	return rt.availableResources.Memory >= resource.Memory &&
		rt.availableResources.VCores >= resource.VCores
}

// UpdateUsedResources 更新已使用资源（用于同步实际使用情况）
func (rt *ResourceTracker) UpdateUsedResources(usedResources common.Resource) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.usedResources = usedResources
	rt.updateAvailableResources()
}

// GetResourceMetrics 获取资源指标
func (rt *ResourceTracker) GetResourceMetrics() *ResourceMetrics {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	memoryUtilization := float64(0)
	if rt.totalResources.Memory > 0 {
		memoryUtilization = float64(rt.usedResources.Memory) / float64(rt.totalResources.Memory)
	}

	vcoreUtilization := float64(0)
	if rt.totalResources.VCores > 0 {
		vcoreUtilization = float64(rt.usedResources.VCores) / float64(rt.totalResources.VCores)
	}

	return &ResourceMetrics{
		TotalMemory:       rt.totalResources.Memory,
		UsedMemory:        rt.usedResources.Memory,
		AvailableMemory:   rt.availableResources.Memory,
		ReservedMemory:    rt.reservedResources.Memory,
		MemoryUtilization: memoryUtilization,

		TotalVCores:      rt.totalResources.VCores,
		UsedVCores:       rt.usedResources.VCores,
		AvailableVCores:  rt.availableResources.VCores,
		ReservedVCores:   rt.reservedResources.VCores,
		VCoreUtilization: vcoreUtilization,

		ActiveAllocations: len(rt.allocations),
		TotalAllocations:  len(rt.allocations), // 这里简化了，实际应该跟踪总分配数
	}
}

// GetAllocation 获取资源分配信息
func (rt *ResourceTracker) GetAllocation(containerID common.ContainerID) (*ResourceAllocation, error) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	allocationKey := rt.getAllocationKey(containerID)
	allocation, exists := rt.allocations[allocationKey]
	if !exists {
		return nil, &AllocationNotFoundError{ContainerID: containerID}
	}

	return allocation, nil
}

// GetAllAllocations 获取所有资源分配
func (rt *ResourceTracker) GetAllAllocations() []*ResourceAllocation {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	allocations := make([]*ResourceAllocation, 0, len(rt.allocations))
	for _, allocation := range rt.allocations {
		allocations = append(allocations, allocation)
	}

	return allocations
}

// TakeSnapshot 创建资源使用快照
func (rt *ResourceTracker) TakeSnapshot() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	utilizationRate := float64(0)
	if rt.totalResources.Memory > 0 {
		utilizationRate = float64(rt.usedResources.Memory) / float64(rt.totalResources.Memory)
	}

	snapshot := &ResourceUsageSnapshot{
		Timestamp:          time.Now(),
		TotalResources:     rt.totalResources,
		UsedResources:      rt.usedResources,
		AvailableResources: rt.availableResources,
		ReservedResources:  rt.reservedResources,
		UtilizationRate:    utilizationRate,
	}

	rt.usageHistory = append(rt.usageHistory, snapshot)

	// 保持历史记录大小在限制内
	if len(rt.usageHistory) > rt.maxHistorySize {
		rt.usageHistory = rt.usageHistory[1:]
	}
}

// GetUsageHistory 获取资源使用历史
func (rt *ResourceTracker) GetUsageHistory() []*ResourceUsageSnapshot {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	history := make([]*ResourceUsageSnapshot, len(rt.usageHistory))
	copy(history, rt.usageHistory)

	return history
}

// updateAvailableResources 更新可用资源
func (rt *ResourceTracker) updateAvailableResources() {
	rt.availableResources = common.Resource{
		Memory: rt.totalResources.Memory - rt.usedResources.Memory - rt.reservedResources.Memory,
		VCores: rt.totalResources.VCores - rt.usedResources.VCores - rt.reservedResources.VCores,
	}

	// 确保不会变成负数
	if rt.availableResources.Memory < 0 {
		rt.availableResources.Memory = 0
	}
	if rt.availableResources.VCores < 0 {
		rt.availableResources.VCores = 0
	}
}

// getAllocationKey 获取分配键
func (rt *ResourceTracker) getAllocationKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ApplicationAttemptID.AttemptID,
		containerID.ContainerID)
}

// GetTotalResources 获取总资源
func (rt *ResourceTracker) GetTotalResources() common.Resource {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.totalResources
}

// GetUsedResources 获取已使用资源
func (rt *ResourceTracker) GetUsedResources() common.Resource {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.usedResources
}

// GetAvailableResources 获取可用资源
func (rt *ResourceTracker) GetAvailableResources() common.Resource {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.availableResources
}

// GetReservedResources 获取预留资源
func (rt *ResourceTracker) GetReservedResources() common.Resource {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.reservedResources
}

// 自定义错误类型

// InsufficientResourceError 资源不足错误
type InsufficientResourceError struct {
	Requested common.Resource
	Available common.Resource
}

func (e *InsufficientResourceError) Error() string {
	return fmt.Sprintf("insufficient resources: requested (memory: %d MB, vcores: %d), available (memory: %d MB, vcores: %d)",
		e.Requested.Memory, e.Requested.VCores,
		e.Available.Memory, e.Available.VCores)
}

// AllocationNotFoundError 分配不存在错误
type AllocationNotFoundError struct {
	ContainerID common.ContainerID
}

func (e *AllocationNotFoundError) Error() string {
	return fmt.Sprintf("allocation not found for container %d", e.ContainerID.ContainerID)
}
