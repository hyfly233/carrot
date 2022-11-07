package resourcemanager

import (
	"testing"
	"time"

	"carrot/internal/common"
	"carrot/internal/resourcemanager/nodemanager"
)

func TestNodeHeartbeatMonitoring(t *testing.T) {
	// 创建配置，设置较短的超时时间用于测试
	config := &common.Config{
		HeartbeatTimeout: 2, // 2秒超时
		MonitorInterval:  1, // 1秒检查一次
	}

	rm := NewResourceManager(config)

	// 验证心跳监测参数是否正确设置
	expectedTimeout := 2 * time.Second
	expectedInterval := 1 * time.Second

	if rm.nodeHeartbeatTimeout != expectedTimeout {
		t.Errorf("Expected heartbeat timeout %v, got %v", expectedTimeout, rm.nodeHeartbeatTimeout)
	}

	if rm.nodeMonitorInterval != expectedInterval {
		t.Errorf("Expected monitor interval %v, got %v", expectedInterval, rm.nodeMonitorInterval)
	}
}

func TestNodeHealthStatusTracking(t *testing.T) {
	rm := NewResourceManager(nil)

	// 添加一个测试节点
	nodeID := common.NodeID{Host: "testhost", Port: 8042}
	node := &nodemanager.Node{
		ID:            nodeID,
		State:         "RUNNING",
		LastHeartbeat: time.Now(),
		TotalResource: common.Resource{Memory: 8192, VCores: 8},
	}

	rm.mu.Lock()
	rm.nodes[rm.getNodeKey(nodeID)] = node
	rm.mu.Unlock()

	// 检查初始健康状态
	healthStatus := rm.GetNodeHealthStatus()
	if healthStatus["RUNNING"] != 1 {
		t.Errorf("Expected 1 running node, got %d", healthStatus["RUNNING"])
	}
	if healthStatus["TOTAL"] != 1 {
		t.Errorf("Expected 1 total node, got %d", healthStatus["TOTAL"])
	}

	// 模拟节点超时
	node.LastHeartbeat = time.Now().Add(-2 * time.Hour) // 设置为2小时前
	rm.markNodeUnhealthy(node)

	// 检查不健康状态
	healthStatus = rm.GetNodeHealthStatus()
	if healthStatus["UNHEALTHY"] != 1 {
		t.Errorf("Expected 1 unhealthy node, got %d", healthStatus["UNHEALTHY"])
	}
	if node.State != "UNHEALTHY" {
		t.Errorf("Expected node state to be UNHEALTHY, got %s", node.State)
	}

	// 恢复节点
	rm.markNodeHealthy(node)
	if node.State != "RUNNING" {
		t.Errorf("Expected node state to be RUNNING after recovery, got %s", node.State)
	}
}

func TestNodeHeartbeatTimeoutDetection(t *testing.T) {
	// 使用非常短的超时时间进行测试
	config := &common.Config{
		HeartbeatTimeout: 1, // 1秒超时
		MonitorInterval:  1, // 1秒检查一次
	}

	rm := NewResourceManager(config)

	// 添加一个测试节点
	nodeID := common.NodeID{Host: "testhost", Port: 8042}
	node := &nodemanager.Node{
		ID:            nodeID,
		State:         "RUNNING",
		LastHeartbeat: time.Now().Add(-2 * time.Second), // 2秒前的心跳，应该超时
		TotalResource: common.Resource{Memory: 8192, VCores: 8},
		Containers:    []*common.Container{},
	}

	rm.mu.Lock()
	rm.nodes[rm.getNodeKey(nodeID)] = node
	rm.mu.Unlock()

	// 运行健康检查
	rm.checkNodeHealth()

	// 验证节点被标记为不健康
	if node.State != "UNHEALTHY" {
		t.Errorf("Expected node to be marked as UNHEALTHY due to timeout, got %s", node.State)
	}

	// 模拟节点恢复心跳
	node.LastHeartbeat = time.Now()
	rm.checkNodeHealth()

	// 验证节点恢复健康
	if node.State != "RUNNING" {
		t.Errorf("Expected node to recover to RUNNING state, got %s", node.State)
	}
}
