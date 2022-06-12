package resourcemanager

import (
	"testing"
	"time"

	"carrot/internal/common"
	"carrot/internal/resourcemanager/applicationmanager"
	"carrot/internal/resourcemanager/nodemanager"
	"carrot/internal/resourcemanager/recovery"
)

// TestResourceManagerModulesConstruction 测试三个核心模块的构造和基本功能
func TestResourceManagerModulesConstruction(t *testing.T) {
	// 1. 测试 ApplicationManager 构造
	t.Run("ApplicationManager Construction", func(t *testing.T) {
		config := &applicationmanager.ApplicationManagerConfig{
			MaxCompletedApps:       10,
			AppCleanupInterval:     30 * time.Second,
			MaxApplicationAttempts: 3,
			EnableRecovery:         false,
		}

		appManager := applicationmanager.NewApplicationManager(config)
		if appManager == nil {
			t.Fatalf("Failed to create ApplicationManager")
		}

		// 验证初始状态
		apps := appManager.GetApplications()
		if apps == nil {
			t.Fatalf("GetApplications() returned nil")
		}

		if len(apps) != 0 {
			t.Fatalf("Expected 0 applications initially, got %d", len(apps))
		}

		// 停止管理器
		err := appManager.Stop()
		if err != nil {
			t.Fatalf("Failed to stop ApplicationManager: %v", err)
		}

		t.Logf("ApplicationManager construction and basic operations passed")
	})

	// 2. 测试 NodeManager 组件构造
	t.Run("NodeManager Construction", func(t *testing.T) {
		// 测试 NodeTracker
		nodeTracker := nodemanager.NewNodeTracker(30 * time.Second)
		if nodeTracker == nil {
			t.Fatalf("Failed to create NodeTracker")
		}

		// 测试 ResourceTracker
		resourceTracker := nodemanager.NewResourceTracker()
		if resourceTracker == nil {
			t.Fatalf("Failed to create ResourceTracker")
		}

		// 验证 ResourceTracker 基本方法
		totalResources := resourceTracker.GetTotalResources()
		usedResources := resourceTracker.GetUsedResources()
		availableResources := resourceTracker.GetAvailableResources()

		// 由于 common.Resource 是结构体，不是指针，所以不能与 nil 比较
		// 这里只检查方法是否可以调用
		_ = totalResources
		_ = usedResources  
		_ = availableResources

		t.Logf("NodeManager components construction and basic operations passed")
	})

	// 3. 测试 Recovery 模块构造
	t.Run("Recovery Construction", func(t *testing.T) {
		config := &recovery.RecoveryManagerConfig{
			Enabled:             true,
			CheckpointInterval:  time.Minute,
			MaxRecoveryAttempts: 3,
			StateStoreType:      "memory",
			StateStoreConfig:    make(map[string]interface{}),
		}

		recoveryManager, err := recovery.NewRecoveryManager(config)
		if err != nil {
			t.Fatalf("Failed to create RecoveryManager: %v", err)
		}

		if recoveryManager == nil {
			t.Fatalf("NewRecoveryManager returned nil")
		}

		// 验证基本方法
		if !recoveryManager.IsEnabled() {
			t.Fatalf("Expected recovery to be enabled")
		}

		status := recoveryManager.GetRecoveryStatus()
		if status == nil {
			t.Fatalf("GetRecoveryStatus() returned nil")
		}

		if !status.Enabled {
			t.Fatalf("Expected recovery status to show enabled")
		}

		// 停止恢复管理器
		err = recoveryManager.Stop()
		if err != nil {
			t.Fatalf("Failed to stop RecoveryManager: %v", err)
		}

		t.Logf("Recovery construction and basic operations passed")
	})

	t.Logf("All module construction tests passed successfully!")
}

// TestRecoverySnapshot 测试快照功能
func TestRecoverySnapshot(t *testing.T) {
	config := &recovery.RecoveryManagerConfig{
		Enabled:             true,
		CheckpointInterval:  time.Minute,
		MaxRecoveryAttempts: 3,
		StateStoreType:      "memory",
		StateStoreConfig:    make(map[string]interface{}),
	}

	recoveryManager, err := recovery.NewRecoveryManager(config)
	if err != nil {
		t.Fatalf("Failed to create RecoveryManager: %v", err)
	}
	defer recoveryManager.Stop()

	// 创建测试快照
	snapshot := &recovery.ClusterSnapshot{
		Timestamp: time.Now(),
		Applications: []*recovery.ApplicationSnapshot{
			{
				ID: common.ApplicationID{
					ClusterTimestamp: time.Now().UnixMilli(),
					ID:               1,
				},
				Name:      "test-app-1",
				Type:      "MAPREDUCE",
				State:     "RUNNING",
				StartTime: time.Now(),
				Progress:  0.5,
			},
		},
		Nodes: []*recovery.NodeSnapshot{
			{
				ID: common.NodeID{
					Host: "test-node-1",
					Port: 8040,
				},
				TotalResource: common.Resource{
					Memory: 8192,
					VCores: 4,
				},
				UsedResource: common.Resource{
					Memory: 1024,
					VCores: 1,
				},
				State:         common.NodeStateRunning,
				HTTPAddress:   "http://test-node-1:8042",
				LastHeartbeat: time.Now(),
			},
		},
		Resources: &recovery.ResourceSnapshot{
			TotalMemory:     8192,
			TotalVCores:     4,
			UsedMemory:      1024,
			UsedVCores:      1,
			AvailableMemory: 7168,
			AvailableVCores: 3,
		},
		Version: "1.0.0",
	}

	// 保存快照
	err = recoveryManager.SaveCheckpoint(snapshot)
	if err != nil {
		t.Fatalf("Failed to save checkpoint: %v", err)
	}

	// 验证快照已保存
	loadedSnapshot, err := recoveryManager.GetLatestSnapshot()
	if err != nil {
		t.Fatalf("Failed to load latest snapshot: %v", err)
	}

	if loadedSnapshot == nil {
		t.Fatalf("Expected snapshot to be loaded, got nil")
	}

	if len(loadedSnapshot.Applications) != 1 {
		t.Fatalf("Expected 1 application in snapshot, got %d", len(loadedSnapshot.Applications))
	}

	if len(loadedSnapshot.Nodes) != 1 {
		t.Fatalf("Expected 1 node in snapshot, got %d", len(loadedSnapshot.Nodes))
	}

	if loadedSnapshot.Resources.TotalMemory != 8192 {
		t.Fatalf("Expected total memory 8192, got %d", loadedSnapshot.Resources.TotalMemory)
	}

	t.Logf("Recovery snapshot test passed successfully!")
}
