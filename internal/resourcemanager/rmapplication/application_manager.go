package rmapplication

import (
	"context"
	"fmt"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ApplicationManager 应用程序管理器
type ApplicationManager struct {
	mu               sync.RWMutex
	applications     map[string]*Application
	completedApps    map[string]*Application
	appIDCounter     int32
	clusterTimestamp int64
	maxCompletedApps int
	logger           *zap.Logger
	ctx              context.Context
	cancel           context.CancelFunc

	// 事件通道
	appEvents chan *ApplicationEvent

	// 配置
	config *ApplicationManagerConfig
}

// ApplicationManagerConfig 应用程序管理器配置
type ApplicationManagerConfig struct {
	MaxCompletedApps       int           `json:"max_completed_apps"`
	AppCleanupInterval     time.Duration `json:"app_cleanup_interval"`
	MaxApplicationAttempts int           `json:"max_application_attempts"`
	EnableRecovery         bool          `json:"enable_recovery"`
}

// ApplicationEvent 应用程序事件
type ApplicationEvent struct {
	Type          string                 `json:"type"`
	ApplicationID common.ApplicationID   `json:"application_id"`
	Timestamp     time.Time              `json:"timestamp"`
	Data          map[string]interface{} `json:"data"`
}

// ApplicationEventType 应用程序事件类型
const (
	AppEventSubmitted     = "APP_SUBMITTED"
	AppEventAccepted      = "APP_ACCEPTED"
	AppEventRunning       = "APP_RUNNING"
	AppEventFinished      = "APP_FINISHED"
	AppEventFailed        = "APP_FAILED"
	AppEventKilled        = "APP_KILLED"
	AppEventAttemptKilled = "APP_ATTEMPT_KILLED"
)

// NewApplicationManager 创建新的应用程序管理器
func NewApplicationManager(config *ApplicationManagerConfig) *ApplicationManager {
	if config == nil {
		config = &ApplicationManagerConfig{
			MaxCompletedApps:       1000,
			AppCleanupInterval:     1 * time.Hour,
			MaxApplicationAttempts: 2,
			EnableRecovery:         true,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	am := &ApplicationManager{
		applications:     make(map[string]*Application),
		completedApps:    make(map[string]*Application),
		appIDCounter:     1,
		clusterTimestamp: time.Now().Unix(),
		maxCompletedApps: config.MaxCompletedApps,
		logger:           common.ComponentLogger("application-manager"),
		ctx:              ctx,
		cancel:           cancel,
		appEvents:        make(chan *ApplicationEvent, 1000),
		config:           config,
	}

	// 启动后台任务
	go am.eventProcessor()
	go am.cleanupCompletedApps()

	return am
}

// SubmitApplication 提交应用程序
func (am *ApplicationManager) SubmitApplication(ctx common.ApplicationSubmissionContext) (*Application, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// 验证提交上下文
	if err := am.validateSubmissionContext(ctx); err != nil {
		return nil, fmt.Errorf("invalid submission context: %w", err)
	}

	// 生成应用程序 ID
	appID := common.ApplicationID{
		ClusterTimestamp: am.clusterTimestamp,
		ID:               am.appIDCounter,
	}
	am.appIDCounter++

	// 创建应用程序
	app := &Application{
		ID:              appID,
		Name:            ctx.ApplicationName,
		Type:            ctx.ApplicationType,
		User:            am.extractUser(ctx),
		Queue:           ctx.Queue,
		State:           common.ApplicationStateSubmitted,
		StartTime:       time.Now(),
		Progress:        0.0,
		Attempts:        make([]*ApplicationAttempt, 0),
		AMContainerSpec: ctx.AMContainerSpec,
		Resource:        ctx.Resource,
	}

	// 存储应用程序
	appKey := am.getApplicationKey(appID)
	am.applications[appKey] = app

	am.logger.Info("Application submitted",
		zap.String("app_id", appKey),
		zap.String("app_name", app.Name),
		zap.String("user", app.User),
		zap.String("queue", app.Queue))

	// 发送事件
	am.sendEvent(&ApplicationEvent{
		Type:          AppEventSubmitted,
		ApplicationID: appID,
		Timestamp:     time.Now(),
		Data: map[string]interface{}{
			"app_name": app.Name,
			"user":     app.User,
			"queue":    app.Queue,
		},
	})

	// 创建第一个应用程序尝试
	if err := am.createApplicationAttempt(app); err != nil {
		am.logger.Error("Failed to create application attempt",
			zap.String("app_id", appKey),
			zap.Error(err))
		return nil, err
	}

	return app, nil
}

// GetApplication 获取应用程序
func (am *ApplicationManager) GetApplication(appID common.ApplicationID) (*Application, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	appKey := am.getApplicationKey(appID)
	if app, exists := am.applications[appKey]; exists {
		return app, nil
	}

	if app, exists := am.completedApps[appKey]; exists {
		return app, nil
	}

	return nil, fmt.Errorf("application %s not found", appKey)
}

// GetApplications 获取所有应用程序
func (am *ApplicationManager) GetApplications() []*Application {
	am.mu.RLock()
	defer am.mu.RUnlock()

	apps := make([]*Application, 0, len(am.applications)+len(am.completedApps))

	for _, app := range am.applications {
		apps = append(apps, app)
	}

	for _, app := range am.completedApps {
		apps = append(apps, app)
	}

	return apps
}

// GetRunningApplications 获取运行中的应用程序
func (am *ApplicationManager) GetRunningApplications() []*Application {
	am.mu.RLock()
	defer am.mu.RUnlock()

	apps := make([]*Application, 0)
	for _, app := range am.applications {
		if app.State == common.ApplicationStateRunning ||
			app.State == common.ApplicationStateAccepted {
			apps = append(apps, app)
		}
	}

	return apps
}

// UpdateApplicationState 更新应用程序状态
func (am *ApplicationManager) UpdateApplicationState(appID common.ApplicationID, newState string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	appKey := am.getApplicationKey(appID)
	app, exists := am.applications[appKey]
	if !exists {
		return fmt.Errorf("application %s not found", appKey)
	}

	oldState := app.State
	app.State = newState

	am.logger.Info("Application state changed",
		zap.String("app_id", appKey),
		zap.String("old_state", oldState),
		zap.String("new_state", newState))

	// 发送状态变更事件
	eventType := am.getEventTypeFromState(newState)
	am.sendEvent(&ApplicationEvent{
		Type:          eventType,
		ApplicationID: appID,
		Timestamp:     time.Now(),
		Data: map[string]interface{}{
			"old_state": oldState,
			"new_state": newState,
		},
	})

	// 如果应用程序完成，移到完成列表
	if am.isApplicationCompleted(newState) {
		app.FinishTime = time.Now()
		am.completedApps[appKey] = app
		delete(am.applications, appKey)

		am.logger.Info("Application completed",
			zap.String("app_id", appKey),
			zap.String("final_state", newState),
			zap.Duration("duration", app.FinishTime.Sub(app.StartTime)))
	}

	return nil
}

// UpdateApplicationProgress 更新应用程序进度
func (am *ApplicationManager) UpdateApplicationProgress(appID common.ApplicationID, progress float32) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	appKey := am.getApplicationKey(appID)
	app, exists := am.applications[appKey]
	if !exists {
		return fmt.Errorf("application %s not found", appKey)
	}

	app.Progress = progress

	am.logger.Debug("Application progress updated",
		zap.String("app_id", appKey),
		zap.Float32("progress", progress))

	return nil
}

// KillApplication 杀死应用程序
func (am *ApplicationManager) KillApplication(appID common.ApplicationID, reason string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	appKey := am.getApplicationKey(appID)
	app, exists := am.applications[appKey]
	if !exists {
		return fmt.Errorf("application %s not found", appKey)
	}

	if am.isApplicationCompleted(app.State) {
		return fmt.Errorf("application %s is already completed", appKey)
	}

	app.State = common.ApplicationStateKilled
	app.FinishTime = time.Now()

	am.logger.Info("Application killed",
		zap.String("app_id", appKey),
		zap.String("reason", reason))

	// 发送事件
	am.sendEvent(&ApplicationEvent{
		Type:          AppEventKilled,
		ApplicationID: appID,
		Timestamp:     time.Now(),
		Data: map[string]interface{}{
			"reason": reason,
		},
	})

	// 移到完成列表
	am.completedApps[appKey] = app
	delete(am.applications, appKey)

	return nil
}

// createApplicationAttempt 创建应用程序尝试
func (am *ApplicationManager) createApplicationAttempt(app *Application) error {
	attemptID := common.ApplicationAttemptID{
		ApplicationID: app.ID,
		AttemptID:     int32(len(app.Attempts) + 1),
	}

	if len(app.Attempts) >= am.config.MaxApplicationAttempts {
		return fmt.Errorf("maximum application attempts (%d) exceeded", am.config.MaxApplicationAttempts)
	}

	attempt := &ApplicationAttempt{
		ID:          attemptID,
		State:       common.ApplicationStateNew,
		StartTime:   time.Now(),
		AMContainer: nil,
		TrackingURL: "",
	}

	app.Attempts = append(app.Attempts, attempt)
	app.State = common.ApplicationStateAccepted

	am.logger.Info("Application attempt created",
		zap.String("app_id", am.getApplicationKey(app.ID)),
		zap.Int32("attempt_id", attemptID.AttemptID))

	return nil
}

// GetCurrentApplicationAttempt 获取当前应用程序尝试
func (am *ApplicationManager) GetCurrentApplicationAttempt(appID common.ApplicationID) (*ApplicationAttempt, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	appKey := am.getApplicationKey(appID)
	app, exists := am.applications[appKey]
	if !exists {
		if app, exists = am.completedApps[appKey]; !exists {
			return nil, fmt.Errorf("application %s not found", appKey)
		}
	}

	if len(app.Attempts) == 0 {
		return nil, fmt.Errorf("no attempts found for application %s", appKey)
	}

	return app.Attempts[len(app.Attempts)-1], nil
}

// validateSubmissionContext 验证应用程序提交上下文
func (am *ApplicationManager) validateSubmissionContext(ctx common.ApplicationSubmissionContext) error {
	if ctx.ApplicationName == "" {
		return fmt.Errorf("application name is required")
	}

	if ctx.ApplicationType == "" {
		return fmt.Errorf("application type is required")
	}

	if ctx.Queue == "" {
		return fmt.Errorf("queue is required")
	}

	if ctx.Resource.Memory <= 0 {
		return fmt.Errorf("memory must be positive")
	}

	if ctx.Resource.VCores <= 0 {
		return fmt.Errorf("vcores must be positive")
	}

	return nil
}

// extractUser 从提交上下文中提取用户信息
func (am *ApplicationManager) extractUser(ctx common.ApplicationSubmissionContext) string {
	// 这里可以从安全上下文中获取真实用户
	// 暂时使用默认用户
	return "default-user"
}

// getApplicationKey 获取应用程序键
func (am *ApplicationManager) getApplicationKey(appID common.ApplicationID) string {
	return fmt.Sprintf("%d_%d", appID.ClusterTimestamp, appID.ID)
}

// getEventTypeFromState 从状态获取事件类型
func (am *ApplicationManager) getEventTypeFromState(state string) string {
	switch state {
	case common.ApplicationStateAccepted:
		return AppEventAccepted
	case common.ApplicationStateRunning:
		return AppEventRunning
	case common.ApplicationStateFinished:
		return AppEventFinished
	case common.ApplicationStateFailed:
		return AppEventFailed
	case common.ApplicationStateKilled:
		return AppEventKilled
	default:
		return "UNKNOWN"
	}
}

// isApplicationCompleted 检查应用程序是否完成
func (am *ApplicationManager) isApplicationCompleted(state string) bool {
	return state == common.ApplicationStateFinished ||
		state == common.ApplicationStateFailed ||
		state == common.ApplicationStateKilled
}

// sendEvent 发送事件
func (am *ApplicationManager) sendEvent(event *ApplicationEvent) {
	select {
	case am.appEvents <- event:
	default:
		am.logger.Warn("Event channel full, dropping event",
			zap.String("event_type", event.Type),
			zap.Any("app_id", event.ApplicationID))
	}
}

// eventProcessor 事件处理器
func (am *ApplicationManager) eventProcessor() {
	defer func() {
		if r := recover(); r != nil {
			am.logger.Error("Event processor panic recovered", zap.Any("error", r))
		}
	}()

	for {
		select {
		case event, ok := <-am.appEvents:
			if !ok {
				// 通道已关闭
				return
			}
			am.logger.Debug("Processing application event",
				zap.String("event_type", event.Type),
				zap.Any("app_id", event.ApplicationID))
			// 这里可以添加事件处理逻辑，比如发送到监控系统
		case <-am.ctx.Done():
			return
		}
	}
}

// cleanupCompletedApps 清理完成的应用程序
func (am *ApplicationManager) cleanupCompletedApps() {
	ticker := time.NewTicker(am.config.AppCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			am.performCleanup()
		case <-am.ctx.Done():
			return
		}
	}
}

// performCleanup 执行清理
func (am *ApplicationManager) performCleanup() {
	am.mu.Lock()
	defer am.mu.Unlock()

	if len(am.completedApps) <= am.maxCompletedApps {
		return
	}

	// 按完成时间排序，删除最老的应用程序
	type appWithTime struct {
		key        string
		finishTime time.Time
	}

	apps := make([]appWithTime, 0, len(am.completedApps))
	for key, app := range am.completedApps {
		apps = append(apps, appWithTime{
			key:        key,
			finishTime: app.FinishTime,
		})
	}

	// 简单排序（按时间升序）
	for i := 0; i < len(apps)-1; i++ {
		for j := i + 1; j < len(apps); j++ {
			if apps[i].finishTime.After(apps[j].finishTime) {
				apps[i], apps[j] = apps[j], apps[i]
			}
		}
	}

	// 删除最老的应用程序
	numToDelete := len(am.completedApps) - am.maxCompletedApps
	for i := 0; i < numToDelete; i++ {
		delete(am.completedApps, apps[i].key)
	}

	am.logger.Info("Cleaned up completed applications",
		zap.Int("deleted", numToDelete),
		zap.Int("remaining", len(am.completedApps)))
}

// GetStatistics 获取统计信息
func (am *ApplicationManager) GetStatistics() map[string]interface{} {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := make(map[string]int)
	for _, app := range am.applications {
		stats[app.State]++
	}

	for _, app := range am.completedApps {
		stats[app.State]++
	}

	return map[string]interface{}{
		"total_applications":     len(am.applications) + len(am.completedApps),
		"running_applications":   len(am.applications),
		"completed_applications": len(am.completedApps),
		"by_state":               stats,
		"app_id_counter":         am.appIDCounter,
	}
}

// Stop 停止应用程序管理器
func (am *ApplicationManager) Stop() error {
	am.logger.Info("Stopping application manager")
	// 先取消 context，让 goroutines 退出
	am.cancel()
	// 然后关闭通道
	close(am.appEvents)
	return nil
}
