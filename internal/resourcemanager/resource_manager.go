package resourcemanager

import (
	"carrot/internal/common"
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
