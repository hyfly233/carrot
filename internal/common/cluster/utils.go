package cluster

import (
	"fmt"
	"time"

	"carrot/internal/common"
)

// CreateLocalNode 创建本地节点信息
func CreateLocalNode(nodeType common.NodeType, host string, port int32, metadata map[string]string) *common.ClusterNode {
	if metadata == nil {
		metadata = make(map[string]string)
	}

	return &common.ClusterNode{
		ID: common.NodeID{
			Host: host,
			Port: port,
		},
		Type:          nodeType,
		State:         common.NodeStateJoining,
		Roles:         []common.NodeRole{common.NodeRoleWorker},
		LastHeartbeat: time.Now(),
		JoinTime:      time.Now(),
		Metadata:      metadata,
		Capabilities: common.NodeCapabilities{
			MaxContainers:     100,
			SupportedFeatures: []string{"containers", "monitoring"},
			Resources: common.Resource{
				Memory: 8192,
				VCores: 8,
			},
			Labels: make(map[string]string),
		},
		Health: common.NodeHealth{
			Status:    common.HealthStatusHealthy,
			LastCheck: time.Now(),
			Issues:    make([]common.HealthIssue, 0),
			Metrics:   make(map[string]float64),
		},
		Version: "1.0.0",
	}
}

// ValidateClusterConfig 验证集群配置
func ValidateClusterConfig(config common.ClusterConfig) error {
	if config.Name == "" {
		return fmt.Errorf("cluster name cannot be empty")
	}

	if config.MinNodes < 1 {
		return fmt.Errorf("minimum nodes must be at least 1")
	}

	if config.MaxNodes < config.MinNodes {
		return fmt.Errorf("maximum nodes must be greater than or equal to minimum nodes")
	}

	if config.ElectionTimeout <= 0 {
		return fmt.Errorf("election timeout must be positive")
	}

	if config.HeartbeatInterval <= 0 {
		return fmt.Errorf("heartbeat interval must be positive")
	}

	if config.FailureDetectionWindow <= config.HeartbeatInterval {
		return fmt.Errorf("failure detection window must be greater than heartbeat interval")
	}

	validDiscoveryMethods := []string{"static", "dns", "etcd", "consul"}
	validMethod := false
	for _, method := range validDiscoveryMethods {
		if config.DiscoveryMethod == method {
			validMethod = true
			break
		}
	}
	if !validMethod {
		return fmt.Errorf("invalid discovery method: %s", config.DiscoveryMethod)
	}

	return nil
}

// GetNodeRole 根据节点类型获取默认角色
func GetNodeRole(nodeType common.NodeType) []common.NodeRole {
	switch nodeType {
	case common.NodeTypeResourceManager:
		return []common.NodeRole{common.NodeRoleLeader}
	case common.NodeTypeNodeManager:
		return []common.NodeRole{common.NodeRoleWorker}
	case common.NodeTypeApplicationMaster:
		return []common.NodeRole{common.NodeRoleWorker}
	default:
		return []common.NodeRole{common.NodeRoleWorker}
	}
}

// IsNodeHealthy 检查节点是否健康
func IsNodeHealthy(node *common.ClusterNode, maxHeartbeatAge time.Duration) bool {
	// 检查心跳时间
	if time.Since(node.LastHeartbeat) > maxHeartbeatAge {
		return false
	}

	// 检查健康状态
	if node.Health.Status == common.HealthStatusCritical {
		return false
	}

	// 检查节点状态
	if node.State == common.NodeStateFailed {
		return false
	}

	return true
}

// GetHealthyNodes 获取健康的节点列表
func GetHealthyNodes(nodes map[string]*common.ClusterNode, maxHeartbeatAge time.Duration) []*common.ClusterNode {
	var healthyNodes []*common.ClusterNode

	for _, node := range nodes {
		if IsNodeHealthy(node, maxHeartbeatAge) {
			healthyNodes = append(healthyNodes, node)
		}
	}

	return healthyNodes
}

// CalculateClusterResources 计算集群资源
func CalculateClusterResources(nodes map[string]*common.ClusterNode) (total, used, available common.Resource) {
	for _, node := range nodes {
		if IsNodeHealthy(node, 5*time.Minute) {
			total.Memory += node.Capabilities.Resources.Memory
			total.VCores += node.Capabilities.Resources.VCores

			// 这里应该从实际的资源使用情况计算，简化为固定比例
			usedRatio := 0.3 // 假设使用了30%的资源
			used.Memory += int64(float64(node.Capabilities.Resources.Memory) * usedRatio)
			used.VCores += int32(float64(node.Capabilities.Resources.VCores) * usedRatio)
		}
	}

	available.Memory = total.Memory - used.Memory
	available.VCores = total.VCores - used.VCores

	if available.Memory < 0 {
		available.Memory = 0
	}
	if available.VCores < 0 {
		available.VCores = 0
	}

	return
}

// GenerateNodeID 生成唯一的节点ID
func GenerateNodeID() string {
	return fmt.Sprintf("node-%d", time.Now().UnixNano())
}

// FormatDuration 格式化时间间隔
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	} else {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
