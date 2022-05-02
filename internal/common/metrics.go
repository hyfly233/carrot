package common

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// Metrics 系统指标
type Metrics struct {
	mu sync.RWMutex

	// 系统指标
	StartTime    time.Time                `json:"start_time"`
	RequestCount map[string]int64         `json:"request_count"`
	ResponseTime map[string]time.Duration `json:"response_time"`
	ErrorCount   map[string]int64         `json:"error_count"`

	// 资源指标
	TotalApplications   int64 `json:"total_applications"`
	RunningApplications int64 `json:"running_applications"`
	TotalNodes          int64 `json:"total_nodes"`
	ActiveNodes         int64 `json:"active_nodes"`

	// 容器指标
	TotalContainers   int64 `json:"total_containers"`
	RunningContainers int64 `json:"running_containers"`

	// 资源使用指标
	TotalMemoryMB int64 `json:"total_memory_mb"`
	UsedMemoryMB  int64 `json:"used_memory_mb"`
	TotalVCores   int32 `json:"total_vcores"`
	UsedVCores    int32 `json:"used_vcores"`
}

var globalMetrics = &Metrics{
	StartTime:    time.Now(),
	RequestCount: make(map[string]int64),
	ResponseTime: make(map[string]time.Duration),
	ErrorCount:   make(map[string]int64),
}

// GetMetrics 获取全局指标实例
func GetMetrics() *Metrics {
	return globalMetrics
}

// IncrementRequestCount 增加请求计数
func (m *Metrics) IncrementRequestCount(endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequestCount[endpoint]++
}

// RecordResponseTime 记录响应时间
func (m *Metrics) RecordResponseTime(endpoint string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseTime[endpoint] = duration
}

// IncrementErrorCount 增加错误计数
func (m *Metrics) IncrementErrorCount(endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorCount[endpoint]++
}

// UpdateApplicationMetrics 更新应用程序指标
func (m *Metrics) UpdateApplicationMetrics(total, running int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalApplications = total
	m.RunningApplications = running
}

// UpdateNodeMetrics 更新节点指标
func (m *Metrics) UpdateNodeMetrics(total, active int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalNodes = total
	m.ActiveNodes = active
}

// UpdateContainerMetrics 更新容器指标
func (m *Metrics) UpdateContainerMetrics(total, running int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalContainers = total
	m.RunningContainers = running
}

// UpdateResourceMetrics 更新资源使用指标
func (m *Metrics) UpdateResourceMetrics(totalMem, usedMem int64, totalCores, usedCores int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalMemoryMB = totalMem
	m.UsedMemoryMB = usedMem
	m.TotalVCores = totalCores
	m.UsedVCores = usedCores
}

// GetSnapshot 获取指标快照
func (m *Metrics) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 获取系统内存统计
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return map[string]interface{}{
		"uptime_seconds":       time.Since(m.StartTime).Seconds(),
		"request_count":        m.RequestCount,
		"response_time_ms":     convertDurationToMs(m.ResponseTime),
		"error_count":          m.ErrorCount,
		"total_applications":   m.TotalApplications,
		"running_applications": m.RunningApplications,
		"total_nodes":          m.TotalNodes,
		"active_nodes":         m.ActiveNodes,
		"total_containers":     m.TotalContainers,
		"running_containers":   m.RunningContainers,
		"total_memory_mb":      m.TotalMemoryMB,
		"used_memory_mb":       m.UsedMemoryMB,
		"total_vcores":         m.TotalVCores,
		"used_vcores":          m.UsedVCores,
		"system_memory_mb":     int64(memStats.Sys / 1024 / 1024),
		"heap_memory_mb":       int64(memStats.HeapInuse / 1024 / 1024),
		"goroutines":           runtime.NumGoroutine(),
	}
}

// convertDurationToMs 将时间持续转换为毫秒
func convertDurationToMs(durations map[string]time.Duration) map[string]float64 {
	result := make(map[string]float64)
	for k, v := range durations {
		result[k] = float64(v.Nanoseconds()) / 1e6
	}
	return result
}

// MetricsCollector 指标收集器
type MetricsCollector struct {
	metrics  *Metrics
	stopChan chan struct{}
	interval time.Duration
}

// NewMetricsCollector 创建新的指标收集器
func NewMetricsCollector(interval time.Duration) *MetricsCollector {
	return &MetricsCollector{
		metrics:  GetMetrics(),
		stopChan: make(chan struct{}),
		interval: interval,
	}
}

// Start 启动指标收集
func (mc *MetricsCollector) Start(ctx context.Context) {
	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-mc.stopChan:
			return
		case <-ticker.C:
			// 这里可以添加定期收集指标的逻辑
			// 例如：清理过期的指标数据
		}
	}
}

// Stop 停止指标收集
func (mc *MetricsCollector) Stop() {
	close(mc.stopChan)
}
