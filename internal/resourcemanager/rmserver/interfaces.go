package rmserver

import (
	"carrot/internal/common"
)

// ResourceManagerInterface 定义 ResourceManager 接口
type ResourceManagerInterface interface {
	GetApplications() []*common.ApplicationReport
	SubmitApplication(ctx common.ApplicationSubmissionContext) (*common.ApplicationID, error)
	GetNodes() []*common.NodeReport
	RegisterNode(nodeID common.NodeID, resource common.Resource, httpAddress string) error
	NodeHeartbeat(nodeID common.NodeID, usedResource common.Resource, containers []*common.Container) error
	GetClusterTimestamp() int64
	GetNodeHealthStatus() map[string]int
}
