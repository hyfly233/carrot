package scheduler

import "carrot/internal/common"

// Scheduler 调度器接口
type Scheduler interface {
	Schedule(app *ApplicationInfo) ([]*common.Container, error)
	AllocateContainers(requests []common.ContainerRequest) ([]*common.Container, error)
	SetResourceManager(rm ResourceManagerInterface)
}
