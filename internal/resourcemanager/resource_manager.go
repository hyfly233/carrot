package resourcemanager

import (
	"carrot/internal/common"
	"carrot/internal/resourcemanager/scheduler"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ResourceManager 资源管理器
type ResourceManager struct {
	mu               sync.RWMutex
	applications     map[string]*Application
	nodes            map[string]*Node
	scheduler        Scheduler
	appIDCounter     int32
	clusterTimestamp int64
	httpServer       *http.Server
}

// Application 应用程序
type Application struct {
	ID              common.ApplicationID          `json:"id"`
	Name            string                        `json:"name"`
	Type            string                        `json:"type"`
	User            string                        `json:"user"`
	Queue           string                        `json:"queue"`
	State           string                        `json:"state"`
	StartTime       time.Time                     `json:"start_time"`
	FinishTime      time.Time                     `json:"finish_time,omitempty"`
	Progress        float32                       `json:"progress"`
	Attempts        []*ApplicationAttempt         `json:"attempts"`
	AMContainerSpec common.ContainerLaunchContext `json:"am_container_spec"`
	Resource        common.Resource               `json:"resource"`
}

// ApplicationAttempt 应用程序尝试
type ApplicationAttempt struct {
	ID          common.ApplicationAttemptID `json:"id"`
	State       string                      `json:"state"`
	StartTime   time.Time                   `json:"start_time"`
	FinishTime  time.Time                   `json:"finish_time,omitempty"`
	AMContainer *common.Container           `json:"am_container,omitempty"`
	TrackingURL string                      `json:"tracking_url"`
}

// Node 节点
type Node struct {
	ID                common.NodeID                `json:"id"`
	HTTPAddress       string                       `json:"http_address"`
	RackName          string                       `json:"rack_name"`
	TotalResource     common.Resource              `json:"total_resource"`
	UsedResource      common.Resource              `json:"used_resource"`
	AvailableResource common.Resource              `json:"available_resource"`
	State             string                       `json:"state"`
	LastHeartbeat     time.Time                    `json:"last_heartbeat"`
	Containers        map[string]*common.Container `json:"containers"`
}

// Scheduler 调度器接口
type Scheduler interface {
	Schedule(app *scheduler.ApplicationInfo) ([]*common.Container, error)
	AllocateContainers(requests []common.ContainerRequest) ([]*common.Container, error)
	SetResourceManager(rm scheduler.ResourceManagerInterface)
}

// NewResourceManager 创建新的资源管理器
func NewResourceManager() *ResourceManager {
	rm := &ResourceManager{
		applications:     make(map[string]*Application),
		nodes:            make(map[string]*Node),
		clusterTimestamp: time.Now().Unix(),
	}

	// 创建调度器并设置资源管理器引用
	fifoScheduler := scheduler.NewFIFOScheduler()
	fifoScheduler.SetResourceManager(rm)
	rm.scheduler = fifoScheduler

	return rm
}

// Start 启动资源管理器
func (rm *ResourceManager) Start(port int) error {
	mux := http.NewServeMux()

	// REST API 端点
	mux.HandleFunc("/ws/v1/cluster/apps", rm.handleApplications)
	mux.HandleFunc("/ws/v1/cluster/apps/new-application", rm.handleNewApplication)
	mux.HandleFunc("/ws/v1/cluster/apps/", rm.handleApplication)
	mux.HandleFunc("/ws/v1/cluster/nodes", rm.handleNodes)
	mux.HandleFunc("/ws/v1/cluster/nodes/register", rm.handleNodeRegistration)
	mux.HandleFunc("/ws/v1/cluster/nodes/heartbeat", rm.handleNodeHeartbeat)
	mux.HandleFunc("/ws/v1/cluster/info", rm.handleClusterInfo)

	rm.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	log.Printf("ResourceManager starting on port %d", port)
	return rm.httpServer.ListenAndServe()
}

// Stop 停止资源管理器
func (rm *ResourceManager) Stop() error {
	if rm.httpServer != nil {
		return rm.httpServer.Shutdown(context.Background())
	}
	return nil
}

// SubmitApplication 提交应用程序
func (rm *ResourceManager) SubmitApplication(ctx common.ApplicationSubmissionContext) (*common.ApplicationID, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	appID := common.ApplicationID{
		ClusterTimestamp: rm.clusterTimestamp,
		ID:               rm.appIDCounter,
	}
	rm.appIDCounter++

	app := &Application{
		ID:              appID,
		Name:            ctx.ApplicationName,
		Type:            ctx.ApplicationType,
		User:            "default", // TODO: 从上下文获取用户
		Queue:           ctx.Queue,
		State:           common.ApplicationStateSubmitted,
		StartTime:       time.Now(),
		AMContainerSpec: ctx.AMContainerSpec,
		Resource:        ctx.Resource,
		Attempts:        make([]*ApplicationAttempt, 0),
	}

	rm.applications[rm.getAppKey(appID)] = app

	// 创建应用程序尝试
	attemptID := common.ApplicationAttemptID{
		ApplicationID: appID,
		AttemptID:     1,
	}

	attempt := &ApplicationAttempt{
		ID:        attemptID,
		State:     common.ApplicationStateNew,
		StartTime: time.Now(),
	}

	app.Attempts = append(app.Attempts, attempt)

	// 启动调度
	go rm.scheduleApplication(app)

	return &appID, nil
}

// RegisterNode 注册节点
func (rm *ResourceManager) RegisterNode(nodeID common.NodeID, resource common.Resource, httpAddress string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	node := &Node{
		ID:                nodeID,
		HTTPAddress:       httpAddress,
		TotalResource:     resource,
		AvailableResource: resource,
		UsedResource:      common.Resource{Memory: 0, VCores: 0},
		State:             common.NodeStateRunning,
		LastHeartbeat:     time.Now(),
		Containers:        make(map[string]*common.Container),
	}

	rm.nodes[rm.getNodeKey(nodeID)] = node
	log.Printf("Node registered: %s:%d", nodeID.Host, nodeID.Port)

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
	node.Containers = make(map[string]*common.Container)
	for _, container := range containers {
		node.Containers[rm.getContainerKey(container.ID)] = container
	}

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
		}
	}
	return nodeInfos
}

// GetClusterTimestamp 获取集群时间戳
func (rm *ResourceManager) GetClusterTimestamp() int64 {
	return rm.clusterTimestamp
}

func (rm *ResourceManager) scheduleApplication(app *Application) {
	// 将应用程序信息转换为调度器可用的格式
	appInfo := &scheduler.ApplicationInfo{
		ID:       app.ID,
		Resource: app.Resource,
	}

	// 简单的调度逻辑，为 AM 分配容器
	containers, err := rm.scheduler.Schedule(appInfo)
	if err != nil {
		log.Printf("Failed to schedule application %v: %v", app.ID, err)
		return
	}

	if len(containers) > 0 {
		app.State = common.ApplicationStateRunning
		if len(app.Attempts) > 0 {
			app.Attempts[0].AMContainer = containers[0]
			app.Attempts[0].State = common.ApplicationStateRunning
		}
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"application-id": appID,
	})
}

func (rm *ResourceManager) handleApplication(w http.ResponseWriter, r *http.Request) {
	// TODO: 实现单个应用程序的处理
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (rm *ResourceManager) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := rm.GetNodes()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": map[string]interface{}{
			"node": nodes,
		},
	})
}

func (rm *ResourceManager) handleClusterInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	json.NewEncoder(w).Encode(info)
}
