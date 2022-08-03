package resourcemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"carrot/internal/common"
	"carrot/internal/resourcemanager/applicationmanager"
	"carrot/internal/resourcemanager/nodemanager"
	"carrot/internal/resourcemanager/scheduler"

	"github.com/gorilla/mux"
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
	config           *common.Config
	logger           *zap.Logger
	ctx              context.Context
	cancel           context.CancelFunc
}

// NewResourceManager 创建新的资源管理器
func NewResourceManager(config *common.Config) *ResourceManager {
	ctx, cancel := context.WithCancel(context.Background())

	rm := &ResourceManager{
		applications:     make(map[string]*applicationmanager.Application),
		nodes:            make(map[string]*nodemanager.Node),
		clusterTimestamp: time.Now().Unix(),
		config:           config,
		logger:           common.ComponentLogger(fmt.Sprintf("rm-%d", time.Now().Unix())),
		ctx:              ctx,
		cancel:           cancel,
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

	return rm
}

// Start 启动资源管理器
func (rm *ResourceManager) Start(port int) error {
	router := mux.NewRouter()

	// API版本前缀
	v1 := router.PathPrefix("/ws/v1").Subrouter()

	// 添加中间件
	v1.Use(rm.loggingMiddleware)
	v1.Use(rm.corsMiddleware)

	// 集群信息路由
	cluster := v1.PathPrefix("/cluster").Subrouter()
	cluster.HandleFunc("/info", rm.handleClusterInfo).Methods("GET")

	// 应用程序路由
	apps := cluster.PathPrefix("/apps").Subrouter()
	apps.HandleFunc("", rm.handleApplications).Methods("GET", "POST")
	apps.HandleFunc("/new-application", rm.handleNewApplication).Methods("POST")
	apps.HandleFunc("/{appId}", rm.handleApplication).Methods("GET", "DELETE")

	// 节点路由
	nodes := cluster.PathPrefix("/nodes").Subrouter()
	nodes.HandleFunc("", rm.handleNodes).Methods("GET")
	nodes.HandleFunc("/register", rm.handleNodeRegistration).Methods("POST")
	nodes.HandleFunc("/heartbeat", rm.handleNodeHeartbeat).Methods("POST")

	rm.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	rm.logger.Info("ResourceManager starting",
		zap.Int("port", port),
		zap.Int64("cluster_timestamp", rm.clusterTimestamp))

	return rm.httpServer.ListenAndServe()
}

// Stop 停止资源管理器
func (rm *ResourceManager) Stop() error {
	rm.logger.Info("Stopping ResourceManager")

	// 取消上下文
	rm.cancel()

	// 关闭HTTP服务器
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

	info := map[string]interface{}{
		"clusterInfo": map[string]interface{}{
			"id":                     rm.clusterTimestamp,
			"startedOn":              time.Unix(rm.clusterTimestamp, 0),
			"state":                  "STARTED",
			"haState":                "ACTIVE",
			"resourceManagerVersion": "carrot-1.0.0",
		},
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
