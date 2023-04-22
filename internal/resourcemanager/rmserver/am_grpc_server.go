package rmserver

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

	ampb "carrot/api/proto/applicationmaster"
	"carrot/internal/common"
)

// ApplicationMasterManager 定义 ApplicationMaster 管理接口，避免循环导入
type ApplicationMasterManager interface {
	RegisterApplicationMaster(appID common.ApplicationID, host string, rpcPort int32, trackingURL string) (common.Resource, error)
	AllocateContainers(appID common.ApplicationID, asks []*common.ContainerRequest, releases []common.ContainerID, completed []*common.Container, progress float32) ([]*common.Container, []common.NodeReport, error)
	FinishApplicationMaster(appID common.ApplicationID, finalStatus string, diagnostics string, trackingURL string) error
	GetApplicationReport(appID common.ApplicationID) (*common.ApplicationReport, error)
	GetClusterMetrics() (*common.ClusterMetrics, error)
}

// ApplicationMasterGRPCServer ResourceManager 为 ApplicationMaster 提供的 gRPC 服务器
type ApplicationMasterGRPCServer struct {
	ampb.UnimplementedApplicationMasterServiceServer
	mu               sync.RWMutex
	amManager        ApplicationMasterManager
	server           *grpc.Server
	allocateCounters map[string]int32 // 跟踪每个 AM 的分配请求序号
	running          bool
}

// NewApplicationMasterGRPCServer 创建新的 ApplicationMaster gRPC 服务器
func NewApplicationMasterGRPCServer(amManager ApplicationMasterManager) *ApplicationMasterGRPCServer {
	return &ApplicationMasterGRPCServer{
		amManager:        amManager,
		allocateCounters: make(map[string]int32),
		running:          false,
	}
}

// Start 启动 gRPC 服务器
func (s *ApplicationMasterGRPCServer) Start(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %v", port, err)
	}

	s.server = grpc.NewServer()
	ampb.RegisterApplicationMasterServiceServer(s.server, s)

	s.running = true
	log.Printf("ApplicationMaster gRPC rmserver starting on port %d", port)

	return s.server.Serve(lis)
}

// Stop 停止 gRPC 服务器
func (s *ApplicationMasterGRPCServer) Stop() {
	if s.server != nil {
		s.running = false
		s.server.GracefulStop()
		log.Printf("ApplicationMaster gRPC rmserver stopped")
	}
}

// RegisterApplicationMaster 注册 ApplicationMaster
func (s *ApplicationMasterGRPCServer) RegisterApplicationMaster(ctx context.Context, req *ampb.RegisterApplicationMasterRequest) (*ampb.RegisterApplicationMasterResponse, error) {
	if req.Host == "" {
		return nil, status.Error(codes.InvalidArgument, "host is required")
	}

	// TODO: 从上下文或其他方式获取应用程序 ID
	// 这里暂时使用模拟的应用程序 ID，实际实现中需要从请求上下文中获取
	appID := common.ApplicationID{
		ClusterTimestamp: time.Now().Unix(),
		ID:               1, // 这应该从实际的应用程序上下文中获取
	}

	// 调用 ApplicationMasterManager 的注册方法
	maxResource, err := s.amManager.RegisterApplicationMaster(
		appID,
		req.Host,
		req.RpcPort,
		req.TrackingUrl,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register ApplicationMaster: %v", err)
	}

	// 初始化分配计数器
	s.mu.Lock()
	s.allocateCounters[appID.String()] = 0
	s.mu.Unlock()

	log.Printf("ApplicationMaster registered: %s at %s:%d", appID.String(), req.Host, req.RpcPort)

	return &ampb.RegisterApplicationMasterResponse{
		MaximumResourceCapability: &ampb.Resource{
			MemoryMb: maxResource.Memory,
			Vcores:   maxResource.VCores,
		},
		ApplicationAcls: []string{}, // 暂时为空，实际实现中需要填充 ACL
		Queue:           "default",  // 默认队列
	}, nil
}

// Allocate 处理资源分配请求
func (s *ApplicationMasterGRPCServer) Allocate(ctx context.Context, req *ampb.AllocateRequest) (*ampb.AllocateResponse, error) {
	// TODO: 从上下文获取应用程序 ID，这里暂时使用模拟方式
	appID := common.ApplicationID{
		ClusterTimestamp: time.Now().Unix(),
		ID:               1,
	}

	// 更新分配计数器
	s.mu.Lock()
	s.allocateCounters[appID.String()]++
	responseID := s.allocateCounters[appID.String()]
	s.mu.Unlock()

	// 转换容器请求
	var containerRequests []*common.ContainerRequest
	for _, ask := range req.Ask {
		containerRequests = append(containerRequests, &common.ContainerRequest{
			Priority: ask.Priority,
			Resource: common.Resource{
				Memory: ask.Capability.MemoryMb,
				VCores: ask.Capability.Vcores,
			},
			Locality:  "", // protobuf 中没有直接对应
			NodeLabel: ask.NodeLabelExpression,
		})
	}

	// 转换释放的容器 ID
	var releaseContainers []common.ContainerID
	for _, release := range req.Release {
		releaseContainers = append(releaseContainers, common.ContainerID{
			ApplicationAttemptID: common.ApplicationAttemptID{
				ApplicationID: appID,
				AttemptID:     1, // 简化处理
			},
			ContainerID: release.ContainerId,
		})
	}

	// 转换已完成的容器状态
	var completedContainers []*common.Container
	for _, completed := range req.CompletedContainers {
		completedContainers = append(completedContainers, &common.Container{
			ID: common.ContainerID{
				ApplicationAttemptID: common.ApplicationAttemptID{
					ApplicationID: appID,
					AttemptID:     1,
				},
				ContainerID: completed.ContainerId.ContainerId,
			},
			Status: completed.Diagnostics,
			State:  completed.Diagnostics, // 简化处理
		})
	}

	// 调用 ApplicationMasterManager 的分配方法
	allocatedContainers, updatedNodes, err := s.amManager.AllocateContainers(
		appID,
		containerRequests,
		releaseContainers,
		completedContainers,
		req.Progress,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "allocation failed: %v", err)
	}

	// 转换响应
	var pbAllocatedContainers []*ampb.Container
	for _, container := range allocatedContainers {
		pbAllocatedContainers = append(pbAllocatedContainers, &ampb.Container{
			Id: &ampb.ContainerID{
				ApplicationAttemptId: fmt.Sprintf("%s_%d", container.ID.ApplicationAttemptID.ApplicationID.String(), container.ID.ApplicationAttemptID.AttemptID),
				ContainerId:          container.ID.ContainerID,
			},
			NodeId: &ampb.NodeID{
				Host: container.NodeID.Host,
				Port: container.NodeID.Port,
			},
			NodeHttpAddress: fmt.Sprintf("http://%s:%d", container.NodeID.Host, container.NodeID.Port),
			Resource: &ampb.Resource{
				MemoryMb: container.Resource.Memory,
				Vcores:   container.Resource.VCores,
			},
			Priority: 1, // 简化处理
		})
	}

	var pbUpdatedNodes []*ampb.NodeReport
	for _, node := range updatedNodes {
		pbUpdatedNodes = append(pbUpdatedNodes, &ampb.NodeReport{
			NodeId: &ampb.NodeID{
				Host: node.NodeID.Host,
				Port: node.NodeID.Port,
			},
			NodeHttpAddress: fmt.Sprintf("http://%s:%d", node.NodeID.Host, node.NodeID.Port),
			Used: &ampb.Resource{
				MemoryMb: node.UsedResource.Memory,
				Vcores:   node.UsedResource.VCores,
			},
			Capability: &ampb.Resource{
				MemoryMb: node.TotalResource.Memory,
				Vcores:   node.TotalResource.VCores,
			},
			NumContainers: int32(len(node.Containers)),
			NodeState:     int32(1), // 简化处理，假设节点健康
		})
	}

	return &ampb.AllocateResponse{
		AllocatedContainers: pbAllocatedContainers,
		CompletedContainers: []*ampb.ContainerStatus{}, // 暂时为空
		Limit:               100,                       // 硬编码限制
		UpdatedNodes:        pbUpdatedNodes,
		NumClusterNodes:     int32(len(updatedNodes)),
		AvailableResources: &ampb.Resource{
			MemoryMb: 8192, // 硬编码可用资源，实际应该从集群状态获取
			Vcores:   8,
		},
		ResponseId: responseID,
	}, nil
}

// FinishApplicationMaster 完成 ApplicationMaster
func (s *ApplicationMasterGRPCServer) FinishApplicationMaster(ctx context.Context, req *ampb.FinishApplicationMasterRequest) (*ampb.FinishApplicationMasterResponse, error) {
	// TODO: 从上下文获取应用程序 ID
	appID := common.ApplicationID{
		ClusterTimestamp: time.Now().Unix(),
		ID:               1,
	}

	// 调用 ApplicationMasterManager 的完成方法
	err := s.amManager.FinishApplicationMaster(
		appID,
		req.FinalApplicationStatus,
		req.Diagnostics,
		req.TrackingUrl,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to finish ApplicationMaster: %v", err)
	}

	// 清理分配计数器
	s.mu.Lock()
	delete(s.allocateCounters, appID.String())
	s.mu.Unlock()

	log.Printf("ApplicationMaster finished: %s", appID.String())

	return &ampb.FinishApplicationMasterResponse{
		IsUnregistered: true,
	}, nil
}

// GetApplicationReport 获取应用程序报告
func (s *ApplicationMasterGRPCServer) GetApplicationReport(ctx context.Context, req *ampb.GetApplicationReportRequest) (*ampb.GetApplicationReportResponse, error) {
	if req.ApplicationId == nil {
		return nil, status.Error(codes.InvalidArgument, "application_id is required")
	}

	appID := common.ApplicationID{
		ClusterTimestamp: req.ApplicationId.ClusterTimestamp,
		ID:               req.ApplicationId.Id,
	}

	// 调用 ApplicationMasterManager 获取应用程序报告
	report, err := s.amManager.GetApplicationReport(appID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "应用未找到: %v", err)
	}

	return &ampb.GetApplicationReportResponse{
		ApplicationReport: &ampb.ApplicationReport{
			ApplicationId: &ampb.ApplicationID{
				ClusterTimestamp: report.ApplicationID.ClusterTimestamp,
				Id:               report.ApplicationID.ID,
			},
			CurrentApplicationAttemptId: &ampb.ApplicationAttemptID{
				ApplicationId: &ampb.ApplicationID{
					ClusterTimestamp: report.ApplicationID.ClusterTimestamp,
					Id:               report.ApplicationID.ID,
				},
				AttemptId: 1, // 简化处理
			},
			User:                   report.User,
			Queue:                  report.Queue,
			Name:                   report.ApplicationName,
			Host:                   report.Host,
			RpcPort:                int32(report.RPCPort),
			TrackingUrl:            report.TrackingURL,
			ApplicationType:        report.ApplicationType,
			YarnApplicationState:   int32(1), // 简化处理
			FinalApplicationStatus: report.FinalStatus,
			Progress:               report.Progress,
			StartTime:              timestamppb.New(report.StartTime),
			FinishTime:             timestamppb.New(report.FinishTime),
			Diagnostics:            report.Diagnostics,
		},
	}, nil
}

// GetClusterMetrics 获取集群指标
func (s *ApplicationMasterGRPCServer) GetClusterMetrics(ctx context.Context, req *ampb.GetClusterMetricsRequest) (*ampb.GetClusterMetricsResponse, error) {
	// 调用 ApplicationMasterManager 获取集群指标
	metrics, err := s.amManager.GetClusterMetrics()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get cluster metrics: %v", err)
	}

	return &ampb.GetClusterMetricsResponse{
		ClusterMetrics: &ampb.ClusterMetrics{
			AppsSubmitted:         int32(metrics.AppsSubmitted),
			AppsCompleted:         int32(metrics.AppsCompleted),
			AppsPending:           int32(metrics.AppsPending),
			AppsRunning:           int32(metrics.AppsRunning),
			AppsFailed:            int32(metrics.AppsFailed),
			AppsKilled:            int32(metrics.AppsKilled),
			ActiveNodes:           int32(metrics.ActiveNodes),
			LostNodes:             int32(metrics.LostNodes),
			UnhealthyNodes:        int32(metrics.UnhealthyNodes),
			DecommissionedNodes:   int32(metrics.DecommissionedNodes),
			TotalNodes:            int32(metrics.TotalNodes),
			ReservedVirtualCores:  int32(metrics.ReservedVirtualCores),
			AvailableVirtualCores: int32(metrics.AvailableVirtualCores),
			AllocatedVirtualCores: int32(metrics.AllocatedVirtualCores),
		},
	}, nil
}
