package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// StaticDiscovery 静态服务发现实现
type StaticDiscovery struct {
	config     common.ClusterConfig
	localNode  *common.ClusterNode
	logger     *zap.Logger
	nodes      map[string]*common.ClusterNode
	nodesMutex sync.RWMutex
	stopChan   chan struct{}
	watchers   []func([]common.ClusterNode)
}

// NewStaticDiscovery 创建静态服务发现实例
func NewStaticDiscovery(config common.ClusterConfig, localNode *common.ClusterNode, logger *zap.Logger) (*StaticDiscovery, error) {
	return &StaticDiscovery{
		config:    config,
		localNode: localNode,
		logger:    logger.With(zap.String("component", "static_discovery")),
		nodes:     make(map[string]*common.ClusterNode),
		stopChan:  make(chan struct{}),
		watchers:  make([]func([]common.ClusterNode), 0),
	}, nil
}

// Start 启动静态服务发现
func (sd *StaticDiscovery) Start(ctx context.Context) error {
	sd.logger.Info("Starting static discovery")

	// 从配置中加载静态节点列表
	if err := sd.loadStaticNodes(); err != nil {
		return fmt.Errorf("failed to load static nodes: %w", err)
	}

	// 注册本地节点
	if err := sd.RegisterNode(sd.localNode); err != nil {
		return fmt.Errorf("failed to register local node: %w", err)
	}

	// 启动监听器
	go sd.watchLoop(ctx)

	sd.logger.Info("Static discovery started")
	return nil
}

// Stop 停止静态服务发现
func (sd *StaticDiscovery) Stop() error {
	sd.logger.Info("Stopping static discovery")
	close(sd.stopChan)
	return nil
}

// DiscoverNodes 发现节点
func (sd *StaticDiscovery) DiscoverNodes() ([]common.ClusterNode, error) {
	sd.nodesMutex.RLock()
	defer sd.nodesMutex.RUnlock()

	nodes := make([]common.ClusterNode, 0, len(sd.nodes))
	for _, node := range sd.nodes {
		nodes = append(nodes, *node)
	}

	sd.logger.Debug("Discovered nodes", zap.Int("count", len(nodes)))
	return nodes, nil
}

// RegisterNode 注册节点
func (sd *StaticDiscovery) RegisterNode(node *common.ClusterNode) error {
	nodeID := node.ID.HostPortString()

	sd.nodesMutex.Lock()
	sd.nodes[nodeID] = node
	sd.nodesMutex.Unlock()

	sd.logger.Info("Node registered", zap.String("node", nodeID))

	// 通知观察者
	sd.notifyWatchers()

	return nil
}

// UnregisterNode 注销节点
func (sd *StaticDiscovery) UnregisterNode(nodeID common.NodeID) error {
	nodeIDStr := nodeID.HostPortString()

	sd.nodesMutex.Lock()
	delete(sd.nodes, nodeIDStr)
	sd.nodesMutex.Unlock()

	sd.logger.Info("Node unregistered", zap.String("node", nodeIDStr))

	// 通知观察者
	sd.notifyWatchers()

	return nil
}

// Watch 监听节点变化
func (sd *StaticDiscovery) Watch(callback func([]common.ClusterNode)) error {
	sd.watchers = append(sd.watchers, callback)
	return nil
}

// 私有方法

func (sd *StaticDiscovery) loadStaticNodes() error {
	// 从配置中获取静态节点列表
	staticNodes, ok := sd.config.DiscoveryConfig["nodes"].([]interface{})
	if !ok {
		sd.logger.Info("No static nodes configured")
		return nil
	}

	for i, nodeConfig := range staticNodes {
		nodeMap, ok := nodeConfig.(map[string]interface{})
		if !ok {
			sd.logger.Warn("Invalid node configuration", zap.Int("index", i))
			continue
		}

		node, err := sd.parseNodeConfig(nodeMap)
		if err != nil {
			sd.logger.Warn("Failed to parse node configuration",
				zap.Int("index", i), zap.Error(err))
			continue
		}

		sd.nodes[node.ID.HostPortString()] = node
	}

	sd.logger.Info("Loaded static nodes", zap.Int("count", len(sd.nodes)))
	return nil
}

func (sd *StaticDiscovery) parseNodeConfig(config map[string]interface{}) (*common.ClusterNode, error) {
	host, ok := config["host"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid host")
	}

	port, ok := config["port"].(int)
	if !ok {
		return nil, fmt.Errorf("missing or invalid port")
	}

	nodeType, ok := config["type"].(string)
	if !ok {
		nodeType = "nodemanager" // 默认类型
	}

	node := &common.ClusterNode{
		ID: common.NodeID{
			Host: host,
			Port: int32(port),
		},
		Type:          common.NodeType(nodeType),
		State:         common.NodeStateActive,
		Roles:         []common.NodeRole{common.NodeRoleWorker},
		LastHeartbeat: time.Now(),
		JoinTime:      time.Now(),
		Metadata:      make(map[string]string),
		Health: common.NodeHealth{
			Status:    common.HealthStatusHealthy,
			LastCheck: time.Now(),
			Issues:    make([]common.HealthIssue, 0),
			Metrics:   make(map[string]float64),
		},
	}

	// 解析元数据
	if metadata, ok := config["metadata"].(map[string]interface{}); ok {
		for key, value := range metadata {
			if strValue, ok := value.(string); ok {
				node.Metadata[key] = strValue
			}
		}
	}

	return node, nil
}

func (sd *StaticDiscovery) watchLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // 每10秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 静态发现中，节点变化较少，主要是心跳更新
			sd.updateNodeHeartbeats()
		case <-ctx.Done():
			return
		case <-sd.stopChan:
			return
		}
	}
}

func (sd *StaticDiscovery) updateNodeHeartbeats() {
	sd.nodesMutex.Lock()
	defer sd.nodesMutex.Unlock()

	// 更新本地节点的心跳
	if localNode, exists := sd.nodes[sd.localNode.ID.HostPortString()]; exists {
		localNode.LastHeartbeat = time.Now()
	}
}

func (sd *StaticDiscovery) notifyWatchers() {
	nodes, _ := sd.DiscoverNodes()
	for _, watcher := range sd.watchers {
		go watcher(nodes)
	}
}
