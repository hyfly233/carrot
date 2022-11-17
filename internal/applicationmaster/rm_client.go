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

// ResourceManagerClient ResourceManager 客户端
type ResourceManagerClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// RegisterApplicationMasterRequest 注册 ApplicationMaster 请求
type RegisterApplicationMasterRequest struct {
	Host        string `json:"host"`
	RPCPort     int32  `json:"rpc_port"`
	TrackingURL string `json:"tracking_url"`
}

// RegisterApplicationMasterResponse 注册 ApplicationMaster 响应
type RegisterApplicationMasterResponse struct {
	MaximumResourceCapability common.Resource   `json:"maximum_resource_capability"`
	ApplicationACLs           map[string]string `json:"application_acls"`
	Queue                     string            `json:"queue"`
}

// AllocateRequest 分配请求
type AllocateRequest struct {
	Ask                 []*common.ContainerRequest `json:"ask"`
	Release             []common.ContainerID       `json:"release"`
	CompletedContainers []*common.Container        `json:"completed_containers"`
	Progress            float32                    `json:"progress"`
}

// AllocateResponse 分配响应
type AllocateResponse struct {
	AllocatedContainers []*common.Container `json:"allocated_containers"`
	CompletedContainers []*common.Container `json:"completed_containers"`
	Limit               int32               `json:"limit"`
	UpdatedNodes        []common.NodeReport `json:"updated_nodes"`
	NumClusterNodes     int32               `json:"num_cluster_nodes"`
}

// FinishApplicationMasterRequest 完成 ApplicationMaster 请求
type FinishApplicationMasterRequest struct {
	FinalApplicationStatus string `json:"final_application_status"`
	Diagnostics            string `json:"diagnostics"`
	TrackingURL            string `json:"tracking_url"`
}

// FinishApplicationMasterResponse 完成 ApplicationMaster 响应
type FinishApplicationMasterResponse struct {
	IsUnregistered bool `json:"is_unregistered"`
}

// NewResourceManagerClient 创建新的 ResourceManager 客户端
func NewResourceManagerClient(rmAddress string, logger *zap.Logger) *ResourceManagerClient {
	return &ResourceManagerClient{
		baseURL: rmAddress,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// RegisterApplicationMaster 注册 ApplicationMaster
func (rm *ResourceManagerClient) RegisterApplicationMaster(request *RegisterApplicationMasterRequest) (*RegisterApplicationMasterResponse, error) {
	url := fmt.Sprintf("%s/ws/v1/cluster/apps/new-application", rm.baseURL)

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := rm.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registration failed with status: %d", resp.StatusCode)
	}

	var response RegisterApplicationMasterResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	rm.logger.Info("ApplicationMaster registered successfully",
		zap.String("queue", response.Queue))

	return &response, nil
}

// Allocate 发送分配请求
func (rm *ResourceManagerClient) Allocate(request *AllocateRequest) (*AllocateResponse, error) {
	url := fmt.Sprintf("%s/ws/v1/cluster/apps/allocate", rm.baseURL)

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := rm.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("allocation failed with status: %d", resp.StatusCode)
	}

	var response AllocateResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	rm.logger.Debug("Allocation response received",
		zap.Int("allocated_containers", len(response.AllocatedContainers)),
		zap.Int("completed_containers", len(response.CompletedContainers)))

	return &response, nil
}

// FinishApplicationMaster 完成 ApplicationMaster
func (rm *ResourceManagerClient) FinishApplicationMaster(request *FinishApplicationMasterRequest) (*FinishApplicationMasterResponse, error) {
	url := fmt.Sprintf("%s/ws/v1/cluster/apps/finish", rm.baseURL)

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := rm.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("finish failed with status: %d", resp.StatusCode)
	}

	var response FinishApplicationMasterResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	rm.logger.Info("ApplicationMaster finished successfully",
		zap.Bool("unregistered", response.IsUnregistered))

	return &response, nil
}

// GetApplicationReport 获取应用程序报告
func (rm *ResourceManagerClient) GetApplicationReport(appID common.ApplicationID) (*common.ApplicationReport, error) {
	url := fmt.Sprintf("%s/ws/v1/cluster/apps/%d_%d", rm.baseURL, appID.ClusterTimestamp, appID.ID)

	resp, err := rm.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get application report failed with status: %d", resp.StatusCode)
	}

	var response struct {
		App common.ApplicationReport `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response.App, nil
}

// GetClusterMetrics 获取集群指标
func (rm *ResourceManagerClient) GetClusterMetrics() (*ClusterMetrics, error) {
	url := fmt.Sprintf("%s/ws/v1/cluster/metrics", rm.baseURL)

	resp, err := rm.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get cluster metrics failed with status: %d", resp.StatusCode)
	}

	var response struct {
		ClusterMetrics ClusterMetrics `json:"clusterMetrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response.ClusterMetrics, nil
}

// ClusterMetrics 集群指标
type ClusterMetrics struct {
	AppsSubmitted         int32 `json:"appsSubmitted"`
	AppsCompleted         int32 `json:"appsCompleted"`
	AppsPending           int32 `json:"appsPending"`
	AppsRunning           int32 `json:"appsRunning"`
	AppsFailed            int32 `json:"appsFailed"`
	AppsKilled            int32 `json:"appsKilled"`
	ReservedMB            int64 `json:"reservedMB"`
	AvailableMB           int64 `json:"availableMB"`
	AllocatedMB           int64 `json:"allocatedMB"`
	ReservedVirtualCores  int32 `json:"reservedVirtualCores"`
	AvailableVirtualCores int32 `json:"availableVirtualCores"`
	AllocatedVirtualCores int32 `json:"allocatedVirtualCores"`
	ContainersAllocated   int32 `json:"containersAllocated"`
	ContainersReserved    int32 `json:"containersReserved"`
	ContainersPending     int32 `json:"containersPending"`
	TotalMB               int64 `json:"totalMB"`
	TotalVirtualCores     int32 `json:"totalVirtualCores"`
	TotalNodes            int32 `json:"totalNodes"`
	LostNodes             int32 `json:"lostNodes"`
	UnhealthyNodes        int32 `json:"unhealthyNodes"`
	DecommissionedNodes   int32 `json:"decommissionedNodes"`
	RebootedNodes         int32 `json:"rebootedNodes"`
	ActiveNodes           int32 `json:"activeNodes"`
}
