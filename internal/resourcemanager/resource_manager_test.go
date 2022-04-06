package resourcemanager

import (
	"carrot/internal/common"
	"testing"
	"time"
)

func TestResourceManagerCreation(t *testing.T) {
	rm := NewResourceManager()
	if rm == nil {
		t.Fatal("Failed to create ResourceManager")
	}

	if rm.clusterTimestamp == 0 {
		t.Error("Cluster timestamp not set")
	}

	if rm.applications == nil {
		t.Error("Applications map not initialized")
	}

	if rm.nodes == nil {
		t.Error("Nodes map not initialized")
	}
}

func TestNodeRegistration(t *testing.T) {
	rm := NewResourceManager()

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
	rm := NewResourceManager()

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
