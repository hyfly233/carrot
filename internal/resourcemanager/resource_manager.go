package resourcemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"carrot/internal/common"
	"carrot/internal/common/cluster"
	"carrot/internal/resourcemanager/applicationmanager"
	"carrot/internal/resourcemanager/nodemanager"
	"carrot/internal/resourcemanager/scheduler"
	"carrot/internal/resourcemanager/server"

	"go.uber.org/zap"
)

// ResourceManager 资源管理器
type ResourceManager struct {
	mu               sync.RWMutex
	applications     map[string]*applicationmanager.Application
	nodes            map[string]*nodemanager.Node
	scheduler        scheduler.Scheduler
	appIDCounter     int32
	clusterTimestamp int64
	httpServer       *http.Server
	ginServer        *server.GinServer          // HTTP 服务器
	grpcServer       *server.ResourceManagerGRPCServer // gRPC 服务器
	config           *common.Config
	logger           *zap.Logger
	ctx              context.Context
	cancel           context.CancelFunc

	// 心跳监测相关
	nodeHeartbeatTimeout time.Duration
	nodeMonitorInterval  time.Duration

	// 集群支持
	clusterManager *cluster.ClusterManager
	isLeader       bool
}

// NewResourceManager 创建新的资源管理器
func NewResourceManager(config *common.Config) *ResourceManager {
	ctx, cancel := context.WithCancel(context.Background())

	// 设置默认的心跳超时和监测间隔
	nodeHeartbeatTimeout := 90 * time.Second // 90秒心跳超时
	nodeMonitorInterval := 30 * time.Second  // 30秒检查一次

	// 从配置中读取心跳参数（如果有的话）
	if config != nil {
		if config.HeartbeatTimeout > 0 {
			nodeHeartbeatTimeout = time.Duration(config.HeartbeatTimeout) * time.Second
		}
		if config.MonitorInterval > 0 {
			nodeMonitorInterval = time.Duration(config.MonitorInterval) * time.Second
		}
	}

	rm := &ResourceManager{
		applications:         make(map[string]*applicationmanager.Application),
		nodes:                make(map[string]*nodemanager.Node),
		clusterTimestamp:     time.Now().Unix(),
		config:               config,
		logger:               common.ComponentLogger(fmt.Sprintf("rm-%d", time.Now().Unix())),
		ctx:                  ctx,
		cancel:               cancel,
		nodeHeartbeatTimeout: nodeHeartbeatTimeout,
		nodeMonitorInterval:  nodeMonitorInterval,
		isLeader:             false,
	}

	// 初始化集群管理器
	if err := rm.initClusterManager(); err != nil {
		rm.logger.Error("Failed to initialize cluster manager", zap.Error(err))
		// 不是致命错误，可以继续以单机模式运行
	}

	// 使用调度器工厂创建调度器
	sch, err := scheduler.CreateScheduler(config)
	if err != nil {
		rm.logger.Error("Failed to create scheduler, falling back to FIFO",
			zap.Error(err))
		// 回退到FIFO调度器
		sch, _ = scheduler.CreateScheduler(nil)
	}

	sch.SetResourceManager(rm)
	rm.scheduler = sch

	schedulerType := "fifo"
	if config != nil && config.Scheduler.Type != "" {
		schedulerType = config.Scheduler.Type
	}

	rm.logger.Info("Scheduler initialized",
		zap.String("type", schedulerType))

	// 初始化 HTTP 服务器
	rm.ginServer = server.NewGinServer(rm, rm.logger)

	// 初始化 gRPC 服务器
	rm.grpcServer = server.NewResourceManagerGRPCServer(rm)

	return rm
}

// Start 启动资源管理器
func (rm *ResourceManager) Start(httpPort, grpcPort int) error {
	rm.logger.Info("ResourceManager starting",
		zap.Int("http_port", httpPort),
		zap.Int("grpc_port", grpcPort),
		zap.Int64("cluster_timestamp", rm.clusterTimestamp),
		zap.Duration("heartbeat_timeout", rm.nodeHeartbeatTimeout),
		zap.Duration("monitor_interval", rm.nodeMonitorInterval))

	// 启动集群管理器
	if rm.clusterManager != nil {
		if err := rm.startClusterManager(); err != nil {
			rm.logger.Warn("Failed to start cluster manager", zap.Error(err))
		}
	}

	// 启动心跳监测
	go rm.startNodeMonitor()

	// 启动 gRPC 服务器
	go func() {
		rm.logger.Info("Starting gRPC server", zap.Int("port", grpcPort))
		if err := rm.grpcServer.Start(grpcPort); err != nil {
			rm.logger.Error("Failed to start gRPC server", zap.Error(err))
		}
	}()

	// 启动 HTTP 服务器
	rm.logger.Info("Starting HTTP server", zap.Int("port", httpPort))
	return rm.ginServer.Start(httpPort)
}

// Stop 停止资源管理器
func (rm *ResourceManager) Stop() error {
	rm.logger.Info("Stopping ResourceManager")

	// 停止集群管理器
	rm.stopClusterManager()

	// 取消上下文
	rm.cancel()

	// 停止 gRPC 服务器
	if rm.grpcServer != nil {
		rm.grpcServer.Stop()
	}

	// 关闭 HTTP 服务器
	if rm.ginServer != nil {
		return rm.ginServer.Stop()
	}

	// 关闭HTTP服务器 (保留兼容性)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if rm.httpServer != nil {
		return rm.httpServer.Shutdown(ctx)
	}

	return nil
}

// loggingMiddleware 日志中间件
func (rm *ResourceManager) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 记录请求
		rm.logger.Debug("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr))

		next.ServeHTTP(w, r)

		// 记录响应
		duration := time.Since(start)
		rm.logger.Debug("HTTP response",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("duration", duration))
	})
}

// corsMiddleware CORS中间件
func (rm *ResourceManager) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SubmitApplication 提交应用程序
func (rm *ResourceManager) SubmitApplication(ctx common.ApplicationSubmissionContext) (*common.ApplicationID, error) {
	// 验证提交上下文
	if err := common.ValidateApplicationSubmissionContext(ctx); err != nil {
		rm.logger.Error("Invalid application submission context", zap.Error(err))
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	appID := common.ApplicationID{
		ClusterTimestamp: rm.clusterTimestamp,
		ID:               rm.appIDCounter,
	}
	rm.appIDCounter++

	app := &applicationmanager.Application{
		ID:              appID,
		Name:            ctx.ApplicationName,
		Type:            ctx.ApplicationType,
		User:            rm.getUserFromContext(ctx), // 改进用户获取逻辑
		Queue:           ctx.Queue,
		State:           common.ApplicationStateSubmitted,
		StartTime:       time.Now(),
		Progress:        0.0,
		AMContainerSpec: ctx.AMContainerSpec,
		Resource:        ctx.Resource,
		Attempts:        make([]*applicationmanager.ApplicationAttempt, 0),
	}

	rm.applications[rm.getAppKey(appID)] = app

	// 创建应用程序尝试
	attemptID := common.ApplicationAttemptID{
		ApplicationID: appID,
		AttemptID:     1,
	}

	attempt := &applicationmanager.ApplicationAttempt{
		ID:        attemptID,
		State:     common.ApplicationStateNew,
		StartTime: time.Now(),
	}

	app.Attempts = append(app.Attempts, attempt)

	rm.logger.Info("Application submitted",
		zap.String("app_id", rm.getAppKey(appID)),
		zap.String("app_name", ctx.ApplicationName),
		zap.String("queue", ctx.Queue))

	// 启动调度
	go rm.scheduleApplication(app)

	return &appID, nil
}

// getUserFromContext 从上下文获取用户信息
func (rm *ResourceManager) getUserFromContext(ctx common.ApplicationSubmissionContext) string {
	// TODO: 实现从认证令牌或请求头获取用户信息
	return "default"
}

// RegisterNode 注册节点
func (rm *ResourceManager) RegisterNode(nodeID common.NodeID, resource common.Resource, httpAddress string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	node := &nodemanager.Node{
		ID:                nodeID,
		HTTPAddress:       httpAddress,
		TotalResource:     resource,
		AvailableResource: resource,
		UsedResource:      common.Resource{Memory: 0, VCores: 0},
		State:             common.NodeStateRunning,
		LastHeartbeat:     time.Now(),
		Containers:        []*common.Container{},
	}

	rm.nodes[rm.getNodeKey(nodeID)] = node
	rm.logger.Info("Node registered",
		zap.String("host", nodeID.Host),
		zap.Int32("port", nodeID.Port))

	return nil
}

// NodeHeartbeat 节点心跳
func (rm *ResourceManager) NodeHeartbeat(nodeID common.NodeID, usedResource common.Resource, containers []*common.Container) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	nodeKey := rm.getNodeKey(nodeID)
	node, exists := rm.nodes[nodeKey]
	if !exists {
		return fmt.Errorf("node not found: %s:%d", nodeID.Host, nodeID.Port)
	}

	node.LastHeartbeat = time.Now()
	node.UsedResource = usedResource
	node.AvailableResource = common.Resource{
		Memory: node.TotalResource.Memory - usedResource.Memory,
		VCores: node.TotalResource.VCores - usedResource.VCores,
	}

	// 更新容器信息
	node.Containers = containers

	return nil
}

// GetApplications 获取应用程序列表
func (rm *ResourceManager) GetApplications() []*common.ApplicationReport {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	reports := make([]*common.ApplicationReport, 0, len(rm.applications))
	for _, app := range rm.applications {
		report := &common.ApplicationReport{
			ApplicationID:   app.ID,
			ApplicationName: app.Name,
			ApplicationType: app.Type,
			User:            app.User,
			Queue:           app.Queue,
			StartTime:       app.StartTime,
			FinishTime:      app.FinishTime,
			State:           app.State,
			Progress:        app.Progress,
		}
		reports = append(reports, report)
	}

	return reports
}

// GetNodes 获取节点列表
func (rm *ResourceManager) GetNodes() []*common.NodeReport {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	reports := make([]*common.NodeReport, 0, len(rm.nodes))
	for _, node := range rm.nodes {
		report := &common.NodeReport{
			NodeID:           node.ID,
			HTTPAddress:      node.HTTPAddress,
			RackName:         node.RackName,
			Used:             node.UsedResource,
			Capability:       node.TotalResource,
			NumContainers:    int32(len(node.Containers)),
			State:            node.State,
			LastHealthUpdate: node.LastHeartbeat,
		}
		reports = append(reports, report)
	}

	return reports
}

// GetNodesForScheduler 为调度器提供节点信息（避免循环依赖）
func (rm *ResourceManager) GetNodesForScheduler() map[string]*scheduler.NodeInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	nodeInfos := make(map[string]*scheduler.NodeInfo)
	for key, node := range rm.nodes {
		nodeInfos[key] = &scheduler.NodeInfo{
			ID:                node.ID,
			State:             node.State,
			AvailableResource: node.AvailableResource,
			UsedResource:      node.UsedResource,
			LastHeartbeat:     node.LastHeartbeat,
		}
	}
	return nodeInfos
}

// GetAvailableNodes 获取可用节点列表
func (rm *ResourceManager) GetAvailableNodes() []scheduler.NodeInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var availableNodes []scheduler.NodeInfo
	for _, node := range rm.nodes {
		if node.State == "running" {
			availableNodes = append(availableNodes, scheduler.NodeInfo{
				ID:                node.ID,
				Resource:          node.TotalResource,
				AvailableResource: node.AvailableResource,
				State:             node.State,
				UsedResource:      node.UsedResource,
				LastHeartbeat:     node.LastHeartbeat,
			})
		}
	}
	return availableNodes
}

// GetClusterTimestamp 获取集群时间戳
func (rm *ResourceManager) GetClusterTimestamp() int64 {
	return rm.clusterTimestamp
}

func (rm *ResourceManager) scheduleApplication(app *applicationmanager.Application) {
	// 将应用程序信息转换为调度器可用的格式
	appInfo := &scheduler.ApplicationInfo{
		ID:         app.ID,
		Resource:   app.Resource,
		SubmitTime: app.StartTime,
		Queue:      app.Queue,
		Priority:   0, // 默认优先级
	}

	// 简单的调度逻辑，为 AM 分配容器
	allocations, err := rm.scheduler.Schedule(appInfo)
	if err != nil {
		rm.logger.Error("Failed to schedule application",
			zap.Any("app_id", app.ID),
			zap.Error(err))
		return
	}

	if len(allocations) > 0 {
		rm.mu.Lock()
		app.State = common.ApplicationStateRunning
		if len(app.Attempts) > 0 {
			// 转换 ContainerAllocation 为 Container
			container := &common.Container{
				ID:       allocations[0].ID,
				NodeID:   allocations[0].NodeID,
				Resource: allocations[0].Resource,
				Status:   "ALLOCATED",
				State:    common.ContainerStateNew,
			}
			app.Attempts[0].AMContainer = container
			app.Attempts[0].State = common.ApplicationStateRunning
		}
		rm.mu.Unlock()
	}
}

func (rm *ResourceManager) getAppKey(appID common.ApplicationID) string {
	return fmt.Sprintf("%d_%d", appID.ClusterTimestamp, appID.ID)
}

func (rm *ResourceManager) getNodeKey(nodeID common.NodeID) string {
	return fmt.Sprintf("%s:%d", nodeID.Host, nodeID.Port)
}

func (rm *ResourceManager) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ContainerID)
}

func (rm *ResourceManager) handleApplications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apps := rm.GetApplications()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"apps": map[string]interface{}{
				"app": apps,
			},
		})
	case http.MethodPost:
		var ctx common.ApplicationSubmissionContext
		if err := json.NewDecoder(r.Body).Decode(&ctx); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		appID, err := rm.SubmitApplication(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"application-id": appID,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (rm *ResourceManager) handleNewApplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查领导者权限
	if err := rm.validateLeadership(); err != nil {
		rm.logger.Warn("Application submission denied: not leader", zap.Error(err))
		http.Error(w, fmt.Sprintf("Not leader: %v", err), http.StatusServiceUnavailable)
		return
	}

	rm.mu.Lock()
	appID := common.ApplicationID{
		ClusterTimestamp: rm.clusterTimestamp,
		ID:               rm.appIDCounter,
	}
	rm.appIDCounter++
	rm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(map[string]interface{}{
		"application-id": appID,
	})
	if err != nil {
		rm.logger.Error("Failed to encode new application", zap.Error(err))
	}
}

func (rm *ResourceManager) handleApplication(w http.ResponseWriter, r *http.Request) {
	// TODO: 实现单个应用程序的处理
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (rm *ResourceManager) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		rm.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := rm.GetNodes()
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": map[string]interface{}{
			"node": nodes,
		},
	})
	if err != nil {
		rm.logger.Error("Failed to encode nodes", zap.Error(err))
	}
}

func (rm *ResourceManager) handleClusterInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		rm.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 基础集群信息
	info := map[string]interface{}{
		"clusterInfo": map[string]interface{}{
			"id":                     rm.clusterTimestamp,
			"startedOn":              time.Unix(rm.clusterTimestamp, 0),
			"state":                  "STARTED",
			"haState":                "ACTIVE",
			"resourceManagerVersion": "carrot-1.0.0",
		},
	}

	// 如果启用了集群模式，添加集群详细信息
	if rm.clusterManager != nil {
		clusterInfo := rm.GetClusterInfo()
		if clusterInfo != nil {
			info["cluster"] = map[string]interface{}{
				"name":           clusterInfo.ID.Name,
				"id":             clusterInfo.ID.ID,
				"state":          string(clusterInfo.State),
				"leader":         clusterInfo.Leader,
				"nodes":          len(clusterInfo.Nodes),
				"activeNodes":    clusterInfo.Statistics.ActiveNodes,
				"failedNodes":    clusterInfo.Statistics.FailedNodes,
				"totalResources": clusterInfo.Statistics.TotalResources,
				"usedResources":  clusterInfo.Statistics.UsedResources,
				"uptime":         clusterInfo.Statistics.Uptime,
				"isLeader":       rm.IsLeader(),
			}
		}
		
		// 添加集群节点信息
		clusterNodes := rm.GetClusterNodes()
		nodeList := make([]map[string]interface{}, 0, len(clusterNodes))
		for _, node := range clusterNodes {
			nodeInfo := map[string]interface{}{
				"id":            node.ID.String(),
				"type":          string(node.Type),
				"state":         string(node.State),
				"roles":         node.Roles,
				"lastHeartbeat": node.LastHeartbeat,
				"joinTime":      node.JoinTime,
				"health":        node.Health.Status,
				"version":       node.Version,
			}
			nodeList = append(nodeList, nodeInfo)
		}
		info["clusterNodes"] = nodeList
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(info)
	if err != nil {
		rm.logger.Error("Failed to encode cluster info", zap.Error(err))
	}
}

func (rm *ResourceManager) handleNodeRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rm.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var registrationData struct {
		NodeID      common.NodeID   `json:"node_id"`
		Resource    common.Resource `json:"resource"`
		HTTPAddress string          `json:"http_address"`
	}

	if err := json.NewDecoder(r.Body).Decode(&registrationData); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := rm.RegisterNode(registrationData.NodeID, registrationData.Resource, registrationData.HTTPAddress)
	if err != nil {
		rm.logger.Error("Failed to register node", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "registered",
	})
	if err != nil {
		rm.logger.Error("Failed to encode registration response", zap.Error(err))
	}
}

func (rm *ResourceManager) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rm.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var heartbeatData struct {
		NodeID       common.NodeID       `json:"node_id"`
		UsedResource common.Resource     `json:"used_resource"`
		Containers   []*common.Container `json:"containers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&heartbeatData); err != nil {
		rm.logger.Error("Failed to decode heartbeat", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := rm.NodeHeartbeat(heartbeatData.NodeID, heartbeatData.UsedResource, heartbeatData.Containers)
	if err != nil {
		rm.logger.Error("Failed to process node heartbeat", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "heartbeat_received",
	})
	if err != nil {
		rm.logger.Error("Failed to encode heartbeat response", zap.Error(err))
	}
}

// handleNodeHealth 处理节点健康状态查询
func (rm *ResourceManager) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	healthStatus := rm.GetNodeHealthStatus()

	// 获取详细的节点信息
	rm.mu.RLock()
	nodeDetails := make([]map[string]interface{}, 0, len(rm.nodes))
	for nodeKey, node := range rm.nodes {
		timeSinceLastHeartbeat := time.Since(node.LastHeartbeat)

		detail := map[string]interface{}{
			"node_key":                  nodeKey,
			"node_id":                   node.ID,
			"state":                     node.State,
			"last_heartbeat":            node.LastHeartbeat.Format(time.RFC3339),
			"time_since_last_heartbeat": timeSinceLastHeartbeat.String(),
			"health_report":             node.HealthReport,
			"total_resource":            node.TotalResource,
			"used_resource":             node.UsedResource,
			"available_resource":        node.AvailableResource,
			"num_containers":            len(node.Containers),
		}
		nodeDetails = append(nodeDetails, detail)
	}
	rm.mu.RUnlock()

	response := map[string]interface{}{
		"summary":      healthStatus,
		"node_details": nodeDetails,
		"monitor_config": map[string]interface{}{
			"heartbeat_timeout": rm.nodeHeartbeatTimeout.String(),
			"monitor_interval":  rm.nodeMonitorInterval.String(),
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		rm.logger.Error("Failed to encode node health response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// startNodeMonitor 启动节点心跳监测
func (rm *ResourceManager) startNodeMonitor() {
	rm.logger.Info("Starting node heartbeat monitor",
		zap.Duration("check_interval", rm.nodeMonitorInterval),
		zap.Duration("timeout_threshold", rm.nodeHeartbeatTimeout))

	ticker := time.NewTicker(rm.nodeMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			rm.logger.Info("Node monitor stopped")
			return
		case <-ticker.C:
			rm.checkNodeHealth()
		}
	}
}

// checkNodeHealth 检查节点健康状态
func (rm *ResourceManager) checkNodeHealth() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	unhealthyNodes := make([]string, 0)

	for nodeKey, node := range rm.nodes {
		timeSinceLastHeartbeat := now.Sub(node.LastHeartbeat)

		if timeSinceLastHeartbeat > rm.nodeHeartbeatTimeout {
			// 节点超时，标记为不健康
			if node.State != "UNHEALTHY" {
				rm.logger.Warn("Node heartbeat timeout, marking as unhealthy",
					zap.String("node_id", nodeKey),
					zap.String("node_host", node.ID.Host),
					zap.Int32("node_port", node.ID.Port),
					zap.Duration("time_since_last_heartbeat", timeSinceLastHeartbeat),
					zap.Duration("timeout_threshold", rm.nodeHeartbeatTimeout))

				rm.markNodeUnhealthy(node)
				unhealthyNodes = append(unhealthyNodes, nodeKey)
			}
		} else if node.State == "UNHEALTHY" && timeSinceLastHeartbeat <= rm.nodeHeartbeatTimeout {
			// 节点恢复心跳，标记为健康
			rm.logger.Info("Node heartbeat recovered, marking as healthy",
				zap.String("node_id", nodeKey),
				zap.String("node_host", node.ID.Host),
				zap.Int32("node_port", node.ID.Port))

			rm.markNodeHealthy(node)
		}
	}

	// 如果有不健康的节点，通知调度器
	if len(unhealthyNodes) > 0 {
		rm.notifySchedulerNodeChanges(unhealthyNodes)
	}
}

// markNodeUnhealthy 标记节点为不健康状态
func (rm *ResourceManager) markNodeUnhealthy(node *nodemanager.Node) {
	node.State = "UNHEALTHY"
	node.HealthReport = fmt.Sprintf("Node heartbeat timeout at %s", time.Now().Format(time.RFC3339))

	// 将节点上的容器标记为失败/丢失
	for _, container := range node.Containers {
		if container.State == "RUNNING" {
			container.State = "LOST"
			rm.logger.Warn("Container lost due to node unhealthy",
				zap.Any("container_id", container.ID),
				zap.String("node_host", node.ID.Host))
		}
	}
}

// markNodeHealthy 标记节点为健康状态
func (rm *ResourceManager) markNodeHealthy(node *nodemanager.Node) {
	node.State = "RUNNING"
	node.HealthReport = fmt.Sprintf("Node recovered at %s", time.Now().Format(time.RFC3339))
}

// notifySchedulerNodeChanges 通知调度器节点状态变化
func (rm *ResourceManager) notifySchedulerNodeChanges(nodeKeys []string) {
	// 这里可以通知调度器有节点状态变化
	// 调度器可以据此重新分配资源或处理失败的容器
	if rm.scheduler != nil {
		for _, nodeKey := range nodeKeys {
			rm.logger.Debug("Notifying scheduler of node change",
				zap.String("node_key", nodeKey))
		}
	}
}

// GetNodeHealthStatus 获取节点健康状态统计
func (rm *ResourceManager) GetNodeHealthStatus() map[string]int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	status := map[string]int{
		"RUNNING":   0,
		"UNHEALTHY": 0,
		"TOTAL":     len(rm.nodes),
	}

	for _, node := range rm.nodes {
		if count, exists := status[node.State]; exists {
			status[node.State] = count + 1
		}
	}

	return status
}

// 集群管理相关方法

// initClusterManager 初始化集群管理器
func (rm *ResourceManager) initClusterManager() error {
	if rm.config == nil {
		rm.logger.Info("No config provided, running in standalone mode")
		return nil
	}

	// 如果集群名称为空，跳过集群初始化
	if rm.config.Cluster.Name == "" {
		rm.logger.Info("No cluster name configured, running in standalone mode")
		return nil
	}

	// 验证集群配置
	if err := cluster.ValidateClusterConfig(rm.config.Cluster); err != nil {
		return fmt.Errorf("invalid cluster config: %w", err)
	}

	// 创建本地节点信息
	localNode := cluster.CreateLocalNode(
		common.NodeTypeResourceManager,
		rm.config.ResourceManager.Address,
		int32(rm.config.ResourceManager.Port),
		map[string]string{
			"role": "resourcemanager",
			"version": "1.0.0",
		},
	)

	// 设置领导者角色
	localNode.Roles = []common.NodeRole{common.NodeRoleLeader}

	// 创建集群管理器
	cm := cluster.NewClusterManager(rm.config.Cluster, localNode, rm.logger)

	// 注册回调函数
	cm.OnNodeJoined(rm.onNodeJoined)
	cm.OnNodeLeft(rm.onNodeLeft)
	cm.OnLeaderChange(rm.onLeaderChange)

	rm.clusterManager = cm

	rm.logger.Info("Cluster manager initialized",
		zap.String("cluster", rm.config.Cluster.Name),
		zap.String("discovery", rm.config.Cluster.DiscoveryMethod))

	return nil
}

// startClusterManager 启动集群管理器
func (rm *ResourceManager) startClusterManager() error {
	if rm.clusterManager == nil {
		return fmt.Errorf("cluster manager not initialized")
	}

	// 启动集群管理器
	if err := rm.clusterManager.Start(rm.ctx); err != nil {
		return fmt.Errorf("failed to start cluster manager: %w", err)
	}

	// 加入集群
	if err := rm.clusterManager.JoinCluster(); err != nil {
		rm.logger.Warn("Failed to join cluster, continuing in standalone mode", zap.Error(err))
	}

	return nil
}

// stopClusterManager 停止集群管理器
func (rm *ResourceManager) stopClusterManager() {
	if rm.clusterManager != nil {
		rm.clusterManager.Stop()
	}
}

// GetClusterInfo 获取集群信息
func (rm *ResourceManager) GetClusterInfo() *common.ClusterInfo {
	if rm.clusterManager == nil {
		return nil
	}
	return rm.clusterManager.GetClusterInfo()
}

// IsLeader 检查是否为领导者
func (rm *ResourceManager) IsLeader() bool {
	if rm.clusterManager == nil {
		return true // 单机模式下总是领导者
	}
	return rm.clusterManager.IsLeader()
}

// GetClusterNodes 获取集群节点列表
func (rm *ResourceManager) GetClusterNodes() map[string]*common.ClusterNode {
	if rm.clusterManager == nil {
		return make(map[string]*common.ClusterNode)
	}
	return rm.clusterManager.GetNodes()
}

// 集群事件回调函数

// onNodeJoined 处理节点加入事件
func (rm *ResourceManager) onNodeJoined(node *common.ClusterNode) {
	rm.logger.Info("Cluster node joined",
		zap.String("node", node.ID.String()),
		zap.String("type", string(node.Type)))

	// 如果是NodeManager节点，等待它主动注册
	if node.Type == common.NodeTypeNodeManager {
		rm.logger.Info("NodeManager joined cluster, waiting for registration",
			zap.String("node", node.ID.String()))
	}
}

// onNodeLeft 处理节点离开事件
func (rm *ResourceManager) onNodeLeft(node *common.ClusterNode) {
	rm.logger.Info("Cluster node left",
		zap.String("node", node.ID.String()),
		zap.String("type", string(node.Type)))

	// 如果是NodeManager节点，从注册列表中移除
	if node.Type == common.NodeTypeNodeManager {
		nodeID := node.ID.String()
		rm.mu.Lock()
		if rmNode, exists := rm.nodes[nodeID]; exists {
			delete(rm.nodes, nodeID)
			rm.logger.Info("Removed node from registry",
				zap.String("node", nodeID))
			
			// 处理该节点上的容器
			rm.handleFailedNode(rmNode)
		}
		rm.mu.Unlock()
	}
}

// onLeaderChange 处理领导者变更事件
func (rm *ResourceManager) onLeaderChange(oldLeader, newLeader *common.ClusterNode) {
	if newLeader != nil && rm.clusterManager != nil {
		rm.isLeader = rm.clusterManager.IsLeader()
		
		rm.logger.Info("Leader changed",
			zap.Bool("is_leader", rm.isLeader),
			zap.String("new_leader", newLeader.ID.String()))

		if rm.isLeader {
			rm.logger.Info("Became cluster leader, taking over resource management")
			// 如果成为领导者，可以在这里初始化领导者特有的任务
		} else {
			rm.logger.Info("No longer leader, stepping down from resource management")
			// 如果不再是领导者，可以在这里清理领导者特有的任务
		}
	}
}

// handleFailedNode 处理失败的节点
func (rm *ResourceManager) handleFailedNode(node *nodemanager.Node) {
	// 标记节点上的所有容器为失败
	for _, container := range node.Containers {
		container.Status = "FAILED"
		container.State = "COMPLETE"
		
		rm.logger.Warn("Container failed due to node failure",
			zap.Any("container_id", container.ID),
			zap.String("node", node.ID.String()))
	}

	// 通知调度器节点失败，可能需要重新调度应用
	if rm.scheduler != nil {
		rm.logger.Debug("Notifying scheduler of node failure",
			zap.String("node", node.ID.String()))
	}
}

// requiresLeadership 检查操作是否需要领导者权限
func (rm *ResourceManager) requiresLeadership() bool {
	return rm.clusterManager != nil && !rm.IsLeader()
}

// validateLeadership 验证领导者权限
func (rm *ResourceManager) validateLeadership() error {
	if rm.requiresLeadership() {
		leader, exists := rm.clusterManager.GetLeader()
		if exists {
			return fmt.Errorf("operation requires leadership, current leader: %v", leader.ID.String())
		}
		return fmt.Errorf("operation requires leadership, no leader elected")
	}
	return nil
}
