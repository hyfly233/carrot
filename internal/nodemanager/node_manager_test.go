package nodemanager

import (
	"carrot/internal/common"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNodeManagerCreation(t *testing.T) {
	nodeID := common.NodeID{Host: "test-host", Port: 8042}
	resource := common.Resource{Memory: 4096, VCores: 4}
	rmURL := "http://localhost:8088"

	nm := NewNodeManager(nodeID, resource, rmURL)

	if nm == nil {
		t.Fatal("Failed to create NodeManager")
	}

	if nm.nodeID.Host != "test-host" {
		t.Errorf("Expected host 'test-host', got '%s'", nm.nodeID.Host)
	}

	if nm.nodeID.Port != 8042 {
		t.Errorf("Expected port 8042, got %d", nm.nodeID.Port)
	}

	if nm.totalResource.Memory != 4096 {
		t.Errorf("Expected memory 4096, got %d", nm.totalResource.Memory)
	}

	if nm.totalResource.VCores != 4 {
		t.Errorf("Expected vcores 4, got %d", nm.totalResource.VCores)
	}

	if nm.resourceManagerURL != rmURL {
		t.Errorf("Expected RM URL '%s', got '%s'", rmURL, nm.resourceManagerURL)
	}
}

func TestContainerManagement(t *testing.T) {
	nodeID := common.NodeID{Host: "test-host", Port: 8042}
	resource := common.Resource{Memory: 4096, VCores: 4}
	nm := NewNodeManager(nodeID, resource, "http://localhost:8088")

	// 测试容器启动
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		ContainerID: 1,
	}

	launchContext := common.ContainerLaunchContext{
		Commands: []string{"echo 'test container'"},
		Environment: map[string]string{
			"TEST_VAR": "test_value",
		},
	}

	containerResource := common.Resource{Memory: 1024, VCores: 1}

	err := nm.StartContainer(containerID, launchContext, containerResource)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	// 验证容器状态
	container, err := nm.GetContainerStatus(containerID)
	if err != nil {
		t.Fatalf("Failed to get container status: %v", err)
	}

	if container.State != common.ContainerStateRunning && container.State != common.ContainerStateNew {
		t.Errorf("Expected container state RUNNING or NEW, got %s", container.State)
	}

	// 验证资源使用
	if nm.usedResource.Memory != 1024 {
		t.Errorf("Expected used memory 1024, got %d", nm.usedResource.Memory)
	}

	if nm.usedResource.VCores != 1 {
		t.Errorf("Expected used vcores 1, got %d", nm.usedResource.VCores)
	}

	// 测试容器停止
	err = nm.StopContainer(containerID)
	if err != nil {
		t.Fatalf("Failed to stop container: %v", err)
	}

	// 验证资源释放
	if nm.usedResource.Memory != 0 {
		t.Errorf("Expected used memory 0 after stopping container, got %d", nm.usedResource.Memory)
	}

	if nm.usedResource.VCores != 0 {
		t.Errorf("Expected used vcores 0 after stopping container, got %d", nm.usedResource.VCores)
	}
}

func TestResourceValidation(t *testing.T) {
	nodeID := common.NodeID{Host: "test-host", Port: 8042}
	resource := common.Resource{Memory: 2048, VCores: 2}
	nm := NewNodeManager(nodeID, resource, "http://localhost:8088")

	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: time.Now().Unix(),
				ID:               1,
			},
			AttemptID: 1,
		},
		ContainerID: 1,
	}

	launchContext := common.ContainerLaunchContext{
		Commands: []string{"echo 'test'"},
	}

	// 测试资源不足的情况
	insufficientResource := common.Resource{Memory: 4096, VCores: 4} // 超过节点总资源

	err := nm.StartContainer(containerID, launchContext, insufficientResource)
	if err == nil {
		t.Error("Expected error for insufficient resources, but got none")
	}

	// 测试正常资源分配
	normalResource := common.Resource{Memory: 1024, VCores: 1}

	err = nm.StartContainer(containerID, launchContext, normalResource)
	if err != nil {
		t.Errorf("Failed to start container with sufficient resources: %v", err)
	}

	// 清理
	nm.StopContainer(containerID)
}

func TestHTTPHandlers(t *testing.T) {
	nodeID := common.NodeID{Host: "test-host", Port: 8042}
	resource := common.Resource{Memory: 4096, VCores: 4}
	nm := NewNodeManager(nodeID, resource, "http://localhost:8088")

	// 测试容器列表端点
	t.Run("ContainersList", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws/v1/node/containers", nil)
		w := httptest.NewRecorder()

		nm.handleContainers(w, req)

		if w.Code != 200 {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
		}
	})
}
