package resourcemanager

import (
	"carrot/internal/common"
	"carrot/internal/resourcemanager/scheduler"
	"context"
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
