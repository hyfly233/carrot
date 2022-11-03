package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// SystemMonitor 系统监控器
type SystemMonitor struct {
	mu      sync.RWMutex
	metrics *SystemMetrics
	config  *SystemMonitorConfig
	logger  *zap.Logger
	ctx     context.Context
	cancel  context.CancelFunc

	// 监控状态
	isRunning bool

	// 事件通道
	eventChan chan *SystemEvent

	// 历史数据
	history    []*SystemMetrics
	maxHistory int
}

// SystemMonitorConfig 系统监控器配置
type SystemMonitorConfig struct {
	CollectionInterval  time.Duration     `json:"collection_interval"`
	HistoryRetention    int               `json:"history_retention"`
	AlertThresholds     *SystemThresholds `json:"alert_thresholds"`
	EnableDetailedStats bool              `json:"enable_detailed_stats"`
	ProcPath            string            `json:"proc_path"`
	SysPath             string            `json:"sys_path"`
}

// SystemMetrics 系统指标
type SystemMetrics struct {
	Timestamp time.Time `json:"timestamp"`

	// CPU 指标
	CPUUsage *CPUMetrics `json:"cpu_usage"`

	// 内存指标
	MemoryUsage *MemoryMetrics `json:"memory_usage"`

	// 磁盘指标
	DiskUsage map[string]*DiskMetrics `json:"disk_usage"`

	// 网络指标
	NetworkUsage map[string]*NetworkMetrics `json:"network_usage"`

	// 负载指标
	LoadAverage *LoadMetrics `json:"load_average"`

	// 进程指标
	ProcessStats *ProcessMetrics `json:"process_stats"`

	// 系统信息
	SystemInfo *SystemInfo `json:"system_info"`
}

// CPUMetrics CPU 指标
type CPUMetrics struct {
	UsagePercent   float64 `json:"usage_percent"`
	UserPercent    float64 `json:"user_percent"`
	SystemPercent  float64 `json:"system_percent"`
	IdlePercent    float64 `json:"idle_percent"`
	IowaitPercent  float64 `json:"iowait_percent"`
	IRQPercent     float64 `json:"irq_percent"`
	SoftIRQPercent float64 `json:"softirq_percent"`
	StealPercent   float64 `json:"steal_percent"`
	GuestPercent   float64 `json:"guest_percent"`

	// 每个CPU核心的使用率
	PerCPUUsage []float64 `json:"per_cpu_usage"`

	// 容器监控所需的字段
	UserTime   int64 `json:"user_time"`
	SystemTime int64 `json:"system_time"`
	Throttled  int64 `json:"throttled"`
}

// MemoryMetrics 内存指标
type MemoryMetrics struct {
	Total            int64   `json:"total"`
	Available        int64   `json:"available"`
	Used             int64   `json:"used"`
	UsagePercent     float64 `json:"usage_percent"`
	Free             int64   `json:"free"`
	Buffers          int64   `json:"buffers"`
	Cached           int64   `json:"cached"`
	SwapTotal        int64   `json:"swap_total"`
	SwapUsed         int64   `json:"swap_used"`
	SwapFree         int64   `json:"swap_free"`
	SwapUsagePercent float64 `json:"swap_usage_percent"`

	// 详细内存信息
	Active      int64 `json:"active"`
	Inactive    int64 `json:"inactive"`
	Shared      int64 `json:"shared"`
	Slab        int64 `json:"slab"`
	PageTables  int64 `json:"page_tables"`
	VmallocUsed int64 `json:"vmalloc_used"`

	// 容器监控所需的字段
	Usage int64 `json:"usage"`
	Limit int64 `json:"limit"`
	Cache int64 `json:"cache"`
	Swap  int64 `json:"swap"`
}

// DiskMetrics 磁盘指标
type DiskMetrics struct {
	Device       string  `json:"device"`
	MountPoint   string  `json:"mount_point"`
	Total        int64   `json:"total"`
	Used         int64   `json:"used"`
	Free         int64   `json:"free"`
	UsagePercent float64 `json:"usage_percent"`

	// IO 统计
	ReadBytes  int64 `json:"read_bytes"`
	WriteBytes int64 `json:"write_bytes"`
	ReadOps    int64 `json:"read_ops"`
	WriteOps   int64 `json:"write_ops"`
	ReadTime   int64 `json:"read_time"`
	WriteTime  int64 `json:"write_time"`
	IOTime     int64 `json:"io_time"`

	// Inode 信息
	InodesTotal        int64   `json:"inodes_total"`
	InodesUsed         int64   `json:"inodes_used"`
	InodesFree         int64   `json:"inodes_free"`
	InodesUsagePercent float64 `json:"inodes_usage_percent"`
}

// NetworkMetrics 网络指标
type NetworkMetrics struct {
	Interface string `json:"interface"`
	RxBytes   int64  `json:"rx_bytes"`
	TxBytes   int64  `json:"tx_bytes"`
	RxPackets int64  `json:"rx_packets"`
	TxPackets int64  `json:"tx_packets"`
	RxErrors  int64  `json:"rx_errors"`
	TxErrors  int64  `json:"tx_errors"`
	RxDropped int64  `json:"rx_dropped"`
	TxDropped int64  `json:"tx_dropped"`

	// 速率统计
	RxBytesPerSec   float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec   float64 `json:"tx_bytes_per_sec"`
	RxPacketsPerSec float64 `json:"rx_packets_per_sec"`
	TxPacketsPerSec float64 `json:"tx_packets_per_sec"`
}

// LoadMetrics 负载指标
type LoadMetrics struct {
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
	RunningProcs int     `json:"running_procs"`
	TotalProcs   int     `json:"total_procs"`
}

// ProcessMetrics 进程指标
type ProcessMetrics struct {
	TotalProcesses    int `json:"total_processes"`
	RunningProcesses  int `json:"running_processes"`
	SleepingProcesses int `json:"sleeping_processes"`
	ZombieProcesses   int `json:"zombie_processes"`
	StoppedProcesses  int `json:"stopped_processes"`

	// 容器监控所需的字段
	NumThreads int `json:"num_threads"`
	NumFDs     int `json:"num_fds"`
}

// SystemInfo 系统信息
type SystemInfo struct {
	Hostname      string `json:"hostname"`
	Uptime        int64  `json:"uptime"`
	BootTime      int64  `json:"boot_time"`
	OSVersion     string `json:"os_version"`
	KernelVersion string `json:"kernel_version"`
	Architecture  string `json:"architecture"`
	CPUCount      int    `json:"cpu_count"`
}

// SystemThresholds 系统告警阈值
type SystemThresholds struct {
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	DiskUsagePercent   float64 `json:"disk_usage_percent"`
	SwapUsagePercent   float64 `json:"swap_usage_percent"`
	Load1Threshold     float64 `json:"load1_threshold"`
	Load5Threshold     float64 `json:"load5_threshold"`
	Load15Threshold    float64 `json:"load15_threshold"`
	InodeUsagePercent  float64 `json:"inode_usage_percent"`
}

// SystemEvent 系统事件
type SystemEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
}

// SystemEventType 系统事件类型
const (
	EventTypeSystemMetrics   = "SYSTEM_METRICS"
	EventTypeSystemAlert     = "SYSTEM_ALERT"
	EventTypeHighCPUUsage    = "HIGH_CPU_USAGE"
	EventTypeHighMemoryUsage = "HIGH_MEMORY_USAGE"
	EventTypeHighDiskUsage   = "HIGH_DISK_USAGE"
	EventTypeHighLoad        = "HIGH_LOAD"
	EventTypeSystemHealthy   = "SYSTEM_HEALTHY"
)

// ResourceCollector 资源收集器
type ResourceCollector struct {
	config   *ContainerMonitorConfig
	logger   *zap.Logger
	lastCPU  map[string]*CPUStats
	lastNet  map[string]*NetworkStats
	lastDisk map[string]*DiskStats
	mu       sync.RWMutex
}

// HealthChecker 健康检查器
type HealthChecker struct {
	config *ContainerMonitorConfig
	logger *zap.Logger
}

// CPUStats CPU 统计
type CPUStats struct {
	User   float64
	System float64
	Idle   float64
	Total  float64
}

// NetworkStats 网络统计
type NetworkStats struct {
	RxBytes   int64
	TxBytes   int64
	RxPackets int64
	TxPackets int64
	RxErrors  int64
	TxErrors  int64
}

// DiskStats 磁盘统计
type DiskStats struct {
	ReadBytes  int64
	WriteBytes int64
	ReadOps    int64
	WriteOps   int64
}

// NewSystemMonitor 创建系统监控器
func NewSystemMonitor(config *SystemMonitorConfig) *SystemMonitor {
	if config == nil {
		config = &SystemMonitorConfig{
			CollectionInterval:  30 * time.Second,
			HistoryRetention:    100,
			EnableDetailedStats: true,
			ProcPath:            "/proc",
			SysPath:             "/sys",
			AlertThresholds: &SystemThresholds{
				CPUUsagePercent:    80.0,
				MemoryUsagePercent: 85.0,
				DiskUsagePercent:   90.0,
				SwapUsagePercent:   50.0,
				Load1Threshold:     8.0,
				Load5Threshold:     6.0,
				Load15Threshold:    4.0,
				InodeUsagePercent:  90.0,
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	sm := &SystemMonitor{
		config:     config,
		logger:     zap.NewNop(), // 在实际使用中应该注入正确的logger
		ctx:        ctx,
		cancel:     cancel,
		eventChan:  make(chan *SystemEvent, 1000),
		history:    make([]*SystemMetrics, 0, config.HistoryRetention),
		maxHistory: config.HistoryRetention,
	}

	return sm
}

// Start 启动系统监控器
func (sm *SystemMonitor) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.isRunning {
		return fmt.Errorf("system monitor is already running")
	}

	sm.isRunning = true
	sm.logger.Info("Starting system monitor")

	// 启动监控任务
	go sm.monitoringWorker()

	return nil
}

// Stop 停止系统监控器
func (sm *SystemMonitor) Stop() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.isRunning {
		return nil
	}

	sm.logger.Info("Stopping system monitor")
	sm.isRunning = false
	sm.cancel()
	close(sm.eventChan)

	return nil
}

// GetCurrentMetrics 获取当前指标
func (sm *SystemMonitor) GetCurrentMetrics() *SystemMetrics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.metrics == nil {
		return nil
	}

	// 返回副本
	return sm.copyMetrics(sm.metrics)
}

// GetHistoryMetrics 获取历史指标
func (sm *SystemMonitor) GetHistoryMetrics(limit int) []*SystemMetrics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if limit <= 0 || limit > len(sm.history) {
		limit = len(sm.history)
	}

	result := make([]*SystemMetrics, limit)
	start := len(sm.history) - limit

	for i := 0; i < limit; i++ {
		result[i] = sm.copyMetrics(sm.history[start+i])
	}

	return result
}

// GetEvents 获取事件通道
func (sm *SystemMonitor) GetEvents() <-chan *SystemEvent {
	return sm.eventChan
}

// monitoringWorker 监控工作线程
func (sm *SystemMonitor) monitoringWorker() {
	ticker := time.NewTicker(sm.config.CollectionInterval)
	defer ticker.Stop()

	// 立即收集一次
	sm.collectMetrics()

	for {
		select {
		case <-ticker.C:
			sm.collectMetrics()
		case <-sm.ctx.Done():
			return
		}
	}
}

// collectMetrics 收集系统指标
func (sm *SystemMonitor) collectMetrics() {
	metrics := &SystemMetrics{
		Timestamp: time.Now(),
	}

	// 收集 CPU 指标
	if cpuMetrics, err := sm.collectCPUMetrics(); err == nil {
		metrics.CPUUsage = cpuMetrics
	} else {
		sm.logger.Error("Failed to collect CPU metrics", zap.Error(err))
	}

	// 收集内存指标
	if memMetrics, err := sm.collectMemoryMetrics(); err == nil {
		metrics.MemoryUsage = memMetrics
	} else {
		sm.logger.Error("Failed to collect memory metrics", zap.Error(err))
	}

	// 收集磁盘指标
	if diskMetrics, err := sm.collectDiskMetrics(); err == nil {
		metrics.DiskUsage = diskMetrics
	} else {
		sm.logger.Error("Failed to collect disk metrics", zap.Error(err))
	}

	// 收集网络指标
	if netMetrics, err := sm.collectNetworkMetrics(); err == nil {
		metrics.NetworkUsage = netMetrics
	} else {
		sm.logger.Error("Failed to collect network metrics", zap.Error(err))
	}

	// 收集负载指标
	if loadMetrics, err := sm.collectLoadMetrics(); err == nil {
		metrics.LoadAverage = loadMetrics
	} else {
		sm.logger.Error("Failed to collect load metrics", zap.Error(err))
	}

	// 收集进程指标
	if procMetrics, err := sm.collectProcessMetrics(); err == nil {
		metrics.ProcessStats = procMetrics
	} else {
		sm.logger.Error("Failed to collect process metrics", zap.Error(err))
	}

	// 收集系统信息
	if sysInfo, err := sm.collectSystemInfo(); err == nil {
		metrics.SystemInfo = sysInfo
	} else {
		sm.logger.Error("Failed to collect system info", zap.Error(err))
	}

	// 更新当前指标
	sm.mu.Lock()
	sm.metrics = metrics

	// 添加到历史记录
	sm.history = append(sm.history, sm.copyMetrics(metrics))
	if len(sm.history) > sm.maxHistory {
		sm.history = sm.history[1:]
	}
	sm.mu.Unlock()

	// 检查告警阈值
	sm.checkSystemThresholds(metrics)

	// 发送指标事件
	sm.sendEvent(&SystemEvent{
		Type:      EventTypeSystemMetrics,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"cpu_usage":    metrics.CPUUsage.UsagePercent,
			"memory_usage": metrics.MemoryUsage.UsagePercent,
		},
	})
}

// collectCPUMetrics 收集 CPU 指标
func (sm *SystemMonitor) collectCPUMetrics() (*CPUMetrics, error) {
	statFile := filepath.Join(sm.config.ProcPath, "stat")
	data, err := os.ReadFile(statFile)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty stat file")
	}

	// 解析总的 CPU 统计
	fields := strings.Fields(lines[0])
	if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") {
		return nil, fmt.Errorf("invalid cpu stat line")
	}

	var values []int64
	for i := 1; i < 8; i++ {
		val, err := strconv.ParseInt(fields[i], 10, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, val)
	}

	// 计算各项指标
	user := float64(values[0])
	nice := float64(values[1])
	system := float64(values[2])
	idle := float64(values[3])
	iowait := float64(values[4])
	irq := float64(values[5])
	softirq := float64(values[6])

	total := user + nice + system + idle + iowait + irq + softirq

	if total == 0 {
		return nil, fmt.Errorf("total CPU time is zero")
	}

	metrics := &CPUMetrics{
		UserPercent:    (user + nice) / total * 100,
		SystemPercent:  system / total * 100,
		IdlePercent:    idle / total * 100,
		IowaitPercent:  iowait / total * 100,
		IRQPercent:     irq / total * 100,
		SoftIRQPercent: softirq / total * 100,
	}

	metrics.UsagePercent = 100 - metrics.IdlePercent

	// 收集每个 CPU 核心的使用率
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "cpu") {
			break
		}

		cpuFields := strings.Fields(line)
		if len(cpuFields) < 8 {
			continue
		}

		var cpuValues []int64
		for j := 1; j < 8; j++ {
			val, err := strconv.ParseInt(cpuFields[j], 10, 64)
			if err != nil {
				continue
			}
			cpuValues = append(cpuValues, val)
		}

		if len(cpuValues) >= 7 {
			cpuUser := float64(cpuValues[0])
			cpuNice := float64(cpuValues[1])
			cpuSystem := float64(cpuValues[2])
			cpuIdle := float64(cpuValues[3])
			cpuIowait := float64(cpuValues[4])
			cpuIrq := float64(cpuValues[5])
			cpuSoftirq := float64(cpuValues[6])

			cpuTotal := cpuUser + cpuNice + cpuSystem + cpuIdle + cpuIowait + cpuIrq + cpuSoftirq
			if cpuTotal > 0 {
				cpuUsage := 100 - (cpuIdle/cpuTotal)*100
				metrics.PerCPUUsage = append(metrics.PerCPUUsage, cpuUsage)
			}
		}
	}

	return metrics, nil
}

// collectMemoryMetrics 收集内存指标
func (sm *SystemMonitor) collectMemoryMetrics() (*MemoryMetrics, error) {
	meminfoFile := filepath.Join(sm.config.ProcPath, "meminfo")
	data, err := os.ReadFile(meminfoFile)
	if err != nil {
		return nil, err
	}

	meminfo := make(map[string]int64)
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			value, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				meminfo[key] = value * 1024 // 转换为字节
			}
		}
	}

	metrics := &MemoryMetrics{
		Total:     meminfo["MemTotal"],
		Free:      meminfo["MemFree"],
		Available: meminfo["MemAvailable"],
		Buffers:   meminfo["Buffers"],
		Cached:    meminfo["Cached"],
		SwapTotal: meminfo["SwapTotal"],
		SwapFree:  meminfo["SwapFree"],
		Active:    meminfo["Active"],
		Inactive:  meminfo["Inactive"],
		Shared:    meminfo["Shmem"],
		Slab:      meminfo["Slab"],
	}

	if metrics.Available == 0 {
		metrics.Available = metrics.Free + metrics.Buffers + metrics.Cached
	}

	metrics.Used = metrics.Total - metrics.Available
	if metrics.Total > 0 {
		metrics.UsagePercent = float64(metrics.Used) / float64(metrics.Total) * 100
	}

	metrics.SwapUsed = metrics.SwapTotal - metrics.SwapFree
	if metrics.SwapTotal > 0 {
		metrics.SwapUsagePercent = float64(metrics.SwapUsed) / float64(metrics.SwapTotal) * 100
	}

	return metrics, nil
}

// collectDiskMetrics 收集磁盘指标
func (sm *SystemMonitor) collectDiskMetrics() (map[string]*DiskMetrics, error) {
	diskMetrics := make(map[string]*DiskMetrics)

	// 读取挂载信息
	mountsFile := filepath.Join(sm.config.ProcPath, "mounts")
	data, err := os.ReadFile(mountsFile)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		// 只监控实际的磁盘设备
		if !strings.HasPrefix(device, "/dev/") ||
			fsType == "proc" || fsType == "sysfs" || fsType == "tmpfs" {
			continue
		}

		// 获取磁盘使用统计
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		blockSize := int64(stat.Bsize)
		total := int64(stat.Blocks) * blockSize
		free := int64(stat.Bavail) * blockSize
		used := total - free

		var usagePercent float64
		if total > 0 {
			usagePercent = float64(used) / float64(total) * 100
		}

		metrics := &DiskMetrics{
			Device:       device,
			MountPoint:   mountPoint,
			Total:        total,
			Used:         used,
			Free:         free,
			UsagePercent: usagePercent,
			InodesTotal:  int64(stat.Files),
			InodesFree:   int64(stat.Ffree),
		}

		metrics.InodesUsed = metrics.InodesTotal - metrics.InodesFree
		if metrics.InodesTotal > 0 {
			metrics.InodesUsagePercent = float64(metrics.InodesUsed) / float64(metrics.InodesTotal) * 100
		}

		diskMetrics[device] = metrics
	}

	return diskMetrics, nil
}

// collectNetworkMetrics 收集网络指标
func (sm *SystemMonitor) collectNetworkMetrics() (map[string]*NetworkMetrics, error) {
	netDevFile := filepath.Join(sm.config.ProcPath, "net/dev")
	data, err := os.ReadFile(netDevFile)
	if err != nil {
		return nil, err
	}

	networkMetrics := make(map[string]*NetworkMetrics)
	lines := strings.Split(string(data), "\n")

	for i, line := range lines {
		if i < 2 || line == "" { // 跳过头部
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		iface := strings.TrimSpace(parts[0])
		stats := strings.Fields(strings.TrimSpace(parts[1]))

		if len(stats) < 16 {
			continue
		}

		rxBytes, _ := strconv.ParseInt(stats[0], 10, 64)
		rxPackets, _ := strconv.ParseInt(stats[1], 10, 64)
		rxErrors, _ := strconv.ParseInt(stats[2], 10, 64)
		rxDropped, _ := strconv.ParseInt(stats[3], 10, 64)
		txBytes, _ := strconv.ParseInt(stats[8], 10, 64)
		txPackets, _ := strconv.ParseInt(stats[9], 10, 64)
		txErrors, _ := strconv.ParseInt(stats[10], 10, 64)
		txDropped, _ := strconv.ParseInt(stats[11], 10, 64)

		metrics := &NetworkMetrics{
			Interface: iface,
			RxBytes:   rxBytes,
			TxBytes:   txBytes,
			RxPackets: rxPackets,
			TxPackets: txPackets,
			RxErrors:  rxErrors,
			TxErrors:  txErrors,
			RxDropped: rxDropped,
			TxDropped: txDropped,
		}

		networkMetrics[iface] = metrics
	}

	return networkMetrics, nil
}

// collectLoadMetrics 收集负载指标
func (sm *SystemMonitor) collectLoadMetrics() (*LoadMetrics, error) {
	loadavgFile := filepath.Join(sm.config.ProcPath, "loadavg")
	data, err := os.ReadFile(loadavgFile)
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return nil, fmt.Errorf("invalid loadavg format")
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, err
	}

	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, err
	}

	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, err
	}

	// 解析运行进程数
	procInfo := strings.Split(fields[3], "/")
	var runningProcs, totalProcs int
	if len(procInfo) == 2 {
		runningProcs, _ = strconv.Atoi(procInfo[0])
		totalProcs, _ = strconv.Atoi(procInfo[1])
	}

	return &LoadMetrics{
		Load1:        load1,
		Load5:        load5,
		Load15:       load15,
		RunningProcs: runningProcs,
		TotalProcs:   totalProcs,
	}, nil
}

// collectProcessMetrics 收集进程指标
func (sm *SystemMonitor) collectProcessMetrics() (*ProcessMetrics, error) {
	statFile := filepath.Join(sm.config.ProcPath, "stat")
	data, err := os.ReadFile(statFile)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	metrics := &ProcessMetrics{}

	for _, line := range lines {
		if strings.HasPrefix(line, "processes ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				metrics.TotalProcesses, _ = strconv.Atoi(fields[1])
			}
		} else if strings.HasPrefix(line, "procs_running ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				metrics.RunningProcesses, _ = strconv.Atoi(fields[1])
			}
		} else if strings.HasPrefix(line, "procs_blocked ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				blocked, _ := strconv.Atoi(fields[1])
				metrics.SleepingProcesses = blocked
			}
		}
	}

	return metrics, nil
}

// collectSystemInfo 收集系统信息
func (sm *SystemMonitor) collectSystemInfo() (*SystemInfo, error) {
	info := &SystemInfo{}

	// 获取主机名
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}

	// 获取系统正常运行时间
	uptimeFile := filepath.Join(sm.config.ProcPath, "uptime")
	if data, err := os.ReadFile(uptimeFile); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			if uptime, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info.Uptime = int64(uptime)
				info.BootTime = time.Now().Unix() - info.Uptime
			}
		}
	}

	// 获取 CPU 数量
	if cpuinfo, err := os.ReadFile(filepath.Join(sm.config.ProcPath, "cpuinfo")); err == nil {
		lines := strings.Split(string(cpuinfo), "\n")
		cpuCount := 0
		for _, line := range lines {
			if strings.HasPrefix(line, "processor") {
				cpuCount++
			}
		}
		info.CPUCount = cpuCount
	}

	return info, nil
}

// checkSystemThresholds 检查系统阈值
func (sm *SystemMonitor) checkSystemThresholds(metrics *SystemMetrics) {
	thresholds := sm.config.AlertThresholds
	if thresholds == nil {
		return
	}

	// 检查 CPU 使用率
	if metrics.CPUUsage != nil && metrics.CPUUsage.UsagePercent > thresholds.CPUUsagePercent {
		sm.sendAlert("CPU usage too high", map[string]interface{}{
			"current_usage": metrics.CPUUsage.UsagePercent,
			"threshold":     thresholds.CPUUsagePercent,
		}, EventSeverityWarning)
	}

	// 检查内存使用率
	if metrics.MemoryUsage != nil && metrics.MemoryUsage.UsagePercent > thresholds.MemoryUsagePercent {
		severity := EventSeverityWarning
		if metrics.MemoryUsage.UsagePercent > 95.0 {
			severity = EventSeverityCritical
		}

		sm.sendAlert("Memory usage too high", map[string]interface{}{
			"current_usage": metrics.MemoryUsage.UsagePercent,
			"threshold":     thresholds.MemoryUsagePercent,
		}, severity)
	}

	// 检查磁盘使用率
	if metrics.DiskUsage != nil {
		for device, diskMetrics := range metrics.DiskUsage {
			if diskMetrics.UsagePercent > thresholds.DiskUsagePercent {
				sm.sendAlert("Disk usage too high", map[string]interface{}{
					"device":        device,
					"mount_point":   diskMetrics.MountPoint,
					"current_usage": diskMetrics.UsagePercent,
					"threshold":     thresholds.DiskUsagePercent,
				}, EventSeverityWarning)
			}

			if diskMetrics.InodesUsagePercent > thresholds.InodeUsagePercent {
				sm.sendAlert("Inode usage too high", map[string]interface{}{
					"device":        device,
					"mount_point":   diskMetrics.MountPoint,
					"current_usage": diskMetrics.InodesUsagePercent,
					"threshold":     thresholds.InodeUsagePercent,
				}, EventSeverityWarning)
			}
		}
	}

	// 检查系统负载
	if metrics.LoadAverage != nil {
		if metrics.LoadAverage.Load1 > thresholds.Load1Threshold {
			sm.sendAlert("Load average 1m too high", map[string]interface{}{
				"current_load": metrics.LoadAverage.Load1,
				"threshold":    thresholds.Load1Threshold,
			}, EventSeverityWarning)
		}

		if metrics.LoadAverage.Load5 > thresholds.Load5Threshold {
			sm.sendAlert("Load average 5m too high", map[string]interface{}{
				"current_load": metrics.LoadAverage.Load5,
				"threshold":    thresholds.Load5Threshold,
			}, EventSeverityWarning)
		}

		if metrics.LoadAverage.Load15 > thresholds.Load15Threshold {
			sm.sendAlert("Load average 15m too high", map[string]interface{}{
				"current_load": metrics.LoadAverage.Load15,
				"threshold":    thresholds.Load15Threshold,
			}, EventSeverityWarning)
		}
	}
}

// sendAlert 发送告警
func (sm *SystemMonitor) sendAlert(message string, data map[string]interface{}, severity EventSeverity) {
	sm.sendEvent(&SystemEvent{
		Type:      EventTypeSystemAlert,
		Timestamp: time.Now(),
		Severity:  severity,
		Data: map[string]interface{}{
			"message": message,
			"details": data,
		},
	})
}

// sendEvent 发送事件
func (sm *SystemMonitor) sendEvent(event *SystemEvent) {
	select {
	case sm.eventChan <- event:
	default:
		sm.logger.Warn("System event channel full, dropping event",
			zap.String("event_type", event.Type))
	}
}

// copyMetrics 复制指标
func (sm *SystemMonitor) copyMetrics(metrics *SystemMetrics) *SystemMetrics {
	if metrics == nil {
		return nil
	}

	// 这里为了简化，直接返回原指标
	// 在实际实现中应该进行深拷贝
	return metrics
}

// NewResourceCollector 创建资源收集器
func NewResourceCollector(config *ContainerMonitorConfig) *ResourceCollector {
	return &ResourceCollector{
		config:   config,
		logger:   zap.NewNop(),
		lastCPU:  make(map[string]*CPUStats),
		lastNet:  make(map[string]*NetworkStats),
		lastDisk: make(map[string]*DiskStats),
	}
}

// CollectCPUMetrics 收集CPU指标
func (rc *ResourceCollector) CollectCPUMetrics(containerID string, pid int) (*CPUMetrics, error) {
	// 简化实现，返回模拟数据
	return &CPUMetrics{
		UsagePercent: 10.5,
		UserTime:     1200,
		SystemTime:   800,
		Throttled:    0,
	}, nil
}

// CollectMemoryMetrics 收集内存指标
func (rc *ResourceCollector) CollectMemoryMetrics(containerID string, pid int) (*MemoryMetrics, error) {
	// 简化实现，返回模拟数据
	return &MemoryMetrics{
		Total:        1024 * 1024 * 1024, // 1GB
		Used:         512 * 1024 * 1024,  // 512MB
		UsagePercent: 50.0,
	}, nil
}

// CollectNetworkMetrics 收集网络指标
func (rc *ResourceCollector) CollectNetworkMetrics(containerID string, pid int) (*NetworkMetrics, error) {
	// 简化实现，返回模拟数据
	return &NetworkMetrics{
		Interface: "eth0",
		RxBytes:   1024,
		TxBytes:   2048,
		RxPackets: 10,
		TxPackets: 15,
	}, nil
}

// CollectDiskMetrics 收集磁盘指标
func (rc *ResourceCollector) CollectDiskMetrics(containerID string, pid int) (*DiskMetrics, error) {
	// 简化实现，返回模拟数据
	return &DiskMetrics{
		Device:     "/dev/sda1",
		ReadBytes:  4096,
		WriteBytes: 8192,
		ReadOps:    5,
		WriteOps:   10,
	}, nil
}

// CollectProcessMetrics 收集进程指标
func (rc *ResourceCollector) CollectProcessMetrics(containerID string, pid int) (*ProcessMetrics, error) {
	// 简化实现，返回模拟数据
	return &ProcessMetrics{
		TotalProcesses:   150,
		RunningProcesses: 3,
	}, nil
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(config *ContainerMonitorConfig) *HealthChecker {
	return &HealthChecker{
		config: config,
		logger: zap.NewNop(),
	}
}

// CheckContainer 检查容器健康状态
func (hc *HealthChecker) CheckContainer(containerID string) []HealthCheckResult {
	// 简化实现，返回模拟数据
	return []HealthCheckResult{
		{
			CheckType: "process",
			Status:    "HEALTHY",
			Message:   "Process is running",
			Timestamp: time.Now(),
			Duration:  time.Millisecond * 10,
		},
		{
			CheckType: "resource",
			Status:    "HEALTHY",
			Message:   "Resource usage is normal",
			Timestamp: time.Now(),
			Duration:  time.Millisecond * 5,
		},
	}
}
