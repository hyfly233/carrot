package fifo

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

func TestFIFOSchedulerCreation(t *testing.T) {
	scheduler := NewFIFOScheduler()

	assert.NotNil(t, scheduler)
	assert.NotNil(t, scheduler.pendingApplications)
	assert.Equal(t, 0, len(scheduler.pendingApplications))
}

func TestFIFOSchedulerScheduling(t *testing.T) {
	scheduler := NewFIFOScheduler()
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 添加测试节点
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

func TestFIFOSchedulerMultipleApplications(t *testing.T) {
	scheduler := NewFIFOScheduler()
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 添加测试节点
	nodeID := common.NodeID{Host: "test-node", Port: 8042}
	availableResource := common.Resource{Memory: 4096, VCores: 4}
	usedResource := common.Resource{Memory: 0, VCores: 0}
	mockRM.AddNode(nodeID, availableResource, usedResource)

	// 创建多个应用程序
	app1 := &ApplicationInfo{
		ID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		Resource:   common.Resource{Memory: 1024, VCores: 1},
		SubmitTime: time.Now().Add(-2 * time.Minute), // 更早提交
	}

	app2 := &ApplicationInfo{
		ID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               2,
		},
		Resource:   common.Resource{Memory: 1024, VCores: 1},
		SubmitTime: time.Now().Add(-1 * time.Minute), // 稍晚提交
	}

	// 调度应用程序
	containers1, err := scheduler.Schedule(app1)
	require.NoError(t, err)
	assert.Len(t, containers1, 1)

	containers2, err := scheduler.Schedule(app2)
	require.NoError(t, err)
	assert.Len(t, containers2, 1)

	// 验证FIFO顺序：应该将应用程序添加到待处理队列
	assert.Equal(t, 2, len(scheduler.pendingApplications))
}

func TestFIFOSchedulerResourceConstraints(t *testing.T) {
	scheduler := NewFIFOScheduler()
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 添加资源有限的节点
	nodeID := common.NodeID{Host: "limited-node", Port: 8042}
	availableResource := common.Resource{Memory: 1024, VCores: 1}
	usedResource := common.Resource{Memory: 0, VCores: 0}
	mockRM.AddNode(nodeID, availableResource, usedResource)

	// 尝试调度需要更多资源的应用程序
	appInfo := &ApplicationInfo{
		ID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		Resource: common.Resource{Memory: 2048, VCores: 2}, // 超出节点能力
	}

	// 调度应该失败
	containers, err := scheduler.Schedule(appInfo)

	assert.Error(t, err)
	assert.Empty(t, containers)
	assert.Contains(t, err.Error(), "no suitable node found")
}

func TestFIFOSchedulerNoNodes(t *testing.T) {
	scheduler := NewFIFOScheduler()
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 不添加任何节点

	// 创建应用程序
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

func TestFIFOSchedulerNodeSelection(t *testing.T) {
	scheduler := NewFIFOScheduler()
	mockRM := NewMockResourceManager()
	scheduler.SetResourceManager(mockRM)

	// 添加多个不同容量的节点
	node1 := common.NodeID{Host: "node1", Port: 8042}
	node2 := common.NodeID{Host: "node2", Port: 8042}

	mockRM.AddNode(node1, common.Resource{Memory: 2048, VCores: 2}, common.Resource{Memory: 0, VCores: 0})
	mockRM.AddNode(node2, common.Resource{Memory: 4096, VCores: 4}, common.Resource{Memory: 0, VCores: 0})

	// 创建适合两个节点的应用程序
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

	// 应该选择其中一个节点
	allocatedNode := containers[0].NodeID
	assert.True(t, allocatedNode.Host == "node1" || allocatedNode.Host == "node2")
}
