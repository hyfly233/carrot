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
	"sync"
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
