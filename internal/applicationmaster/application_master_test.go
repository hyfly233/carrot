package applicationmaster

import (
	"fmt"
	"testing"
	"time"

	"carrot/internal/common"

	"github.com/stretchr/testify/assert"
)

func TestNewApplicationMaster(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)

	assert.NotNil(t, am)
	assert.Equal(t, config.ApplicationID, am.applicationID)
	assert.Equal(t, config.ApplicationAttemptID, am.applicationAttemptID)
	assert.Equal(t, common.ApplicationStateRunning, am.applicationState)
	assert.Equal(t, common.FinalApplicationStatusUndefined, am.finalStatus)
	assert.Equal(t, float32(0.0), am.progress)
}

func TestApplicationMasterContainerManagement(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)

	// 测试请求容器
	requests := []*common.ContainerRequest{
		{
			Resource: common.Resource{Memory: 1024, VCores: 1},
			Priority: 1,
		},
		{
			Resource: common.Resource{Memory: 2048, VCores: 2},
			Priority: 2,
		},
	}

	am.RequestContainers(requests)

	assert.Len(t, am.pendingRequests, 2)
	assert.Equal(t, int64(1024), am.pendingRequests[0].Resource.Memory)
	assert.Equal(t, int32(1), am.pendingRequests[0].Resource.VCores)

	// 测试处理新容器
	container := &common.Container{
		ID: common.ContainerID{
			ApplicationAttemptID: am.applicationAttemptID,
			ContainerID:          1,
		},
		NodeID: common.NodeID{
			Host: "test-host",
			Port: 8042,
		},
		Resource: common.Resource{Memory: 1024, VCores: 1},
		Status:   "ALLOCATED",
		State:    common.ContainerStateNew,
	}

	am.handleNewContainer(container)

	containerKey := am.getContainerKey(container.ID)
	assert.Contains(t, am.allocatedContainers, containerKey)
	assert.Equal(t, container, am.allocatedContainers[containerKey])
}

func TestApplicationMasterProgress(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)

	// 初始进度应该是 0
	assert.Equal(t, float32(0.0), am.GetProgress())
	assert.Equal(t, common.ApplicationStateRunning, am.GetState())

	// 更新进度
	am.mu.Lock()
	am.progress = 0.5
	am.mu.Unlock()

	assert.Equal(t, float32(0.5), am.GetProgress())

	// 获取容器统计
	stats := am.GetContainerStatistics()
	assert.Equal(t, 0, stats["allocated"])
	assert.Equal(t, 0, stats["completed"])
	assert.Equal(t, 0, stats["failed"])
	assert.Equal(t, 0, stats["pending"])
}

func TestSimpleApplication(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)
	app := NewSimpleApplication(am, 3)

	assert.NotNil(t, app)
	assert.Equal(t, 3, app.totalTasks)
	assert.Equal(t, 0, app.completedTasks)

	// 测试进度计算
	assert.Equal(t, float32(0.0), app.GetProgress())

	// 模拟完成一些任务
	app.completedTasks = 1
	assert.Equal(t, float32(1.0/3.0), app.GetProgress())

	app.completedTasks = 3
	assert.Equal(t, float32(1.0), app.GetProgress())
}

func TestDistributedApplication(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)
	app := NewDistributedApplication(am, 3)

	assert.NotNil(t, app)
	assert.NotNil(t, app.masterTask)
	assert.Len(t, app.workerTasks, 3)

	// 验证主任务
	assert.Equal(t, "master", app.masterTask.ID)
	assert.Equal(t, "master", app.masterTask.Type)
	assert.Equal(t, "PENDING", app.masterTask.Status)
	assert.Equal(t, int64(2048), app.masterTask.Resource.Memory)
	assert.Equal(t, int32(2), app.masterTask.Resource.VCores)

	// 验证工作任务
	for i, task := range app.workerTasks {
		assert.Equal(t, fmt.Sprintf("worker-%d", i), task.ID)
		assert.Equal(t, "worker", task.Type)
		assert.Equal(t, "PENDING", task.Status)
		assert.Equal(t, int64(1024), task.Resource.Memory)
		assert.Equal(t, int32(1), task.Resource.VCores)
	}

	// 测试任务状态
	status := app.GetTaskStatus()
	assert.Contains(t, status, "master")
	assert.Contains(t, status, "workers")

	masterStatus := status["master"].(map[string]interface{})
	assert.Equal(t, "master", masterStatus["id"])
	assert.Equal(t, "PENDING", masterStatus["status"])
	assert.Equal(t, "master", masterStatus["type"])

	workers := status["workers"].([]map[string]interface{})
	assert.Len(t, workers, 3)
}

func TestApplicationMasterKeyGeneration(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)

	// 测试容器键生成
	containerID := common.ContainerID{
		ApplicationAttemptID: am.applicationAttemptID,
		ContainerID:          12345,
	}
	containerKey := am.getContainerKey(containerID)
	assert.Equal(t, "12345", containerKey)

	// 测试节点键生成
	nodeID := common.NodeID{
		Host: "test-host",
		Port: 8042,
	}
	nodeKey := am.getNodeKey(nodeID)
	assert.Equal(t, "test-host:8042", nodeKey)
}

func TestApplicationMasterContainerStateUpdate(t *testing.T) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)

	// 添加一个分配的容器
	containerID := common.ContainerID{
		ApplicationAttemptID: am.applicationAttemptID,
		ContainerID:          1,
	}
	container := &common.Container{
		ID:       containerID,
		NodeID:   common.NodeID{Host: "test-host", Port: 8042},
		Resource: common.Resource{Memory: 1024, VCores: 1},
		Status:   "ALLOCATED",
		State:    common.ContainerStateRunning,
	}

	containerKey := am.getContainerKey(containerID)
	am.allocatedContainers[containerKey] = container

	// 更新容器状态为完成
	am.updateContainerState(containerID, common.ContainerStateComplete)

	// 验证容器已移动到完成列表
	assert.NotContains(t, am.allocatedContainers, containerKey)
	assert.Contains(t, am.completedContainers, containerKey)
	assert.Equal(t, common.ContainerStateComplete, am.completedContainers[containerKey].State)
}

// 基准测试
func BenchmarkApplicationMasterContainerHandling(b *testing.B) {
	config := &ApplicationMasterConfig{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               1,
		},
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		RMAddress:           "http://localhost:8030",
		TrackingURL:         "http://localhost:8088",
		HeartbeatInterval:   10 * time.Second,
		MaxContainerRetries: 3,
		Port:                8088,
	}

	am := NewApplicationMaster(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container := &common.Container{
			ID: common.ContainerID{
				ApplicationAttemptID: am.applicationAttemptID,
				ContainerID:          int64(i),
			},
			NodeID: common.NodeID{
				Host: "test-host",
				Port: 8042,
			},
			Resource: common.Resource{Memory: 1024, VCores: 1},
			Status:   "ALLOCATED",
			State:    common.ContainerStateNew,
		}
		am.handleNewContainer(container)
	}
}
