package fair

import (
	"carrot/internal/common"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockResourceManager 模拟资源管理器
type MockResourceManager struct {
	nodes            map[string]*NodeInfo
	clusterTimestamp int64
}

func NewMockResourceManager() *MockResourceManager {
	return &MockResourceManager{
		nodes:            make(map[string]*NodeInfo),
		clusterTimestamp: time.Now().Unix(),
	}
}

func (m *MockResourceManager) GetNodesForScheduler() map[string]*NodeInfo {
	return m.nodes
}

func (m *MockResourceManager) GetClusterTimestamp() int64 {
	return m.clusterTimestamp
}

func (m *MockResourceManager) AddNode(nodeID common.NodeID, available, used common.Resource) {
	key := nodeID.Host + ":" + string(rune(nodeID.Port))
	m.nodes[key] = &NodeInfo{
		ID:                nodeID,
		State:             common.NodeStateRunning,
		AvailableResource: available,
		UsedResource:      used,
	}
}

func TestFairSchedulerCreation(t *testing.T) {
	scheduler := NewFairScheduler(nil)

	assert.NotNil(t, scheduler)
	assert.NotNil(t, scheduler.queues)
	assert.NotNil(t, scheduler.config)
	assert.NotEmpty(t, scheduler.queues)

	// 检查默认队列是否存在
	rootQueue := scheduler.getQueue("root")
	assert.NotNil(t, rootQueue)

	defaultQueue := scheduler.getQueue("root.default")
	assert.NotNil(t, defaultQueue)
}

func TestFairSchedulerScheduling(t *testing.T) {
	scheduler := NewFairScheduler(nil)
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 添加一个测试节点
	nodeID := common.NodeID{Host: "test-node", Port: 8042}
	availableResource := common.Resource{Memory: 4096, VCores: 4}
	usedResource := common.Resource{Memory: 0, VCores: 0}
	mockRM.AddNode(nodeID, availableResource, usedResource)

	// 创建应用程序信息
	appInfo := &ApplicationInfo{
		ID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		Resource: common.Resource{Memory: 1024, VCores: 1},
	}

	// 调度应用程序
	containers, err := scheduler.Schedule(appInfo)

	require.NoError(t, err)
	assert.Len(t, containers, 1)
	assert.Equal(t, nodeID, containers[0].NodeID)
	assert.Equal(t, appInfo.Resource, containers[0].Resource)
}

func TestFairSchedulerWeightedQueues(t *testing.T) {
	// 创建具有不同权重的队列配置
	config := &FairSchedulerConfig{
		Queues: map[string]*FairQueueConfig{
			"root": {
				Name:   "root",
				Type:   "parent",
				Weight: 1.0,
				Children: map[string]*FairQueueConfig{
					"high": {
						Name:   "high",
						Type:   "leaf",
						Weight: 3.0, // 权重3
					},
					"low": {
						Name:   "low",
						Type:   "leaf",
						Weight: 1.0, // 权重1
					},
				},
			},
		},
	}

	scheduler := NewFairScheduler(config)
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 添加集群资源
	nodeID := common.NodeID{Host: "cluster-node", Port: 8042}
	clusterResource := common.Resource{Memory: 4000, VCores: 4}
	mockRM.AddNode(nodeID, clusterResource, common.Resource{})

	// 更新公平共享
	scheduler.updateFairShares()

	// 验证公平共享计算
	highQueue := scheduler.getQueue("root.high")
	lowQueue := scheduler.getQueue("root.low")

	require.NotNil(t, highQueue)
	require.NotNil(t, lowQueue)

	// high队列权重3，low队列权重1，总权重4
	// high队列应该得到3/4的资源，low队列应该得到1/4的资源
	expectedHighMemory := int64(3000) // 4000 * 3/4
	expectedLowMemory := int64(1000)  // 4000 * 1/4

	assert.Equal(t, expectedHighMemory, highQueue.FairShare.Memory)
	assert.Equal(t, expectedLowMemory, lowQueue.FairShare.Memory)
}

func TestFairSchedulerResourceConstraints(t *testing.T) {
	config := &FairSchedulerConfig{
		Queues: map[string]*FairQueueConfig{
			"root": {
				Name: "root",
				Type: "parent",
				Children: map[string]*FairQueueConfig{
					"limited": {
						Name:           "limited",
						Type:           "leaf",
						Weight:         1.0,
						MaxResources:   common.Resource{Memory: 2048, VCores: 2},
						MaxRunningApps: 3,
					},
				},
			},
		},
	}

	scheduler := NewFairScheduler(config)
	queue := scheduler.getQueue("root.limited")
	require.NotNil(t, queue)

	// 测试资源限制
	// 在限制内
	canAllocate := scheduler.canAllocateInQueue(queue, common.Resource{Memory: 1024, VCores: 1})
	assert.True(t, canAllocate)

	// 更新队列使用情况
	scheduler.updateQueueUsage(queue, common.Resource{Memory: 1024, VCores: 1}, true)

	// 现在尝试分配超过限制的资源
	canAllocate = scheduler.canAllocateInQueue(queue, common.Resource{Memory: 1500, VCores: 2})
	assert.False(t, canAllocate) // 应该失败，因为会超过内存限制

	// 测试应用数量限制
	for i := 0; i < 3; i++ {
		queue.RunningApplications = append(queue.RunningApplications, &ApplicationInfo{})
	}

	canAllocate = scheduler.canAllocateInQueue(queue, common.Resource{Memory: 100, VCores: 1})
	assert.False(t, canAllocate) // 应该失败，因为达到了应用数量限制
}

func TestFairSchedulerNoSuitableNode(t *testing.T) {
	scheduler := NewFairScheduler(nil)
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 不添加任何节点

	// 创建应用程序信息
	appInfo := &ApplicationInfo{
		ID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		Resource: common.Resource{Memory: 1024, VCores: 1},
	}

	// 调度应该失败
	containers, err := scheduler.Schedule(appInfo)

	assert.Error(t, err)
	assert.Empty(t, containers)
	assert.Contains(t, err.Error(), "no suitable node found")
}
