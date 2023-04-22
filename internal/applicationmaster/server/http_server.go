package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"carrot/internal/common"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// HTTPServer ApplicationMaster HTTP 服务器
type HTTPServer struct {
	server  *http.Server
	logger  *zap.Logger
	am      ApplicationMasterInterface
	address string
}

// ApplicationMasterInterface 定义 ApplicationMaster 接口
type ApplicationMasterInterface interface {
	GetApplicationID() common.ApplicationID
	GetApplicationAttemptID() string
	GetState() string
	GetProgress() float64
	GetFinalStatus() string
	GetTrackingURL() string
	GetContainerStatistics() map[string]int
	GetAllocatedContainers() map[string]*common.Container
	GetCompletedContainers() map[string]*common.Container
	GetFailedContainers() map[string]*common.Container
	GetPendingRequests() []*common.ResourceRequest
	SetApplicationState(state string)
	SetFinalStatus(status string)
	SendShutdownSignal()
	CalculateRequestedMemory() int64
	CalculateAllocatedMemory() int64
	CalculateRequestedVCores() int32
	CalculateAllocatedVCores() int32
}

// ContainerInfo 容器信息
type ContainerInfo struct {
	Container common.Container `json:"container"`
	Status    string           `json:"status"`
}

// NewHTTPServer 创建新的 HTTP 服务器
func NewHTTPServer(am ApplicationMasterInterface, logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		am:     am,
		logger: logger,
	}
}

// Start 启动 HTTP 服务器
func (s *HTTPServer) Start(port int) error {
	s.address = fmt.Sprintf(":%d", port)
	router := mux.NewRouter()

	// 添加中间件
	router.Use(s.loggingMiddleware)
	router.Use(s.corsMiddleware)

	// API 路由
	api := router.PathPrefix("/ws/v1").Subrouter()
	api.HandleFunc("/appmaster/info", s.handleAppMasterInfo).Methods("GET")
	api.HandleFunc("/appmaster/status", s.handleAppMasterStatus).Methods("GET")
	api.HandleFunc("/appmaster/containers", s.handleContainers).Methods("GET")
	api.HandleFunc("/appmaster/containers/{containerId}", s.handleContainer).Methods("GET")
	api.HandleFunc("/appmaster/progress", s.handleProgress).Methods("GET")
	api.HandleFunc("/appmaster/metrics", s.handleMetrics).Methods("GET")
	api.HandleFunc("/appmaster/shutdown", s.handleShutdown).Methods("POST")

	// 静态文件路由（用于 Web UI）
	router.PathPrefix("/").Handler(http.HandlerFunc(s.handleWebUI))

	// 创建 HTTP 服务器
	s.server = &http.Server{
		Addr:         s.address,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 在后台启动服务器
	go func() {
		s.logger.Info("Starting ApplicationMaster HTTP rmserver", zap.String("addr", s.server.Addr))
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("ApplicationMaster HTTP rmserver failed", zap.Error(err))
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

	s.logger.Info("Stopping ApplicationMaster HTTP rmserver")
	return s.server.Shutdown(ctx)
}

// handleAppMasterInfo 处理应用程序主控信息请求
func (s *HTTPServer) handleAppMasterInfo(w http.ResponseWriter, r *http.Request) {
	appID := s.am.GetApplicationID()
	info := map[string]interface{}{
		"applicationId":        fmt.Sprintf("%d_%d", appID.ClusterTimestamp, appID.ID),
		"applicationAttemptId": s.am.GetApplicationAttemptID(),
		"state":                s.am.GetState(),
		"finalStatus":          s.am.GetFinalStatus(),
		"progress":             s.am.GetProgress(),
		"trackingUrl":          s.am.GetTrackingURL(),
		"startTime":            time.Now().Unix(), // 可以从实际的开始时间获取
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"appmaster": info,
	})
}

// handleAppMasterStatus 处理应用程序主控状态请求
func (s *HTTPServer) handleAppMasterStatus(w http.ResponseWriter, r *http.Request) {
	stats := s.am.GetContainerStatistics()

	status := map[string]interface{}{
		"state":      s.am.GetState(),
		"progress":   s.am.GetProgress(),
		"containers": stats,
		"isRunning":  s.am.GetState() == "RUNNING",
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"status": status,
	})
}

// handleContainers 处理容器列表请求
func (s *HTTPServer) handleContainers(w http.ResponseWriter, r *http.Request) {
	containers := make([]*ContainerInfo, 0)

	// 添加已分配的容器
	for _, container := range s.am.GetAllocatedContainers() {
		containers = append(containers, &ContainerInfo{
			Container: *container,
			Status:    "ALLOCATED",
		})
	}

	// 添加已完成的容器
	for _, container := range s.am.GetCompletedContainers() {
		containers = append(containers, &ContainerInfo{
			Container: *container,
			Status:    "COMPLETED",
		})
	}

	// 添加失败的容器
	for _, container := range s.am.GetFailedContainers() {
		containers = append(containers, &ContainerInfo{
			Container: *container,
			Status:    "FAILED",
		})
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"containers": containers,
	})
}

// handleContainer 处理单个容器请求
func (s *HTTPServer) handleContainer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerIDStr := vars["containerId"]

	// 查找容器
	var container *ContainerInfo
	if c, exists := s.am.GetAllocatedContainers()[containerIDStr]; exists {
		container = &ContainerInfo{
			Container: *c,
			Status:    "ALLOCATED",
		}
	} else if c, exists := s.am.GetCompletedContainers()[containerIDStr]; exists {
		container = &ContainerInfo{
			Container: *c,
			Status:    "COMPLETED",
		}
	} else if c, exists := s.am.GetFailedContainers()[containerIDStr]; exists {
		container = &ContainerInfo{
			Container: *c,
			Status:    "FAILED",
		}
	}

	if container == nil {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"container": container,
	})
}

// handleProgress 处理进度请求
func (s *HTTPServer) handleProgress(w http.ResponseWriter, r *http.Request) {
	progress := map[string]interface{}{
		"progress":   s.am.GetProgress(),
		"state":      s.am.GetState(),
		"statistics": s.am.GetContainerStatistics(),
	}

	s.writeJSONResponse(w, progress)
}

// handleMetrics 处理指标请求
func (s *HTTPServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	appID := s.am.GetApplicationID()
	metrics := map[string]interface{}{
		"applicationId":        fmt.Sprintf("%d_%d", appID.ClusterTimestamp, appID.ID),
		"applicationAttemptId": s.am.GetApplicationAttemptID(),
		"state":                s.am.GetState(),
		"progress":             s.am.GetProgress(),
		"containers": map[string]interface{}{
			"allocated": len(s.am.GetAllocatedContainers()),
			"completed": len(s.am.GetCompletedContainers()),
			"failed":    len(s.am.GetFailedContainers()),
			"pending":   len(s.am.GetPendingRequests()),
		},
		"memory": map[string]interface{}{
			"requested": s.am.CalculateRequestedMemory(),
			"allocated": s.am.CalculateAllocatedMemory(),
		},
		"vcores": map[string]interface{}{
			"requested": s.am.CalculateRequestedVCores(),
			"allocated": s.am.CalculateAllocatedVCores(),
		},
	}

	s.writeJSONResponse(w, map[string]interface{}{
		"metrics": metrics,
	})
}

// handleShutdown 处理关闭请求
func (s *HTTPServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("Received shutdown request")

	// 设置最终状态
	s.am.SetApplicationState("FINISHING")
	s.am.SetFinalStatus("SUCCEEDED")

	// 发送关闭信号
	go s.am.SendShutdownSignal()

	s.writeJSONResponse(w, map[string]interface{}{
		"message": "Shutdown initiated",
	})
}

// handleWebUI 处理 Web UI 请求
func (s *HTTPServer) handleWebUI(w http.ResponseWriter, r *http.Request) {
	appID := s.am.GetApplicationID()
	// 简单的 HTML 页面
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>ApplicationMaster Web UI</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background: #f0f0f0; padding: 20px; margin-bottom: 20px; }
        .section { margin-bottom: 30px; }
        .container-table { width: 100%%; border-collapse: collapse; }
        .container-table th, .container-table td { 
            border: 1px solid #ddd; padding: 8px; text-align: left; 
        }
        .container-table th { background-color: #f2f2f2; }
        .status-running { color: green; }
        .status-completed { color: blue; }
        .status-failed { color: red; }
    </style>
</head>
<body>
    <div class="header">
        <h1>ApplicationMaster Web UI</h1>
        <p>Application ID: %d_%d</p>
        <p>State: <span id="state">%s</span></p>
        <p>Progress: <span id="progress">%.1f%%</span></p>
    </div>

    <div class="section">
        <h2>Container Statistics</h2>
        <div id="container-stats">
            Loading...
        </div>
    </div>

    <div class="section">
        <h2>Containers</h2>
        <table class="container-table" id="container-table">
            <thead>
                <tr>
                    <th>Container ID</th>
                    <th>Node</th>
                    <th>Memory</th>
                    <th>VCores</th>
                    <th>Status</th>
                    <th>State</th>
                </tr>
            </thead>
            <tbody id="container-tbody">
                <tr><td colspan="6">Loading...</td></tr>
            </tbody>
        </table>
    </div>

    <script>
        function updateData() {
            // 更新状态
            fetch('/ws/v1/appmaster/status')
                .then(response => response.json())
                .then(data => {
                    document.getElementById('state').textContent = data.status.state;
                    document.getElementById('progress').textContent = (data.status.progress * 100).toFixed(1) + '%%';
                    
                    const stats = data.status.containers;
                    document.getElementById('container-stats').innerHTML = 
                        'Allocated: ' + stats.allocated + 
                        ', Completed: ' + stats.completed + 
                        ', Failed: ' + stats.failed + 
                        ', Pending: ' + stats.pending;
                })
                .catch(error => console.error('Error fetching status:', error));

            // 更新容器列表
            fetch('/ws/v1/appmaster/containers')
                .then(response => response.json())
                .then(data => {
                    const tbody = document.getElementById('container-tbody');
                    tbody.innerHTML = '';
                    
                    if (data.containers && data.containers.length > 0) {
                        data.containers.forEach(container => {
                            const row = tbody.insertRow();
                            row.innerHTML = 
                                '<td>' + container.Container.id.container_id + '</td>' +
                                '<td>' + container.Container.node_id.host + ':' + container.Container.node_id.port + '</td>' +
                                '<td>' + container.Container.resource.memory + ' MB</td>' +
                                '<td>' + container.Container.resource.vcores + '</td>' +
                                '<td class="status-' + container.Status.toLowerCase() + '">' + container.Status + '</td>' +
                                '<td>' + container.Container.state + '</td>';
                        });
                    } else {
                        const row = tbody.insertRow();
                        row.innerHTML = '<td colspan="6">No containers</td>';
                    }
                })
                .catch(error => console.error('Error fetching containers:', error));
        }

        // 初始加载
        updateData();
        
        // 每5秒更新一次
        setInterval(updateData, 5000);
    </script>
</body>
</html>`, appID.ClusterTimestamp, appID.ID, s.am.GetState(), s.am.GetProgress()*100)

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// loggingMiddleware 日志中间件
func (s *HTTPServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Debug("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("duration", time.Since(start)))
	})
}

// corsMiddleware CORS 中间件
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

// GetAddress 获取服务器地址
func (s *HTTPServer) GetAddress() string {
	return s.address
}

// GetType 获取服务器类型
func (s *HTTPServer) GetType() common.ServerType {
	return common.ServerTypeHTTP
}

// IsRunning 检查服务器是否在运行
func (s *HTTPServer) IsRunning() bool {
	return s.server != nil
}
