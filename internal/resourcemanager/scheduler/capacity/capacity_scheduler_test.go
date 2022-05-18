package capacity

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

func TestCapacitySchedulerCreation(t *testing.T) {
	scheduler := NewCapacityScheduler(nil)

	assert.NotNil(t, scheduler)
	assert.NotNil(t, scheduler.queues)
	assert.NotNil(t, scheduler.config)

	// 检查默认队列是否存在
	rootQueue := scheduler.getQueue("root")
	assert.NotNil(t, rootQueue)

	defaultQueue := scheduler.getQueue("root.default")
	assert.NotNil(t, defaultQueue)
}

func TestCapacitySchedulerScheduling(t *testing.T) {
	scheduler := NewCapacityScheduler(nil)
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

func TestCapacitySchedulerQueueHierarchy(t *testing.T) {
	// 创建具有层次结构的配置
	config := &CapacitySchedulerConfig{
		Queues: map[string]*CapacityQueueConfig{
			"root": {
				Name:     "root",
				Type:     "parent",
				Capacity: 100.0,
				Children: map[string]*CapacityQueueConfig{
					"production": {
						Name:        "production",
						Type:        "leaf",
						Capacity:    70.0,
						MaxCapacity: 80.0,
					},
					"development": {
						Name:        "development",
						Type:        "leaf",
						Capacity:    30.0,
						MaxCapacity: 50.0,
					},
				},
			},
		},
	}

	scheduler := NewCapacityScheduler(config)

	// 验证队列层次结构
	rootQueue := scheduler.getQueue("root")
	require.NotNil(t, rootQueue)
	assert.Equal(t, "parent", rootQueue.Type)
	assert.Len(t, rootQueue.Children, 2)

	prodQueue := scheduler.getQueue("root.production")
	require.NotNil(t, prodQueue)
	assert.Equal(t, "leaf", prodQueue.Type)
	assert.Equal(t, 70.0, prodQueue.Capacity)

	devQueue := scheduler.getQueue("root.development")
	require.NotNil(t, devQueue)
	assert.Equal(t, "leaf", devQueue.Type)
	assert.Equal(t, 30.0, devQueue.Capacity)
}

func TestCapacitySchedulerAbsoluteCapacities(t *testing.T) {
	config := &CapacitySchedulerConfig{
		Queues: map[string]*CapacityQueueConfig{
			"root": {
				Name:     "root",
				Type:     "parent",
				Capacity: 100.0,
				Children: map[string]*CapacityQueueConfig{
					"queue1": {
						Name:     "queue1",
						Type:     "leaf",
						Capacity: 60.0,
					},
					"queue2": {
						Name:     "queue2",
						Type:     "leaf",
						Capacity: 40.0,
					},
				},
			},
		},
	}

	scheduler := NewCapacityScheduler(config)

	// 验证绝对容量计算
	queue1 := scheduler.getQueue("root.queue1")
	queue2 := scheduler.getQueue("root.queue2")

	require.NotNil(t, queue1)
	require.NotNil(t, queue2)

	// 绝对容量应该是相对容量 * 父容量 / 100
	assert.Equal(t, 60.0, queue1.AbsoluteCapacity)
	assert.Equal(t, 40.0, queue2.AbsoluteCapacity)
}

func TestCapacitySchedulerNoSuitableNode(t *testing.T) {
	scheduler := NewCapacityScheduler(nil)
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
