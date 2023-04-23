package nodemanager

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	rmpb "carrot/api/proto/resourcemanager"
)

// GRPCClient NodeManager 的 gRPC 客户端
type GRPCClient struct {
	conn      *grpc.ClientConn
	client    rmpb.ResourceManagerServiceClient
	nodeID    string
	rmAddress string
	connected bool
}

// NewGRPCClient 创建新的 gRPC 客户端
func NewGRPCClient(nodeID, rmAddress string) *GRPCClient {
	return &GRPCClient{
		nodeID:    nodeID,
		rmAddress: rmAddress,
		connected: false,
	}
}

// Connect 连接到 ResourceManager
func (c *GRPCClient) Connect() error {
	conn, err := grpc.Dial(c.rmAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("无法连接到 ResourceManager: %v", err)
	}

	c.conn = conn
	c.client = rmpb.NewResourceManagerServiceClient(conn)
	c.connected = true

	log.Printf("连接到 ResourceManager %s", c.rmAddress)
	return nil
}

// Disconnect 断开连接
func (c *GRPCClient) Disconnect() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.connected = false
		return err
	}
	return nil
}

// RegisterNode 向 ResourceManager 注册节点
func (c *GRPCClient) RegisterNode(nodeInfo *NodeInfo, capability *ResourceCapability, httpAddress string) error {
	if !c.connected {
		return fmt.Errorf("not connected to ResourceManager")
	}

	req := &rmpb.RegisterNodeRequest{
		NodeInfo: &rmpb.NodeInfo{
			NodeId:    nodeInfo.NodeID,
			Hostname:  nodeInfo.Hostname,
			IpAddress: nodeInfo.IPAddress,
			Port:      int32(nodeInfo.Port),
			RackName:  nodeInfo.RackName,
			Labels:    nodeInfo.Labels,
		},
		TotalCapability: &rmpb.ResourceCapability{
			MemoryMb: capability.MemoryMB,
			Vcores:   int32(capability.VCores),
		},
		HttpAddress: httpAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.client.RegisterNode(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to register node: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("node registration failed: %s", resp.Message)
	}

	log.Printf("节点已注册 successfully: %s", resp.Message)
	return nil
}

// SendHeartbeat 发送心跳到 ResourceManager
func (c *GRPCClient) SendHeartbeat(usedResources *ResourceUsage, containerStatuses []*ContainerStatus) (*HeartbeatResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("not connected to ResourceManager")
	}

	// 转换容器状态
	var pbContainerStatuses []*rmpb.ContainerStatus
	for _, cs := range containerStatuses {
		pbContainerStatuses = append(pbContainerStatuses, &rmpb.ContainerStatus{
			ContainerId:   cs.ContainerID,
			ApplicationId: cs.ApplicationID,
			State:         convertToContainerState(cs.State),
			ExitCode:      int32(cs.ExitCode),
			Diagnostics:   cs.Diagnostics,
		})
	}

	req := &rmpb.NodeHeartbeatRequest{
		NodeId: c.nodeID,
		UsedResources: &rmpb.ResourceUsage{
			MemoryMb: usedResources.MemoryMB,
			Vcores:   int32(usedResources.VCores),
		},
		ContainerStatuses: pbContainerStatuses,
		Timestamp:         timestamppb.New(time.Now()),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.NodeHeartbeat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to send heartbeat: %v", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("heartbeat failed: %s", resp.Message)
	}

	// 转换容器操作
	var actions []*ContainerAction
	for _, action := range resp.ContainerActions {
		actions = append(actions, &ContainerAction{
			ContainerID: action.ContainerId,
			Action:      convertFromActionType(action.Action),
			Reason:      action.Reason,
		})
	}

	return &HeartbeatResponse{
		Success:           resp.Success,
		Message:           resp.Message,
		ContainerActions:  actions,
		ResponseID:        resp.ResponseId,
		HeartbeatInterval: resp.HeartbeatInterval,
		ShouldResync:      resp.ShouldResync,
	}, nil
}

// 定义 NodeManager 需要的数据类型

type NodeInfo struct {
	NodeID    string
	Hostname  string
	IPAddress string
	Port      int
	RackName  string
	Labels    []string
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

type HeartbeatResponse struct {
	Success           bool
	Message           string
	ContainerActions  []*ContainerAction
	ResponseID        int64
	HeartbeatInterval int32
	ShouldResync      bool
}

// 辅助函数：类型转换

func convertToContainerState(state string) rmpb.ContainerState {
	switch state {
	case "NEW":
		return rmpb.ContainerState_CONTAINER_STATE_NEW
	case "RUNNING":
		return rmpb.ContainerState_CONTAINER_STATE_RUNNING
	case "COMPLETE":
		return rmpb.ContainerState_CONTAINER_STATE_COMPLETE
	case "FAILED":
		return rmpb.ContainerState_CONTAINER_STATE_FAILED
	case "KILLED":
		return rmpb.ContainerState_CONTAINER_STATE_KILLED
	default:
		return rmpb.ContainerState_CONTAINER_STATE_UNSPECIFIED
	}
}

func convertFromActionType(action rmpb.ActionType) string {
	switch action {
	case rmpb.ActionType_ACTION_TYPE_LAUNCH:
		return "LAUNCH"
	case rmpb.ActionType_ACTION_TYPE_STOP:
		return "STOP"
	case rmpb.ActionType_ACTION_TYPE_KILL:
		return "KILL"
	case rmpb.ActionType_ACTION_TYPE_CLEANUP:
		return "CLEANUP"
	default:
		return "UNKNOWN"
	}
}
