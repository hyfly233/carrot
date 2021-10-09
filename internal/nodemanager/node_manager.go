package nodemanager

import (
	"carrot/internal/common"
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
