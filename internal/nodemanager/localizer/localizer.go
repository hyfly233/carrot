package localizer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// Localizer 资源本地化器
type Localizer struct {
	mu            sync.RWMutex
	localizations map[string]*LocalizationRequest
	downloadQueue chan *LocalizationRequest
	config        *LocalizerConfig
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc

	// 统计信息
	stats *LocalizationStats

	// 工作线程
	workers []*LocalizationWorker
}

// LocalizerConfig 本地化器配置
type LocalizerConfig struct {
	LocalCacheDir          string        `json:"local_cache_dir"`
	MaxConcurrentDownloads int           `json:"max_concurrent_downloads"`
	DownloadTimeout        time.Duration `json:"download_timeout"`
	RetryAttempts          int           `json:"retry_attempts"`
	RetryDelay             time.Duration `json:"retry_delay"`
	CleanupInterval        time.Duration `json:"cleanup_interval"`
	MaxCacheSize           int64         `json:"max_cache_size"`
	EnableCompression      bool          `json:"enable_compression"`
}

// LocalizationRequest 本地化请求
type LocalizationRequest struct {
	ID             string             `json:"id"`
	ContainerID    common.ContainerID `json:"container_id"`
	Resources      []*LocalResource   `json:"resources"`
	Destination    string             `json:"destination"`
	State          LocalizationState  `json:"state"`
	StartTime      time.Time          `json:"start_time"`
	CompletionTime time.Time          `json:"completion_time"`
	Progress       float64            `json:"progress"`
	Error          error              `json:"error,omitempty"`

	// 回调
	Callback LocalizationCallback `json:"-"`

	// 内部状态
	mu              sync.RWMutex
	downloadedBytes int64
	totalBytes      int64
}

func (lr *LocalizationRequest) Clone() *LocalizationRequest {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	return &LocalizationRequest{
		ID:              lr.ID,
		ContainerID:     lr.ContainerID,
		Resources:       lr.Resources,
		Destination:     lr.Destination,
		State:           lr.State,
		StartTime:       lr.StartTime,
		CompletionTime:  lr.CompletionTime,
		Progress:        lr.Progress,
		Error:           lr.Error,
		Callback:        lr.Callback,
		downloadedBytes: lr.downloadedBytes,
		totalBytes:      lr.totalBytes,
		// mu 字段会自动初始化为零值（新的锁）
	}
}

// LocalResource 本地资源
type LocalResource struct {
	URL        string             `json:"url"`
	LocalPath  string             `json:"local_path"`
	Type       ResourceType       `json:"type"`
	Size       int64              `json:"size"`
	Checksum   string             `json:"checksum"`
	Visibility ResourceVisibility `json:"visibility"`
	Timestamp  time.Time          `json:"timestamp"`

	// 下载状态
	Downloaded   bool      `json:"downloaded"`
	DownloadTime time.Time `json:"download_time"`
}

// ResourceType 资源类型
type ResourceType int

const (
	ResourceTypeFile ResourceType = iota
	ResourceTypeArchive
	ResourceTypePattern
)

// String 返回资源类型字符串
func (rt ResourceType) String() string {
	switch rt {
	case ResourceTypeFile:
		return "FILE"
	case ResourceTypeArchive:
		return "ARCHIVE"
	case ResourceTypePattern:
		return "PATTERN"
	default:
		return "UNKNOWN"
	}
}

// ResourceVisibility 资源可见性
type ResourceVisibility int

const (
	ResourceVisibilityPrivate ResourceVisibility = iota
	ResourceVisibilityApplication
	ResourceVisibilityPublic
)

// String 返回资源可见性字符串
func (rv ResourceVisibility) String() string {
	switch rv {
	case ResourceVisibilityPrivate:
		return "PRIVATE"
	case ResourceVisibilityApplication:
		return "APPLICATION"
	case ResourceVisibilityPublic:
		return "PUBLIC"
	default:
		return "UNKNOWN"
	}
}

// LocalizationState 本地化状态
type LocalizationState int

const (
	LocalizationStatePending LocalizationState = iota
	LocalizationStateDownloading
	LocalizationStateCompleted
	LocalizationStateFailed
	LocalizationStateCancelled
)

// String 返回本地化状态字符串
func (ls LocalizationState) String() string {
	switch ls {
	case LocalizationStatePending:
		return "PENDING"
	case LocalizationStateDownloading:
		return "DOWNLOADING"
	case LocalizationStateCompleted:
		return "COMPLETED"
	case LocalizationStateFailed:
		return "FAILED"
	case LocalizationStateCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// LocalizationCallback 本地化回调接口
type LocalizationCallback interface {
	OnProgress(requestID string, progress float64)
	OnCompleted(requestID string, resources []*LocalResource)
	OnFailed(requestID string, err error)
}

// LocalizationStats 本地化统计信息
type LocalizationStats struct {
	TotalRequests        int64 `json:"total_requests"`
	CompletedRequests    int64 `json:"completed_requests"`
	FailedRequests       int64 `json:"failed_requests"`
	TotalBytesDownloaded int64 `json:"total_bytes_downloaded"`
	ActiveDownloads      int   `json:"active_downloads"`
}

// LocalizationWorker 本地化工作线程
type LocalizationWorker struct {
	id        int
	localizer *Localizer
	logger    *zap.Logger
}

// NewLocalizer 创建新的本地化器
func NewLocalizer(config *LocalizerConfig) *Localizer {
	if config == nil {
		config = &LocalizerConfig{
			LocalCacheDir:          "/tmp/carrot/cache",
			MaxConcurrentDownloads: 4,
			DownloadTimeout:        30 * time.Minute,
			RetryAttempts:          3,
			RetryDelay:             5 * time.Second,
			CleanupInterval:        1 * time.Hour,
			MaxCacheSize:           10 * 1024 * 1024 * 1024, // 10GB
			EnableCompression:      true,
		}
	}

	// 确保缓存目录存在
	os.MkdirAll(config.LocalCacheDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())

	localizer := &Localizer{
		localizations: make(map[string]*LocalizationRequest),
		downloadQueue: make(chan *LocalizationRequest, 1000),
		config:        config,
		logger:        common.ComponentLogger("localizer"),
		ctx:           ctx,
		cancel:        cancel,
		stats:         &LocalizationStats{},
	}

	// 启动工作线程
	localizer.workers = make([]*LocalizationWorker, config.MaxConcurrentDownloads)
	for i := 0; i < config.MaxConcurrentDownloads; i++ {
		worker := &LocalizationWorker{
			id:        i,
			localizer: localizer,
			logger:    localizer.logger.With(zap.Int("worker_id", i)),
		}
		localizer.workers[i] = worker
		go worker.run()
	}

	// 启动清理任务
	go localizer.cleanupWorker()

	return localizer
}

// LocalizeResources 本地化资源
func (l *Localizer) LocalizeResources(containerID common.ContainerID, resources []*LocalResource, callback LocalizationCallback) (string, error) {
	requestID := l.generateRequestID(containerID)

	// 创建目标目录
	destination := filepath.Join(l.config.LocalCacheDir, requestID)
	if err := os.MkdirAll(destination, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directory: %v", err)
	}

	// 计算总大小
	var totalBytes int64
	for _, resource := range resources {
		totalBytes += resource.Size
	}

	// 创建本地化请求
	request := &LocalizationRequest{
		ID:          requestID,
		ContainerID: containerID,
		Resources:   resources,
		Destination: destination,
		State:       LocalizationStatePending,
		StartTime:   time.Now(),
		Callback:    callback,
		totalBytes:  totalBytes,
	}

	// 设置资源的本地路径
	for _, resource := range resources {
		resource.LocalPath = filepath.Join(destination, filepath.Base(resource.URL))
	}

	l.mu.Lock()
	l.localizations[requestID] = request
	l.stats.TotalRequests++
	l.mu.Unlock()

	l.logger.Info("Starting resource localization",
		zap.String("request_id", requestID),
		zap.String("container_id", l.getContainerKey(containerID)),
		zap.Int("resource_count", len(resources)),
		zap.Int64("total_bytes", totalBytes))

	// 将请求加入下载队列
	select {
	case l.downloadQueue <- request:
		return requestID, nil
	default:
		return "", fmt.Errorf("download queue is full")
	}
}

// GetLocalizationStatus 获取本地化状态
func (l *Localizer) GetLocalizationStatus(requestID string) (*LocalizationRequest, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	request, exists := l.localizations[requestID]
	if !exists {
		return nil, fmt.Errorf("localization request not found: %s", requestID)
	}

	// 返回副本
	return request.Clone(), nil
}

// CancelLocalization 取消本地化
func (l *Localizer) CancelLocalization(requestID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	request, exists := l.localizations[requestID]
	if !exists {
		return fmt.Errorf("localization request not found: %s", requestID)
	}

	if request.State == LocalizationStateCompleted {
		return fmt.Errorf("localization already completed")
	}

	request.mu.Lock()
	request.State = LocalizationStateCancelled
	request.mu.Unlock()

	l.logger.Info("Localization cancelled", zap.String("request_id", requestID))
	return nil
}

// CleanupResources 清理资源
func (l *Localizer) CleanupResources(requestID string) error {
	l.mu.Lock()
	request, exists := l.localizations[requestID]
	if exists {
		delete(l.localizations, requestID)
	}
	l.mu.Unlock()

	if !exists {
		return fmt.Errorf("localization request not found: %s", requestID)
	}

	// 删除目标目录
	if err := os.RemoveAll(request.Destination); err != nil {
		l.logger.Error("Failed to cleanup localization directory",
			zap.String("request_id", requestID),
			zap.String("destination", request.Destination),
			zap.Error(err))
		return err
	}

	l.logger.Info("Localization resources cleaned up", zap.String("request_id", requestID))
	return nil
}

// GetStats 获取统计信息
func (l *Localizer) GetStats() *LocalizationStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// 计算当前活跃下载数
	activeDownloads := 0
	for _, request := range l.localizations {
		if request.State == LocalizationStateDownloading {
			activeDownloads++
		}
	}

	stats := *l.stats
	stats.ActiveDownloads = activeDownloads
	return &stats
}

// Stop 停止本地化器
func (l *Localizer) Stop() error {
	l.logger.Info("Stopping localizer")
	l.cancel()
	close(l.downloadQueue)
	return nil
}

// generateRequestID 生成请求ID
func (l *Localizer) generateRequestID(containerID common.ContainerID) string {
	return fmt.Sprintf("loc_%d_%d_%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ApplicationAttemptID.AttemptID,
		containerID.ContainerID,
		time.Now().UnixNano())
}

// getContainerKey 获取容器键
func (l *Localizer) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ApplicationAttemptID.AttemptID,
		containerID.ContainerID)
}

// cleanupWorker 清理工作线程
func (l *Localizer) cleanupWorker() {
	ticker := time.NewTicker(l.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.performCleanup()
		case <-l.ctx.Done():
			return
		}
	}
}

// performCleanup 执行清理
func (l *Localizer) performCleanup() {
	l.logger.Debug("Starting cache cleanup")

	// 获取缓存目录大小
	cacheSize, err := l.getCacheSize()
	if err != nil {
		l.logger.Error("Failed to get cache size", zap.Error(err))
		return
	}

	if cacheSize <= l.config.MaxCacheSize {
		return // 不需要清理
	}

	l.logger.Info("Cache size exceeded limit, starting cleanup",
		zap.Int64("cache_size", cacheSize),
		zap.Int64("max_size", l.config.MaxCacheSize))

	// 获取所有完成的本地化请求，按完成时间排序
	l.mu.RLock()
	var completedRequests []*LocalizationRequest
	for _, request := range l.localizations {
		if request.State == LocalizationStateCompleted {
			completedRequests = append(completedRequests, request)
		}
	}
	l.mu.RUnlock()

	// 按完成时间排序（最老的在前面）
	for i := 0; i < len(completedRequests)-1; i++ {
		for j := i + 1; j < len(completedRequests); j++ {
			if completedRequests[i].CompletionTime.After(completedRequests[j].CompletionTime) {
				completedRequests[i], completedRequests[j] = completedRequests[j], completedRequests[i]
			}
		}
	}

	// 删除最老的请求直到缓存大小满足要求
	for _, request := range completedRequests {
		if cacheSize <= l.config.MaxCacheSize {
			break
		}

		if err := l.CleanupResources(request.ID); err != nil {
			l.logger.Error("Failed to cleanup request",
				zap.String("request_id", request.ID),
				zap.Error(err))
			continue
		}

		// 重新计算缓存大小
		newCacheSize, err := l.getCacheSize()
		if err == nil {
			cacheSize = newCacheSize
		}
	}

	l.logger.Info("Cache cleanup completed", zap.Int64("cache_size", cacheSize))
}

// getCacheSize 获取缓存大小
func (l *Localizer) getCacheSize() (int64, error) {
	var size int64

	err := filepath.Walk(l.config.LocalCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// LocalizationWorker 方法实现

// run 运行工作线程
func (lw *LocalizationWorker) run() {
	lw.logger.Debug("Localization worker started")

	for {
		select {
		case request := <-lw.localizer.downloadQueue:
			if request == nil {
				// 通道已关闭
				return
			}
			lw.processRequest(request)

		case <-lw.localizer.ctx.Done():
			return
		}
	}
}

// processRequest 处理本地化请求
func (lw *LocalizationWorker) processRequest(request *LocalizationRequest) {
	lw.logger.Info("Processing localization request",
		zap.String("request_id", request.ID),
		zap.Int("resource_count", len(request.Resources)))

	request.mu.Lock()
	request.State = LocalizationStateDownloading
	request.mu.Unlock()

	// 下载所有资源
	var completedResources []*LocalResource
	var lastError error

	for _, resource := range request.Resources {
		// 检查是否已取消
		if request.State == LocalizationStateCancelled {
			break
		}

		lw.logger.Debug("Downloading resource",
			zap.String("request_id", request.ID),
			zap.String("url", resource.URL),
			zap.String("local_path", resource.LocalPath))

		if err := lw.downloadResource(request, resource); err != nil {
			lw.logger.Error("Failed to download resource",
				zap.String("request_id", request.ID),
				zap.String("url", resource.URL),
				zap.Error(err))
			lastError = err
			break
		}

		resource.Downloaded = true
		resource.DownloadTime = time.Now()
		completedResources = append(completedResources, resource)

		// 更新进度
		request.mu.Lock()
		request.downloadedBytes += resource.Size
		request.Progress = float64(request.downloadedBytes) / float64(request.totalBytes)
		request.mu.Unlock()

		// 通知进度
		if request.Callback != nil {
			request.Callback.OnProgress(request.ID, request.Progress)
		}
	}

	// 更新最终状态
	request.mu.Lock()
	request.CompletionTime = time.Now()
	if lastError != nil {
		request.State = LocalizationStateFailed
		request.Error = lastError
	} else if request.State != LocalizationStateCancelled {
		request.State = LocalizationStateCompleted
		lw.localizer.mu.Lock()
		lw.localizer.stats.CompletedRequests++
		lw.localizer.stats.TotalBytesDownloaded += request.downloadedBytes
		lw.localizer.mu.Unlock()
	}
	request.mu.Unlock()

	// 通知回调
	if request.Callback != nil {
		if lastError != nil {
			request.Callback.OnFailed(request.ID, lastError)
			lw.localizer.mu.Lock()
			lw.localizer.stats.FailedRequests++
			lw.localizer.mu.Unlock()
		} else if request.State == LocalizationStateCompleted {
			request.Callback.OnCompleted(request.ID, completedResources)
		}
	}

	lw.logger.Info("Localization request completed",
		zap.String("request_id", request.ID),
		zap.String("state", request.State.String()),
		zap.Float64("progress", request.Progress))
}

// downloadResource 下载资源
func (lw *LocalizationWorker) downloadResource(request *LocalizationRequest, resource *LocalResource) error {
	// 检查资源是否已存在且有效
	if lw.isResourceValid(resource) {
		lw.logger.Debug("Resource already exists and is valid",
			zap.String("local_path", resource.LocalPath))
		return nil
	}

	// 重试机制
	var lastError error
	for attempt := 0; attempt < lw.localizer.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(lw.localizer.config.RetryDelay)
			lw.logger.Debug("Retrying download",
				zap.String("url", resource.URL),
				zap.Int("attempt", attempt+1))
		}

		if err := lw.downloadFile(resource.URL, resource.LocalPath); err != nil {
			lastError = err
			continue
		}

		// 验证下载的文件
		if lw.isResourceValid(resource) {
			return nil
		}

		lastError = fmt.Errorf("downloaded file validation failed")
	}

	return fmt.Errorf("failed to download resource after %d attempts: %v",
		lw.localizer.config.RetryAttempts, lastError)
}

// downloadFile 下载文件
func (lw *LocalizationWorker) downloadFile(url, localPath string) error {
	// 创建 HTTP 客户端
	client := &http.Client{
		Timeout: lw.localizer.config.DownloadTimeout,
	}

	// 发送请求
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// 确保本地目录存在
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// 创建本地文件
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %v", err)
	}
	defer file.Close()

	// 复制数据
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(localPath) // 清理失败的下载
		return fmt.Errorf("failed to copy data: %v", err)
	}

	return nil
}

// isResourceValid 验证资源是否有效
func (lw *LocalizationWorker) isResourceValid(resource *LocalResource) bool {
	stat, err := os.Stat(resource.LocalPath)
	if err != nil {
		return false
	}

	// 检查文件大小
	if resource.Size > 0 && stat.Size() != resource.Size {
		return false
	}

	// TODO: 添加校验和验证
	// if resource.Checksum != "" {
	//     return lw.verifyChecksum(resource.LocalPath, resource.Checksum)
	// }

	return true
}
