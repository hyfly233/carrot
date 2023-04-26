package client

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ampb "carrot/api/proto/applicationmaster"
	"carrot/internal/common"
)

// ApplicationMasterGRPCClient ApplicationMaster 的 gRPC 客户端，用于与 ResourceManager 通信
type ApplicationMasterGRPCClient struct {
	conn      *grpc.ClientConn
	client    ampb.ApplicationMasterServiceClient
	appID     common.ApplicationID
	rmAddress string
	connected bool
}

// NewApplicationMasterGRPCClient 创建新的 ApplicationMaster gRPC 客户端
func NewApplicationMasterGRPCClient(appID common.ApplicationID, rmAddress string) *ApplicationMasterGRPCClient {
	return &ApplicationMasterGRPCClient{
		appID:     appID,
		rmAddress: rmAddress,
		connected: false,
	}
}

// Connect 连接到 ResourceManager
func (c *ApplicationMasterGRPCClient) Connect() error {
	conn, err := grpc.Dial(c.rmAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to ResourceManager: %v", err)
	}

	c.conn = conn
	c.client = ampb.NewApplicationMasterServiceClient(conn)
	c.connected = true

	log.Printf("Connected to ResourceManager at %s", c.rmAddress)
	return nil
}

// Disconnect 断开连接
func (c *ApplicationMasterGRPCClient) Disconnect() error {
	if c.conn != nil {
		c.connected = false
		return c.conn.Close()
	}
	return nil
}

// RegisterApplicationMaster 注册 ApplicationMaster
func (c *ApplicationMasterGRPCClient) RegisterApplicationMaster(host string, rpcPort int32, trackingURL string) (*ampb.RegisterApplicationMasterResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("无法连接到 RM")
	}

	req := &ampb.RegisterApplicationMasterRequest{
		Host:        host,
		RpcPort:     rpcPort,
		TrackingUrl: trackingURL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.client.RegisterApplicationMaster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to register ApplicationMaster: %v", err)
	}

	log.Printf("ApplicationMaster 注册成功")

	// 直接返回 protobuf 响应
	return resp, nil
}

// Allocate 发送资源分配请求
func (c *ApplicationMasterGRPCClient) Allocate(ask []*common.ContainerRequest, release []common.ContainerID, completedContainers []*common.Container, progress float32) (*ampb.AllocateResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("无法连接到 RM")
	}

	// 转换容器请求
	var pbAsks []*ampb.ContainerRequest
	for _, askReq := range ask {
		pbAsks = append(pbAsks, &ampb.ContainerRequest{
			Capability: &ampb.Resource{
				MemoryMb: askReq.Resource.Memory,
				Vcores:   askReq.Resource.VCores,
			},
			Priority:            askReq.Priority,
			RelaxLocality:       true, // 简化处理
			NodeLabelExpression: askReq.NodeLabel,
		})
	}

	// 转换释放的容器
	var pbReleases []*ampb.ContainerID
	for _, releaseID := range release {
		pbReleases = append(pbReleases, &ampb.ContainerID{
			ApplicationAttemptId: fmt.Sprintf("%s_%d", releaseID.ApplicationAttemptID.ApplicationID.String(), releaseID.ApplicationAttemptID.AttemptID),
			ContainerId:          releaseID.ContainerID,
		})
	}

	// 转换已完成的容器状态
	var pbCompleted []*ampb.ContainerStatus
	for _, completed := range completedContainers {
		pbCompleted = append(pbCompleted, &ampb.ContainerStatus{
			ContainerId: &ampb.ContainerID{
				ApplicationAttemptId: fmt.Sprintf("%s_%d", completed.ID.ApplicationAttemptID.ApplicationID.String(), completed.ID.ApplicationAttemptID.AttemptID),
				ContainerId:          completed.ID.ContainerID,
			},
			State:       1, // 简化处理
			Diagnostics: completed.Status,
		})
	}

	req := &ampb.AllocateRequest{
		Ask:                 pbAsks,
		Release:             pbReleases,
		CompletedContainers: pbCompleted,
		Progress:            progress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.client.Allocate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("分配失败: %v", err)
	}

	// 转换响应
	var allocatedContainers []*common.Container
	for _, allocated := range resp.AllocatedContainers {
		allocatedContainers = append(allocatedContainers, &common.Container{
			ID: common.ContainerID{
				// 简化解析，实际应该正确解析 ApplicationAttemptId
				ApplicationAttemptID: common.ApplicationAttemptID{
					ApplicationID: c.appID,
					AttemptID:     1,
				},
				ContainerID: allocated.Id.ContainerId,
			},
			NodeID: common.NodeID{
				Host: allocated.NodeId.Host,
				Port: allocated.NodeId.Port,
			},
			Resource: common.Resource{
				Memory: allocated.Resource.MemoryMb,
				VCores: allocated.Resource.Vcores,
			},
			Status: "ALLOCATED",
			State:  "NEW",
		})
	}

	var updatedNodes []common.NodeReport
	for _, node := range resp.UpdatedNodes {
		updatedNodes = append(updatedNodes, common.NodeReport{
			NodeID: common.NodeID{
				Host: node.NodeId.Host,
				Port: node.NodeId.Port,
			},
			HTTPAddress: node.NodeHttpAddress,
			UsedResource: common.Resource{
				Memory: node.Used.MemoryMb,
				VCores: node.Used.Vcores,
			},
			TotalResource: common.Resource{
				Memory: node.Capability.MemoryMb,
				VCores: node.Capability.Vcores,
			},
			NumContainers: node.NumContainers,
			State:         fmt.Sprintf("%d", node.NodeState),
		})
	}

	return resp, nil
}

// FinishApplicationMaster 完成 ApplicationMaster
func (c *ApplicationMasterGRPCClient) FinishApplicationMaster(finalStatus string, diagnostics string, trackingURL string) (*ampb.FinishApplicationMasterResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("无法连接到 RM")
	}

	req := &ampb.FinishApplicationMasterRequest{
		FinalApplicationStatus: finalStatus,
		Diagnostics:            diagnostics,
		TrackingUrl:            trackingURL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.client.FinishApplicationMaster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to finish ApplicationMaster: %v", err)
	}

	log.Printf("ApplicationMaster finished successfully")

	return resp, nil
}

// GetApplicationReport 获取应用程序报告
func (c *ApplicationMasterGRPCClient) GetApplicationReport() (*common.ApplicationReport, error) {
	if !c.connected {
		return nil, fmt.Errorf("无法连接到 RM")
	}

	req := &ampb.GetApplicationReportRequest{
		ApplicationId: &ampb.ApplicationID{
			ClusterTimestamp: c.appID.ClusterTimestamp,
			Id:               c.appID.ID,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.GetApplicationReport(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get application report: %v", err)
	}

	return &common.ApplicationReport{
		ApplicationID: common.ApplicationID{
			ClusterTimestamp: resp.ApplicationReport.ApplicationId.ClusterTimestamp,
			ID:               resp.ApplicationReport.ApplicationId.Id,
		},
		ApplicationName: resp.ApplicationReport.Name,
		ApplicationType: resp.ApplicationReport.ApplicationType,
		User:            resp.ApplicationReport.User,
		Queue:           resp.ApplicationReport.Queue,
		Host:            resp.ApplicationReport.Host,
		RPCPort:         int(resp.ApplicationReport.RpcPort),
		TrackingURL:     resp.ApplicationReport.TrackingUrl,
		StartTime:       resp.ApplicationReport.StartTime.AsTime(),
		FinishTime:      resp.ApplicationReport.FinishTime.AsTime(),
		FinalStatus:     resp.ApplicationReport.FinalApplicationStatus,
		State:           fmt.Sprintf("%d", resp.ApplicationReport.YarnApplicationState),
		Progress:        resp.ApplicationReport.Progress,
		Diagnostics:     resp.ApplicationReport.Diagnostics,
	}, nil
}

// GetClusterMetrics 获取集群指标
func (c *ApplicationMasterGRPCClient) GetClusterMetrics() (*ampb.GetClusterMetricsResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("无法连接到 RM")
	}

	req := &ampb.GetClusterMetricsRequest{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.GetClusterMetrics(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster metrics: %v", err)
	}

	return resp, nil
}
