package applicationmaster

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	cmpb "carrot/api/proto/containermanager"
	"carrot/internal/common"
)

// ContainerManagerGRPCClient ApplicationMaster 的容器管理 gRPC 客户端
type ContainerManagerGRPCClient struct {
	conn   *grpc.ClientConn
	client cmpb.ContainerManagerServiceClient
}

// NewContainerManagerGRPCClient 创建新的容器管理 gRPC 客户端
func NewContainerManagerGRPCClient(nodeManagerAddress string) (*ContainerManagerGRPCClient, error) {
	// 连接到 NodeManager 的容器管理 gRPC 服务
	conn, err := grpc.Dial(nodeManagerAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("连接到 NodeManager gRPC 服务失败: %v", err)
	}

	client := cmpb.NewContainerManagerServiceClient(conn)

	return &ContainerManagerGRPCClient{
		conn:   conn,
		client: client,
	}, nil
}

// Close 关闭 gRPC 连接
func (c *ContainerManagerGRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// StartContainer 启动容器
func (c *ContainerManagerGRPCClient) StartContainer(containerID *common.ContainerID, launchContext *common.ContainerLaunchContext, allocatedResource *common.Resource) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 转换为 protobuf 格式
	req := &cmpb.StartContainerRequest{
		ContainerId: &cmpb.ContainerID{
			ApplicationAttemptId: fmt.Sprintf("%d_%d",
				containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
				containerID.ApplicationAttemptID.AttemptID),
			ContainerId: containerID.ContainerID,
		},
		LaunchContext: &cmpb.ContainerLaunchContext{
			Commands:    launchContext.Commands,
			Environment: launchContext.Environment,
			LocalResources: func() []*cmpb.LocalResource {
				resources := make([]*cmpb.LocalResource, 0, len(launchContext.LocalResources))
				for _, resource := range launchContext.LocalResources {
					resources = append(resources, &cmpb.LocalResource{
						Url:        resource.URL,
						Size:       resource.Size,
						Timestamp:  timestamppb.New(time.Unix(resource.Timestamp, 0)),
						Type:       resource.Type,
						Visibility: resource.Visibility,
						Pattern:    resource.Pattern,
					})
				}
				return resources
			}(),
			ServiceData: func() map[string]string {
				data := make(map[string]string)
				for service, serviceData := range launchContext.ServiceData {
					data[service] = string(serviceData)
				}
				return data
			}(),
		},
		AllocatedResource: &cmpb.Resource{
			MemoryMb: allocatedResource.Memory,
			Vcores:   allocatedResource.VCores,
		},
	}

	// 调用 gRPC 服务
	resp, err := c.client.StartContainer(ctx, req)
	if err != nil {
		return fmt.Errorf("启动容器失败: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("启动容器失败: %s", resp.Message)
	}

	log.Printf("容器 %d 启动成功", containerID.ContainerID)
	return nil
}

// StopContainer 停止容器
func (c *ContainerManagerGRPCClient) StopContainer(containerID *common.ContainerID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &cmpb.StopContainerRequest{
		ContainerId: &cmpb.ContainerID{
			ApplicationAttemptId: fmt.Sprintf("%d_%d",
				containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
				containerID.ApplicationAttemptID.AttemptID),
			ContainerId: containerID.ContainerID,
		},
	}

	resp, err := c.client.StopContainer(ctx, req)
	if err != nil {
		return fmt.Errorf("停止容器失败: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("停止容器失败: %s", resp.Message)
	}

	log.Printf("容器 %d 停止成功", containerID.ContainerID)
	return nil
}

// GetContainerStatus 获取容器状态
func (c *ContainerManagerGRPCClient) GetContainerStatus(containerID *common.ContainerID) (*ContainerStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &cmpb.GetContainerStatusRequest{
		ContainerId: &cmpb.ContainerID{
			ApplicationAttemptId: fmt.Sprintf("%d_%d",
				containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
				containerID.ApplicationAttemptID.AttemptID),
			ContainerId: containerID.ContainerID,
		},
	}

	resp, err := c.client.GetContainerStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("获取容器状态失败: %v", err)
	}

	if resp.ContainerStatus == nil {
		return nil, fmt.Errorf("获取容器状态失败: 响应为空")
	}

	// 转换为内部格式
	status := &ContainerStatus{
		ContainerID: common.ContainerID{
			ApplicationAttemptID: common.ApplicationAttemptID{
				ApplicationID: common.ApplicationID{
					ClusterTimestamp: 0, // TODO: 从 ApplicationAttemptId 解析
				},
				AttemptID: 0, // TODO: 从 ApplicationAttemptId 解析
			},
			ContainerID: resp.ContainerStatus.ContainerId.ContainerId,
		},
		State:       resp.ContainerStatus.State.String(),
		ExitCode:    resp.ContainerStatus.ExitCode,
		Diagnostics: resp.ContainerStatus.Diagnostics,
	}

	return status, nil
}

// GetContainerLogs 获取容器日志
func (c *ContainerManagerGRPCClient) GetContainerLogs(containerID *common.ContainerID) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &cmpb.GetContainerLogsRequest{
		ContainerId: &cmpb.ContainerID{
			ApplicationAttemptId: fmt.Sprintf("%d_%d",
				containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
				containerID.ApplicationAttemptID.AttemptID),
			ContainerId: containerID.ContainerID,
		},
		LogType: "stdout", // 默认获取 stdout 日志
	}

	resp, err := c.client.GetContainerLogs(ctx, req)
	if err != nil {
		return "", fmt.Errorf("获取容器日志失败: %v", err)
	}

	return resp.LogContent, nil
}

// ListContainers 列出所有容器
func (c *ContainerManagerGRPCClient) ListContainers() ([]*common.Container, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &cmpb.ListContainersRequest{}

	resp, err := c.client.ListContainers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("列出容器失败: %v", err)
	}

	// 转换为内部格式
	containers := make([]*common.Container, len(resp.Containers))
	for i, pbContainer := range resp.Containers {
		containers[i] = &common.Container{
			ID: common.ContainerID{
				ApplicationAttemptID: common.ApplicationAttemptID{
					ApplicationID: common.ApplicationID{
						ClusterTimestamp: 0, // TODO: 从 ApplicationAttemptId 解析
					},
					AttemptID: 0, // TODO: 从 ApplicationAttemptId 解析
				},
				ContainerID: pbContainer.Id.ContainerId,
			},
			NodeID: common.NodeID{
				Host: pbContainer.NodeId.Host,
				Port: pbContainer.NodeId.Port,
			},
			Resource: common.Resource{
				Memory: pbContainer.Resource.MemoryMb,
				VCores: pbContainer.Resource.Vcores,
			},
			Status: pbContainer.Diagnostics,
			State:  pbContainer.State.String(),
		}
	}

	return containers, nil
}
