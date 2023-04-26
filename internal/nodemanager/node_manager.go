package nodemanager

import (
	"carrot/internal/nodemanager/client"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"carrot/internal/common"
	"carrot/internal/nodemanager/containermanager"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// NodeManager 节点管理器
type NodeManager struct {
	mu                     sync.RWMutex
	nodeID                 common.NodeID
	resourceManagerURL     string
	resourceManagerGRPCURL string
	grpcClient             *client.GRPCClient
	totalResource          common.Resource
	usedResource           common.Resource
	containers             map[string]*containermanager.Container
	httpServer             *http.Server
	heartbeatInterval      time.Duration
	stopChan               chan struct{}
	logger                 *zap.Logger
}

// NewNodeManager 创建新的节点管理器
func NewNodeManager(nodeID common.NodeID, totalResource common.Resource, rmURL, rmGRPCURL string) *NodeManager {
	return &NodeManager{
		nodeID:                 nodeID,
		resourceManagerURL:     rmURL,
		resourceManagerGRPCURL: rmGRPCURL,
		totalResource:          totalResource,
		usedResource:           common.Resource{Memory: 0, VCores: 0},
		containers:             make(map[string]*containermanager.Container),
		heartbeatInterval:      3 * time.Second,
		stopChan:               make(chan struct{}),
		logger:                 common.ComponentLogger(fmt.Sprintf("nm-%s", nodeID.HostPortString())),
	}
}

// Start 启动节点管理器
func (nm *NodeManager) Start(port int) error {
	// 初始化 gRPC 客户端
	grpcClient := client.NewGRPCClient(nm.nodeID.HostPortString(), nm.resourceManagerGRPCURL)
	if err := grpcClient.Connect(); err != nil {
		nm.logger.Warn("无法通过 gRPC 连接到 RM", zap.Error(err))
	}

	// 注册到 ResourceManager
	if err := nm.registerWithRM(); err != nil {
		return fmt.Errorf("无法在 RM 注册: %v", err)
	}

	// 启动 HTTP 服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/v1/node/containers", nm.handleContainers)
	mux.HandleFunc("/ws/v1/node/containers/", nm.handleContainer)

	nm.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// 启动心跳
	go nm.startHeartbeat()

	// 启动容器监控
	go nm.monitorContainers()

	nm.logger.Info("NodeManager starting", zap.Int("port", port))
	return nm.httpServer.ListenAndServe()
}

// Stop 停止节点管理器
func (nm *NodeManager) Stop() error {
	close(nm.stopChan)

	// 停止所有容器
	nm.mu.Lock()
	for _, container := range nm.containers {
		nm.stopContainer(container)
	}
	nm.mu.Unlock()

	// 关闭 gRPC 连接
	if nm.grpcClient != nil {
		nm.grpcClient.Disconnect()
	}

	if nm.httpServer != nil {
		return nm.httpServer.Shutdown(context.Background())
	}
	return nil
}

// StartContainer 启动容器
func (nm *NodeManager) StartContainer(containerID common.ContainerID, launchContext common.ContainerLaunchContext, resource common.Resource) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	containerKey := nm.getContainerKey(containerID)

	// 检查资源是否足够
	if nm.usedResource.Memory+resource.Memory > nm.totalResource.Memory ||
		nm.usedResource.VCores+resource.VCores > nm.totalResource.VCores {
		return fmt.Errorf("insufficient resources")
	}

	container := &containermanager.Container{
		ID:            containerID,
		LaunchContext: launchContext,
		Resource:      resource,
		State:         common.ContainerStateNew,
		StartTime:     time.Now(),
	}

	// 启动容器进程
	if err := nm.launchContainer(container); err != nil {
		return fmt.Errorf("failed to launch container: %v", err)
	}

	nm.containers[containerKey] = container
	nm.usedResource.Memory += resource.Memory
	nm.usedResource.VCores += resource.VCores

	nm.logger.Info("Started container", zap.Any("container_id", containerID))
	return nil
}

// StopContainer 停止容器
func (nm *NodeManager) StopContainer(containerID common.ContainerID) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	containerKey := nm.getContainerKey(containerID)
	container, exists := nm.containers[containerKey]
	if !exists {
		return fmt.Errorf("container not found")
	}

	nm.stopContainer(container)
	delete(nm.containers, containerKey)

	nm.usedResource.Memory -= container.Resource.Memory
	nm.usedResource.VCores -= container.Resource.VCores

	nm.logger.Info("Stopped container", zap.Any("container_id", containerID))
	return nil
}

// GetContainerStatus 获取容器状态
func (nm *NodeManager) GetContainerStatus(containerID common.ContainerID) (*containermanager.Container, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	containerKey := nm.getContainerKey(containerID)
	container, exists := nm.containers[containerKey]
	if !exists {
		return nil, fmt.Errorf("container not found")
	}

	return container, nil
}

func (nm *NodeManager) registerWithRM() error {
	nodeInfo := &client.NodeInfo{
		NodeID:    nm.nodeID.HostPortString(),
		Hostname:  nm.nodeID.Host,
		IPAddress: nm.nodeID.Host,
		Port:      int(nm.nodeID.Port),
		RackName:  "default-rack",
		Labels:    []string{},
	}

	capability := &client.ResourceCapability{
		MemoryMB: nm.totalResource.Memory,
		VCores:   int(nm.totalResource.VCores),
	}

	httpAddress := fmt.Sprintf("http://%s:%d", nm.nodeID.Host, nm.nodeID.Port)

	err := nm.grpcClient.RegisterNode(nodeInfo, capability, httpAddress)
	if err != nil {
		nm.logger.Warn("gRPC 注册失败", zap.Error(err))
	}
	nm.logger.Info("已通过 gRPC 成功向 RM 注册")
	return nil

}

func (nm *NodeManager) startHeartbeat() {
	ticker := time.NewTicker(nm.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			nm.sendHeartbeat()
		case <-nm.stopChan:
			return
		}
	}
}

func (nm *NodeManager) sendHeartbeat() {
	nm.mu.RLock()
	containers := make([]*common.Container, 0, len(nm.containers))
	for _, container := range nm.containers {
		containers = append(containers, &common.Container{
			ID:       container.ID,
			NodeID:   nm.nodeID,
			Resource: container.Resource,
			Status:   container.State,
			State:    container.State,
		})
	}
	usedResource := nm.usedResource
	nm.mu.RUnlock()

	// 尝试使用 gRPC 发送心跳
	usage := &client.ResourceUsage{
		MemoryMB: usedResource.Memory,
		VCores:   int(usedResource.VCores),
	}

	containerStatuses := make([]*client.ContainerStatus, len(containers))
	for i, container := range containers {
		containerStatuses[i] = &client.ContainerStatus{
			ContainerID: fmt.Sprintf("container_%d_%d_%d",
				container.ID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
				container.ID.ApplicationAttemptID.ApplicationID.ID,
				container.ID.ContainerID),
			ApplicationID: fmt.Sprintf("application_%d_%d",
				container.ID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
				container.ID.ApplicationAttemptID.ApplicationID.ID),
			State:    container.State,
			ExitCode: 0,
		}
	}

	if _, err := nm.grpcClient.SendHeartbeat(usage, containerStatuses); err != nil {
		nm.logger.Warn("gRPC 心跳失败", zap.Error(err))
	}
	return // gRPC 心跳成功，直接返回
}

func (nm *NodeManager) launchContainer(container *containermanager.Container) error {
	if len(container.LaunchContext.Commands) == 0 {
		return fmt.Errorf("no commands specified")
	}

	// 创建容器工作目录
	workDir := fmt.Sprintf("/tmp/yarn-containers/%s", nm.getContainerKey(container.ID))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %v", err)
	}

	// 准备命令
	command := container.LaunchContext.Commands[0]
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = workDir

	// 设置环境变量
	env := os.Environ()
	for key, value := range container.LaunchContext.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	cmd.Env = env

	// 启动进程
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %v", err)
	}

	container.Process = cmd.Process
	container.State = common.ContainerStateRunning

	// 监控进程
	go nm.monitorContainerProcess(container, cmd)

	return nil
}

func (nm *NodeManager) monitorContainerProcess(container *containermanager.Container, cmd *exec.Cmd) {
	err := cmd.Wait()

	nm.mu.Lock()
	defer nm.mu.Unlock()

	container.FinishTime = time.Now()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			container.ExitCode = exitError.ExitCode()
		} else {
			container.ExitCode = -1
		}
		container.Diagnostics = err.Error()
	}
	container.State = common.ContainerStateComplete
}

func (nm *NodeManager) stopContainer(container *containermanager.Container) {
	if container.Process != nil {
		// 先尝试优雅关闭
		container.Process.Signal(syscall.SIGTERM)

		// 等待 5 秒
		time.Sleep(5 * time.Second)

		// 强制关闭
		container.Process.Kill()
	}
	container.State = common.ContainerStateComplete
	container.FinishTime = time.Now()
}

func (nm *NodeManager) monitorContainers() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			nm.cleanupFinishedContainers()
		case <-nm.stopChan:
			return
		}
	}
}

func (nm *NodeManager) cleanupFinishedContainers() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	for key, container := range nm.containers {
		if container.State == common.ContainerStateComplete {
			// 清理完成的容器（这里可以添加更多清理逻辑）
			workDir := fmt.Sprintf("/tmp/yarn-containers/%s", key)
			os.RemoveAll(workDir)

			// 可以选择是否立即删除容器记录，或保留一段时间用于查询
			// delete(nm.containers, key)
		}
	}
}

func (nm *NodeManager) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ContainerID)
}

func (nm *NodeManager) handleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nm.mu.RLock()
		containers := make([]*containermanager.Container, 0, len(nm.containers))
		for _, container := range nm.containers {
			containers = append(containers, container)
		}
		nm.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"containers": containers,
		})
	case http.MethodPost:
		var request struct {
			ContainerID   common.ContainerID            `json:"container_id"`
			LaunchContext common.ContainerLaunchContext `json:"launch_context"`
			Resource      common.Resource               `json:"resource"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := nm.StartContainer(request.ContainerID, request.LaunchContext, request.Resource)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (nm *NodeManager) handleContainer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerIDStr := vars["containerId"]

	if containerIDStr == "" {
		http.Error(w, "Container ID 是必需的", http.StatusBadRequest)
		return
	}

	// 解析容器ID
	containerID, err := common.ParseContainerID(containerIDStr)
	if err != nil {
		nm.logger.Error("Invalid container ID", zap.String("container_id", containerIDStr), zap.Error(err))
		http.Error(w, "Invalid container ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		nm.handleGetContainer(w, r, containerID)
	case http.MethodDelete:
		nm.handleDeleteContainer(w, r, containerID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (nm *NodeManager) handleGetContainer(w http.ResponseWriter, r *http.Request, containerID common.ContainerID) {
	nm.mu.RLock()
	container, exists := nm.containers[nm.getContainerKey(containerID)]
	nm.mu.RUnlock()

	if !exists {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	response := struct {
		ContainerID common.ContainerID `json:"container_id"`
		State       string             `json:"state"`
		Resource    common.Resource    `json:"resource"`
	}{
		ContainerID: container.ID,
		State:       string(container.State),
		Resource:    container.Resource,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (nm *NodeManager) handleDeleteContainer(w http.ResponseWriter, r *http.Request, containerID common.ContainerID) {
	err := nm.StopContainer(containerID)
	if err != nil {
		nm.logger.Error("Failed to stop container", zap.Any("container_id", containerID), zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
