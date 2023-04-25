package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"carrot/internal/common"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// HTTPServer NodeManager HTTP 服务器
type HTTPServer struct {
	server *http.Server
	logger *zap.Logger
	nm     NodeManagerInterface
}

// NodeManagerInterface 定义 NodeManager 接口
type NodeManagerInterface interface {
	StartContainer(containerID common.ContainerID, launchContext common.ContainerLaunchContext, resource common.Resource) error
	StopContainer(containerID common.ContainerID) error
	GetContainers() map[string]*common.Container
	GetContainer(containerID common.ContainerID) (*common.Container, bool)
	GetContainerStatus(containerID common.ContainerID) (*common.Container, error)
	GetContainerLogs(containerID common.ContainerID, logType string) (string, error)
	GetNodeID() common.NodeID
	GetTotalResource() common.Resource
	GetUsedResource() common.Resource
	GetAvailableResource() common.Resource
	GetNodeStatus() string
}

// NewHTTPServer 创建新的 HTTP 服务器
func NewHTTPServer(nm NodeManagerInterface, logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		nm:     nm,
		logger: logger,
	}
}

// Start 启动 HTTP 服务器
func (s *HTTPServer) Start(port int) error {
	router := mux.NewRouter()

	// 添加中间件
	router.Use(s.loggingMiddleware)
	router.Use(s.corsMiddleware)

	// API 路由
	v1 := router.PathPrefix("/ws/v1").Subrouter()
	node := v1.PathPrefix("/node").Subrouter()

	// 容器相关路由
	node.HandleFunc("/containers", s.handleContainers).Methods("GET", "POST")
	node.HandleFunc("/containers/{containerId}", s.handleContainer).Methods("GET", "DELETE")

	// 节点信息路由
	node.HandleFunc("/info", s.handleNodeInfo).Methods("GET")
	node.HandleFunc("/status", s.handleNodeStatus).Methods("GET")

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 在后台启动服务器
	go func() {
		s.logger.Info("Starting NodeManager HTTP rmserver", zap.String("addr", s.server.Addr))
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("NodeManager HTTP rmserver failed", zap.Error(err))
		}
	}()

	return nil
}

// Stop 停止 HTTP 服务器
func (s *HTTPServer) Stop() error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.logger.Info("Stopping NodeManager HTTP rmserver")
	return s.server.Shutdown(ctx)
}

// GetType 获取服务器类型
func (s *HTTPServer) GetType() ServerType {
	return ServerTypeHTTP
}

// GetAddress 获取服务器地址
func (s *HTTPServer) GetAddress() string {
	if s.server != nil {
		return s.server.Addr
	}
	return ""
}

// IsRunning 检查服务器是否在运行
func (s *HTTPServer) IsRunning() bool {
	return s.server != nil
}

// handleContainers 处理容器列表请求
func (s *HTTPServer) handleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		containers := s.nm.GetContainers()
		containerList := make([]*common.Container, 0, len(containers))
		for _, container := range containers {
			containerList = append(containerList, container)
		}

		s.writeJSONResponse(w, map[string]interface{}{
			"containers": containerList,
		})

	case http.MethodPost:
		var request struct {
			ContainerID   common.ContainerID            `json:"container_id"`
			LaunchContext common.ContainerLaunchContext `json:"launch_context"`
			Resource      common.Resource               `json:"resource"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := s.nm.StartContainer(request.ContainerID, request.LaunchContext, request.Resource)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		s.writeJSONResponse(w, map[string]interface{}{
			"status":       "container_started",
			"container_id": request.ContainerID,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleContainer 处理单个容器请求
func (s *HTTPServer) handleContainer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerIDStr := vars["containerId"]

	if containerIDStr == "" {
		http.Error(w, "Container ID 是必需的", http.StatusBadRequest)
		return
	}

	// 解析容器ID
	containerID, err := s.parseContainerID(containerIDStr)
	if err != nil {
		s.logger.Error("Invalid container ID", zap.String("container_id", containerIDStr), zap.Error(err))
		http.Error(w, "Invalid container ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetContainer(w, r, containerID)
	case http.MethodDelete:
		s.handleDeleteContainer(w, r, containerID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetContainer 处理获取容器信息请求
func (s *HTTPServer) handleGetContainer(w http.ResponseWriter, r *http.Request, containerID common.ContainerID) {
	container, exists := s.nm.GetContainer(containerID)
	if !exists {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	response := struct {
		ContainerID common.ContainerID `json:"container_id"`
		State       string             `json:"state"`
		Resource    common.Resource    `json:"resource"`
	}{
		ContainerID: container.ID,
		State:       container.State,
		Resource:    container.Resource,
	}

	s.writeJSONResponse(w, response)
}

// handleDeleteContainer 处理删除容器请求
func (s *HTTPServer) handleDeleteContainer(w http.ResponseWriter, r *http.Request, containerID common.ContainerID) {
	err := s.nm.StopContainer(containerID)
	if err != nil {
		s.logger.Error("Failed to stop container", zap.Any("container_id", containerID), zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleNodeInfo 处理节点信息请求
func (s *HTTPServer) handleNodeInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeID := s.nm.GetNodeID()
	totalResource := s.nm.GetTotalResource()
	usedResource := s.nm.GetUsedResource()
	availableResource := s.nm.GetAvailableResource()

	info := map[string]interface{}{
		"node_id":            nodeID,
		"total_resource":     totalResource,
		"used_resource":      usedResource,
		"available_resource": availableResource,
		"status":             s.nm.GetNodeStatus(),
		"timestamp":          time.Now().Format(time.RFC3339),
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"nodeInfo": info,
	})
}

// handleNodeStatus 处理节点状态请求
func (s *HTTPServer) handleNodeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	containers := s.nm.GetContainers()

	status := map[string]interface{}{
		"status":             s.nm.GetNodeStatus(),
		"container_count":    len(containers),
		"total_resource":     s.nm.GetTotalResource(),
		"used_resource":      s.nm.GetUsedResource(),
		"available_resource": s.nm.GetAvailableResource(),
		"timestamp":          time.Now().Format(time.RFC3339),
	}

	s.writeJSONResponse(w, status)
}

// parseContainerID 解析容器 ID
func (s *HTTPServer) parseContainerID(containerIDStr string) (common.ContainerID, error) {
	// 简化的容器 ID 解析逻辑
	// 实际应用中需要根据具体的 ID 格式进行解析
	parts := strings.Split(containerIDStr, "_")
	if len(parts) < 3 {
		return common.ContainerID{}, fmt.Errorf("invalid container ID format")
	}

	// 这里需要根据实际的 ContainerID 结构进行解析
	// 暂时返回一个空的 ContainerID
	return common.ContainerID{}, nil
}

// loggingMiddleware 日志中间件
func (s *HTTPServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 记录请求
		s.logger.Debug("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr))

		next.ServeHTTP(w, r)

		// 记录响应
		duration := time.Since(start)
		s.logger.Debug("HTTP response",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("duration", duration))
	})
}

// corsMiddleware CORS中间件
func (s *HTTPServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeJSONResponse 写入 JSON 响应
func (s *HTTPServer) writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// ServerType 服务器类型
type ServerType string

const (
	ServerTypeHTTP ServerType = "http"
	ServerTypeGRPC ServerType = "grpc"
	ServerTypeTCP  ServerType = "tcp"
	ServerTypeUDP  ServerType = "udp"
)
