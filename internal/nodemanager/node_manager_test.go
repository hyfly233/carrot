package nodemanager

import (
	"carrot/internal/common"
	"testing"
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
