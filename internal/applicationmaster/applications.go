package applicationmaster

import (
	"context"
	"fmt"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// SimpleApplication 简单应用程序示例
type SimpleApplication struct {
	am             *ApplicationMaster
	logger         *zap.Logger
	totalTasks     int
	completedTasks int
}

// NewSimpleApplication 创建简单应用程序
func NewSimpleApplication(am *ApplicationMaster, totalTasks int) *SimpleApplication {
	return &SimpleApplication{
		am:         am,
		logger:     common.ComponentLogger("simple-app"),
		totalTasks: totalTasks,
	}
}

// Run 运行应用程序
func (app *SimpleApplication) Run(ctx context.Context) error {
	app.logger.Info("Starting simple application",
		zap.Int("total_tasks", app.totalTasks))

	// 第一步：请求容器
	if err := app.requestContainers(); err != nil {
		return fmt.Errorf("failed to request containers: %w", err)
	}

	// 第二步：等待容器分配并监控任务完成
	if err := app.monitorTasks(ctx); err != nil {
		return fmt.Errorf("failed to monitor tasks: %w", err)
	}

	app.logger.Info("Simple application completed successfully")
	return nil
}

// requestContainers 请求容器
func (app *SimpleApplication) requestContainers() error {
	requests := make([]*common.ContainerRequest, app.totalTasks)

	for i := 0; i < app.totalTasks; i++ {
		requests[i] = &common.ContainerRequest{
			Resource: common.Resource{
				Memory: 1024, // 1GB
				VCores: 1,    // 1 vCore
			},
			Priority: 1,
		}
	}

	app.am.RequestContainers(requests)
	app.logger.Info("Requested containers", zap.Int("count", len(requests)))

	return nil
}

// monitorTasks 监控任务执行
func (app *SimpleApplication) monitorTasks(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute) // 10分钟超时

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("application timeout")
		case <-ticker.C:
			stats := app.am.GetContainerStatistics()
			app.completedTasks = stats["completed"]

			progress := float32(app.completedTasks) / float32(app.totalTasks)
			app.logger.Info("Task progress",
				zap.Int("completed", app.completedTasks),
				zap.Int("total", app.totalTasks),
				zap.Float32("progress", progress))

			// 检查是否所有任务都已完成
			if app.completedTasks >= app.totalTasks {
				app.logger.Info("All tasks completed")
				return nil
			}

			// 检查是否有失败的容器需要重试
			if stats["failed"] > 0 {
				app.logger.Warn("Some containers failed",
					zap.Int("failed_count", stats["failed"]))
				// 这里可以实现重试逻辑
			}
		}
	}
}

// GetProgress 获取应用程序进度
func (app *SimpleApplication) GetProgress() float32 {
	if app.totalTasks == 0 {
		return 1.0
	}
	return float32(app.completedTasks) / float32(app.totalTasks)
}

// DistributedApplication 分布式应用程序示例
type DistributedApplication struct {
	am                   *ApplicationMaster
	logger               *zap.Logger
	masterTask           *Task
	workerTasks          []*Task
	coordinatorContainer *common.Container
	workerContainers     []*common.Container
}

// Task 任务定义
type Task struct {
	ID          string
	Type        string // "master", "worker"
	Command     []string
	Resource    common.Resource
	Status      string
	ContainerID *common.ContainerID
}

// NewDistributedApplication 创建分布式应用程序
func NewDistributedApplication(am *ApplicationMaster, numWorkers int) *DistributedApplication {
	app := &DistributedApplication{
		am:          am,
		logger:      common.ComponentLogger("distributed-app"),
		workerTasks: make([]*Task, numWorkers),
	}

	// 创建主任务
	app.masterTask = &Task{
		ID:   "master",
		Type: "master",
		Command: []string{
			"echo 'Starting master task'",
			"sleep 60", // 模拟长时间运行的主任务
			"echo 'Master task completed'",
		},
		Resource: common.Resource{Memory: 2048, VCores: 2},
		Status:   "PENDING",
	}

	// 创建工作任务
	for i := 0; i < numWorkers; i++ {
		app.workerTasks[i] = &Task{
			ID:   fmt.Sprintf("worker-%d", i),
			Type: "worker",
			Command: []string{
				fmt.Sprintf("echo 'Starting worker task %d'", i),
				"sleep 30", // 模拟工作任务
				fmt.Sprintf("echo 'Worker task %d completed'", i),
			},
			Resource: common.Resource{Memory: 1024, VCores: 1},
			Status:   "PENDING",
		}
	}

	return app
}

// Run 运行分布式应用程序
func (app *DistributedApplication) Run(ctx context.Context) error {
	app.logger.Info("Starting distributed application",
		zap.Int("num_workers", len(app.workerTasks)))

	// 第一阶段：启动主任务
	if err := app.startMasterTask(); err != nil {
		return fmt.Errorf("failed to start master task: %w", err)
	}

	// 第二阶段：等待主任务获得容器
	if err := app.waitForMasterContainer(ctx); err != nil {
		return fmt.Errorf("failed to get master container: %w", err)
	}

	// 第三阶段：启动工作任务
	if err := app.startWorkerTasks(); err != nil {
		return fmt.Errorf("failed to start worker tasks: %w", err)
	}

	// 第四阶段：监控所有任务
	if err := app.monitorAllTasks(ctx); err != nil {
		return fmt.Errorf("failed to monitor tasks: %w", err)
	}

	app.logger.Info("Distributed application completed successfully")
	return nil
}

// startMasterTask 启动主任务
func (app *DistributedApplication) startMasterTask() error {
	request := &common.ContainerRequest{
		Resource: app.masterTask.Resource,
		Priority: 10, // 高优先级
	}

	app.am.RequestContainers([]*common.ContainerRequest{request})
	app.masterTask.Status = "REQUESTED"

	app.logger.Info("Requested master container")
	return nil
}

// waitForMasterContainer 等待主任务容器
func (app *DistributedApplication) waitForMasterContainer(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for master container")
		case <-ticker.C:
			stats := app.am.GetContainerStatistics()
			if stats["allocated"] > 0 {
				app.masterTask.Status = "RUNNING"
				app.logger.Info("Master container allocated")
				return nil
			}
		}
	}
}

// startWorkerTasks 启动工作任务
func (app *DistributedApplication) startWorkerTasks() error {
	requests := make([]*common.ContainerRequest, len(app.workerTasks))

	for i, task := range app.workerTasks {
		requests[i] = &common.ContainerRequest{
			Resource: task.Resource,
			Priority: 5, // 普通优先级
		}
		task.Status = "REQUESTED"
	}

	app.am.RequestContainers(requests)
	app.logger.Info("Requested worker containers", zap.Int("count", len(requests)))

	return nil
}

// monitorAllTasks 监控所有任务
func (app *DistributedApplication) monitorAllTasks(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(15 * time.Minute)
	totalTasks := 1 + len(app.workerTasks) // master + workers

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("application timeout")
		case <-ticker.C:
			stats := app.am.GetContainerStatistics()
			completed := stats["completed"]

			app.logger.Info("Task progress",
				zap.Int("completed", completed),
				zap.Int("total", totalTasks),
				zap.Int("running", stats["allocated"]),
				zap.Int("failed", stats["failed"]))

			// 检查是否所有任务都已完成
			if completed >= totalTasks {
				app.logger.Info("All tasks completed")
				return nil
			}

			// 检查失败情况
			if stats["failed"] > 0 {
				app.logger.Error("Some tasks failed", zap.Int("failed_count", stats["failed"]))
				// 在实际应用中，这里可能需要实现重试或故障恢复逻辑
			}
		}
	}
}

// GetTaskStatus 获取任务状态
func (app *DistributedApplication) GetTaskStatus() map[string]interface{} {
	status := map[string]interface{}{
		"master": map[string]interface{}{
			"id":     app.masterTask.ID,
			"status": app.masterTask.Status,
			"type":   app.masterTask.Type,
		},
		"workers": make([]map[string]interface{}, len(app.workerTasks)),
	}

	workers := status["workers"].([]map[string]interface{})
	for i, task := range app.workerTasks {
		workers[i] = map[string]interface{}{
			"id":     task.ID,
			"status": task.Status,
			"type":   task.Type,
		}
	}

	return status
}
