package common

import (
	"testing"
	"time"
)

func TestTypeConverter(t *testing.T) {
	converter := NewTypeConverter()

	// 测试资源转换
	t.Run("ConvertFromOldResource", func(t *testing.T) {
		resource := converter.ConvertFromOldResource(2048, 2)
		if resource.GetMemory() != 2048 {
			t.Errorf("Expected memory 2048, got %d", resource.GetMemory())
		}
		if resource.GetVCores() != 2 {
			t.Errorf("Expected vcores 2, got %d", resource.GetVCores())
		}
	})

	// 测试基础资源转换
	t.Run("ConvertToBaseResource", func(t *testing.T) {
		extResource := &ExtendedResource{
			BaseResource: BaseResource{Memory: 4096, VCores: 4},
			GPU:          1,
		}
		baseResource := converter.ConvertToBaseResource(extResource)
		if baseResource.Memory != 4096 {
			t.Errorf("Expected memory 4096, got %d", baseResource.Memory)
		}
		if baseResource.VCores != 4 {
			t.Errorf("Expected vcores 4, got %d", baseResource.VCores)
		}
	})

	// 测试扩展资源转换
	t.Run("ConvertToExtendedResource", func(t *testing.T) {
		baseResource := &BaseResource{Memory: 2048, VCores: 2}
		extResource := converter.ConvertToExtendedResource(baseResource, 1, 10240, 1000)

		if extResource.GetMemory() != 2048 {
			t.Errorf("Expected memory 2048, got %d", extResource.GetMemory())
		}
		if extResource.GPU != 1 {
			t.Errorf("Expected GPU 1, got %d", extResource.GPU)
		}
		if extResource.Disk != 10240 {
			t.Errorf("Expected disk 10240, got %d", extResource.Disk)
		}
	})
}

func TestNodeConversion(t *testing.T) {
	converter := NewTypeConverter()

	// 测试基础节点转换
	t.Run("ConvertToBaseNode", func(t *testing.T) {
		node := converter.ConvertToBaseNode("worker-1", 8080)
		if node.GetHost() != "worker-1" {
			t.Errorf("Expected host worker-1, got %s", node.GetHost())
		}
		if node.GetPort() != 8080 {
			t.Errorf("Expected port 8080, got %d", node.GetPort())
		}
	})

	// 测试扩展节点转换
	t.Run("ConvertToExtendedNode", func(t *testing.T) {
		baseNode := converter.ConvertToBaseNode("worker-1", 8080)
		totalResource := converter.ConvertFromOldResource(8192, 8)
		labels := []string{"gpu", "ssd"}

		extNode := converter.ConvertToExtendedNode(baseNode, totalResource, "rack-01", labels)

		if extNode.GetHost() != "worker-1" {
			t.Errorf("Expected host worker-1, got %s", extNode.GetHost())
		}
		if extNode.RackName != "rack-01" {
			t.Errorf("Expected rack rack-01, got %s", extNode.RackName)
		}
		if len(extNode.Labels) != 2 {
			t.Errorf("Expected 2 labels, got %d", len(extNode.Labels))
		}
		if extNode.TotalResource.GetMemory() != 8192 {
			t.Errorf("Expected total memory 8192, got %d", extNode.TotalResource.GetMemory())
		}
	})
}

func TestContainerConversion(t *testing.T) {
	converter := NewTypeConverter()

	containerID := ContainerID{
		ApplicationAttemptID: ApplicationAttemptID{
			ApplicationID: ApplicationID{ClusterTimestamp: 123456789, ID: 1},
			AttemptID:     1,
		},
		ContainerID: 1001,
	}
	nodeID := NodeID{Host: "worker-1", Port: 8080}
	resource := converter.ConvertFromOldResource(2048, 2)

	// 测试基础容器转换
	t.Run("ConvertToBaseContainer", func(t *testing.T) {
		container := converter.ConvertToBaseContainer(containerID, nodeID, resource, ContainerStateNew)

		if container.GetID().ContainerID != 1001 {
			t.Errorf("Expected container ID 1001, got %d", container.GetID().ContainerID)
		}
		if container.GetNodeID().Host != "worker-1" {
			t.Errorf("Expected node host worker-1, got %s", container.GetNodeID().Host)
		}
		if container.GetState() != ContainerStateNew {
			t.Errorf("Expected state %s, got %s", ContainerStateNew, container.GetState())
		}
	})

	// 测试扩展容器转换
	t.Run("ConvertToExtendedContainer", func(t *testing.T) {
		baseContainer := converter.ConvertToBaseContainer(containerID, nodeID, resource, ContainerStateNew)
		launchContext := &ContainerLaunchContext{
			Commands: []string{"java", "-jar", "app.jar"},
		}

		extContainer := converter.ConvertToExtendedContainer(baseContainer, "ALLOCATED", launchContext)

		if extContainer.GetID().ContainerID != 1001 {
			t.Errorf("Expected container ID 1001, got %d", extContainer.GetID().ContainerID)
		}
		if extContainer.Status != "ALLOCATED" {
			t.Errorf("Expected status ALLOCATED, got %s", extContainer.Status)
		}
		if extContainer.LaunchContext == nil {
			t.Error("Expected launch context to be set")
		}
	})
}

func TestApplicationConversion(t *testing.T) {
	converter := NewTypeConverter()

	appID := ApplicationID{ClusterTimestamp: 123456789, ID: 1}

	// 测试基础应用转换
	t.Run("ConvertToBaseApplication", func(t *testing.T) {
		app := converter.ConvertToBaseApplication(appID, "test-app", ApplicationStateSubmitted, "testuser")

		if app.GetID().ID != 1 {
			t.Errorf("Expected app ID 1, got %d", app.GetID().ID)
		}
		if app.GetName() != "test-app" {
			t.Errorf("Expected name test-app, got %s", app.GetName())
		}
		if app.GetUser() != "testuser" {
			t.Errorf("Expected user testuser, got %s", app.GetUser())
		}
	})

	// 测试扩展应用转换
	t.Run("ConvertToExtendedApplication", func(t *testing.T) {
		baseApp := converter.ConvertToBaseApplication(appID, "test-app", ApplicationStateSubmitted, "testuser")
		resource := converter.ConvertFromOldResource(4096, 4)

		extApp := converter.ConvertToExtendedApplication(baseApp, "MapReduce", "default", 1, resource)

		if extApp.GetName() != "test-app" {
			t.Errorf("Expected name test-app, got %s", extApp.GetName())
		}
		if extApp.Type != "MapReduce" {
			t.Errorf("Expected type MapReduce, got %s", extApp.Type)
		}
		if extApp.Queue != "default" {
			t.Errorf("Expected queue default, got %s", extApp.Queue)
		}
		if extApp.Priority != 1 {
			t.Errorf("Expected priority 1, got %d", extApp.Priority)
		}
	})
}

func TestReportConversions(t *testing.T) {
	converter := NewTypeConverter()

	// 测试NodeReport转换
	t.Run("ConvertFromNodeReport", func(t *testing.T) {
		report := &NodeReport{
			NodeID:           NodeID{Host: "worker-1", Port: 8080},
			HTTPAddress:      "http://worker-1:8080",
			RackName:         "rack-01",
			TotalResource:    Resource{Memory: 8192, VCores: 8},
			UsedResource:     Resource{Memory: 2048, VCores: 2},
			NumContainers:    2,
			State:            "RUNNING",
			NodeLabels:       []string{"gpu", "ssd"},
			LastHealthUpdate: time.Now(),
		}

		extNode := converter.ConvertFromNodeReport(report)

		if extNode.GetHost() != "worker-1" {
			t.Errorf("Expected host worker-1, got %s", extNode.GetHost())
		}
		if extNode.RackName != "rack-01" {
			t.Errorf("Expected rack rack-01, got %s", extNode.RackName)
		}
		if extNode.TotalResource.GetMemory() != 8192 {
			t.Errorf("Expected total memory 8192, got %d", extNode.TotalResource.GetMemory())
		}
		if len(extNode.Labels) != 2 {
			t.Errorf("Expected 2 labels, got %d", len(extNode.Labels))
		}
	})

	// 测试ExtendedNode到NodeReport转换
	t.Run("ConvertToNodeReport", func(t *testing.T) {
		totalResource := converter.ConvertFromOldResource(8192, 8)
		usedResource := converter.ConvertFromOldResource(2048, 2)
		availableResource := totalResource.Subtract(usedResource)

		extNode := &ExtendedNode{
			BaseNode: BaseNode{
				ID:   NodeID{Host: "worker-1", Port: 8080},
				Host: "worker-1",
				Port: 8080,
			},
			RackName:          "rack-01",
			Labels:            []string{"gpu", "ssd"},
			LastHeartbeat:     time.Now(),
			TotalResource:     totalResource,
			AvailableResource: availableResource,
		}

		report := converter.ConvertToNodeReport(extNode)

		if report.NodeID.Host != "worker-1" {
			t.Errorf("Expected host worker-1, got %s", report.NodeID.Host)
		}
		if report.RackName != "rack-01" {
			t.Errorf("Expected rack rack-01, got %s", report.RackName)
		}
		if report.TotalResource.Memory != 8192 {
			t.Errorf("Expected total memory 8192, got %d", report.TotalResource.Memory)
		}
		if report.UsedResource.Memory != 2048 {
			t.Errorf("Expected used memory 2048, got %d", report.UsedResource.Memory)
		}
	})

	// 测试ApplicationReport转换
	t.Run("ConvertFromApplicationReport", func(t *testing.T) {
		report := &ApplicationReport{
			ApplicationID:   ApplicationID{ClusterTimestamp: 123456789, ID: 1},
			ApplicationName: "test-app",
			ApplicationType: "MapReduce",
			User:            "testuser",
			Queue:           "default",
			State:           ApplicationStateRunning,
			Progress:        0.75,
			StartTime:       time.Now().Add(-1 * time.Hour),
			TrackingURL:     "http://app.example.com",
		}

		extApp := converter.ConvertFromApplicationReport(report)

		if extApp.GetName() != "test-app" {
			t.Errorf("Expected name test-app, got %s", extApp.GetName())
		}
		if extApp.Type != "MapReduce" {
			t.Errorf("Expected type MapReduce, got %s", extApp.Type)
		}
		if extApp.Progress != 0.75 {
			t.Errorf("Expected progress 0.75, got %f", extApp.Progress)
		}
		if extApp.TrackingURL != "http://app.example.com" {
			t.Errorf("Expected tracking URL http://app.example.com, got %s", extApp.TrackingURL)
		}
	})

	// 测试ExtendedApplication到ApplicationReport转换
	t.Run("ConvertToApplicationReport", func(t *testing.T) {
		appID := ApplicationID{ClusterTimestamp: 123456789, ID: 1}
		extApp := &ExtendedApplication{
			BaseApplication: BaseApplication{
				ID:    appID,
				Name:  "test-app",
				State: ApplicationStateRunning,
				User:  "testuser",
			},
			Type:        "MapReduce",
			Queue:       "default",
			Progress:    0.75,
			StartTime:   time.Now().Add(-1 * time.Hour),
			TrackingURL: "http://app.example.com",
		}

		report := converter.ConvertToApplicationReport(extApp)

		if report.ApplicationName != "test-app" {
			t.Errorf("Expected name test-app, got %s", report.ApplicationName)
		}
		if report.ApplicationType != "MapReduce" {
			t.Errorf("Expected type MapReduce, got %s", report.ApplicationType)
		}
		if report.Progress != 0.75 {
			t.Errorf("Expected progress 0.75, got %f", report.Progress)
		}
		if report.TrackingURL != "http://app.example.com" {
			t.Errorf("Expected tracking URL http://app.example.com, got %s", report.TrackingURL)
		}
	})
}

func TestBuilderPatterns(t *testing.T) {
	// 测试ResourceBuilder
	t.Run("ResourceBuilder", func(t *testing.T) {
		resource := NewResourceBuilder().
			WithMemory(4096).
			WithVCores(4).
			WithGPU(1).
			WithDisk(20480).
			WithNetwork(1000).
			WithAttribute("zone", "us-west-1").
			WithAttribute("instance-type", "c5.xlarge").
			Build()

		extRes := resource.(*ExtendedResource)
		if extRes.GetMemory() != 4096 {
			t.Errorf("Expected memory 4096, got %d", extRes.GetMemory())
		}
		if extRes.GetVCores() != 4 {
			t.Errorf("Expected vcores 4, got %d", extRes.GetVCores())
		}
		if extRes.GPU != 1 {
			t.Errorf("Expected GPU 1, got %d", extRes.GPU)
		}
		if extRes.Disk != 20480 {
			t.Errorf("Expected disk 20480, got %d", extRes.Disk)
		}
		if extRes.Network != 1000 {
			t.Errorf("Expected network 1000, got %d", extRes.Network)
		}
		if extRes.Attributes["zone"] != "us-west-1" {
			t.Errorf("Expected zone us-west-1, got %s", extRes.Attributes["zone"])
		}
		if extRes.Attributes["instance-type"] != "c5.xlarge" {
			t.Errorf("Expected instance-type c5.xlarge, got %s", extRes.Attributes["instance-type"])
		}
	})

	// 测试NodeBuilder
	t.Run("NodeBuilder", func(t *testing.T) {
		resource := NewResource(8192, 8)
		node := NewNodeBuilder().
			WithID("worker-1", 8080).
			WithRackName("rack-01").
			WithLabels([]string{"gpu", "ssd", "high-memory"}).
			WithTotalResource(resource).
			WithCapability("max_containers", 100).
			WithCapability("docker_version", "20.10.7").
			Build()

		if node.GetHost() != "worker-1" {
			t.Errorf("Expected host worker-1, got %s", node.GetHost())
		}
		if node.GetPort() != 8080 {
			t.Errorf("Expected port 8080, got %d", node.GetPort())
		}
		if node.RackName != "rack-01" {
			t.Errorf("Expected rack rack-01, got %s", node.RackName)
		}
		if len(node.Labels) != 3 {
			t.Errorf("Expected 3 labels, got %d", len(node.Labels))
		}
		if !node.HasLabel("gpu") {
			t.Error("Node should have gpu label")
		}
		if !node.HasLabel("ssd") {
			t.Error("Node should have ssd label")
		}
		if !node.HasLabel("high-memory") {
			t.Error("Node should have high-memory label")
		}
		if node.TotalResource.GetMemory() != 8192 {
			t.Errorf("Expected total memory 8192, got %d", node.TotalResource.GetMemory())
		}
		if node.Capabilities["max_containers"] != 100 {
			t.Errorf("Expected max_containers 100, got %v", node.Capabilities["max_containers"])
		}
		if node.Capabilities["docker_version"] != "20.10.7" {
			t.Errorf("Expected docker_version 20.10.7, got %v", node.Capabilities["docker_version"])
		}
	})
}

func TestConvenienceFunctions(t *testing.T) {
	// 测试ConvertResource便捷函数
	t.Run("ConvertResource", func(t *testing.T) {
		resource := ConvertResource(2048, 2)
		if resource.GetMemory() != 2048 {
			t.Errorf("Expected memory 2048, got %d", resource.GetMemory())
		}
		if resource.GetVCores() != 2 {
			t.Errorf("Expected vcores 2, got %d", resource.GetVCores())
		}
	})

	// 测试BuildResource便捷函数
	t.Run("BuildResource", func(t *testing.T) {
		resource := BuildResource().
			WithMemory(1024).
			WithVCores(1).
			Build()

		if resource.GetMemory() != 1024 {
			t.Errorf("Expected memory 1024, got %d", resource.GetMemory())
		}
		if resource.GetVCores() != 1 {
			t.Errorf("Expected vcores 1, got %d", resource.GetVCores())
		}
	})

	// 测试BuildNode便捷函数
	t.Run("BuildNode", func(t *testing.T) {
		node := BuildNode().
			WithID("test-node", 9090).
			WithRackName("test-rack").
			Build()

		if node.GetHost() != "test-node" {
			t.Errorf("Expected host test-node, got %s", node.GetHost())
		}
		if node.GetPort() != 9090 {
			t.Errorf("Expected port 9090, got %d", node.GetPort())
		}
		if node.RackName != "test-rack" {
			t.Errorf("Expected rack test-rack, got %s", node.RackName)
		}
	})
}

// 基准测试
func BenchmarkTypeConversion(b *testing.B) {
	converter := NewTypeConverter()

	b.Run("ConvertFromOldResource", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = converter.ConvertFromOldResource(2048, 2)
		}
	})

	b.Run("ConvertToBaseResource", func(b *testing.B) {
		resource := &ExtendedResource{
			BaseResource: BaseResource{Memory: 2048, VCores: 2},
			GPU:          1,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = converter.ConvertToBaseResource(resource)
		}
	})

	b.Run("ResourceBuilder", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = NewResourceBuilder().
				WithMemory(2048).
				WithVCores(2).
				WithGPU(1).
				Build()
		}
	})

	b.Run("NodeBuilder", func(b *testing.B) {
		resource := NewResource(8192, 8)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = NewNodeBuilder().
				WithID("worker-1", 8080).
				WithTotalResource(resource).
				Build()
		}
	})
}
