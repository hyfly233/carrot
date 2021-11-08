package nodemanager

import (
	"bytes"
	"carrot/internal/common"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// NodeManager 节点管理器
type NodeManager struct {
	mu                 sync.RWMutex
	nodeID             common.NodeID
	resourceManagerURL string
	totalResource      common.Resource
	usedResource       common.Resource
	containers         map[string]*Container
	httpServer         *http.Server
	heartbeatInterval  time.Duration
	stopChan           chan struct{}
}

// Container 容器
type Container struct {
	ID            common.ContainerID            `json:"id"`
	LaunchContext common.ContainerLaunchContext `json:"launch_context"`
	Resource      common.Resource               `json:"resource"`
	State         string                        `json:"state"`
	ExitCode      int                           `json:"exit_code"`
	Diagnostics   string                        `json:"diagnostics"`
	Process       *os.Process                   `json:"-"`
	StartTime     time.Time                     `json:"start_time"`
	FinishTime    time.Time                     `json:"finish_time,omitempty"`
}

// NewNodeManager 创建新的节点管理器
func NewNodeManager(nodeID common.NodeID, totalResource common.Resource, rmURL string) *NodeManager {
	return &NodeManager{
		nodeID:             nodeID,
		resourceManagerURL: rmURL,
		totalResource:      totalResource,
		usedResource:       common.Resource{Memory: 0, VCores: 0},
		containers:         make(map[string]*Container),
		heartbeatInterval:  3 * time.Second,
		stopChan:           make(chan struct{}),
	}
}

// Start 启动节点管理器
func (nm *NodeManager) Start(port int) error {
	// 注册到 ResourceManager
	if err := nm.registerWithRM(); err != nil {
		return fmt.Errorf("failed to register with RM: %v", err)
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

	log.Printf("NodeManager starting on port %d", port)
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

	container := &Container{
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

	log.Printf("Started container %v", containerID)
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

	log.Printf("Stopped container %v", containerID)
	return nil
}

// GetContainerStatus 获取容器状态
func (nm *NodeManager) GetContainerStatus(containerID common.ContainerID) (*Container, error) {
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
	registrationData := map[string]interface{}{
		"node_id":      nm.nodeID,
		"resource":     nm.totalResource,
		"http_address": fmt.Sprintf("http://%s:%d", nm.nodeID.Host, nm.nodeID.Port),
	}

	jsonData, err := json.Marshal(registrationData)
	if err != nil {
		return err
	}

	resp, err := http.Post(
		nm.resourceManagerURL+"/ws/v1/cluster/nodes/register",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration failed with status: %d", resp.StatusCode)
	}

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

	heartbeatData := map[string]interface{}{
		"node_id":       nm.nodeID,
		"used_resource": usedResource,
		"containers":    containers,
	}

	jsonData, err := json.Marshal(heartbeatData)
	if err != nil {
		log.Printf("Failed to marshal heartbeat data: %v", err)
		return
	}

	_, err = http.Post(
		nm.resourceManagerURL+"/ws/v1/cluster/nodes/heartbeat",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		log.Printf("Failed to send heartbeat: %v", err)
	}
}

func (nm *NodeManager) launchContainer(container *Container) error {
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

func (nm *NodeManager) monitorContainerProcess(container *Container, cmd *exec.Cmd) {
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

func (nm *NodeManager) stopContainer(container *Container) {
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
		containers := make([]*Container, 0, len(nm.containers))
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
