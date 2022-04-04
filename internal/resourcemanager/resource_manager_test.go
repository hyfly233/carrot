package resourcemanager

import "testing"

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
