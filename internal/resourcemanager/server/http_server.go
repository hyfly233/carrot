package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"carrot/internal/common"
	"carrot/internal/resourcemanager/applicationmanager"
	"carrot/internal/resourcemanager/nodemanager"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// HTTPServer ResourceManager HTTP 服务器
type HTTPServer struct {
	server *http.Server
	logger *zap.Logger
	rm     ResourceManagerInterface
}

// ResourceManagerInterface 定义 ResourceManager 接口
type ResourceManagerInterface interface {
	GetApplications() []*applicationmanager.Application
	SubmitApplication(ctx common.ApplicationSubmissionContext) (*common.ApplicationID, error)
	GetNodes() []*nodemanager.Node
	RegisterNode(nodeID common.NodeID, resource common.Resource, httpAddress string) error
	NodeHeartbeat(nodeID common.NodeID, usedResource common.Resource, containers []*common.Container) error
	GetClusterTimestamp() int64
	GetNodeHealthStatus() map[string]int
}

// NewHTTPServer 创建新的 HTTP 服务器
func NewHTTPServer(rm ResourceManagerInterface, logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		rm:     rm,
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
	v1.Use(s.loggingMiddleware)
	v1.Use(s.corsMiddleware)

	// 集群信息路由
	cluster := v1.PathPrefix("/cluster").Subrouter()
	cluster.HandleFunc("/info", s.handleClusterInfo).Methods("GET")

	// 应用程序路由
	apps := cluster.PathPrefix("/apps").Subrouter()
	apps.HandleFunc("", s.handleApplications).Methods("GET", "POST")
	apps.HandleFunc("/new-application", s.handleNewApplication).Methods("POST")
	apps.HandleFunc("/{appId}", s.handleApplication).Methods("GET", "DELETE")

	// 节点路由
	nodes := cluster.PathPrefix("/nodes").Subrouter()
	nodes.HandleFunc("", s.handleNodes).Methods("GET")
	nodes.HandleFunc("/register", s.handleNodeRegistration).Methods("POST")
	nodes.HandleFunc("/heartbeat", s.handleNodeHeartbeat).Methods("POST")
	nodes.HandleFunc("/health", s.handleNodeHealth).Methods("GET")

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 在后台启动服务器
	go func() {
		s.logger.Info("Starting ResourceManager HTTP server", zap.String("addr", s.server.Addr))
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("ResourceManager HTTP server failed", zap.Error(err))
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

	s.logger.Info("Stopping ResourceManager HTTP server")
	return s.server.Shutdown(ctx)
}

// GetType 获取服务器类型
func (s *HTTPServer) GetType() common.ServerType {
	return common.ServerTypeHTTP
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

// handleApplications 处理应用程序请求
func (s *HTTPServer) handleApplications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apps := s.rm.GetApplications()
		s.writeJSONResponse(w, map[string]interface{}{
			"apps": map[string]interface{}{
				"app": apps,
			},
		})
	case http.MethodPost:
		var ctx common.ApplicationSubmissionContext
		if err := json.NewDecoder(r.Body).Decode(&ctx); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		appID, err := s.rm.SubmitApplication(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.writeJSONResponse(w, map[string]interface{}{
			"application-id": appID,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNewApplication 处理新应用程序请求
func (s *HTTPServer) handleNewApplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appID := common.ApplicationID{
		ClusterTimestamp: s.rm.GetClusterTimestamp(),
		ID:               int32(time.Now().UnixNano() % 1000000), // 临时生成 ID，转换为 int32
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"application-id": appID,
	})
}

// handleApplication 处理单个应用程序请求
func (s *HTTPServer) handleApplication(w http.ResponseWriter, r *http.Request) {
	// TODO: 实现单个应用程序的处理
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

// handleNodes 处理节点请求
func (s *HTTPServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := s.rm.GetNodes()
	s.writeJSONResponse(w, map[string]interface{}{
		"nodes": map[string]interface{}{
			"node": nodes,
		},
	})
}

// handleClusterInfo 处理集群信息请求
func (s *HTTPServer) handleClusterInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterTimestamp := s.rm.GetClusterTimestamp()
	info := map[string]interface{}{
		"clusterInfo": map[string]interface{}{
			"id":                     clusterTimestamp,
			"startedOn":              time.Unix(clusterTimestamp, 0),
			"state":                  "STARTED",
			"haState":                "ACTIVE",
			"resourceManagerVersion": "carrot-1.0.0",
		},
	}

	s.writeJSONResponse(w, info)
}

// handleNodeRegistration 处理节点注册请求
func (s *HTTPServer) handleNodeRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var registrationData struct {
		NodeID      common.NodeID   `json:"node_id"`
		Resource    common.Resource `json:"resource"`
		HTTPAddress string          `json:"http_address"`
	}

	if err := json.NewDecoder(r.Body).Decode(&registrationData); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := s.rm.RegisterNode(registrationData.NodeID, registrationData.Resource, registrationData.HTTPAddress)
	if err != nil {
		s.logger.Error("Failed to register node", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"status": "registered",
	})
}

// handleNodeHeartbeat 处理节点心跳请求
func (s *HTTPServer) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.logger.Warn("Method not allowed", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var heartbeatData struct {
		NodeID       common.NodeID       `json:"node_id"`
		UsedResource common.Resource     `json:"used_resource"`
		Containers   []*common.Container `json:"containers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&heartbeatData); err != nil {
		s.logger.Error("Failed to decode heartbeat", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := s.rm.NodeHeartbeat(heartbeatData.NodeID, heartbeatData.UsedResource, heartbeatData.Containers)
	if err != nil {
		s.logger.Error("Failed to process node heartbeat", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"status": "heartbeat_received",
	})
}

// handleNodeHealth 处理节点健康状态查询
func (s *HTTPServer) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	healthStatus := s.rm.GetNodeHealthStatus()

	response := map[string]interface{}{
		"summary":   healthStatus,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	s.writeJSONResponse(w, response)
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
