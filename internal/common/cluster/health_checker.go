package cluster

import (
	"context"
	"runtime"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// DefaultHealthChecker 默认健康检查器实现
type DefaultHealthChecker struct {
	config         common.ClusterConfig
	localNode      *common.ClusterNode
	clusterManager *ClusterManager
	logger         *zap.Logger

	// 健康检查
	healthChecks map[string]func() *common.HealthIssue
	healthMutex  sync.RWMutex

	// 节点健康状态
	nodeHealth   *common.NodeHealth
	healthMutex2 sync.RWMutex

	// 控制
	stopChan      chan struct{}
	checkInterval time.Duration
}

// NewDefaultHealthChecker 创建默认健康检查器
func NewDefaultHealthChecker(config common.ClusterConfig, localNode *common.ClusterNode,
	clusterManager *ClusterManager, logger *zap.Logger) (*DefaultHealthChecker, error) {

	hc := &DefaultHealthChecker{
		config:         config,
		localNode:      localNode,
		clusterManager: clusterManager,
		logger:         logger.With(zap.String("component", "health_checker")),
		healthChecks:   make(map[string]func() *common.HealthIssue),
		stopChan:       make(chan struct{}),
		checkInterval:  30 * time.Second, // 默认30秒检查一次
		nodeHealth: &common.NodeHealth{
			Status:    common.HealthStatusUnknown,
			LastCheck: time.Now(),
			Issues:    make([]common.HealthIssue, 0),
			Metrics:   make(map[string]float64),
		},
	}

	// 注册默认健康检查
	hc.registerDefaultChecks()

	return hc, nil
}

// Start 启动健康检查
func (hc *DefaultHealthChecker) Start(ctx context.Context) error {
	hc.logger.Info("Starting health checker")

	// 启动定期健康检查
	go hc.healthCheckLoop(ctx)

	// 启动节点监控
	go hc.monitorNodes(ctx)

	hc.logger.Info("Health checker started")
	return nil
}

// Stop 停止健康检查
func (hc *DefaultHealthChecker) Stop() error {
	hc.logger.Info("Stopping health checker")
	close(hc.stopChan)
	return nil
}

// CheckNode 检查节点健康状态
func (hc *DefaultHealthChecker) CheckNode(nodeID common.NodeID) (*common.NodeHealth, error) {
	if nodeID.HostPortString() == hc.localNode.ID.HostPortString() {
		return hc.GetHealthStatus(), nil
	}

	// 对于远程节点，这里应该发送健康检查请求
	// 简化实现：返回基本状态
	return &common.NodeHealth{
		Status:    common.HealthStatusUnknown,
		LastCheck: time.Now(),
		Issues:    make([]common.HealthIssue, 0),
		Metrics:   make(map[string]float64),
	}, nil
}

// RegisterHealthCheck 注册健康检查
func (hc *DefaultHealthChecker) RegisterHealthCheck(name string, checker func() *common.HealthIssue) {
	hc.healthMutex.Lock()
	defer hc.healthMutex.Unlock()

	hc.healthChecks[name] = checker
	hc.logger.Info("Health check registered", zap.String("name", name))
}

// GetHealthStatus 获取健康状态
func (hc *DefaultHealthChecker) GetHealthStatus() *common.NodeHealth {
	hc.healthMutex2.RLock()
	defer hc.healthMutex2.RUnlock()

	// 复制健康状态
	health := &common.NodeHealth{
		Status:    hc.nodeHealth.Status,
		LastCheck: hc.nodeHealth.LastCheck,
		Issues:    make([]common.HealthIssue, len(hc.nodeHealth.Issues)),
		Metrics:   make(map[string]float64),
	}

	copy(health.Issues, hc.nodeHealth.Issues)
	for k, v := range hc.nodeHealth.Metrics {
		health.Metrics[k] = v
	}

	return health
}

// 私有方法

func (hc *DefaultHealthChecker) registerDefaultChecks() {
	// 内存检查
	hc.RegisterHealthCheck("memory", hc.checkMemory)

	// 磁盘检查
	hc.RegisterHealthCheck("disk", hc.checkDisk)

	// CPU检查
	hc.RegisterHealthCheck("cpu", hc.checkCPU)

	// 网络检查
	hc.RegisterHealthCheck("network", hc.checkNetwork)
}

func (hc *DefaultHealthChecker) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(hc.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.performHealthCheck()
		case <-ctx.Done():
			return
		case <-hc.stopChan:
			return
		}
	}
}

func (hc *DefaultHealthChecker) performHealthCheck() {
	hc.logger.Debug("Performing health check")

	hc.healthMutex.RLock()
	checks := make(map[string]func() *common.HealthIssue)
	for name, check := range hc.healthChecks {
		checks[name] = check
	}
	hc.healthMutex.RUnlock()

	var issues []common.HealthIssue
	metrics := make(map[string]float64)

	// 执行所有健康检查
	for name, check := range checks {
		if issue := check(); issue != nil {
			issue.Timestamp = time.Now()
			issues = append(issues, *issue)
			hc.logger.Warn("Health check failed",
				zap.String("check", name),
				zap.String("message", issue.Message))
		}
	}

	// 收集系统指标
	hc.collectMetrics(metrics)

	// 确定整体健康状态
	status := hc.determineHealthStatus(issues)

	// 更新健康状态
	hc.healthMutex2.Lock()
	hc.nodeHealth.Status = status
	hc.nodeHealth.LastCheck = time.Now()
	hc.nodeHealth.Issues = issues
	hc.nodeHealth.Metrics = metrics
	hc.healthMutex2.Unlock()

	// 更新本地节点的健康状态
	hc.localNode.Health = *hc.nodeHealth
}

func (hc *DefaultHealthChecker) checkMemory() *common.HealthIssue {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 简单的内存检查：如果堆内存超过1GB，发出警告
	if m.HeapInuse > 1024*1024*1024 {
		return &common.HealthIssue{
			Type:     "memory",
			Message:  "High memory usage detected",
			Severity: "warning",
		}
	}

	return nil
}

func (hc *DefaultHealthChecker) checkDisk() *common.HealthIssue {
	// 简化的磁盘检查
	// 在实际实现中，应该检查磁盘使用率、IO等
	return nil
}

func (hc *DefaultHealthChecker) checkCPU() *common.HealthIssue {
	// 简化的CPU检查
	// 在实际实现中，应该检查CPU使用率
	return nil
}

func (hc *DefaultHealthChecker) checkNetwork() *common.HealthIssue {
	// 简化的网络检查
	// 在实际实现中，应该检查网络连接性
	return nil
}

func (hc *DefaultHealthChecker) collectMetrics(metrics map[string]float64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	metrics["memory.heap_inuse"] = float64(m.HeapInuse)
	metrics["memory.heap_sys"] = float64(m.HeapSys)
	metrics["memory.stack_inuse"] = float64(m.StackInuse)
	metrics["goroutines"] = float64(runtime.NumGoroutine())
	metrics["cpu.cores"] = float64(runtime.NumCPU())
}

func (hc *DefaultHealthChecker) determineHealthStatus(issues []common.HealthIssue) common.HealthStatus {
	if len(issues) == 0 {
		return common.HealthStatusHealthy
	}

	hasCritical := false
	hasWarning := false

	for _, issue := range issues {
		switch issue.Severity {
		case "critical":
			hasCritical = true
		case "warning":
			hasWarning = true
		}
	}

	if hasCritical {
		return common.HealthStatusCritical
	}
	if hasWarning {
		return common.HealthStatusWarning
	}

	return common.HealthStatusHealthy
}

func (hc *DefaultHealthChecker) monitorNodes(ctx context.Context) {
	ticker := time.NewTicker(time.Minute) // 每分钟检查一次节点状态
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.checkClusterNodesHealth()
		case <-ctx.Done():
			return
		case <-hc.stopChan:
			return
		}
	}
}

func (hc *DefaultHealthChecker) checkClusterNodesHealth() {
	nodes := hc.clusterManager.GetNodes()

	for nodeID, node := range nodes {
		if nodeID == hc.localNode.ID.HostPortString() {
			continue // 跳过本地节点
		}

		// 检查节点是否响应
		lastHeartbeat := node.LastHeartbeat
		if time.Since(lastHeartbeat) > hc.config.FailureDetectionWindow {
			hc.logger.Warn("节点似乎已关闭",
				zap.String("node", nodeID),
				zap.Duration("since_last_heartbeat", time.Since(lastHeartbeat)))

			// 发布节点失败事件
			hc.clusterManager.publishEvent(common.ClusterEvent{
				Type:      common.ClusterEventNodeFailed,
				Source:    node.ID,
				Timestamp: time.Now(),
				Severity:  common.EventSeverityError,
				Data: map[string]interface{}{
					"reason":         "heartbeat_timeout",
					"last_heartbeat": lastHeartbeat,
				},
			})
		}
	}
}
