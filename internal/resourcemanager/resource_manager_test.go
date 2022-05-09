package resourcemanager

import (
	"bytes"
	"carrot/internal/common"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResourceManagerCreation(t *testing.T) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	assert.NotNil(t, rm, "ResourceManager should not be nil")
	assert.NotZero(t, rm.clusterTimestamp, "Cluster timestamp should be set")
	assert.NotNil(t, rm.applications, "Applications map should be initialized")
	assert.NotNil(t, rm.nodes, "Nodes map should be initialized")
	assert.NotNil(t, rm.logger, "Logger should be initialized")
}

func TestNodeRegistration(t *testing.T) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	nodeID := common.NodeID{
		Host: "test-host",
		Port: 8042,
	}

	resource := common.Resource{
		Memory: 4096,
		VCores: 4,
	}

	err := rm.RegisterNode(nodeID, resource, "http://test-host:8042")
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	nodes := rm.GetNodes()
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(nodes))
	}

	// 验证节点信息
	for _, node := range nodes {
		if node.NodeID.Host != "test-host" {
			t.Errorf("Expected host 'test-host', got '%s'", node.NodeID.Host)
		}
		if node.NodeID.Port != 8042 {
			t.Errorf("Expected port 8042, got %d", node.NodeID.Port)
		}
		if node.Capability.Memory != 4096 {
			t.Errorf("Expected memory 4096, got %d", node.Capability.Memory)
		}
		if node.Capability.VCores != 4 {
			t.Errorf("Expected vcores 4, got %d", node.Capability.VCores)
		}
	}
}

func TestApplicationSubmission(t *testing.T) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	// 先注册一个节点
	nodeID := common.NodeID{Host: "test-host", Port: 8042}
	resource := common.Resource{Memory: 4096, VCores: 4}
	rm.RegisterNode(nodeID, resource, "http://test-host:8042")

	// 提交应用程序
	ctx := common.ApplicationSubmissionContext{
		ApplicationID:   common.ApplicationID{ClusterTimestamp: time.Now().Unix(), ID: 1},
		ApplicationName: "test-app",
		ApplicationType: "YARN",
		Queue:           "default",
		Priority:        1,
		Resource:        common.Resource{Memory: 1024, VCores: 1},
		AMContainerSpec: common.ContainerLaunchContext{
			Commands: []string{"echo 'test'"},
		},
		MaxAppAttempts: 2,
	}

	appID, err := rm.SubmitApplication(ctx)
	if err != nil {
		t.Fatalf("Failed to submit application: %v", err)
	}

	if appID == nil {
		t.Fatal("Application ID is nil")
	}

	apps := rm.GetApplications()
	if len(apps) != 1 {
		t.Errorf("Expected 1 application, got %d", len(apps))
	}
}

func TestHTTPEndpoints(t *testing.T) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	// 测试集群信息端点
	t.Run("ClusterInfo", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws/v1/cluster/info", nil)
		w := httptest.NewRecorder()

		rm.handleClusterInfo(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
		}
	})

	// 测试节点列表端点
	t.Run("NodesList", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws/v1/cluster/nodes", nil)
		w := httptest.NewRecorder()

		rm.handleNodes(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	// 测试应用程序列表端点
	t.Run("AppsList", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws/v1/cluster/apps", nil)
		w := httptest.NewRecorder()

		rm.handleApplications(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	// 测试节点注册端点
	t.Run("NodeRegistration", func(t *testing.T) {
		registrationData := map[string]interface{}{
			"node_id": map[string]interface{}{
				"host": "test-host",
				"port": 8042,
			},
			"resource": map[string]interface{}{
				"memory": 4096,
				"vcores": 4,
			},
			"http_address": "http://test-host:8042",
		}

		jsonData, _ := json.Marshal(registrationData)
		req := httptest.NewRequest("POST", "/ws/v1/cluster/nodes/register", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rm.handleNodeRegistration(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

func TestNodeHeartbeat(t *testing.T) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	// 先注册节点
	nodeID := common.NodeID{Host: "test-host", Port: 8042}
	resource := common.Resource{Memory: 4096, VCores: 4}
	rm.RegisterNode(nodeID, resource, "http://test-host:8042")

	// 发送心跳
	usedResource := common.Resource{Memory: 1024, VCores: 1}
	containers := []*common.Container{}

	err := rm.NodeHeartbeat(nodeID, usedResource, containers)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	// 验证节点状态更新
	nodes := rm.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	for _, node := range nodes {
		if node.Used.Memory != 1024 {
			t.Errorf("Expected used memory 1024, got %d", node.Used.Memory)
		}
		if node.Used.VCores != 1 {
			t.Errorf("Expected used vcores 1, got %d", node.Used.VCores)
		}
	}
}

// 基准测试
func BenchmarkResourceManagerScheduling(b *testing.B) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	// 注册测试节点
	nodeID := common.NodeID{Host: "bench-host", Port: 8042}
	resource := common.Resource{Memory: 8192, VCores: 8}
	rm.RegisterNode(nodeID, resource, "http://bench-host:8042")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		appID := common.ApplicationID{
			ClusterTimestamp: time.Now().Unix(),
			ID:               int32(i),
		}

		ctx := common.ApplicationSubmissionContext{
			ApplicationID:   appID,
			ApplicationName: "bench-app",
			ApplicationType: "YARN",
			Queue:           "default",
			Resource:        common.Resource{Memory: 1024, VCores: 1},
			AMContainerSpec: common.ContainerLaunchContext{
				Commands: []string{"echo 'benchmark'"},
			},
		}

		_, err := rm.SubmitApplication(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNodeHeartbeat(b *testing.B) {
	config := common.GetDefaultConfig()
	rm := NewResourceManager(config)

	// 注册测试节点
	nodeID := common.NodeID{Host: "bench-host", Port: 8042}
	resource := common.Resource{Memory: 8192, VCores: 8}
	rm.RegisterNode(nodeID, resource, "http://bench-host:8042")

	usedResource := common.Resource{Memory: 1024, VCores: 1}
	containers := []*common.Container{}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := rm.NodeHeartbeat(nodeID, usedResource, containers)
		if err != nil {
			b.Fatal(err)
		}
	}
}
