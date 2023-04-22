package server

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	cmpb "carrot/api/proto/containermanager"
	"carrot/internal/common"
)

// ContainerManagerGRPCServer NodeManager 的容器管理 gRPC 服务器
type ContainerManagerGRPCServer struct {
	cmpb.UnimplementedContainerManagerServiceServer
	nm     NodeManagerInterface
	server *grpc.Server
}

// NewContainerManagerGRPCServer 创建新的容器管理 gRPC 服务器
func NewContainerManagerGRPCServer(nm NodeManagerInterface) *ContainerManagerGRPCServer {
	return &ContainerManagerGRPCServer{
		nm: nm,
	}
}

// StartGRPCServer 启动 gRPC 服务器
func (s *ContainerManagerGRPCServer) StartGRPCServer(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	s.server = grpc.NewServer()
	cmpb.RegisterContainerManagerServiceServer(s.server, s)

	log.Printf("Container Manager gRPC rmserver starting on port %d", port)
	return s.server.Serve(lis)
}

// StopGRPCServer 停止 gRPC 服务器
func (s *ContainerManagerGRPCServer) StopGRPCServer() {
	if s.server != nil {
		s.server.GracefulStop()
	}
}

// StartContainer 启动容器
func (s *ContainerManagerGRPCServer) StartContainer(ctx context.Context, req *cmpb.StartContainerRequest) (*cmpb.StartContainerResponse, error) {
	// 转换 ContainerID
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: 0, // 从 application_attempt_id 解析
				ID:               0,
			},
			AttemptID: 0,
		},
		ContainerID: req.ContainerId.ContainerId,
	}

	// 转换 LaunchContext
	launchContext := common.ContainerLaunchContext{
		Commands:    req.LaunchContext.Commands,
		Environment: req.LaunchContext.Environment,
	}

	// 转换 Resource
	resource := common.Resource{
		Memory: req.AllocatedResource.MemoryMb,
		VCores: req.AllocatedResource.Vcores,
	}

	// 调用 NodeManager 启动容器
	err := s.nm.StartContainer(containerID, launchContext, resource)
	if err != nil {
		return &cmpb.StartContainerResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to start container: %v", err),
		}, nil
	}

	return &cmpb.StartContainerResponse{
		Success:         true,
		Message:         "Container started successfully",
		NodeHttpAddress: "localhost:8042", // TODO: 从配置获取
	}, nil
}

// StopContainer 停止容器
func (s *ContainerManagerGRPCServer) StopContainer(ctx context.Context, req *cmpb.StopContainerRequest) (*cmpb.StopContainerResponse, error) {
	// 转换 ContainerID
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: 0,
				ID:               0,
			},
			AttemptID: 0,
		},
		ContainerID: req.ContainerId.ContainerId,
	}

	// 调用 NodeManager 停止容器
	err := s.nm.StopContainer(containerID)
	if err != nil {
		return &cmpb.StopContainerResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to stop container: %v", err),
		}, nil
	}

	return &cmpb.StopContainerResponse{
		Success: true,
		Message: "Container stopped successfully",
	}, nil
}

// GetContainerStatus 获取容器状态
func (s *ContainerManagerGRPCServer) GetContainerStatus(ctx context.Context, req *cmpb.GetContainerStatusRequest) (*cmpb.GetContainerStatusResponse, error) {
	// 转换 ContainerID
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: 0,
				ID:               0,
			},
			AttemptID: 0,
		},
		ContainerID: req.ContainerId.ContainerId,
	}

	// 获取容器状态
	container, err := s.nm.GetContainerStatus(containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get container status: %v", err)
	}

	// 转换状态
	var state cmpb.ContainerState
	switch container.State {
	case "NEW":
		state = cmpb.ContainerState_CONTAINER_NEW
	case "RUNNING":
		state = cmpb.ContainerState_CONTAINER_RUNNING
	case "COMPLETE":
		state = cmpb.ContainerState_CONTAINER_COMPLETE
	case "FAILED":
		state = cmpb.ContainerState_CONTAINER_FAILED
	case "KILLED":
		state = cmpb.ContainerState_CONTAINER_KILLED
	default:
		state = cmpb.ContainerState_CONTAINER_NEW
	}

	status := &cmpb.ContainerStatus{
		ContainerId: &cmpb.ContainerID{
			ApplicationAttemptId: fmt.Sprintf("%d_%d", containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp, containerID.ApplicationAttemptID.AttemptID),
			ContainerId:          containerID.ContainerID,
		},
		State:       state,
		ExitCode:    0, // 简化处理，使用默认值
		Diagnostics: container.Status,
		StartTime:   timestamppb.Now(), // 简化处理，使用当前时间
		FinishTime:  timestamppb.Now(),
		AllocatedResource: &cmpb.Resource{
			MemoryMb: container.Resource.Memory,
			Vcores:   container.Resource.VCores,
		},
		NodeHttpAddress: "localhost:8042", // TODO: 从配置获取
	}

	return &cmpb.GetContainerStatusResponse{
		ContainerStatus: status,
	}, nil
}

// GetContainerLogs 获取容器日志
func (s *ContainerManagerGRPCServer) GetContainerLogs(ctx context.Context, req *cmpb.GetContainerLogsRequest) (*cmpb.GetContainerLogsResponse, error) {
	// 转换 ContainerID
	containerID := common.ContainerID{
		ApplicationAttemptID: common.ApplicationAttemptID{
			ApplicationID: common.ApplicationID{
				ClusterTimestamp: 0,
				ID:               0,
			},
			AttemptID: 0,
		},
		ContainerID: req.ContainerId.ContainerId,
	}

	// 获取容器日志
	logs, err := s.nm.GetContainerLogs(containerID, req.LogType)
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %v", err)
	}

	return &cmpb.GetContainerLogsResponse{
		LogContent: logs,
		TotalSize:  int64(len(logs)),
		HasMore:    false, // 简化处理
	}, nil
}

// ListContainers 列出所有容器
func (s *ContainerManagerGRPCServer) ListContainers(ctx context.Context, req *cmpb.ListContainersRequest) (*cmpb.ListContainersResponse, error) {
	// 获取所有容器
	containers := s.nm.GetContainers()

	var pbContainers []*cmpb.Container
	for _, container := range containers {
		// 转换状态
		var state cmpb.ContainerState
		switch container.State {
		case "NEW":
			state = cmpb.ContainerState_CONTAINER_NEW
		case "RUNNING":
			state = cmpb.ContainerState_CONTAINER_RUNNING
		case "COMPLETE":
			state = cmpb.ContainerState_CONTAINER_COMPLETE
		case "FAILED":
			state = cmpb.ContainerState_CONTAINER_FAILED
		case "KILLED":
			state = cmpb.ContainerState_CONTAINER_KILLED
		default:
			state = cmpb.ContainerState_CONTAINER_NEW
		}

		// 状态过滤
		if req.StateFilter != cmpb.ContainerState_CONTAINER_NEW && req.StateFilter != state {
			continue
		}

		pbContainer := &cmpb.Container{
			Id: &cmpb.ContainerID{
				ApplicationAttemptId: fmt.Sprintf("%d_%d", container.ID.ApplicationAttemptID.ApplicationID.ClusterTimestamp, container.ID.ApplicationAttemptID.AttemptID),
				ContainerId:          container.ID.ContainerID,
			},
			NodeId: &cmpb.NodeID{
				Host: container.NodeID.Host,
				Port: container.NodeID.Port,
			},
			Resource: &cmpb.Resource{
				MemoryMb: container.Resource.Memory,
				Vcores:   container.Resource.VCores,
			},
			ContainerToken: "", // TODO: 实现 token
			State:          state,
			Diagnostics:    container.Status, // 使用 Status 字段
		}

		pbContainers = append(pbContainers, pbContainer)
	}

	return &cmpb.ListContainersResponse{
		Containers: pbContainers,
		TotalCount: int32(len(pbContainers)),
	}, nil
}
