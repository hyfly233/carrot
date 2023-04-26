package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	rmpb "carrot/api/proto/resourcemanager"
	"carrot/internal/common"
)

// 为 gRPC 服务器定义所需的类型
type NodeInfo struct {
	NodeID        string
	Hostname      string
	IPAddress     string
	Port          int
	RackName      string
	Labels        []string
	LastHeartbeat time.Time
}

type ResourceCapability struct {
	MemoryMB int64
	VCores   int
}

type ResourceUsage struct {
	MemoryMB int64
	VCores   int
}

type ContainerStatus struct {
	ContainerID   string
	ApplicationID string
	State         string
	ExitCode      int
	Diagnostics   string
}

type ContainerAction struct {
	ContainerID string
	Action      string
	Reason      string
}

type Application struct {
	ApplicationID   common.ApplicationID
	ApplicationName string
	User            string
	Queue           string
	State           string
	Progress        float64
	TrackingURL     string
	Diagnostics     string
	StartTime       time.Time
	FinishTime      time.Time
	Priority        int
}

type Node struct {
	NodeID          string
	Hostname        string
	IPAddress       string
	Port            int
	RackName        string
	Labels          []string
	TotalCapability ResourceCapability
	UsedResources   ResourceUsage
	State           string
	NumContainers   int
	LastHeartbeat   time.Time
}

type ClusterInfo struct {
	ClusterID                     string
	RMVersion                     string
	ResourceManagerBuildVersion   string
	StartTime                     time.Time
	State                         string
	HAState                       string
	HAEnabled                     bool
	RMStateStore                  string
	ResourceManagerSchedulerClass string
}

type ClusterMetrics struct {
	AppsSubmitted        int
	AppsCompleted        int
	AppsPending          int
	AppsRunning          int
	AppsFailed           int
	AppsKilled           int
	ReservedMemory       int64
	AvailableMemory      int64
	AllocatedMemory      int64
	TotalMemory          int64
	ReservedVCores       int
	AvailableVCores      int
	AllocatedVCores      int
	TotalVCores          int
	ContainersAllocated  int
	ContainersReserved   int
	ContainersPending    int
	TotalNodes           int
	ActiveNodes          int
	LostNodes            int
	UnhealthyNodes       int
	DecommissioningNodes int
	DecommissionedNodes  int
	RebootedNodes        int
}

// ResourceManagerGRPCServer gRPC 服务器
type ResourceManagerGRPCServer struct {
	rmpb.UnimplementedResourceManagerServiceServer
	resourceManager ResourceManagerInterface
	grpcServer      *grpc.Server
	listener        net.Listener
	mu              sync.RWMutex
	heartbeatMap    map[string]*HeartbeatInfo // 心跳信息
}

// HeartbeatInfo 心跳信息
type HeartbeatInfo struct {
	LastHeartbeat time.Time
	SequenceID    int64
	Interval      int32
}

// NewResourceManagerGRPCServer 创建新的 gRPC 服务器
func NewResourceManagerGRPCServer(rm ResourceManagerInterface) *ResourceManagerGRPCServer {
	return &ResourceManagerGRPCServer{
		resourceManager: rm,
		heartbeatMap:    make(map[string]*HeartbeatInfo),
	}
}

// Start 启动 gRPC 服务器
func (s *ResourceManagerGRPCServer) Start(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("端口监听失败 %d: %v", port, err)
	}

	s.listener = lis
	s.grpcServer = grpc.NewServer()
	rmpb.RegisterResourceManagerServiceServer(s.grpcServer, s)

	log.Printf("ResourceManager gRPC 服务器正在端口 %d 上启动", port)
	err = s.grpcServer.Serve(lis)
	if err != nil {
		log.Printf("ResourceManager gRPC 服务器错误: %v", err)
	}
	return err
}

// Stop 停止 gRPC 服务器
func (s *ResourceManagerGRPCServer) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	if s.listener != nil {
		s.listener.Close()
	}
}

// RegisterNode 节点注册
func (s *ResourceManagerGRPCServer) RegisterNode(ctx context.Context, req *rmpb.RegisterNodeRequest) (*rmpb.RegisterNodeResponse, error) {
	if req.NodeInfo == nil {
		return nil, status.Error(codes.InvalidArgument, "node_info 是必需的")
	}

	// 转换为 common 包类型
	nodeID := common.NodeID{
		Host: req.NodeInfo.IpAddress,
		Port: req.NodeInfo.Port,
	}

	resource := common.Resource{
		Memory: req.TotalCapability.MemoryMb,
		VCores: req.TotalCapability.Vcores,
	}

	// 调用 ResourceManager 注册节点
	err := s.resourceManager.RegisterNode(nodeID, resource, req.HttpAddress)
	if err != nil {
		return &rmpb.RegisterNodeResponse{
			Success: false,
			Message: fmt.Sprintf("节点注册失败: %v", err),
		}, nil
	}

	// 初始化心跳信息
	s.mu.Lock()
	s.heartbeatMap[req.NodeInfo.NodeId] = &HeartbeatInfo{
		LastHeartbeat: time.Now(),
		SequenceID:    0,
		Interval:      30, // 30秒心跳间隔
	}
	s.mu.Unlock()

	return &rmpb.RegisterNodeResponse{
		Success:               true,
		Message:               "节点注册成功",
		NodeId:                req.NodeInfo.NodeId,
		RegistrationTimestamp: time.Now().Unix(),
	}, nil
}

// NodeHeartbeat 节点心跳
func (s *ResourceManagerGRPCServer) NodeHeartbeat(ctx context.Context, req *rmpb.NodeHeartbeatRequest) (*rmpb.NodeHeartbeatResponse, error) {
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id 是必需的")
	}

	// 更新心跳信息
	s.mu.Lock()
	heartbeatInfo, exists := s.heartbeatMap[req.NodeId]
	if !exists {
		s.mu.Unlock()
		return &rmpb.NodeHeartbeatResponse{
			Success:      false,
			Message:      "节点未注册",
			ShouldResync: true,
		}, nil
	}

	heartbeatInfo.LastHeartbeat = time.Now()
	heartbeatInfo.SequenceID++
	s.mu.Unlock()

	// 转换为 common 包类型
	nodeID := common.NodeID{
		Host: req.NodeId, // 简化处理，使用节点ID作为主机名
		Port: 8042,       // 默认端口
	}

	usedResource := common.Resource{
		Memory: req.UsedResources.MemoryMb,
		VCores: req.UsedResources.Vcores,
	}

	// 转换容器状态
	var containers []*common.Container
	for _, cs := range req.ContainerStatuses {
		containers = append(containers, &common.Container{
			ID: common.ContainerID{
				ApplicationAttemptID: common.ApplicationAttemptID{
					ApplicationID: common.ApplicationID{
						ClusterTimestamp: time.Now().Unix(),
						ID:               1,
					},
					AttemptID: 1,
				},
				ContainerID: 1,
			},
			NodeID: nodeID,
			Status: cs.Diagnostics,
			State:  convertContainerState(cs.State),
		})
	}

	// 处理心跳
	err := s.resourceManager.NodeHeartbeat(nodeID, usedResource, containers)
	if err != nil {
		return &rmpb.NodeHeartbeatResponse{
			Success: false,
			Message: fmt.Sprintf("心跳处理失败: %v", err),
		}, nil
	}

	return &rmpb.NodeHeartbeatResponse{
		Success:           true,
		Message:           "心跳处理成功",
		ContainerActions:  []*rmpb.ContainerAction{}, // 暂时返回空列表
		ResponseId:        heartbeatInfo.SequenceID,
		HeartbeatInterval: heartbeatInfo.Interval,
		ShouldResync:      false,
	}, nil
}

// GetNodes 获取节点列表
func (s *ResourceManagerGRPCServer) GetNodes(ctx context.Context, req *rmpb.GetNodesRequest) (*rmpb.GetNodesResponse, error) {
	nodes := s.resourceManager.GetNodes()

	var rmpbNodes []*rmpb.Node
	for _, nodeReport := range nodes {
		rmpbNode := &rmpb.Node{
			NodeInfo: &rmpb.NodeInfo{
				NodeId:        nodeReport.NodeID.HostPortString(),
				Hostname:      nodeReport.NodeID.Host,
				IpAddress:     nodeReport.NodeID.Host,
				Port:          nodeReport.NodeID.Port,
				RackName:      nodeReport.RackName,
				Labels:        nodeReport.NodeLabels,
				LastHeartbeat: timestamppb.New(nodeReport.LastHealthUpdate),
			},
			TotalCapability: &rmpb.ResourceCapability{
				MemoryMb: nodeReport.TotalResource.Memory,
				Vcores:   nodeReport.TotalResource.VCores,
			},
			UsedResources: &rmpb.ResourceUsage{
				MemoryMb: nodeReport.UsedResource.Memory,
				Vcores:   nodeReport.UsedResource.VCores,
			},
			State:         convertNodeStateFromString(nodeReport.State),
			NumContainers: nodeReport.NumContainers,
		}
		rmpbNodes = append(rmpbNodes, rmpbNode)
	}

	return &rmpb.GetNodesResponse{
		Nodes:      rmpbNodes,
		TotalCount: int32(len(rmpbNodes)),
	}, nil
}

// SubmitApplication 提交应用程序
func (s *ResourceManagerGRPCServer) SubmitApplication(ctx context.Context, req *rmpb.SubmitApplicationRequest) (*rmpb.SubmitApplicationResponse, error) {
	if req.ApplicationContext == nil {
		return nil, status.Error(codes.InvalidArgument, "application_context 是必需的")
	}

	// 转换为 common 包类型
	submissionCtx := common.ApplicationSubmissionContext{
		ApplicationName: req.ApplicationContext.ApplicationName,
		ApplicationType: "MAPREDUCE", // 默认类型
		Queue:           req.ApplicationContext.Queue,
		Priority:        req.ApplicationContext.Priority,
		Resource: common.Resource{
			Memory: req.ApplicationContext.AmResource.MemoryMb,
			VCores: req.ApplicationContext.AmResource.Vcores,
		},
		MaxAppAttempts: int32(req.ApplicationContext.MaxAppAttempts),
	}

	appID, err := s.resourceManager.SubmitApplication(submissionCtx)
	if err != nil {
		return &rmpb.SubmitApplicationResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to submit application: %v", err),
		}, nil
	}

	return &rmpb.SubmitApplicationResponse{
		Success:       true,
		Message:       "Application submitted successfully",
		ApplicationId: appID.String(),
	}, nil
}

// GetApplications 获取应用程序列表
func (s *ResourceManagerGRPCServer) GetApplications(ctx context.Context, req *rmpb.GetApplicationsRequest) (*rmpb.GetApplicationsResponse, error) {
	apps := s.resourceManager.GetApplications()

	var rmpbApps []*rmpb.Application
	for _, appReport := range apps {
		rmpbApp := &rmpb.Application{
			ApplicationId:   appReport.ApplicationID.String(),
			ApplicationName: appReport.ApplicationName,
			User:            appReport.User,
			Queue:           appReport.Queue,
			State:           convertApplicationStateFromString(appReport.State),
			Progress:        float64(appReport.Progress),
			TrackingUrl:     appReport.TrackingURL,
			Diagnostics:     "", // 暂时留空
			StartTime:       timestamppb.New(appReport.StartTime),
			Priority:        0, // 暂时设为0
		}

		if !appReport.FinishTime.IsZero() {
			rmpbApp.FinishTime = timestamppb.New(appReport.FinishTime)
		}

		rmpbApps = append(rmpbApps, rmpbApp)
	}

	return &rmpb.GetApplicationsResponse{
		Applications: rmpbApps,
		TotalCount:   int32(len(rmpbApps)),
	}, nil
}

// GetApplication 获取单个应用程序 (暂时不支持)
func (s *ResourceManagerGRPCServer) GetApplication(ctx context.Context, req *rmpb.GetApplicationRequest) (*rmpb.GetApplicationResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetApplication not implemented yet")
}

// KillApplication 终止应用程序 (暂时不支持)
func (s *ResourceManagerGRPCServer) KillApplication(ctx context.Context, req *rmpb.KillApplicationRequest) (*rmpb.KillApplicationResponse, error) {
	return nil, status.Error(codes.Unimplemented, "KillApplication not implemented yet")
}

// GetClusterInfo 获取集群信息 (暂时不支持)
func (s *ResourceManagerGRPCServer) GetClusterInfo(ctx context.Context, req *rmpb.GetClusterInfoRequest) (*rmpb.GetClusterInfoResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetClusterInfo not implemented yet")
}

// GetClusterMetrics 获取集群指标 (暂时不支持)
func (s *ResourceManagerGRPCServer) GetClusterMetrics(ctx context.Context, req *rmpb.GetClusterMetricsRequest) (*rmpb.GetClusterMetricsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetClusterMetrics not implemented yet")
}

// 辅助函数：类型转换

func convertContainerState(state rmpb.ContainerState) string {
	switch state {
	case rmpb.ContainerState_CONTAINER_STATE_NEW:
		return "NEW"
	case rmpb.ContainerState_CONTAINER_STATE_RUNNING:
		return "RUNNING"
	case rmpb.ContainerState_CONTAINER_STATE_COMPLETE:
		return "COMPLETE"
	case rmpb.ContainerState_CONTAINER_STATE_FAILED:
		return "FAILED"
	case rmpb.ContainerState_CONTAINER_STATE_KILLED:
		return "KILLED"
	default:
		return "UNKNOWN"
	}
}

func convertActionType(action string) rmpb.ActionType {
	switch action {
	case "LAUNCH":
		return rmpb.ActionType_ACTION_TYPE_LAUNCH
	case "STOP":
		return rmpb.ActionType_ACTION_TYPE_STOP
	case "KILL":
		return rmpb.ActionType_ACTION_TYPE_KILL
	case "CLEANUP":
		return rmpb.ActionType_ACTION_TYPE_CLEANUP
	default:
		return rmpb.ActionType_ACTION_TYPE_UNSPECIFIED
	}
}

func convertNodeState(state string) rmpb.NodeState {
	switch state {
	case "NEW":
		return rmpb.NodeState_NODE_STATE_NEW
	case "RUNNING":
		return rmpb.NodeState_NODE_STATE_RUNNING
	case "UNHEALTHY":
		return rmpb.NodeState_NODE_STATE_UNHEALTHY
	case "DECOMMISSIONING":
		return rmpb.NodeState_NODE_STATE_DECOMMISSIONING
	case "DECOMMISSIONED":
		return rmpb.NodeState_NODE_STATE_DECOMMISSIONED
	case "LOST":
		return rmpb.NodeState_NODE_STATE_LOST
	case "REBOOTED":
		return rmpb.NodeState_NODE_STATE_REBOOTED
	default:
		return rmpb.NodeState_NODE_STATE_UNSPECIFIED
	}
}

func convertNodeStateFromString(state string) rmpb.NodeState {
	switch state {
	case "NEW":
		return rmpb.NodeState_NODE_STATE_NEW
	case "RUNNING":
		return rmpb.NodeState_NODE_STATE_RUNNING
	case "UNHEALTHY":
		return rmpb.NodeState_NODE_STATE_UNHEALTHY
	case "DECOMMISSIONING":
		return rmpb.NodeState_NODE_STATE_DECOMMISSIONING
	case "DECOMMISSIONED":
		return rmpb.NodeState_NODE_STATE_DECOMMISSIONED
	case "LOST":
		return rmpb.NodeState_NODE_STATE_LOST
	case "REBOOTED":
		return rmpb.NodeState_NODE_STATE_REBOOTED
	default:
		return rmpb.NodeState_NODE_STATE_UNSPECIFIED
	}
}

func convertApplicationStateFromString(state string) rmpb.ApplicationState {
	switch state {
	case "NEW":
		return rmpb.ApplicationState_APPLICATION_STATE_NEW
	case "NEW_SAVING":
		return rmpb.ApplicationState_APPLICATION_STATE_NEW_SAVING
	case "SUBMITTED":
		return rmpb.ApplicationState_APPLICATION_STATE_SUBMITTED
	case "ACCEPTED":
		return rmpb.ApplicationState_APPLICATION_STATE_ACCEPTED
	case "RUNNING":
		return rmpb.ApplicationState_APPLICATION_STATE_RUNNING
	case "FINISHED":
		return rmpb.ApplicationState_APPLICATION_STATE_FINISHED
	case "FAILED":
		return rmpb.ApplicationState_APPLICATION_STATE_FAILED
	case "KILLED":
		return rmpb.ApplicationState_APPLICATION_STATE_KILLED
	default:
		return rmpb.ApplicationState_APPLICATION_STATE_UNSPECIFIED
	}
}
