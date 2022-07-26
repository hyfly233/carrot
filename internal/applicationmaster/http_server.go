package applicationmaster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"carrot/internal/common"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// startHTTPServer 启动 HTTP 服务器
func (am *ApplicationMaster) startHTTPServer() error {
	router := mux.NewRouter()

	// 添加中间件
	router.Use(am.loggingMiddleware)
	router.Use(am.corsMiddleware)

	// API 路由
	api := router.PathPrefix("/ws/v1").Subrouter()
	api.HandleFunc("/appmaster/info", am.handleAppMasterInfo).Methods("GET")
	api.HandleFunc("/appmaster/status", am.handleAppMasterStatus).Methods("GET")
	api.HandleFunc("/appmaster/containers", am.handleContainers).Methods("GET")
	api.HandleFunc("/appmaster/containers/{containerId}", am.handleContainer).Methods("GET")
	api.HandleFunc("/appmaster/progress", am.handleProgress).Methods("GET")
	api.HandleFunc("/appmaster/metrics", am.handleMetrics).Methods("GET")
	api.HandleFunc("/appmaster/shutdown", am.handleShutdown).Methods("POST")

	// 静态文件路由（用于 Web UI）
	router.PathPrefix("/").Handler(http.HandlerFunc(am.handleWebUI))

	// 创建 HTTP 服务器
	am.httpServer = &http.Server{
		Addr:    ":8088", // 默认端口
		Handler: router,
	}

	// 在后台启动服务器
	go func() {
		am.logger.Info("Starting HTTP server", zap.String("addr", am.httpServer.Addr))
		if err := am.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			am.logger.Error("HTTP server failed", zap.Error(err))
		}
	}()

	return nil
}

// handleAppMasterInfo 处理应用程序主控信息请求
func (am *ApplicationMaster) handleAppMasterInfo(w http.ResponseWriter, r *http.Request) {
	am.mu.RLock()
	info := map[string]interface{}{
		"applicationId":        am.applicationID,
		"applicationAttemptId": am.applicationAttemptID,
		"state":                am.applicationState,
		"finalStatus":          am.finalStatus,
		"progress":             am.progress,
		"trackingUrl":          am.trackingURL,
		"startTime":            am.applicationAttemptID, // 可以从 attempt 中获取开始时间
	}
	am.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"appmaster": info,
	})
}

// handleAppMasterStatus 处理应用程序主控状态请求
func (am *ApplicationMaster) handleAppMasterStatus(w http.ResponseWriter, r *http.Request) {
	stats := am.GetContainerStatistics()

	status := map[string]interface{}{
		"state":      am.GetState(),
		"progress":   am.GetProgress(),
		"containers": stats,
		"isRunning":  am.GetState() == "RUNNING",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": status,
	})
}

// handleContainers 处理容器列表请求
func (am *ApplicationMaster) handleContainers(w http.ResponseWriter, r *http.Request) {
	am.mu.RLock()
	containers := make([]*ContainerInfo, 0)

	// 添加已分配的容器
	for _, container := range am.allocatedContainers {
		containers = append(containers, &ContainerInfo{
			Container: *container,
			Status:    "ALLOCATED",
		})
	}

	// 添加已完成的容器
	for _, container := range am.completedContainers {
		containers = append(containers, &ContainerInfo{
			Container: *container,
			Status:    "COMPLETED",
		})
	}

	// 添加失败的容器
	for _, container := range am.failedContainers {
		containers = append(containers, &ContainerInfo{
			Container: *container,
			Status:    "FAILED",
		})
	}
	am.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"containers": containers,
	})
}

// handleContainer 处理单个容器请求
func (am *ApplicationMaster) handleContainer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerIDStr := vars["containerId"]

	am.mu.RLock()
	defer am.mu.RUnlock()

	// 查找容器
	var container *ContainerInfo
	if c, exists := am.allocatedContainers[containerIDStr]; exists {
		container = &ContainerInfo{
			Container: *c,
			Status:    "ALLOCATED",
		}
	} else if c, exists := am.completedContainers[containerIDStr]; exists {
		container = &ContainerInfo{
			Container: *c,
			Status:    "COMPLETED",
		}
	} else if c, exists := am.failedContainers[containerIDStr]; exists {
		container = &ContainerInfo{
			Container: *c,
			Status:    "FAILED",
		}
	}

	if container == nil {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"container": container,
	})
}

// handleProgress 处理进度请求
func (am *ApplicationMaster) handleProgress(w http.ResponseWriter, r *http.Request) {
	progress := map[string]interface{}{
		"progress":   am.GetProgress(),
		"state":      am.GetState(),
		"statistics": am.GetContainerStatistics(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(progress)
}

// handleMetrics 处理指标请求
func (am *ApplicationMaster) handleMetrics(w http.ResponseWriter, r *http.Request) {
	am.mu.RLock()
	metrics := map[string]interface{}{
		"applicationId":        am.applicationID,
		"applicationAttemptId": am.applicationAttemptID,
		"state":                am.applicationState,
		"progress":             am.progress,
		"containers": map[string]interface{}{
			"allocated": len(am.allocatedContainers),
			"completed": len(am.completedContainers),
			"failed":    len(am.failedContainers),
			"pending":   len(am.pendingRequests),
		},
		"memory": map[string]interface{}{
			"requested": am.calculateRequestedMemory(),
			"allocated": am.calculateAllocatedMemory(),
		},
		"vcores": map[string]interface{}{
			"requested": am.calculateRequestedVCores(),
			"allocated": am.calculateAllocatedVCores(),
		},
	}
	am.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"metrics": metrics,
	})
}

// handleShutdown 处理关闭请求
func (am *ApplicationMaster) handleShutdown(w http.ResponseWriter, r *http.Request) {
	am.logger.Info("Received shutdown request")

	// 设置最终状态
	am.mu.Lock()
	am.applicationState = "FINISHING"
	am.finalStatus = "SUCCEEDED"
	am.mu.Unlock()

	// 发送关闭信号
	go func() {
		am.shutdownHook <- struct{}{}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Shutdown initiated",
	})
}

// handleWebUI 处理 Web UI 请求
func (am *ApplicationMaster) handleWebUI(w http.ResponseWriter, r *http.Request) {
	// 简单的 HTML 页面
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>ApplicationMaster Web UI</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background: #f0f0f0; padding: 20px; margin-bottom: 20px; }
        .section { margin-bottom: 30px; }
        .container-table { width: 100%; border-collapse: collapse; }
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
        <p>Application ID: ` + fmt.Sprintf("%d_%d", am.applicationID.ClusterTimestamp, am.applicationID.ID) + `</p>
        <p>State: <span id="state">` + am.GetState() + `</span></p>
        <p>Progress: <span id="progress">` + fmt.Sprintf("%.1f%%", am.GetProgress()*100) + `</span></p>
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
                    document.getElementById('progress').textContent = (data.status.progress * 100).toFixed(1) + '%';
                    
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
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// loggingMiddleware 日志中间件
func (am *ApplicationMaster) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		am.logger.Debug("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("duration", time.Since(start)))
	})
}

// corsMiddleware CORS 中间件
func (am *ApplicationMaster) corsMiddleware(next http.Handler) http.Handler {
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

// ContainerInfo 容器信息
type ContainerInfo struct {
	Container common.Container `json:"container"`
	Status    string           `json:"status"`
}

// 计算资源使用情况的辅助方法
func (am *ApplicationMaster) calculateRequestedMemory() int64 {
	var total int64
	for _, req := range am.pendingRequests {
		total += req.Resource.Memory
	}
	return total
}

func (am *ApplicationMaster) calculateAllocatedMemory() int64 {
	var total int64
	for _, container := range am.allocatedContainers {
		total += container.Resource.Memory
	}
	return total
}

func (am *ApplicationMaster) calculateRequestedVCores() int32 {
	var total int32
	for _, req := range am.pendingRequests {
		total += req.Resource.VCores
	}
	return total
}

func (am *ApplicationMaster) calculateAllocatedVCores() int32 {
	var total int32
	for _, container := range am.allocatedContainers {
		total += container.Resource.VCores
	}
	return total
}
