package server

import (
	"fmt"
	"sync"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ApplicationMasterServerExample 展示如何使用新的服务器架构
type ApplicationMasterServerExample struct {
	serverManager *ServerManager
	httpServer    *HTTPServer
	logger        *zap.Logger
	am            ApplicationMasterInterface
}

// NewApplicationMasterServerExample 创建服务器示例
func NewApplicationMasterServerExample(am ApplicationMasterInterface, logger *zap.Logger) *ApplicationMasterServerExample {
	serverManager := NewServerManager(logger)
	httpServer := NewHTTPServer(am, logger)
	
	// 注册 HTTP 服务器
	serverManager.RegisterServer(ServerTypeHTTP, httpServer)
	
	// 预留：注册其他类型的服务器
	// grpcServer := NewGRPCServer(logger)
	// serverManager.RegisterServer(ServerTypeGRPC, grpcServer)
	
	// tcpServer := NewTCPServer(logger)
	// serverManager.RegisterServer(ServerTypeTCP, tcpServer)

	return &ApplicationMasterServerExample{
		serverManager: serverManager,
		httpServer:    httpServer,
		logger:        logger,
		am:            am,
	}
}

// Start 启动所有服务器
func (e *ApplicationMasterServerExample) Start(httpPort int) error {
	// 定义各种服务器的端口
	ports := map[ServerType]int{
		ServerTypeHTTP: httpPort,
		// 预留其他服务器端口
		// ServerTypeGRPC: httpPort + 1,
		// ServerTypeTCP:  httpPort + 2,
	}
	
	// 启动所有已注册的服务器
	return e.serverManager.StartAllServers(ports)
}

// Stop 停止所有服务器
func (e *ApplicationMasterServerExample) Stop() error {
	return e.serverManager.StopAllServers()
}

// GetRunningServers 获取运行中的服务器列表
func (e *ApplicationMasterServerExample) GetRunningServers() []ServerType {
	return e.serverManager.GetRunningServers()
}

// StartHTTPOnly 仅启动 HTTP 服务器
func (e *ApplicationMasterServerExample) StartHTTPOnly(port int) error {
	return e.serverManager.StartServer(ServerTypeHTTP, port)
}

// ApplicationMasterAdapter 适配器，将现有的 ApplicationMaster 适配到接口
type ApplicationMasterAdapter struct {
	am *ApplicationMaster // 假设这是原有的 ApplicationMaster 结构
}

// NewApplicationMasterAdapter 创建适配器
func NewApplicationMasterAdapter(am *ApplicationMaster) *ApplicationMasterAdapter {
	return &ApplicationMasterAdapter{am: am}
}

// 实现 ApplicationMasterInterface 接口的方法
func (a *ApplicationMasterAdapter) GetApplicationID() common.ApplicationID {
	return a.am.applicationID
}

func (a *ApplicationMasterAdapter) GetApplicationAttemptID() string {
	return fmt.Sprintf("%d_%d_%d",
		a.am.applicationAttemptID.ApplicationID.ClusterTimestamp,
		a.am.applicationAttemptID.ApplicationID.ID,
		a.am.applicationAttemptID.AttemptID)
}

func (a *ApplicationMasterAdapter) GetState() string {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	return a.am.applicationState
}

func (a *ApplicationMasterAdapter) GetProgress() float64 {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	return float64(a.am.progress)
}

func (a *ApplicationMasterAdapter) GetFinalStatus() string {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	return a.am.finalStatus
}

func (a *ApplicationMasterAdapter) GetTrackingURL() string {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	return a.am.trackingURL
}

func (a *ApplicationMasterAdapter) GetContainerStatistics() map[string]int {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	return map[string]int{
		"allocated": len(a.am.allocatedContainers),
		"completed": len(a.am.completedContainers),
		"failed":    len(a.am.failedContainers),
		"pending":   len(a.am.pendingRequests),
	}
}

func (a *ApplicationMasterAdapter) GetAllocatedContainers() map[string]*common.Container {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	// 复制 map 以避免并发访问问题
	result := make(map[string]*common.Container)
	for k, v := range a.am.allocatedContainers {
		result[k] = v
	}
	return result
}

func (a *ApplicationMasterAdapter) GetCompletedContainers() map[string]*common.Container {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	result := make(map[string]*common.Container)
	for k, v := range a.am.completedContainers {
		result[k] = v
	}
	return result
}

func (a *ApplicationMasterAdapter) GetFailedContainers() map[string]*common.Container {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	result := make(map[string]*common.Container)
	for k, v := range a.am.failedContainers {
		result[k] = v
	}
	return result
}

func (a *ApplicationMasterAdapter) GetPendingRequests() []*common.ResourceRequest {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	// 转换 ContainerRequest 到 ResourceRequest
	result := make([]*common.ResourceRequest, len(a.am.pendingRequests))
	for i, req := range a.am.pendingRequests {
		result[i] = &common.ResourceRequest{
			Resource: req.Resource,
			Priority: req.Priority,
		}
	}
	return result
}

func (a *ApplicationMasterAdapter) SetApplicationState(state string) {
	a.am.mu.Lock()
	defer a.am.mu.Unlock()
	a.am.applicationState = state
}

func (a *ApplicationMasterAdapter) SetFinalStatus(status string) {
	a.am.mu.Lock()
	defer a.am.mu.Unlock()
	a.am.finalStatus = status
}

func (a *ApplicationMasterAdapter) SendShutdownSignal() {
	select {
	case a.am.shutdownHook <- struct{}{}:
	default:
		// 如果 channel 已满，则忽略
	}
}

func (a *ApplicationMasterAdapter) CalculateRequestedMemory() int64 {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	var total int64
	for _, req := range a.am.pendingRequests {
		total += req.Resource.Memory
	}
	return total
}

func (a *ApplicationMasterAdapter) CalculateAllocatedMemory() int64 {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	var total int64
	for _, container := range a.am.allocatedContainers {
		total += container.Resource.Memory
	}
	return total
}

func (a *ApplicationMasterAdapter) CalculateRequestedVCores() int32 {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	var total int32
	for _, req := range a.am.pendingRequests {
		total += req.Resource.VCores
	}
	return total
}

func (a *ApplicationMasterAdapter) CalculateAllocatedVCores() int32 {
	a.am.mu.RLock()
	defer a.am.mu.RUnlock()
	
	var total int32
	for _, container := range a.am.allocatedContainers {
		total += container.Resource.VCores
	}
	return total
}

// ApplicationMaster 原有的结构体定义（简化版）
type ApplicationMaster struct {
	mu                   sync.RWMutex
	applicationID        common.ApplicationID
	applicationAttemptID common.ApplicationAttemptID
	allocatedContainers  map[string]*common.Container
	completedContainers  map[string]*common.Container
	failedContainers     map[string]*common.Container
	pendingRequests      []*common.ContainerRequest
	applicationState     string
	finalStatus          string
	progress             float32
	trackingURL          string
	shutdownHook         chan struct{}
}

// 使用示例：
// 
// func main() {
//     logger := zap.NewExample()
//     
//     // 创建原有的 ApplicationMaster
//     am := &ApplicationMaster{
//         applicationID: common.ApplicationID{ClusterTimestamp: 1234567890, ID: 1},
//         // ... 其他初始化
//     }
//     
//     // 创建适配器
//     adapter := NewApplicationMasterAdapter(am)
//     
//     // 创建服务器示例
//     example := NewApplicationMasterServerExample(adapter, logger)
//     
//     // 启动服务器
//     if err := example.Start(8088); err != nil {
//         logger.Fatal("Failed to start servers", zap.Error(err))
//     }
//     
//     // 优雅关闭
//     defer example.Stop()
// }
