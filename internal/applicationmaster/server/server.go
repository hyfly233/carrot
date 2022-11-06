package server

import (
	"context"
	"net"

	"go.uber.org/zap"
)

// ServerType 服务器类型
type ServerType string

const (
	ServerTypeHTTP ServerType = "http"
	ServerTypeGRPC ServerType = "grpc"
	ServerTypeTCP  ServerType = "tcp"
	ServerTypeUDP  ServerType = "udp"
)

// Server 通用服务器接口
type Server interface {
	// Start 启动服务器
	Start(port int) error
	
	// Stop 停止服务器
	Stop() error
	
	// GetType 获取服务器类型
	GetType() ServerType
	
	// GetAddress 获取服务器地址
	GetAddress() string
	
	// IsRunning 检查服务器是否在运行
	IsRunning() bool
}

// ServerManager 服务器管理器
type ServerManager struct {
	servers map[ServerType]Server
	logger  *zap.Logger
}

// NewServerManager 创建新的服务器管理器
func NewServerManager(logger *zap.Logger) *ServerManager {
	return &ServerManager{
		servers: make(map[ServerType]Server),
		logger:  logger,
	}
}

// RegisterServer 注册服务器
func (sm *ServerManager) RegisterServer(serverType ServerType, server Server) {
	sm.servers[serverType] = server
	sm.logger.Info("Server registered", 
		zap.String("type", string(serverType)),
		zap.String("address", server.GetAddress()))
}

// StartServer 启动指定类型的服务器
func (sm *ServerManager) StartServer(serverType ServerType, port int) error {
	server, exists := sm.servers[serverType]
	if !exists {
		sm.logger.Error("Server not found", zap.String("type", string(serverType)))
		return ErrServerNotFound
	}
	
	sm.logger.Info("Starting server", 
		zap.String("type", string(serverType)),
		zap.Int("port", port))
	
	return server.Start(port)
}

// StopServer 停止指定类型的服务器
func (sm *ServerManager) StopServer(serverType ServerType) error {
	server, exists := sm.servers[serverType]
	if !exists {
		return ErrServerNotFound
	}
	
	sm.logger.Info("Stopping server", zap.String("type", string(serverType)))
	return server.Stop()
}

// StartAllServers 启动所有服务器
func (sm *ServerManager) StartAllServers(basePorts map[ServerType]int) error {
	for serverType, port := range basePorts {
		if err := sm.StartServer(serverType, port); err != nil {
			sm.logger.Error("Failed to start server", 
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
			sm.logger.Error("Failed to stop server", 
				zap.String("type", string(serverType)),
				zap.Error(err))
			lastErr = err
		}
	}
	return lastErr
}

// GetRunningServers 获取正在运行的服务器
func (sm *ServerManager) GetRunningServers() []ServerType {
	var running []ServerType
	for serverType, server := range sm.servers {
		if server.IsRunning() {
			running = append(running, serverType)
		}
	}
	return running
}

// GRPCServer gRPC 服务器接口 (预留)
type GRPCServer interface {
	Server
	RegisterService(serviceName string, service interface{})
}

// TCPServer TCP 服务器接口 (预留)
type TCPServer interface {
	Server
	SetConnectionHandler(handler func(conn net.Conn))
	GetConnectionCount() int
}

// UDPServer UDP 服务器接口 (预留)  
type UDPServer interface {
	Server
	SetPacketHandler(handler func(data []byte, addr net.Addr))
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
	s.logger.Info("gRPC server start not implemented yet")
	return ErrNotImplemented
}

func (s *grpcServer) Stop() error {
	// TODO: 实现 gRPC 服务器停止逻辑
	s.logger.Info("gRPC server stop not implemented yet")
	return ErrNotImplemented
}

func (s *grpcServer) GetType() ServerType {
	return ServerTypeGRPC
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

// 预留的 TCP 服务器实现
type tcpServer struct {
	address   string
	logger    *zap.Logger
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	connCount int
	handler   func(conn net.Conn)
}

// NewTCPServer 创建新的 TCP 服务器 (预留)
func NewTCPServer(logger *zap.Logger) TCPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &tcpServer{
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *tcpServer) Start(port int) error {
	s.address = net.JoinHostPort("", string(rune(port)))
	// TODO: 实现 TCP 服务器启动逻辑
	s.logger.Info("TCP server start not implemented yet")
	return ErrNotImplemented
}

func (s *tcpServer) Stop() error {
	s.cancel()
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *tcpServer) GetType() ServerType {
	return ServerTypeTCP
}

func (s *tcpServer) GetAddress() string {
	return s.address
}

func (s *tcpServer) IsRunning() bool {
	return s.listener != nil
}

func (s *tcpServer) SetConnectionHandler(handler func(conn net.Conn)) {
	s.handler = handler
}

func (s *tcpServer) GetConnectionCount() int {
	return s.connCount
}

// 预留的 UDP 服务器实现
type udpServer struct {
	address string
	logger  *zap.Logger
	conn    net.PacketConn
	ctx     context.Context
	cancel  context.CancelFunc
	handler func(data []byte, addr net.Addr)
}

// NewUDPServer 创建新的 UDP 服务器 (预留)
func NewUDPServer(logger *zap.Logger) UDPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &udpServer{
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *udpServer) Start(port int) error {
	s.address = net.JoinHostPort("", string(rune(port)))
	// TODO: 实现 UDP 服务器启动逻辑
	s.logger.Info("UDP server start not implemented yet")
	return ErrNotImplemented
}

func (s *udpServer) Stop() error {
	s.cancel()
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *udpServer) GetType() ServerType {
	return ServerTypeUDP
}

func (s *udpServer) GetAddress() string {
	return s.address
}

func (s *udpServer) IsRunning() bool {
	return s.conn != nil
}

func (s *udpServer) SetPacketHandler(handler func(data []byte, addr net.Addr)) {
	s.handler = handler
}
