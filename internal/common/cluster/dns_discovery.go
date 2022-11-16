package cluster

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"carrot/internal/common"
	"go.uber.org/zap"
)

// DNSDiscovery DNS服务发现实现
type DNSDiscovery struct {
	config     common.ClusterConfig
	localNode  *common.ClusterNode
	logger     *zap.Logger
	nodes      map[string]*common.ClusterNode
	nodesMutex sync.RWMutex
	stopChan   chan struct{}
	watchers   []func([]common.ClusterNode)

	// DNS配置
	serviceName string
	domain      string
	port        int32
}

// NewDNSDiscovery 创建DNS服务发现实例
func NewDNSDiscovery(config common.ClusterConfig, localNode *common.ClusterNode, logger *zap.Logger) (*DNSDiscovery, error) {
	// 从配置中获取DNS设置
	serviceName, ok := config.DiscoveryConfig["service_name"].(string)
	if !ok {
		serviceName = "carrot"
	}

	domain, ok := config.DiscoveryConfig["domain"].(string)
	if !ok {
		domain = "local"
	}

	port := int32(8088)
	if p, ok := config.DiscoveryConfig["port"].(int); ok {
		port = int32(p)
	}

	return &DNSDiscovery{
		config:      config,
		localNode:   localNode,
		logger:      logger.With(zap.String("component", "dns_discovery")),
		nodes:       make(map[string]*common.ClusterNode),
		stopChan:    make(chan struct{}),
		watchers:    make([]func([]common.ClusterNode), 0),
		serviceName: serviceName,
		domain:      domain,
		port:        port,
	}, nil
}

// Start 启动DNS服务发现
func (dd *DNSDiscovery) Start(ctx context.Context) error {
	dd.logger.Info("Starting DNS discovery",
		zap.String("service", dd.serviceName),
		zap.String("domain", dd.domain))

	// 注册本地节点
	if err := dd.RegisterNode(dd.localNode); err != nil {
		return fmt.Errorf("failed to register local node: %w", err)
	}

	// 启动定期发现
	go dd.discoveryLoop(ctx)

	dd.logger.Info("DNS discovery started")
	return nil
}

// Stop 停止DNS服务发现
func (dd *DNSDiscovery) Stop() error {
	dd.logger.Info("Stopping DNS discovery")
	close(dd.stopChan)
	return nil
}

// DiscoverNodes 发现节点
func (dd *DNSDiscovery) DiscoverNodes() ([]common.ClusterNode, error) {
	// 执行DNS查询
	if err := dd.performDNSLookup(); err != nil {
		dd.logger.Warn("DNS lookup failed", zap.Error(err))
	}

	dd.nodesMutex.RLock()
	defer dd.nodesMutex.RUnlock()

	nodes := make([]common.ClusterNode, 0, len(dd.nodes))
	for _, node := range dd.nodes {
		nodes = append(nodes, *node)
	}

	dd.logger.Debug("Discovered nodes via DNS", zap.Int("count", len(nodes)))
	return nodes, nil
}

// RegisterNode 注册节点
func (dd *DNSDiscovery) RegisterNode(node *common.ClusterNode) error {
	nodeID := node.ID.String()

	dd.nodesMutex.Lock()
	dd.nodes[nodeID] = node
	dd.nodesMutex.Unlock()

	dd.logger.Info("Node registered", zap.String("node", nodeID))

	// 通知观察者
	dd.notifyWatchers()

	return nil
}

// UnregisterNode 注销节点
func (dd *DNSDiscovery) UnregisterNode(nodeID common.NodeID) error {
	nodeIDStr := nodeID.String()

	dd.nodesMutex.Lock()
	delete(dd.nodes, nodeIDStr)
	dd.nodesMutex.Unlock()

	dd.logger.Info("Node unregistered", zap.String("node", nodeIDStr))

	// 通知观察者
	dd.notifyWatchers()

	return nil
}

// Watch 监听节点变化
func (dd *DNSDiscovery) Watch(callback func([]common.ClusterNode)) error {
	dd.watchers = append(dd.watchers, callback)
	return nil
}

// 私有方法

func (dd *DNSDiscovery) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每30秒执行一次DNS查询
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := dd.performDNSLookup(); err != nil {
				dd.logger.Warn("DNS lookup failed", zap.Error(err))
			}
		case <-ctx.Done():
			return
		case <-dd.stopChan:
			return
		}
	}
}

func (dd *DNSDiscovery) performDNSLookup() error {
	// 构建查询地址
	hostname := fmt.Sprintf("%s.%s", dd.serviceName, dd.domain)

	dd.logger.Debug("Performing DNS lookup", zap.String("hostname", hostname))

	// 查询A记录
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("failed to lookup IPs: %w", err)
	}

	// 也查询SRV记录以获取端口信息
	_, srvRecords, err := net.LookupSRV("", "", hostname)
	portMap := make(map[string]uint16)
	if err == nil {
		for _, srv := range srvRecords {
			portMap[srv.Target] = srv.Port
		}
	}

	// 更新节点列表
	newNodes := make(map[string]*common.ClusterNode)

	for _, ip := range ips {
		ipStr := ip.String()
		port := dd.port

		// 如果有SRV记录，使用SRV记录中的端口
		if srvPort, exists := portMap[ipStr+"."]; exists {
			port = int32(srvPort)
		}

		nodeID := fmt.Sprintf("%s:%d", ipStr, port)

		// 检查是否是新节点
		dd.nodesMutex.RLock()
		existingNode, exists := dd.nodes[nodeID]
		dd.nodesMutex.RUnlock()

		if exists {
			newNodes[nodeID] = existingNode
		} else {
			// 创建新节点
			node := &common.ClusterNode{
				ID: common.NodeID{
					Host: ipStr,
					Port: port,
				},
				Type:          common.NodeTypeNodeManager,
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

			newNodes[nodeID] = node

			dd.logger.Info("Discovered new node via DNS",
				zap.String("node", nodeID))
		}
	}

	// 更新节点列表
	dd.nodesMutex.Lock()
	dd.nodes = newNodes
	dd.nodesMutex.Unlock()

	// 通知观察者
	dd.notifyWatchers()

	return nil
}

func (dd *DNSDiscovery) notifyWatchers() {
	nodes, _ := dd.DiscoverNodes()
	for _, watcher := range dd.watchers {
		go watcher(nodes)
	}
}

// 以下是占位符实现，用于保持接口兼容性

// NewEtcdDiscovery 创建Etcd服务发现实例（占位符）
func NewEtcdDiscovery(config common.ClusterConfig, localNode *common.ClusterNode, logger *zap.Logger) (Discovery, error) {
	logger.Warn("Etcd discovery not implemented, falling back to static discovery")
	return NewStaticDiscovery(config, localNode, logger)
}

// NewConsulDiscovery 创建Consul服务发现实例（占位符）
func NewConsulDiscovery(config common.ClusterConfig, localNode *common.ClusterNode, logger *zap.Logger) (Discovery, error) {
	logger.Warn("Consul discovery not implemented, falling back to static discovery")
	return NewStaticDiscovery(config, localNode, logger)
}
