package common

import (
	"testing"
	"time"
)

func TestBaseResource(t *testing.T) {
	// 测试基础资源创建
	resource := &BaseResource{
		Memory: 2048,
		VCores: 2,
	}

	if resource.GetMemory() != 2048 {
		t.Errorf("Expected memory 2048, got %d", resource.GetMemory())
	}

	if resource.GetVCores() != 2 {
		t.Errorf("Expected vcores 2, got %d", resource.GetVCores())
	}

	if resource.IsEmpty() {
		t.Error("Resource should not be empty")
	}

	// 测试空资源
	emptyResource := &BaseResource{}
	if !emptyResource.IsEmpty() {
		t.Error("Empty resource should be empty")
	}
}

func TestResourceOperations(t *testing.T) {
	r1 := &BaseResource{Memory: 2048, VCores: 2}
	r2 := &BaseResource{Memory: 1024, VCores: 1}

	// 测试加法
	sum := r1.Add(r2)
	if sum.GetMemory() != 3072 {
		t.Errorf("Expected sum memory 3072, got %d", sum.GetMemory())
	}
	if sum.GetVCores() != 3 {
		t.Errorf("Expected sum vcores 3, got %d", sum.GetVCores())
	}

	// 测试减法
	diff := r1.Subtract(r2)
	if diff.GetMemory() != 1024 {
		t.Errorf("Expected diff memory 1024, got %d", diff.GetMemory())
	}
	if diff.GetVCores() != 1 {
		t.Errorf("Expected diff vcores 1, got %d", diff.GetVCores())
	}

	// 测试相等性
	r3 := &BaseResource{Memory: 2048, VCores: 2}
	if !r1.Equals(r3) {
		t.Error("Resources should be equal")
	}

	if r1.Equals(r2) {
		t.Error("Resources should not be equal")
	}
}

func TestExtendedResource(t *testing.T) {
	resource := &ExtendedResource{
		BaseResource: BaseResource{Memory: 4096, VCores: 4},
		GPU:          1,
		Disk:         10240,
		Network:      1000,
		Attributes:   map[string]string{"zone": "us-west-1"},
	}

	if resource.GetMemory() != 4096 {
		t.Errorf("Expected memory 4096, got %d", resource.GetMemory())
	}

	if resource.GPU != 1 {
		t.Errorf("Expected GPU 1, got %d", resource.GPU)
	}

	if resource.Attributes["zone"] != "us-west-1" {
		t.Errorf("Expected zone us-west-1, got %s", resource.Attributes["zone"])
	}

	// 测试扩展资源的加法
	r2 := &ExtendedResource{
		BaseResource: BaseResource{Memory: 2048, VCores: 2},
		GPU:          1,
		Disk:         5120,
		Network:      500,
		Attributes:   map[string]string{"type": "compute"},
	}

	sum := resource.Add(r2).(*ExtendedResource)
	if sum.GetMemory() != 6144 {
		t.Errorf("Expected sum memory 6144, got %d", sum.GetMemory())
	}
	if sum.GPU != 2 {
		t.Errorf("Expected sum GPU 2, got %d", sum.GPU)
	}
	if sum.Disk != 15360 {
		t.Errorf("Expected sum disk 15360, got %d", sum.Disk)
	}
}

func TestBaseNode(t *testing.T) {
	node := &BaseNode{
		ID:   NodeID{Host: "worker-1", Port: 8080},
		Host: "worker-1",
		Port: 8080,
	}

	if node.GetHost() != "worker-1" {
		t.Errorf("Expected host worker-1, got %s", node.GetHost())
	}

	if node.GetPort() != 8080 {
		t.Errorf("Expected port 8080, got %d", node.GetPort())
	}

	expectedAddress := "worker-1:8080"
	if node.GetAddress() != expectedAddress {
		t.Errorf("Expected address %s, got %s", expectedAddress, node.GetAddress())
	}
}

func TestExtendedNode(t *testing.T) {
	totalResource := &BaseResource{Memory: 8192, VCores: 8}
	node := &ExtendedNode{
		BaseNode: BaseNode{
			ID:   NodeID{Host: "worker-1", Port: 8080},
			Host: "worker-1",
			Port: 8080,
		},
		RackName:          "rack-01",
		Labels:            []string{"gpu", "ssd"},
		LastHeartbeat:     time.Now(),
		TotalResource:     totalResource,
		AvailableResource: totalResource,
		Capabilities:      map[string]interface{}{"max_containers": 100},
	}

	// 测试标签检查
	if !node.HasLabel("gpu") {
		t.Error("Node should have gpu label")
	}

	if node.HasLabel("cpu") {
		t.Error("Node should not have cpu label")
	}

	// 测试健康状态
	if !node.IsHealthy() {
		t.Error("Node should be healthy")
	}

	// 测试过期心跳
	node.LastHeartbeat = time.Now().Add(-1 * time.Minute)
	if node.IsHealthy() {
		t.Error("Node should not be healthy with old heartbeat")
	}
}

func TestBaseContainer(t *testing.T) {
	containerID := ContainerID{
		ApplicationAttemptID: ApplicationAttemptID{
			ApplicationID: ApplicationID{ClusterTimestamp: 123456789, ID: 1},
			AttemptID:     1,
		},
		ContainerID: 1001,
	}

	nodeID := NodeID{Host: "worker-1", Port: 8080}
	resource := &BaseResource{Memory: 2048, VCores: 2}

	container := &BaseContainer{
		ID:       containerID,
		NodeID:   nodeID,
		Resource: resource,
		State:    ContainerStateNew,
	}

	if container.GetID().ContainerID != 1001 {
		t.Errorf("Expected container ID 1001, got %d", container.GetID().ContainerID)
	}

	if container.GetNodeID().Host != "worker-1" {
		t.Errorf("Expected node host worker-1, got %s", container.GetNodeID().Host)
	}

	if container.GetState() != ContainerStateNew {
		t.Errorf("Expected state %s, got %s", ContainerStateNew, container.GetState())
	}
}

func TestExtendedContainer(t *testing.T) {
	baseContainer := &BaseContainer{
		ID: ContainerID{
			ApplicationAttemptID: ApplicationAttemptID{
				ApplicationID: ApplicationID{ClusterTimestamp: 123456789, ID: 1},
				AttemptID:     1,
			},
			ContainerID: 1001,
		},
		NodeID:   NodeID{Host: "worker-1", Port: 8080},
		Resource: &BaseResource{Memory: 2048, VCores: 2},
		State:    ContainerStateRunning,
	}

	container := &ExtendedContainer{
		BaseContainer: *baseContainer,
		Status:        "ALLOCATED",
		StartTime:     time.Now(),
		ExitCode:      0,
	}

	if !container.IsRunning() {
		t.Error("Container should be running")
	}

	if container.IsCompleted() {
		t.Error("Container should not be completed")
	}

	// 测试完成状态
	container.State = ContainerStateComplete
	if container.IsRunning() {
		t.Error("Container should not be running")
	}

	if !container.IsCompleted() {
		t.Error("Container should be completed")
	}
}

func TestBaseApplication(t *testing.T) {
	appID := ApplicationID{ClusterTimestamp: 123456789, ID: 1}
	app := &BaseApplication{
		ID:    appID,
		Name:  "test-app",
		State: ApplicationStateSubmitted,
		User:  "testuser",
	}

	if app.GetName() != "test-app" {
		t.Errorf("Expected name test-app, got %s", app.GetName())
	}

	if app.GetUser() != "testuser" {
		t.Errorf("Expected user testuser, got %s", app.GetUser())
	}

	if app.GetState() != ApplicationStateSubmitted {
		t.Errorf("Expected state %s, got %s", ApplicationStateSubmitted, app.GetState())
	}
}

func TestExtendedApplication(t *testing.T) {
	baseApp := &BaseApplication{
		ID:    ApplicationID{ClusterTimestamp: 123456789, ID: 1},
		Name:  "test-app",
		State: ApplicationStateRunning,
		User:  "testuser",
	}

	app := &ExtendedApplication{
		BaseApplication: *baseApp,
		Type:            "MapReduce",
		Queue:           "default",
		Priority:        1,
		Progress:        0.5,
		Resource:        &BaseResource{Memory: 4096, VCores: 4},
		StartTime:       time.Now(),
		Tags:            map[string]string{"env": "test"},
	}

	if app.GetType() != "MapReduce" {
		t.Errorf("Expected type MapReduce, got %s", app.GetType())
	}

	if app.GetQueue() != "default" {
		t.Errorf("Expected queue default, got %s", app.GetQueue())
	}

	if app.GetProgress() != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", app.GetProgress())
	}

	if !app.IsRunning() {
		t.Error("Application should be running")
	}

	if app.IsCompleted() {
		t.Error("Application should not be completed")
	}

	// 测试完成状态
	app.State = ApplicationStateFinished
	if app.IsRunning() {
		t.Error("Application should not be running")
	}

	if !app.IsCompleted() {
		t.Error("Application should be completed")
	}
}

func TestFactoryFunctions(t *testing.T) {
	// 测试资源工厂函数
	resource := NewResource(2048, 2)
	if resource.GetMemory() != 2048 {
		t.Errorf("Expected memory 2048, got %d", resource.GetMemory())
	}

	// 测试扩展资源工厂函数
	extResource := NewExtendedResource(4096, 4, 1, 10240, 1000)
	if extResource.GetMemory() != 4096 {
		t.Errorf("Expected memory 4096, got %d", extResource.GetMemory())
	}

	extRes := extResource.(*ExtendedResource)
	if extRes.GPU != 1 {
		t.Errorf("Expected GPU 1, got %d", extRes.GPU)
	}

	// 测试节点工厂函数
	node := NewNode("worker-1", 8080)
	if node.GetHost() != "worker-1" {
		t.Errorf("Expected host worker-1, got %s", node.GetHost())
	}

	// 测试扩展节点工厂函数
	extNode := NewExtendedNode("worker-1", 8080, resource)
	if extNode.GetHost() != "worker-1" {
		t.Errorf("Expected host worker-1, got %s", extNode.GetHost())
	}

	if extNode.TotalResource.GetMemory() != 2048 {
		t.Errorf("Expected total memory 2048, got %d", extNode.TotalResource.GetMemory())
	}
}

// 基准测试
func BenchmarkResourceAdd(b *testing.B) {
	r1 := &BaseResource{Memory: 2048, VCores: 2}
	r2 := &BaseResource{Memory: 1024, VCores: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r1.Add(r2)
	}
}

func BenchmarkExtendedResourceAdd(b *testing.B) {
	r1 := &ExtendedResource{
		BaseResource: BaseResource{Memory: 2048, VCores: 2},
		GPU:          1,
		Attributes:   map[string]string{"zone": "us-west-1"},
	}
	r2 := &ExtendedResource{
		BaseResource: BaseResource{Memory: 1024, VCores: 1},
		GPU:          0,
		Attributes:   map[string]string{"type": "compute"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r1.Add(r2)
	}
}
