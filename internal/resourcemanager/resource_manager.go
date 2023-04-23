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
	"carrot/internal/resourcemanager/rmam"
	"carrot/internal/resourcemanager/rmnm"
	"carrot/internal/resourcemanager/rmserver"
	"carrot/internal/resourcemanager/scheduler"

	"go.uber.org/zap"
)

// ResourceManager 资源管理器
type ResourceManager struct {
	mu               sync.RWMutex
	applications     map[string]*rmam.Application
	nodes            map[string]*rmnm.Node
	scheduler        scheduler.Scheduler
	appIDCounter     int32
	clusterTimestamp int64
	httpServer       *http.Server
	ginServer        *rmserver.GinServer                   // HTTP 服务器
	grpcServer       *rmserver.ResourceManagerGRPCServer   // NodeManager gRPC 服务器
	amGRPCServer     *rmserver.ApplicationMasterGRPCServer // ApplicationMaster gRPC 服务器
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
		applications:         make(map[string]*rmam.Application),
		nodes:                make(map[string]*rmnm.Node),
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
		rm.logger.Error("初始化 ResourceManager 集群管理器失败", zap.Error(err))
		// 不是致命错误，可以继续以单机模式运行
	}

	// 使用调度器工厂创建调度器
	sch, err := scheduler.CreateScheduler(config)
	if err != nil {
		rm.logger.Error("无法创建调度程序，使用默认的 FIFO 策略", zap.Error(err))
		// 回退到FIFO调度器
		sch, _ = scheduler.CreateScheduler(nil)
	}

	sch.SetResourceManager(rm)
	rm.scheduler = sch

	schedulerType := "fifo"
	if config != nil && config.Scheduler.Type != "" {
		schedulerType = config.Scheduler.Type
	}

	rm.logger.Info("调度器已初始化", zap.String("type", schedulerType))

	// 初始化 HTTP 服务器
	rm.ginServer = rmserver.NewGinServer(rm, rm.logger)

	// 初始化 gRPC 服务器
	rm.grpcServer = rmserver.NewResourceManagerGRPCServer(rm)

	// 初始化 ApplicationMaster gRPC 服务器
	rm.amGRPCServer = rmserver.NewApplicationMasterGRPCServer(rm)

	return rm
}

// Start 启动资源管理器
func (rm *ResourceManager) Start(httpPort, nmGRPCPort, amGRPCPort int) error {

	// 启动集群管理器
	if rm.clusterManager != nil {
		if err := rm.startClusterManager(); err != nil {
			rm.logger.Warn("无法启动集群管理器", zap.Error(err))
		}
	}

	// 启动心跳监测，处理长时间未上报心跳的节点
	go rm.startNodeMonitor()

	// 启动 NodeManager gRPC 服务器
	go func() {
		rm.logger.Info("启动 NodeManager gRPC 服务器", zap.Int("port", nmGRPCPort))
		if err := rm.grpcServer.Start(nmGRPCPort); err != nil {
			rm.logger.Error("启动 NodeManager gRPC rmserver 失败", zap.Error(err))
		}
	}()

	// 启动 ApplicationMaster gRPC 服务器
	go func() {
		rm.logger.Info("启动 ApplicationMaster gRPC 服务器", zap.Int("port", amGRPCPort))
		if err := rm.amGRPCServer.Start(amGRPCPort); err != nil {
			rm.logger.Error("启动 ApplicationMaster gRPC 服务器失败", zap.Error(err))
		}
	}()

	// 启动 HTTP 服务器
	rm.logger.Info("启动 ResourceManager HTTP 服务器", zap.Int("port", httpPort))
	return rm.ginServer.Start(httpPort)
}

// Stop 停止资源管理器
func (rm *ResourceManager) Stop() error {
	rm.logger.Info("停止 ResourceManager 中")

	// 停止集群管理器
	rm.stopClusterManager()

	// 取消上下文
	rm.cancel()

	// 停止 NodeManager gRPC 服务器
	if rm.grpcServer != nil {
		rm.grpcServer.Stop()
	}

	// 停止 ApplicationMaster gRPC 服务器
	if rm.amGRPCServer != nil {
		rm.amGRPCServer.Stop()
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

	app := &rmam.Application{
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
		Attempts:        make([]*rmam.ApplicationAttempt, 0),
	}

	rm.applications[rm.getAppKey(appID)] = app

	// 创建应用程序尝试
	attemptID := common.ApplicationAttemptID{
		ApplicationID: appID,
		AttemptID:     1,
	}

	attempt := &rmam.ApplicationAttempt{
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

	node := &rmnm.Node{
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
	rm.logger.Info("节点已注册",
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
			UsedResource:     node.UsedResource,
			TotalResource:    node.TotalResource,
			NumContainers:    int32(len(node.Containers)),
			State:            node.State,
			LastHealthUpdate: node.LastHeartbeat,
			Containers:       []string{}, // 简化处理
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

func (rm *ResourceManager) scheduleApplication(app *rmam.Application) {
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
				"id":            node.ID.HostPortString(),
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
		http.Error(w, "Internal rmserver error", http.StatusInternalServerError)
	}
}

// startNodeMonitor 启动节点心跳监测
func (rm *ResourceManager) startNodeMonitor() {
	rm.logger.Info("启动节点心跳监视器",
		zap.Duration("check_interval", rm.nodeMonitorInterval),
		zap.Duration("timeout_threshold", rm.nodeHeartbeatTimeout))

	ticker := time.NewTicker(rm.nodeMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			rm.logger.Info("节点心跳监视器停止")
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
				rm.logger.Warn("节点心跳超时，标记为不健康",
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
			rm.logger.Info("节点心跳恢复，标记为健康",
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
func (rm *ResourceManager) markNodeUnhealthy(node *rmnm.Node) {
	node.State = "UNHEALTHY"
	node.HealthReport = fmt.Sprintf("节点于 %s 心跳超时", time.Now().Format(time.RFC3339))

	// 将节点上的容器标记为失败/丢失
	for _, container := range node.Containers {
		if container.State == "RUNNING" {
			container.State = "LOST"
			rm.logger.Warn("由于节点不健康导致容器丢失",
				zap.Any("container_id", container.ID),
				zap.String("node_host", node.ID.Host))
		}
	}
}

// markNodeHealthy 标记节点为健康状态
func (rm *ResourceManager) markNodeHealthy(node *rmnm.Node) {
	node.State = "RUNNING"
	node.HealthReport = fmt.Sprintf("节点于 %s 恢复", time.Now().Format(time.RFC3339))
}

// notifySchedulerNodeChanges 通知调度器节点状态变化
func (rm *ResourceManager) notifySchedulerNodeChanges(nodeKeys []string) {
	// todo 这里可以通知调度器有节点状态变化
	// 调度器可以据此重新分配资源或处理失败的容器
	if rm.scheduler != nil {
		for _, nodeKey := range nodeKeys {
			rm.logger.Debug("通知调度程序节点变化",
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
		rm.logger.Info("未提供配置，以单节点模式运行")
		return nil
	}

	// 如果集群名称为空，跳过集群初始化
	if rm.config.Cluster.Name == "" {
		rm.logger.Info("未配置集群名称，以单节点模式运行")
		return nil
	}

	// 验证集群配置
	if err := cluster.ValidateClusterConfig(rm.config.Cluster); err != nil {
		return fmt.Errorf("无效的集群配置: %w", err)
	}

	// 创建本地节点信息
	localNode := cluster.CreateLocalNode(
		common.NodeTypeResourceManager,
		rm.config.ResourceManager.Address,
		int32(rm.config.ResourceManager.Port),
		map[string]string{
			"role":    "resourcemanager",
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

	rm.logger.Info("ResourceManager 集群管理器已初始化",
		zap.String("cluster", rm.config.Cluster.Name),
		zap.String("discovery", rm.config.Cluster.DiscoveryMethod))

	return nil
}

// startClusterManager 启动集群管理器
func (rm *ResourceManager) startClusterManager() error {
	if rm.clusterManager == nil {
		return fmt.Errorf("集群管理器未初始化")
	}

	// 启动集群管理器
	if err := rm.clusterManager.Start(rm.ctx); err != nil {
		return fmt.Errorf("启动集群管理器失败: %w", err)
	}

	// 加入集群
	if err := rm.clusterManager.JoinCluster(); err != nil {
		rm.logger.Warn("无法加入集群，继续以单节点模式运行", zap.Error(err))
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
	rm.logger.Info("集群节点已加入",
		zap.String("node", node.ID.HostPortString()),
		zap.String("type", string(node.Type)))

	// 如果是NodeManager节点，等待它主动注册
	if node.Type == common.NodeTypeNodeManager {
		rm.logger.Info("NodeManager已加入集群，等待注册",
			zap.String("node", node.ID.HostPortString()))
	}
}

// onNodeLeft 处理节点离开事件
func (rm *ResourceManager) onNodeLeft(node *common.ClusterNode) {
	rm.logger.Info("集群节点已离开",
		zap.String("node", node.ID.HostPortString()),
		zap.String("type", string(node.Type)))

	// 如果是NodeManager节点，从注册列表中移除
	if node.Type == common.NodeTypeNodeManager {
		nodeID := node.ID.HostPortString()
		rm.mu.Lock()
		if rmNode, exists := rm.nodes[nodeID]; exists {
			delete(rm.nodes, nodeID)
			rm.logger.Info("从注册表中删除节点",
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

		rm.logger.Info("集群 leader 已更换",
			zap.Bool("is_leader", rm.isLeader),
			zap.String("new_leader", newLeader.ID.HostPortString()))

		if rm.isLeader {
			rm.logger.Info("成为集群 leader，接管资源管理")
			// 如果成为领导者，可以在这里初始化领导者特有的任务
		} else {
			rm.logger.Info("不再担任集群 leader，停止资源管理")
			// 如果不再是领导者，可以在这里清理领导者特有的任务
		}
	}
}

// handleFailedNode 处理失败的节点
func (rm *ResourceManager) handleFailedNode(node *rmnm.Node) {
	// 标记节点上的所有容器为失败
	for _, container := range node.Containers {
		container.Status = "FAILED"
		container.State = "COMPLETE"

		rm.logger.Warn("容器因节点故障而失败",
			zap.Any("container_id", container.ID),
			zap.String("node", node.ID.HostPortString()))
	}

	// 通知调度器节点失败，可能需要重新调度应用
	if rm.scheduler != nil {
		rm.logger.Debug("通知调度程序节点故障",
			zap.String("node", node.ID.HostPortString()))
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
			return fmt.Errorf("操作需要 leadership, 当前 leader: %v", leader.ID.HostPortString())
		}
		return fmt.Errorf("操作需要 leadership, 当前没有 leader")
	}
	return nil
}

// ===== ApplicationMaster 管理方法 =====

// RegisterApplicationMaster 注册 ApplicationMaster
func (rm *ResourceManager) RegisterApplicationMaster(appID common.ApplicationID, host string, rpcPort int32, trackingURL string) (common.Resource, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 查找应用程序
	app, exists := rm.applications[appID.String()]
	if !exists {
		return common.Resource{}, fmt.Errorf("应用未找到: %s", appID.String())
	}

	// 更新应用程序的 AM 信息
	app.AMHost = host
	app.AMRPCPort = int(rpcPort)
	app.TrackingURL = trackingURL
	app.State = common.ApplicationStateRunning

	rm.logger.Info("ApplicationMaster 支持注册成功",
		zap.String("app_id", appID.String()),
		zap.String("host", host),
		zap.Int32("rpc_port", rpcPort),
		zap.String("tracking_url", trackingURL))

	// 返回集群的最大资源容量
	maxResource := common.Resource{
		Memory: 2048, // 2GB，实际应该从集群配置获取
		VCores: 1,    // 1核，实际应该从集群配置获取
	}

	return maxResource, nil
}

// AllocateContainers 分配容器
func (rm *ResourceManager) AllocateContainers(appID common.ApplicationID, asks []*common.ContainerRequest, releases []common.ContainerID,
	completed []*common.Container, progress float32) ([]*common.Container, []common.NodeReport, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 查找应用程序
	app, exists := rm.applications[appID.String()]
	if !exists {
		return nil, nil, fmt.Errorf("应用未找到: %s", appID.String())
	}

	// 更新应用程序进度
	app.Progress = progress

	// 使用调度器分配容器
	var allocatedContainers []*common.Container
	for _, ask := range asks {
		// 简化的分配逻辑，实际应该使用调度器
		if len(rm.nodes) > 0 {
			// 选择第一个可用节点
			for _, node := range rm.nodes {
				if node.AvailableResource.Memory >= ask.Resource.Memory && node.AvailableResource.VCores >= ask.Resource.VCores {
					// 创建容器
					container := &common.Container{
						ID: common.ContainerID{
							ApplicationAttemptID: common.ApplicationAttemptID{
								ApplicationID: appID,
								AttemptID:     1,
							},
							ContainerID: time.Now().UnixNano(), // 简化的容器ID生成
						},
						NodeID:   node.ID,
						Resource: ask.Resource,
						Status:   "ALLOCATED",
						State:    "NEW",
					}
					allocatedContainers = append(allocatedContainers, container)

					// 更新节点资源
					node.AvailableResource.Memory -= ask.Resource.Memory
					node.AvailableResource.VCores -= ask.Resource.VCores
					break
				}
			}
		}
	}

	// 处理释放的容器
	for _, releaseID := range releases {
		// 查找并释放容器
		rm.logger.Info("Container 已释放", zap.Any("container_id", releaseID))
	}

	// 生成节点报告
	var updatedNodes []common.NodeReport
	for _, node := range rm.nodes {
		updatedNodes = append(updatedNodes, common.NodeReport{
			NodeID:        node.ID,
			HTTPAddress:   fmt.Sprintf("http://%s:%d", node.ID.Host, node.ID.Port),
			UsedResource:  common.Resource{Memory: node.TotalResource.Memory - node.AvailableResource.Memory, VCores: node.TotalResource.VCores - node.AvailableResource.VCores},
			TotalResource: node.TotalResource,
			NumContainers: int32(len(node.Containers)),
			State:         "RUNNING",
			Containers:    []string{}, // 简化处理
		})
	}

	return allocatedContainers, updatedNodes, nil
}

// FinishApplicationMaster 完成 ApplicationMaster
func (rm *ResourceManager) FinishApplicationMaster(appID common.ApplicationID, finalStatus string, diagnostics string, trackingURL string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	app, exists := rm.applications[appID.String()]
	if !exists {
		return fmt.Errorf("应用未找到: %s", appID.String())
	}

	// 更新应用程序状态
	app.FinalStatus = finalStatus
	app.TrackingURL = trackingURL
	app.State = common.ApplicationStateFinished
	app.FinishTime = time.Now()

	rm.logger.Info("ApplicationMaster 已完成",
		zap.String("app_id", appID.String()),
		zap.String("final_status", finalStatus),
		zap.String("diagnostics", diagnostics))

	return nil
}

// GetApplicationReport 获取应用程序报告
func (rm *ResourceManager) GetApplicationReport(appID common.ApplicationID) (*common.ApplicationReport, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	app, exists := rm.applications[appID.String()]
	if !exists {
		return nil, fmt.Errorf("应用未找到: %s", appID.String())
	}

	return &common.ApplicationReport{
		ApplicationID:   appID,
		ApplicationName: app.Name,
		ApplicationType: app.Type,
		User:            app.User,
		Queue:           app.Queue,
		Host:            app.AMHost,
		RPCPort:         app.AMRPCPort,
		TrackingURL:     app.TrackingURL,
		StartTime:       app.StartTime,
		FinishTime:      app.FinishTime,
		FinalStatus:     app.FinalStatus,
		State:           app.State,
		Progress:        app.Progress,
		Diagnostics:     "", // 可以添加诊断信息
	}, nil
}

// GetClusterMetrics 获取集群指标
func (rm *ResourceManager) GetClusterMetrics() (*common.ClusterMetrics, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	metrics := &common.ClusterMetrics{
		AppsSubmitted:         len(rm.applications),
		AppsCompleted:         0,
		AppsPending:           0,
		AppsRunning:           0,
		AppsFailed:            0,
		AppsKilled:            0,
		ActiveNodes:           len(rm.nodes),
		LostNodes:             0,
		UnhealthyNodes:        0,
		DecommissionedNodes:   0,
		TotalNodes:            len(rm.nodes),
		ReservedVirtualCores:  0,
		AvailableVirtualCores: 0,
		AllocatedVirtualCores: 0,
	}

	// 统计应用程序状态
	for _, app := range rm.applications {
		switch app.State {
		case common.ApplicationStateRunning:
			metrics.AppsRunning++
		case common.ApplicationStateFinished:
			if app.FinalStatus == common.FinalApplicationStatusSucceeded {
				metrics.AppsCompleted++
			} else {
				metrics.AppsFailed++
			}
		case common.ApplicationStateSubmitted:
			metrics.AppsPending++
		case common.ApplicationStateKilled:
			metrics.AppsKilled++
		}
	}

	// 统计资源使用情况
	for _, node := range rm.nodes {
		metrics.AvailableVirtualCores += int(node.AvailableResource.VCores)
		metrics.AllocatedVirtualCores += int(node.TotalResource.VCores - node.AvailableResource.VCores)
	}

	return metrics, nil
}
