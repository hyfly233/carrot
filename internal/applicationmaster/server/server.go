package server

import (
	"net"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ServerManager 服务器管理器
type ServerManager struct {
	servers map[common.ServerType]common.Server
	logger  *zap.Logger
}

// NewServerManager 创建新的服务器管理器
func NewServerManager(logger *zap.Logger) *ServerManager {
	return &ServerManager{
		servers: make(map[common.ServerType]common.Server),
		logger:  logger,
	}
}

// RegisterServer 注册服务器
func (sm *ServerManager) RegisterServer(serverType common.ServerType, server common.Server) {
	sm.servers[serverType] = server
	sm.logger.Info("Server registered",
		zap.String("type", string(serverType)),
		zap.String("address", server.GetAddress()))
}

// StartServer 启动指定类型的服务器
func (sm *ServerManager) StartServer(serverType common.ServerType, port int) error {
	server, exists := sm.servers[serverType]
	if !exists {
		sm.logger.Error("Server not found", zap.String("type", string(serverType)))
		return ErrServerNotFound
	}

	sm.logger.Info("Starting rmserver",
		zap.String("type", string(serverType)),
		zap.Int("port", port))

	return server.Start(port)
}

// StopServer 停止指定类型的服务器
func (sm *ServerManager) StopServer(serverType common.ServerType) error {
	server, exists := sm.servers[serverType]
	if !exists {
		return ErrServerNotFound
	}

	sm.logger.Info("Stopping rmserver", zap.String("type", string(serverType)))
	return server.Stop()
}

// StartAllServers 启动所有服务器
func (sm *ServerManager) StartAllServers(basePorts map[common.ServerType]int) error {
	for serverType, port := range basePorts {
		if err := sm.StartServer(serverType, port); err != nil {
			sm.logger.Error("Failed to start rmserver",
				zap.String("type", string(serverType)),
				zap.Error(err))
			return err
		}
	}
	return nil
}

// StopAllServers 停止所有服务器
func (sm *ServerManager) StopAllServers() error {
	var lastErr error
	for serverType := range sm.servers {
		if err := sm.StopServer(serverType); err != nil {
			sm.logger.Error("Failed to stop rmserver",
				zap.String("type", string(serverType)),
				zap.Error(err))
			lastErr = err
		}
	}
	return lastErr
}

// GetRunningServers 获取正在运行的服务器
func (sm *ServerManager) GetRunningServers() []common.ServerType {
	var running []common.ServerType
	for serverType, server := range sm.servers {
		if server.IsRunning() {
			running = append(running, serverType)
		}
	}
	return running
}

// GRPCServer gRPC 服务器接口 (预留)
type GRPCServer interface {
	common.Server
	RegisterService(serviceName string, service interface{})
}

// 预留的 gRPC 服务器实现
type grpcServer struct {
	address string
	logger  *zap.Logger
	// TODO: 添加 gRPC 服务器字段
}

// NewGRPCServer 创建新的 gRPC 服务器 (预留)
func NewGRPCServer(logger *zap.Logger) GRPCServer {
	return &grpcServer{
		logger: logger,
	}
}

func (s *grpcServer) Start(port int) error {
	s.address = net.JoinHostPort("", string(rune(port)))
	// TODO: 实现 gRPC 服务器启动逻辑
	s.logger.Info("gRPC rmserver start not implemented yet")
	return ErrNotImplemented
}

func (s *grpcServer) Stop() error {
	// TODO: 实现 gRPC 服务器停止逻辑
	s.logger.Info("gRPC rmserver stop not implemented yet")
	return ErrNotImplemented
}

func (s *grpcServer) GetType() common.ServerType {
	return common.ServerTypeGRPC
}

func (s *grpcServer) GetAddress() string {
	return s.address
}

func (s *grpcServer) IsRunning() bool {
	// TODO: 实现运行状态检查
	return false
}

func (s *grpcServer) RegisterService(serviceName string, service interface{}) {
	// TODO: 实现服务注册
	s.logger.Info("gRPC service registration not implemented yet",
		zap.String("service", serviceName))
}
