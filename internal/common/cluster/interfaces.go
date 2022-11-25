package cluster

import (
	"carrot/internal/common"
	"context"

	"go.uber.org/zap"
)

// Discovery 服务发现接口
type Discovery interface {
	// Start 启动服务发现
	Start(ctx context.Context) error

	// Stop 停止服务发现
	Stop() error

	// DiscoverNodes 发现节点
	DiscoverNodes() ([]common.ClusterNode, error)

	// RegisterNode 注册节点
	RegisterNode(node *common.ClusterNode) error

	// UnregisterNode 注销节点
	UnregisterNode(nodeID common.NodeID) error

	// Watch 监听节点变化
	Watch(callback func([]common.ClusterNode)) error
}

// NewDiscovery 创建服务发现实例
func NewDiscovery(config common.ClusterConfig, localNode *common.ClusterNode, logger *zap.Logger) (Discovery, error) {
	switch config.DiscoveryMethod {
	case "static":
		return NewStaticDiscovery(config, localNode, logger)
	case "dns":
		return NewDNSDiscovery(config, localNode, logger)
	case "etcd":
		return NewEtcdDiscovery(config, localNode, logger)
	case "consul":
		return NewConsulDiscovery(config, localNode, logger)
	default:
		return NewStaticDiscovery(config, localNode, logger)
	}
}

// Election 领导者选举接口
type Election interface {
	// Start 启动选举
	StartElection() error

	// Stop 停止选举
	Stop() error

	// StepDown 主动放弃领导权
	StepDown() error

	// IsLeader 检查是否为领导者
	IsLeader() bool

	// GetLeader 获取当前领导者
	GetLeader() (*common.NodeID, error)

	// OnLeaderChange 注册领导者变更回调
	OnLeaderChange(callback func(old, new *common.NodeID))
}

// NewElection 创建选举实例
func NewElection(config common.ClusterConfig, localNode *common.ClusterNode, clusterManager *ClusterManager, logger *zap.Logger) (Election, error) {
	// 根据配置选择选举算法
	return NewRaftElection(config, localNode, clusterManager, logger)
}

// HealthChecker 健康检查接口
type HealthChecker interface {
	// Start 启动健康检查
	Start(ctx context.Context) error

	// Stop 停止健康检查
	Stop() error

	// CheckNode 检查节点健康状态
	CheckNode(nodeID common.NodeID) (*common.NodeHealth, error)

	// RegisterHealthCheck 注册健康检查
	RegisterHealthCheck(name string, checker func() *common.HealthIssue)

	// GetHealthStatus 获取健康状态
	GetHealthStatus() *common.NodeHealth
}

// NewHealthChecker 创建健康检查实例
func NewHealthChecker(config common.ClusterConfig, localNode *common.ClusterNode, clusterManager *ClusterManager, logger *zap.Logger) (HealthChecker, error) {
	return NewDefaultHealthChecker(config, localNode, clusterManager, logger)
}

// EventHandler 事件处理接口
type EventHandler interface {
	// HandleEvent 处理事件
	HandleEvent(event common.ClusterEvent) error

	// RegisterEventProcessor 注册事件处理器
	RegisterEventProcessor(eventType common.ClusterEventType, processor func(common.ClusterEvent) error)
}

// NewEventHandler 创建事件处理实例
func NewEventHandler(config common.ClusterConfig, logger *zap.Logger) (EventHandler, error) {
	return NewDefaultEventHandler(config, logger)
}
