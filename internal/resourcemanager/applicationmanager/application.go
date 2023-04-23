package applicationmanager

import (
	"carrot/internal/common"
	"time"
)

// Application 应用程序
type Application struct {
	ID              common.ApplicationID          `json:"id"`
	Name            string                        `json:"name"`
	Type            string                        `json:"type"`
	User            string                        `json:"user"`
	Queue           string                        `json:"queue"`
	State           string                        `json:"state"`
	FinalStatus     string                        `json:"final_status"`
	StartTime       time.Time                     `json:"start_time"`
	FinishTime      time.Time                     `json:"finish_time,omitempty"`
	Progress        float32                       `json:"progress"`
	Attempts        []*ApplicationAttempt         `json:"attempts"`
	AMContainerSpec common.ContainerLaunchContext `json:"am_container_spec"`
	Resource        common.Resource               `json:"resource"`
	// ApplicationMaster 相关字段
	AMHost      string `json:"am_host"`
	AMRPCPort   int    `json:"am_rpc_port"`
	TrackingURL string `json:"tracking_url"`
}
