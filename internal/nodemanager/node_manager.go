package nodemanager

import (
	"carrot/internal/common"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// NodeManager 节点管理器
type NodeManager struct {
	mu                 sync.RWMutex
	nodeID             common.NodeID
	resourceManagerURL string
	totalResource      common.Resource
	usedResource       common.Resource
	containers         map[string]*Container
	httpServer         *http.Server
	heartbeatInterval  time.Duration
	stopChan           chan struct{}
}

// Container 容器
type Container struct {
	ID            common.ContainerID            `json:"id"`
	LaunchContext common.ContainerLaunchContext `json:"launch_context"`
	Resource      common.Resource               `json:"resource"`
	State         string                        `json:"state"`
	ExitCode      int                           `json:"exit_code"`
	Diagnostics   string                        `json:"diagnostics"`
	Process       *os.Process                   `json:"-"`
	StartTime     time.Time                     `json:"start_time"`
	FinishTime    time.Time                     `json:"finish_time,omitempty"`
}

// NewNodeManager 创建新的节点管理器
func NewNodeManager(nodeID common.NodeID, totalResource common.Resource, rmURL string) *NodeManager {
	return &NodeManager{
		nodeID:             nodeID,
		resourceManagerURL: rmURL,
		totalResource:      totalResource,
		usedResource:       common.Resource{Memory: 0, VCores: 0},
		containers:         make(map[string]*Container),
		heartbeatInterval:  3 * time.Second,
		stopChan:           make(chan struct{}),
	}
}

// Start 启动节点管理器
func (nm *NodeManager) Start(port int) error {
	// 注册到 ResourceManager
	if err := nm.registerWithRM(); err != nil {
		return fmt.Errorf("failed to register with RM: %v", err)
	}

	// 启动 HTTP 服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/v1/node/containers", nm.handleContainers)
	mux.HandleFunc("/ws/v1/node/containers/", nm.handleContainer)

	nm.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// 启动心跳
	go nm.startHeartbeat()

	// 启动容器监控
	go nm.monitorContainers()

	log.Printf("NodeManager starting on port %d", port)
	return nm.httpServer.ListenAndServe()
}
