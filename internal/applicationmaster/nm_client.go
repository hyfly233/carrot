package applicationmaster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// NodeManagerClient NodeManager 客户端
type NodeManagerClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// ContainerStatus 容器状态
type ContainerStatus struct {
	ContainerID common.ContainerID `json:"container_id"`
	State       string             `json:"state"`
	ExitCode    int32              `json:"exit_code"`
	Diagnostics string             `json:"diagnostics"`
}

// StartContainerRequest 启动容器请求
type StartContainerRequest struct {
	ContainerID            common.ContainerID            `json:"container_id"`
	ContainerLaunchContext common.ContainerLaunchContext `json:"container_launch_context"`
}

// StopContainerRequest 停止容器请求
type StopContainerRequest struct {
	ContainerID common.ContainerID `json:"container_id"`
}

// GetContainerStatusRequest 获取容器状态请求
type GetContainerStatusRequest struct {
	ContainerID common.ContainerID `json:"container_id"`
}

// NewNodeManagerClient 创建新的 NodeManager 客户端
func NewNodeManagerClient(nmAddress string, logger *zap.Logger) *NodeManagerClient {
	return &NodeManagerClient{
		baseURL: nmAddress,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// StartContainer 启动容器
func (nm *NodeManagerClient) StartContainer(containerID common.ContainerID, launchContext *common.ContainerLaunchContext) error {
	url := fmt.Sprintf("%s/ws/v1/node/containers/%d/start", nm.baseURL, containerID.ContainerID)

	request := StartContainerRequest{
		ContainerID:            containerID,
		ContainerLaunchContext: *launchContext,
	}

	reqBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := nm.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("start container failed with status: %d", resp.StatusCode)
	}

	nm.logger.Info("Container start request sent",
		zap.String("container_id", fmt.Sprintf("%d", containerID.ContainerID)))

	return nil
}

// StopContainer 停止容器
func (nm *NodeManagerClient) StopContainer(containerID common.ContainerID) error {
	url := fmt.Sprintf("%s/ws/v1/node/containers/%d/stop", nm.baseURL, containerID.ContainerID)

	request := StopContainerRequest{
		ContainerID: containerID,
	}

	reqBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := nm.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("stop container failed with status: %d", resp.StatusCode)
	}

	nm.logger.Info("Container stop request sent",
		zap.String("container_id", fmt.Sprintf("%d", containerID.ContainerID)))

	return nil
}

// GetContainerStatus 获取容器状态
func (nm *NodeManagerClient) GetContainerStatus(containerID common.ContainerID) (*ContainerStatus, error) {
	url := fmt.Sprintf("%s/ws/v1/node/containers/%d/status", nm.baseURL, containerID.ContainerID)

	resp, err := nm.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get container status failed with status: %d", resp.StatusCode)
	}

	var response struct {
		ContainerStatus ContainerStatus `json:"container_status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response.ContainerStatus, nil
}

// GetContainerLogs 获取容器日志
func (nm *NodeManagerClient) GetContainerLogs(containerID common.ContainerID, logType string) (string, error) {
	url := fmt.Sprintf("%s/ws/v1/node/containers/%d/logs/%s", nm.baseURL, containerID.ContainerID, logType)

	resp, err := nm.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get container logs failed with status: %d", resp.StatusCode)
	}

	var response struct {
		Logs string `json:"logs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Logs, nil
}

// GetNodeStatus 获取节点状态
func (nm *NodeManagerClient) GetNodeStatus() (*NodeStatus, error) {
	url := fmt.Sprintf("%s/ws/v1/node/info", nm.baseURL)

	resp, err := nm.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get node status failed with status: %d", resp.StatusCode)
	}

	var response struct {
		NodeInfo NodeStatus `json:"nodeInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response.NodeInfo, nil
}

// ListContainers 列出容器
func (nm *NodeManagerClient) ListContainers() ([]*ContainerStatus, error) {
	url := fmt.Sprintf("%s/ws/v1/node/containers", nm.baseURL)

	resp, err := nm.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list containers failed with status: %d", resp.StatusCode)
	}

	var response struct {
		Containers []*ContainerStatus `json:"containers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Containers, nil
}

// NodeStatus 节点状态
type NodeStatus struct {
	ID                      common.NodeID   `json:"id"`
	NodeManagerVersion      string          `json:"nodeManagerVersion"`
	TotalCapability         common.Resource `json:"totalVmemAllocatedForContainers"`
	TotalUsed               common.Resource `json:"totalVmemUsedForContainers"`
	NodeHealthy             bool            `json:"nodeHealthy"`
	NodeManagerBuildVersion string          `json:"nodeManagerBuildVersion"`
	HadoopBuildVersion      string          `json:"hadoopBuildVersion"`
	HadoopVersion           string          `json:"hadoopVersion"`
	LastNodeUpdateTime      int64           `json:"lastNodeUpdateTime"`
	HealthReport            string          `json:"healthReport"`
	ActiveContainers        int32           `json:"containersRunning"`
}
