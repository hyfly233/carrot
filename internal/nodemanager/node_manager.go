package nodemanager

import (
	"carrot/internal/common"
	"net/http"
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
