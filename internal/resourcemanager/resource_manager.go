package resourcemanager

import (
	"net/http"
	"sync"
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
