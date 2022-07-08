package noderesourcemanager

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hyfly233/carrot/internal/common"
	"go.uber.org/zap"
)

// CPUManager CPU管理器
type CPUManager struct {
	mu     sync.RWMutex
	config *CPUManagerConfig
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// CPU拓扑信息
	topology *CPUTopology

	// CPU分配状态
	allocatedCPUs map[string]*CPUAllocation
	availableCPUs []int
	isolatedCPUs  []int

	// CPU集合管理
	cpuSets map[string]*CPUSet

	// 监控状态
	isRunning bool
	eventChan chan *CPUEvent

	// 性能统计
	stats *CPUManagerStats
}

// CPUManagerConfig CPU管理器配置
type CPUManagerConfig struct {
	Policy               CPUPolicy     `json:"policy"`
	CgroupsPath          string        `json:"cgroups_path"`
	CPUSetPath           string        `json:"cpuset_path"`
	TopologyManagerScope string        `json:"topology_manager_scope"`
	ReservedCPUs         []int         `json:"reserved_cpus"`
	IsolatedCPUs         []int         `json:"isolated_cpus"`
	EnableCPUPinning     bool          `json:"enable_cpu_pinning"`
	EnableCFS            bool          `json:"enable_cfs"`
	CFSQuotaPeriod       time.Duration `json:"cfs_quota_period"`
	MonitoringInterval   time.Duration `json:"monitoring_interval"`
}

// CPUPolicy CPU分配策略
type CPUPolicy string

const (
	CPUPolicyNone   CPUPolicy = "none"
	CPUPolicyStatic CPUPolicy = "static"
	CPUPolicyShared CPUPolicy = "shared"
)

// CPUTopology CPU拓扑结构
type CPUTopology struct {
	CPUs           []*CPU      `json:"cpus"`
	Sockets        []*Socket   `json:"sockets"`
	NUMANodes      []*NUMANode `json:"numa_nodes"`
	CPUCount       int         `json:"cpu_count"`
	SocketCount    int         `json:"socket_count"`
	NUMACount      int         `json:"numa_count"`
	CoresPerSocket int         `json:"cores_per_socket"`
	ThreadsPerCore int         `json:"threads_per_core"`
}

// CPU CPU信息
type CPU struct {
	ID          int   `json:"id"`
	SocketID    int   `json:"socket_id"`
	NUMANodeID  int   `json:"numa_node_id"`
	CoreID      int   `json:"core_id"`
	ThreadID    int   `json:"thread_id"`
	Frequency   int64 `json:"frequency"`
	IsOnline    bool  `json:"is_online"`
	IsAvailable bool  `json:"is_available"`
}

// Socket 物理CPU插槽
type Socket struct {
	ID      int   `json:"id"`
	CPUs    []int `json:"cpus"`
	CoreIDs []int `json:"core_ids"`
}

// NUMANode NUMA节点
type NUMANode struct {
	ID          int   `json:"id"`
	CPUs        []int `json:"cpus"`
	MemoryTotal int64 `json:"memory_total"`
	MemoryFree  int64 `json:"memory_free"`
	Distance    []int `json:"distance"`
}

// CPUAllocation CPU分配信息
type CPUAllocation struct {
	ContainerID   string               `json:"container_id"`
	CPURequest    *common.ResourceSpec `json:"cpu_request"`
	AllocatedCPUs []int                `json:"allocated_cpus"`
	CPUQuota      int64                `json:"cpu_quota"`
	CPUPeriod     int64                `json:"cpu_period"`
	CPUShares     int64                `json:"cpu_shares"`
	Timestamp     time.Time            `json:"timestamp"`
	Policy        CPUPolicy            `json:"policy"`
}

// CPUSet CPU集合
type CPUSet struct {
	Name      string    `json:"name"`
	CPUs      []int     `json:"cpus"`
	Memory    []int     `json:"memory"`
	Exclusive bool      `json:"exclusive"`
	CPUQuota  int64     `json:"cpu_quota"`
	CPUPeriod int64     `json:"cpu_period"`
	CPUShares int64     `json:"cpu_shares"`
	CreatedAt time.Time `json:"created_at"`
}

// CPUEvent CPU事件
type CPUEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  EventSeverity          `json:"severity"`
}

// EventSeverity 事件严重级别
type EventSeverity string

const (
	EventSeverityInfo     EventSeverity = "INFO"
	EventSeverityWarning  EventSeverity = "WARNING"
	EventSeverityError    EventSeverity = "ERROR"
	EventSeverityCritical EventSeverity = "CRITICAL"
)

// CPUEventType CPU事件类型
const (
	EventTypeCPUAllocated       = "CPU_ALLOCATED"
	EventTypeCPUDeallocated     = "CPU_DEALLOCATED"
	EventTypeCPUThrottle        = "CPU_THROTTLE"
	EventTypeCPUTopologyChanged = "CPU_TOPOLOGY_CHANGED"
	EventTypeCPUSetCreated      = "CPUSET_CREATED"
	EventTypeCPUSetDeleted      = "CPUSET_DELETED"
)

// CPUManagerStats CPU管理器统计信息
type CPUManagerStats struct {
	TotalCPUs        int                       `json:"total_cpus"`
	AvailableCPUs    int                       `json:"available_cpus"`
	AllocatedCPUs    int                       `json:"allocated_cpus"`
	ReservedCPUs     int                       `json:"reserved_cpus"`
	TotalAllocations int                       `json:"total_allocations"`
	ActiveContainers int                       `json:"active_containers"`
	CPUUtilization   float64                   `json:"cpu_utilization"`
	Allocations      map[string]*CPUAllocation `json:"allocations"`
	LastUpdated      time.Time                 `json:"last_updated"`
}

// NewCPUManager 创建CPU管理器
func NewCPUManager(config *CPUManagerConfig) (*CPUManager, error) {
	if config == nil {
		return nil, fmt.Errorf("CPU manager config cannot be nil")
	}

	// 设置默认值
	if config.CgroupsPath == "" {
		config.CgroupsPath = "/sys/fs/cgroup"
	}
	if config.CPUSetPath == "" {
		config.CPUSetPath = "/sys/fs/cgroup/cpuset"
	}
	if config.MonitoringInterval == 0 {
		config.MonitoringInterval = 30 * time.Second
	}
	if config.CFSQuotaPeriod == 0 {
		config.CFSQuotaPeriod = 100 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())

	cm := &CPUManager{
		config:        config,
		logger:        zap.NewNop(), // 在实际使用中应该注入正确的logger
		ctx:           ctx,
		cancel:        cancel,
		allocatedCPUs: make(map[string]*CPUAllocation),
		cpuSets:       make(map[string]*CPUSet),
		eventChan:     make(chan *CPUEvent, 1000),
		stats: &CPUManagerStats{
			Allocations: make(map[string]*CPUAllocation),
		},
	}

	// 初始化CPU拓扑
	if err := cm.initCPUTopology(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize CPU topology: %v", err)
	}

	// 初始化可用CPU列表
	cm.initAvailableCPUs()

	return cm, nil
}

// Start 启动CPU管理器
func (cm *CPUManager) Start() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.isRunning {
		return fmt.Errorf("CPU manager is already running")
	}

	cm.logger.Info("Starting CPU manager",
		zap.String("policy", string(cm.config.Policy)),
		zap.Int("total_cpus", cm.topology.CPUCount),
		zap.Int("available_cpus", len(cm.availableCPUs)))

	cm.isRunning = true

	// 启动监控任务
	go cm.monitoringWorker()

	// 发送启动事件
	cm.sendEvent(&CPUEvent{
		Type:      "CPU_MANAGER_STARTED",
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"policy":         string(cm.config.Policy),
			"total_cpus":     cm.topology.CPUCount,
			"available_cpus": len(cm.availableCPUs),
		},
	})

	return nil
}

// Stop 停止CPU管理器
func (cm *CPUManager) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.isRunning {
		return nil
	}

	cm.logger.Info("Stopping CPU manager")
	cm.isRunning = false
	cm.cancel()

	// 清理所有CPU分配
	for containerID := range cm.allocatedCPUs {
		if err := cm.releaseCPUsInternal(containerID); err != nil {
			cm.logger.Error("Failed to release CPUs during shutdown",
				zap.String("container_id", containerID),
				zap.Error(err))
		}
	}

	close(cm.eventChan)
	return nil
}

// AllocateCPUs 为容器分配CPU
func (cm *CPUManager) AllocateCPUs(containerID string, request *common.ResourceSpec) (*CPUAllocation, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.isRunning {
		return nil, fmt.Errorf("CPU manager is not running")
	}

	// 检查是否已经分配
	if existing, exists := cm.allocatedCPUs[containerID]; exists {
		return existing, nil
	}

	// 根据策略分配CPU
	var allocation *CPUAllocation
	var err error

	switch cm.config.Policy {
	case CPUPolicyNone:
		allocation, err = cm.allocateNone(containerID, request)
	case CPUPolicyStatic:
		allocation, err = cm.allocateStatic(containerID, request)
	case CPUPolicyShared:
		allocation, err = cm.allocateShared(containerID, request)
	default:
		err = fmt.Errorf("unsupported CPU policy: %s", cm.config.Policy)
	}

	if err != nil {
		return nil, err
	}

	// 保存分配信息
	cm.allocatedCPUs[containerID] = allocation

	// 应用CPU限制
	if err := cm.applyCPULimits(containerID, allocation); err != nil {
		// 回滚分配
		delete(cm.allocatedCPUs, containerID)
		cm.returnCPUs(allocation.AllocatedCPUs)
		return nil, fmt.Errorf("failed to apply CPU limits: %v", err)
	}

	// 更新统计信息
	cm.updateStats()

	// 发送分配事件
	cm.sendEvent(&CPUEvent{
		Type:      EventTypeCPUAllocated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"container_id":   containerID,
			"allocated_cpus": allocation.AllocatedCPUs,
			"cpu_request":    request.CPU,
			"policy":         string(allocation.Policy),
		},
	})

	cm.logger.Info("CPU allocated",
		zap.String("container_id", containerID),
		zap.Ints("allocated_cpus", allocation.AllocatedCPUs),
		zap.String("policy", string(allocation.Policy)))

	return allocation, nil
}

// ReleaseCPUs 释放容器的CPU
func (cm *CPUManager) ReleaseCPUs(containerID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.releaseCPUsInternal(containerID)
}

// releaseCPUsInternal 内部释放CPU方法（不加锁）
func (cm *CPUManager) releaseCPUsInternal(containerID string) error {
	allocation, exists := cm.allocatedCPUs[containerID]
	if !exists {
		return fmt.Errorf("container %s has no CPU allocation", containerID)
	}

	// 移除cgroup限制
	if err := cm.removeCPULimits(containerID); err != nil {
		cm.logger.Error("Failed to remove CPU limits",
			zap.String("container_id", containerID),
			zap.Error(err))
	}

	// 归还CPU
	cm.returnCPUs(allocation.AllocatedCPUs)

	// 删除分配记录
	delete(cm.allocatedCPUs, containerID)

	// 更新统计信息
	cm.updateStats()

	// 发送释放事件
	cm.sendEvent(&CPUEvent{
		Type:      EventTypeCPUDeallocated,
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"container_id":  containerID,
			"released_cpus": allocation.AllocatedCPUs,
		},
	})

	cm.logger.Info("CPU released",
		zap.String("container_id", containerID),
		zap.Ints("released_cpus", allocation.AllocatedCPUs))

	return nil
}

// GetCPUAllocation 获取容器的CPU分配信息
func (cm *CPUManager) GetCPUAllocation(containerID string) (*CPUAllocation, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	allocation, exists := cm.allocatedCPUs[containerID]
	if !exists {
		return nil, fmt.Errorf("container %s has no CPU allocation", containerID)
	}

	// 返回副本
	return &CPUAllocation{
		ContainerID:   allocation.ContainerID,
		CPURequest:    allocation.CPURequest,
		AllocatedCPUs: append([]int{}, allocation.AllocatedCPUs...),
		CPUQuota:      allocation.CPUQuota,
		CPUPeriod:     allocation.CPUPeriod,
		CPUShares:     allocation.CPUShares,
		Timestamp:     allocation.Timestamp,
		Policy:        allocation.Policy,
	}, nil
}

// GetTopology 获取CPU拓扑信息
func (cm *CPUManager) GetTopology() *CPUTopology {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 返回副本
	topology := &CPUTopology{
		CPUCount:       cm.topology.CPUCount,
		SocketCount:    cm.topology.SocketCount,
		NUMACount:      cm.topology.NUMACount,
		CoresPerSocket: cm.topology.CoresPerSocket,
		ThreadsPerCore: cm.topology.ThreadsPerCore,
	}

	// 复制CPU列表
	for _, cpu := range cm.topology.CPUs {
		topology.CPUs = append(topology.CPUs, &CPU{
			ID:          cpu.ID,
			SocketID:    cpu.SocketID,
			NUMANodeID:  cpu.NUMANodeID,
			CoreID:      cpu.CoreID,
			ThreadID:    cpu.ThreadID,
			Frequency:   cpu.Frequency,
			IsOnline:    cpu.IsOnline,
			IsAvailable: cpu.IsAvailable,
		})
	}

	return topology
}

// GetStats 获取统计信息
func (cm *CPUManager) GetStats() *CPUManagerStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 返回副本
	stats := &CPUManagerStats{
		TotalCPUs:        cm.stats.TotalCPUs,
		AvailableCPUs:    cm.stats.AvailableCPUs,
		AllocatedCPUs:    cm.stats.AllocatedCPUs,
		ReservedCPUs:     cm.stats.ReservedCPUs,
		TotalAllocations: cm.stats.TotalAllocations,
		ActiveContainers: cm.stats.ActiveContainers,
		CPUUtilization:   cm.stats.CPUUtilization,
		LastUpdated:      cm.stats.LastUpdated,
		Allocations:      make(map[string]*CPUAllocation),
	}

	// 复制分配信息
	for id, alloc := range cm.stats.Allocations {
		stats.Allocations[id] = &CPUAllocation{
			ContainerID:   alloc.ContainerID,
			CPURequest:    alloc.CPURequest,
			AllocatedCPUs: append([]int{}, alloc.AllocatedCPUs...),
			CPUQuota:      alloc.CPUQuota,
			CPUPeriod:     alloc.CPUPeriod,
			CPUShares:     alloc.CPUShares,
			Timestamp:     alloc.Timestamp,
			Policy:        alloc.Policy,
		}
	}

	return stats
}

// GetEvents 获取事件通道
func (cm *CPUManager) GetEvents() <-chan *CPUEvent {
	return cm.eventChan
}

// initCPUTopology 初始化CPU拓扑
func (cm *CPUManager) initCPUTopology() error {
	topology := &CPUTopology{
		CPUs:      make([]*CPU, 0),
		Sockets:   make([]*Socket, 0),
		NUMANodes: make([]*NUMANode, 0),
	}

	// 读取CPU信息
	cpuinfoPath := "/proc/cpuinfo"
	data, err := ioutil.ReadFile(cpuinfoPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", cpuinfoPath, err)
	}

	lines := strings.Split(string(data), "\n")
	var currentCPU *CPU
	socketMap := make(map[int]*Socket)
	numaMap := make(map[int]*NUMANode)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if currentCPU != nil {
				topology.CPUs = append(topology.CPUs, currentCPU)
				currentCPU = nil
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "processor":
			if currentCPU != nil {
				topology.CPUs = append(topology.CPUs, currentCPU)
			}
			currentCPU = &CPU{IsOnline: true, IsAvailable: true}
			if id, err := strconv.Atoi(value); err == nil {
				currentCPU.ID = id
			}

		case "physical id":
			if currentCPU != nil {
				if socketID, err := strconv.Atoi(value); err == nil {
					currentCPU.SocketID = socketID
					if socket, exists := socketMap[socketID]; exists {
						socket.CPUs = append(socket.CPUs, currentCPU.ID)
					} else {
						socketMap[socketID] = &Socket{
							ID:   socketID,
							CPUs: []int{currentCPU.ID},
						}
					}
				}
			}

		case "core id":
			if currentCPU != nil {
				if coreID, err := strconv.Atoi(value); err == nil {
					currentCPU.CoreID = coreID
				}
			}

		case "cpu MHz":
			if currentCPU != nil {
				if freq, err := strconv.ParseFloat(value, 64); err == nil {
					currentCPU.Frequency = int64(freq * 1000000) // 转换为Hz
				}
			}
		}
	}

	// 添加最后一个CPU
	if currentCPU != nil {
		topology.CPUs = append(topology.CPUs, currentCPU)
	}

	// 设置拓扑信息
	topology.CPUCount = len(topology.CPUs)
	topology.SocketCount = len(socketMap)

	// 转换socket map为slice
	for _, socket := range socketMap {
		topology.Sockets = append(topology.Sockets, socket)
	}

	// 尝试读取NUMA信息
	cm.loadNUMAInfo(topology, numaMap)

	cm.topology = topology
	return nil
}

// loadNUMAInfo 加载NUMA信息
func (cm *CPUManager) loadNUMAInfo(topology *CPUTopology, numaMap map[int]*NUMANode) {
	numaPath := "/sys/devices/system/node"
	entries, err := ioutil.ReadDir(numaPath)
	if err != nil {
		cm.logger.Debug("Failed to read NUMA info", zap.Error(err))
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "node") {
			continue
		}

		nodeIDStr := strings.TrimPrefix(entry.Name(), "node")
		nodeID, err := strconv.Atoi(nodeIDStr)
		if err != nil {
			continue
		}

		node := &NUMANode{
			ID:   nodeID,
			CPUs: make([]int, 0),
		}

		// 读取NUMA节点的CPU列表
		cpuListPath := filepath.Join(numaPath, entry.Name(), "cpulist")
		if data, err := ioutil.ReadFile(cpuListPath); err == nil {
			cpuList := strings.TrimSpace(string(data))
			node.CPUs = cm.parseCPUList(cpuList)
		}

		// 读取内存信息
		meminfoPath := filepath.Join(numaPath, entry.Name(), "meminfo")
		if data, err := ioutil.ReadFile(meminfoPath); err == nil {
			cm.parseNUMAMeminfo(string(data), node)
		}

		numaMap[nodeID] = node
		topology.NUMANodes = append(topology.NUMANodes, node)
	}

	topology.NUMACount = len(topology.NUMANodes)

	// 设置CPU的NUMA节点ID
	for _, cpu := range topology.CPUs {
		for _, node := range topology.NUMANodes {
			for _, cpuID := range node.CPUs {
				if cpuID == cpu.ID {
					cpu.NUMANodeID = node.ID
					break
				}
			}
		}
	}
}

// parseCPUList 解析CPU列表
func (cm *CPUManager) parseCPUList(cpuList string) []int {
	var cpus []int
	parts := strings.Split(cpuList, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			// 范围格式，如 "0-3"
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start, err1 := strconv.Atoi(rangeParts[0])
				end, err2 := strconv.Atoi(rangeParts[1])
				if err1 == nil && err2 == nil {
					for i := start; i <= end; i++ {
						cpus = append(cpus, i)
					}
				}
			}
		} else {
			// 单个CPU
			if cpu, err := strconv.Atoi(part); err == nil {
				cpus = append(cpus, cpu)
			}
		}
	}

	return cpus
}

// parseNUMAMeminfo 解析NUMA内存信息
func (cm *CPUManager) parseNUMAMeminfo(meminfo string, node *NUMANode) {
	lines := strings.Split(meminfo, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Node") && strings.Contains(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				if total, err := strconv.ParseInt(parts[3], 10, 64); err == nil {
					node.MemoryTotal = total * 1024 // 转换为字节
				}
			}
		} else if strings.HasPrefix(line, "Node") && strings.Contains(line, "MemFree:") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				if free, err := strconv.ParseInt(parts[3], 10, 64); err == nil {
					node.MemoryFree = free * 1024 // 转换为字节
				}
			}
		}
	}
}

// initAvailableCPUs 初始化可用CPU列表
func (cm *CPUManager) initAvailableCPUs() {
	cm.availableCPUs = make([]int, 0)
	cm.isolatedCPUs = append([]int{}, cm.config.IsolatedCPUs...)

	// 获取所有在线CPU
	for _, cpu := range cm.topology.CPUs {
		if !cpu.IsOnline {
			continue
		}

		// 检查是否为保留CPU
		isReserved := false
		for _, reserved := range cm.config.ReservedCPUs {
			if cpu.ID == reserved {
				isReserved = true
				break
			}
		}

		// 检查是否为隔离CPU
		isIsolated := false
		for _, isolated := range cm.config.IsolatedCPUs {
			if cpu.ID == isolated {
				isIsolated = true
				break
			}
		}

		if !isReserved && !isIsolated {
			cm.availableCPUs = append(cm.availableCPUs, cpu.ID)
		}
	}

	cm.logger.Info("Initialized CPU lists",
		zap.Ints("available_cpus", cm.availableCPUs),
		zap.Ints("reserved_cpus", cm.config.ReservedCPUs),
		zap.Ints("isolated_cpus", cm.config.IsolatedCPUs))
}

// allocateNone none策略分配
func (cm *CPUManager) allocateNone(containerID string, request *common.ResourceSpec) (*CPUAllocation, error) {
	// none策略不进行CPU绑定，只设置CFS配额
	cpuQuota := int64(-1) // 默认不限制
	cpuPeriod := int64(cm.config.CFSQuotaPeriod.Microseconds())
	cpuShares := int64(1024) // 默认权重

	if request.CPU > 0 {
		// 将CPU请求转换为quota
		cpuQuota = int64(float64(cpuPeriod) * request.CPU)

		// 计算CPU shares (相对权重)
		cpuShares = int64(request.CPU * 1024)
	}

	allocation := &CPUAllocation{
		ContainerID:   containerID,
		CPURequest:    request,
		AllocatedCPUs: []int{}, // none策略不绑定特定CPU
		CPUQuota:      cpuQuota,
		CPUPeriod:     cpuPeriod,
		CPUShares:     cpuShares,
		Timestamp:     time.Now(),
		Policy:        CPUPolicyNone,
	}

	return allocation, nil
}

// allocateStatic static策略分配
func (cm *CPUManager) allocateStatic(containerID string, request *common.ResourceSpec) (*CPUAllocation, error) {
	// static策略需要独占CPU
	if request.CPU <= 0 {
		return nil, fmt.Errorf("static policy requires CPU request > 0")
	}

	// 检查请求的CPU数量是否为整数
	if request.CPU != float64(int(request.CPU)) {
		return nil, fmt.Errorf("static policy requires integer CPU request")
	}

	requestedCPUs := int(request.CPU)
	if requestedCPUs > len(cm.availableCPUs) {
		return nil, fmt.Errorf("insufficient CPUs: requested %d, available %d",
			requestedCPUs, len(cm.availableCPUs))
	}

	// 分配CPU（使用NUMA亲和性）
	allocatedCPUs := cm.allocateCPUsWithTopology(requestedCPUs)
	if len(allocatedCPUs) != requestedCPUs {
		return nil, fmt.Errorf("failed to allocate %d CPUs", requestedCPUs)
	}

	// 从可用列表中移除已分配的CPU
	cm.removeCPUs(allocatedCPUs)

	allocation := &CPUAllocation{
		ContainerID:   containerID,
		CPURequest:    request,
		AllocatedCPUs: allocatedCPUs,
		CPUQuota:      -1, // static策略不使用quota
		CPUPeriod:     int64(cm.config.CFSQuotaPeriod.Microseconds()),
		CPUShares:     int64(requestedCPUs * 1024),
		Timestamp:     time.Now(),
		Policy:        CPUPolicyStatic,
	}

	return allocation, nil
}

// allocateShared shared策略分配
func (cm *CPUManager) allocateShared(containerID string, request *common.ResourceSpec) (*CPUAllocation, error) {
	// shared策略允许共享CPU，不独占
	cpuQuota := int64(-1)
	cpuPeriod := int64(cm.config.CFSQuotaPeriod.Microseconds())
	cpuShares := int64(1024)

	if request.CPU > 0 {
		cpuQuota = int64(float64(cpuPeriod) * request.CPU)
		cpuShares = int64(request.CPU * 1024)
	}

	// shared策略可以使用所有非独占的CPU
	sharedCPUs := make([]int, 0)
	for _, cpuID := range cm.availableCPUs {
		sharedCPUs = append(sharedCPUs, cpuID)
	}

	allocation := &CPUAllocation{
		ContainerID:   containerID,
		CPURequest:    request,
		AllocatedCPUs: sharedCPUs,
		CPUQuota:      cpuQuota,
		CPUPeriod:     cpuPeriod,
		CPUShares:     cpuShares,
		Timestamp:     time.Now(),
		Policy:        CPUPolicyShared,
	}

	return allocation, nil
}

// allocateCPUsWithTopology 基于拓扑分配CPU
func (cm *CPUManager) allocateCPUsWithTopology(count int) []int {
	if count <= 0 || count > len(cm.availableCPUs) {
		return []int{}
	}

	// 优先从同一NUMA节点分配
	numaAllocation := cm.allocateFromSameNUMA(count)
	if len(numaAllocation) == count {
		return numaAllocation
	}

	// 如果单个NUMA节点不够，从可用CPU中分配
	allocated := make([]int, 0, count)
	for i := 0; i < count && i < len(cm.availableCPUs); i++ {
		allocated = append(allocated, cm.availableCPUs[i])
	}

	return allocated
}

// allocateFromSameNUMA 从同一NUMA节点分配CPU
func (cm *CPUManager) allocateFromSameNUMA(count int) []int {
	// 按NUMA节点分组可用CPU
	numaGroups := make(map[int][]int)
	for _, cpuID := range cm.availableCPUs {
		for _, cpu := range cm.topology.CPUs {
			if cpu.ID == cpuID {
				numaGroups[cpu.NUMANodeID] = append(numaGroups[cpu.NUMANodeID], cpuID)
				break
			}
		}
	}

	// 找到有足够CPU的NUMA节点
	for _, cpus := range numaGroups {
		if len(cpus) >= count {
			return cpus[:count]
		}
	}

	return []int{}
}

// removeCPUs 从可用列表中移除CPU
func (cm *CPUManager) removeCPUs(cpuIDs []int) {
	for _, cpuID := range cpuIDs {
		for i, availableCPU := range cm.availableCPUs {
			if availableCPU == cpuID {
				cm.availableCPUs = append(cm.availableCPUs[:i], cm.availableCPUs[i+1:]...)
				break
			}
		}
	}
}

// returnCPUs 归还CPU到可用列表
func (cm *CPUManager) returnCPUs(cpuIDs []int) {
	for _, cpuID := range cpuIDs {
		// 检查是否已经在可用列表中
		found := false
		for _, availableCPU := range cm.availableCPUs {
			if availableCPU == cpuID {
				found = true
				break
			}
		}

		if !found {
			cm.availableCPUs = append(cm.availableCPUs, cpuID)
		}
	}
}

// applyCPULimits 应用CPU限制
func (cm *CPUManager) applyCPULimits(containerID string, allocation *CPUAllocation) error {
	if !cm.config.EnableCFS && allocation.Policy != CPUPolicyStatic {
		return nil
	}

	cgroupPath := filepath.Join(cm.config.CgroupsPath, "cpu", containerID)

	// 创建cgroup目录
	if err := os.MkdirAll(cgroupPath, 0755); err != nil {
		return fmt.Errorf("failed to create cgroup directory: %v", err)
	}

	// 设置CPU quota
	if allocation.CPUQuota > 0 {
		quotaPath := filepath.Join(cgroupPath, "cpu.cfs_quota_us")
		if err := ioutil.WriteFile(quotaPath, []byte(fmt.Sprintf("%d", allocation.CPUQuota)), 0644); err != nil {
			return fmt.Errorf("failed to set CPU quota: %v", err)
		}
	}

	// 设置CPU period
	periodPath := filepath.Join(cgroupPath, "cpu.cfs_period_us")
	if err := ioutil.WriteFile(periodPath, []byte(fmt.Sprintf("%d", allocation.CPUPeriod)), 0644); err != nil {
		return fmt.Errorf("failed to set CPU period: %v", err)
	}

	// 设置CPU shares
	sharesPath := filepath.Join(cgroupPath, "cpu.shares")
	if err := ioutil.WriteFile(sharesPath, []byte(fmt.Sprintf("%d", allocation.CPUShares)), 0644); err != nil {
		return fmt.Errorf("failed to set CPU shares: %v", err)
	}

	// 设置cpuset（仅对static和shared策略）
	if len(allocation.AllocatedCPUs) > 0 && cm.config.EnableCPUPinning {
		cpusetPath := filepath.Join(cm.config.CPUSetPath, containerID)
		if err := os.MkdirAll(cpusetPath, 0755); err != nil {
			return fmt.Errorf("failed to create cpuset directory: %v", err)
		}

		// 设置CPU列表
		cpuList := cm.formatCPUList(allocation.AllocatedCPUs)
		cpusPath := filepath.Join(cpusetPath, "cpuset.cpus")
		if err := ioutil.WriteFile(cpusPath, []byte(cpuList), 0644); err != nil {
			return fmt.Errorf("failed to set cpuset.cpus: %v", err)
		}

		// 设置内存节点
		memNodes := cm.getCPUMemoryNodes(allocation.AllocatedCPUs)
		memsPath := filepath.Join(cpusetPath, "cpuset.mems")
		if err := ioutil.WriteFile(memsPath, []byte(memNodes), 0644); err != nil {
			return fmt.Errorf("failed to set cpuset.mems: %v", err)
		}
	}

	return nil
}

// removeCPULimits 移除CPU限制
func (cm *CPUManager) removeCPULimits(containerID string) error {
	// 移除cgroup
	cgroupPath := filepath.Join(cm.config.CgroupsPath, "cpu", containerID)
	if err := os.RemoveAll(cgroupPath); err != nil && !os.IsNotExist(err) {
		cm.logger.Error("Failed to remove CPU cgroup", zap.Error(err))
	}

	// 移除cpuset
	cpusetPath := filepath.Join(cm.config.CPUSetPath, containerID)
	if err := os.RemoveAll(cpusetPath); err != nil && !os.IsNotExist(err) {
		cm.logger.Error("Failed to remove cpuset", zap.Error(err))
	}

	return nil
}

// formatCPUList 格式化CPU列表
func (cm *CPUManager) formatCPUList(cpuIDs []int) string {
	if len(cpuIDs) == 0 {
		return ""
	}

	cpuStrs := make([]string, len(cpuIDs))
	for i, cpuID := range cpuIDs {
		cpuStrs[i] = strconv.Itoa(cpuID)
	}

	return strings.Join(cpuStrs, ",")
}

// getCPUMemoryNodes 获取CPU对应的内存节点
func (cm *CPUManager) getCPUMemoryNodes(cpuIDs []int) string {
	nodeSet := make(map[int]bool)

	for _, cpuID := range cpuIDs {
		for _, cpu := range cm.topology.CPUs {
			if cpu.ID == cpuID {
				nodeSet[cpu.NUMANodeID] = true
				break
			}
		}
	}

	nodes := make([]string, 0, len(nodeSet))
	for nodeID := range nodeSet {
		nodes = append(nodes, strconv.Itoa(nodeID))
	}

	if len(nodes) == 0 {
		return "0" // 默认使用节点0
	}

	return strings.Join(nodes, ",")
}

// updateStats 更新统计信息
func (cm *CPUManager) updateStats() {
	cm.stats.TotalCPUs = cm.topology.CPUCount
	cm.stats.AvailableCPUs = len(cm.availableCPUs)
	cm.stats.ReservedCPUs = len(cm.config.ReservedCPUs)
	cm.stats.TotalAllocations = len(cm.allocatedCPUs)
	cm.stats.ActiveContainers = len(cm.allocatedCPUs)
	cm.stats.LastUpdated = time.Now()

	// 计算已分配CPU数量
	allocatedCount := 0
	cm.stats.Allocations = make(map[string]*CPUAllocation)

	for containerID, allocation := range cm.allocatedCPUs {
		if allocation.Policy == CPUPolicyStatic {
			allocatedCount += len(allocation.AllocatedCPUs)
		}

		// 复制分配信息
		cm.stats.Allocations[containerID] = &CPUAllocation{
			ContainerID:   allocation.ContainerID,
			CPURequest:    allocation.CPURequest,
			AllocatedCPUs: append([]int{}, allocation.AllocatedCPUs...),
			CPUQuota:      allocation.CPUQuota,
			CPUPeriod:     allocation.CPUPeriod,
			CPUShares:     allocation.CPUShares,
			Timestamp:     allocation.Timestamp,
			Policy:        allocation.Policy,
		}
	}

	cm.stats.AllocatedCPUs = allocatedCount

	// 计算CPU利用率
	if cm.stats.TotalCPUs > 0 {
		cm.stats.CPUUtilization = float64(cm.stats.AllocatedCPUs) / float64(cm.stats.TotalCPUs) * 100
	}
}

// monitoringWorker 监控工作线程
func (cm *CPUManager) monitoringWorker() {
	ticker := time.NewTicker(cm.config.MonitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.performMonitoring()
		case <-cm.ctx.Done():
			return
		}
	}
}

// performMonitoring 执行监控
func (cm *CPUManager) performMonitoring() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 更新统计信息
	cm.updateStats()

	// 检查CPU限制违规
	cm.checkCPUThrottling()

	// 发送监控事件
	cm.sendEvent(&CPUEvent{
		Type:      "CPU_MONITORING",
		Timestamp: time.Now(),
		Severity:  EventSeverityInfo,
		Data: map[string]interface{}{
			"total_cpus":        cm.stats.TotalCPUs,
			"available_cpus":    cm.stats.AvailableCPUs,
			"allocated_cpus":    cm.stats.AllocatedCPUs,
			"cpu_utilization":   cm.stats.CPUUtilization,
			"active_containers": cm.stats.ActiveContainers,
		},
	})
}

// checkCPUThrottling 检查CPU限制
func (cm *CPUManager) checkCPUThrottling() {
	for containerID := range cm.allocatedCPUs {
		// 读取CPU限制统计
		cgroupPath := filepath.Join(cm.config.CgroupsPath, "cpu", containerID)
		statPath := filepath.Join(cgroupPath, "cpu.stat")

		if data, err := ioutil.ReadFile(statPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "nr_throttled") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						if throttled, err := strconv.ParseInt(parts[1], 10, 64); err == nil && throttled > 0 {
							cm.sendEvent(&CPUEvent{
								Type:      EventTypeCPUThrottle,
								Timestamp: time.Now(),
								Severity:  EventSeverityWarning,
								Data: map[string]interface{}{
									"container_id":    containerID,
									"throttled_count": throttled,
								},
							})
						}
					}
				}
			}
		}
	}
}

// sendEvent 发送事件
func (cm *CPUManager) sendEvent(event *CPUEvent) {
	select {
	case cm.eventChan <- event:
	default:
		cm.logger.Warn("CPU event channel full, dropping event",
			zap.String("event_type", event.Type))
	}
}
