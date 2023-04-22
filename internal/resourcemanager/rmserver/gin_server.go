package rmserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"carrot/internal/common"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

// GinServer ResourceManager Gin HTTP 服务器
type GinServer struct {
	engine *gin.Engine
	server *http.Server
	logger *zap.Logger
	rm     ResourceManagerInterface
}

// NewGinServer 创建新的 Gin 服务器
func NewGinServer(rm ResourceManagerInterface, logger *zap.Logger) *GinServer {
	return &GinServer{
		rm:     rm,
		logger: logger,
	}
}

// Start 启动 Gin HTTP 服务器
func (s *GinServer) Start(port int) error {
	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	s.engine = gin.New()

	// 添加中间件
	s.engine.Use(s.ginLoggerMiddleware())
	s.engine.Use(gin.Recovery())
	s.engine.Use(s.corsMiddleware())

	// Swagger 文档路由
	s.engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	// 添加 Swagger 根路径重定向
	s.engine.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})

	// API 路由组
	v1 := s.engine.Group("/ws/v1")
	{
		cluster := v1.Group("/cluster")
		{
			cluster.GET("/info", s.handleClusterInfo)

			apps := cluster.Group("/apps")
			{
				apps.GET("", s.handleApplicationsList)
				apps.POST("", s.handleApplicationSubmit)
				apps.POST("/new-application", s.handleNewApplication)
				apps.GET("/:appId", s.handleApplicationGet)
				apps.DELETE("/:appId", s.handleApplicationDelete)
			}

			nodes := cluster.Group("/nodes")
			{
				nodes.GET("", s.handleNodesList)
				nodes.POST("/register", s.handleNodeRegistration)
				nodes.POST("/heartbeat", s.handleNodeHeartbeat)
				nodes.GET("/health", s.handleNodeHealth)
			}
		}
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s.engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 在后台启动服务器
	go func() {
		s.logger.Info("启动 ResourceManager Gin HTTP 服务器中", zap.String("addr", s.server.Addr))
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("启动 ResourceManager Gin HTTP 服务器失败", zap.Error(err))
		}
	}()

	return nil
}

// Stop 停止 HTTP 服务器
func (s *GinServer) Stop() error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.logger.Info("停止 ResourceManager Gin HTTP 服务器")
	return s.server.Shutdown(ctx)
}

// GetType 获取服务器类型
func (s *GinServer) GetType() common.ServerType {
	return common.ServerTypeHTTP
}

// GetAddress 获取服务器地址
func (s *GinServer) GetAddress() string {
	if s.server != nil {
		return s.server.Addr
	}
	return ""
}

// IsRunning 检查服务器是否在运行
func (s *GinServer) IsRunning() bool {
	return s.server != nil
}

// handleClusterInfo 处理集群信息请求
// @Summary 获取集群信息
// @Description 获取 YARN 集群的基本信息和状态
// @Tags cluster
// @Produce json
// @Success 200 {object} map[string]interface{} "集群信息"
// @Router /cluster/info [get]
func (s *GinServer) handleClusterInfo(c *gin.Context) {
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

	c.JSON(http.StatusOK, info)
}

// handleApplicationsList 处理应用程序列表请求
// @Summary 获取应用程序列表
// @Description 获取集群中所有应用程序的列表
// @Tags applications
// @Produce json
// @Success 200 {object} map[string]interface{} "应用程序列表"
// @Router /cluster/apps [get]
func (s *GinServer) handleApplicationsList(c *gin.Context) {
	apps := s.rm.GetApplications()
	c.JSON(http.StatusOK, map[string]interface{}{
		"apps": map[string]interface{}{
			"app": apps,
		},
	})
}

// ApplicationSubmissionRequest 应用程序提交请求
type ApplicationSubmissionRequest struct {
	ApplicationID   common.ApplicationID          `json:"application_id" binding:"required"`
	ApplicationName string                        `json:"application_name" binding:"required"`
	ApplicationType string                        `json:"application_type" binding:"required"`
	Queue           string                        `json:"queue" binding:"required"`
	Priority        int32                         `json:"priority"`
	Resource        common.Resource               `json:"resource" binding:"required"`
	AMContainerSpec common.ContainerLaunchContext `json:"am_container_spec" binding:"required"`
	MaxAppAttempts  int32                         `json:"max_app_attempts"`
}

// handleApplicationSubmit 处理应用程序提交请求
// @Summary 提交应用程序
// @Description 向 YARN 集群提交新的应用程序
// @Tags applications
// @Accept json
// @Produce json
// @Param application body ApplicationSubmissionRequest true "应用程序提交信息"
// @Success 201 {object} map[string]interface{} "应用程序ID"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 500 {object} map[string]interface{} "内部服务器错误"
// @Router /cluster/apps [post]
func (s *GinServer) handleApplicationSubmit(c *gin.Context) {
	var req ApplicationSubmissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Error("Invalid request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := common.ApplicationSubmissionContext{
		ApplicationID:   req.ApplicationID,
		ApplicationName: req.ApplicationName,
		ApplicationType: req.ApplicationType,
		Queue:           req.Queue,
		Priority:        req.Priority,
		Resource:        req.Resource,
		AMContainerSpec: req.AMContainerSpec,
		MaxAppAttempts:  req.MaxAppAttempts,
	}

	appID, err := s.rm.SubmitApplication(ctx)
	if err != nil {
		s.logger.Error("Failed to submit application", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"application-id": appID})
}

// handleNewApplication 处理新应用程序ID请求
// @Summary 获取新应用程序ID
// @Description 为新应用程序分配唯一的应用程序ID
// @Tags applications
// @Produce json
// @Success 200 {object} map[string]interface{} "新应用程序ID"
// @Router /cluster/apps/new-application [post]
func (s *GinServer) handleNewApplication(c *gin.Context) {
	clusterTimestamp := s.rm.GetClusterTimestamp()

	// 生成新的应用程序ID (简化实现)
	appID := &common.ApplicationID{
		ClusterTimestamp: clusterTimestamp,
		ID:               int32(time.Now().Unix() % 10000),
	}

	c.JSON(http.StatusOK, gin.H{"application-id": appID})
}

// handleApplicationGet 处理单个应用程序查询请求
// @Summary 获取应用程序详情
// @Description 根据应用程序ID获取应用程序的详细信息
// @Tags applications
// @Produce json
// @Param appId path string true "应用程序ID"
// @Success 200 {object} map[string]interface{} "应用程序详情"
// @Failure 404 {object} map[string]interface{} "应用程序未找到"
// @Router /cluster/apps/{appId} [get]
func (s *GinServer) handleApplicationGet(c *gin.Context) {
	appId := c.Param("appId")

	apps := s.rm.GetApplications()
	for _, app := range apps {
		if app.ApplicationID.String() == appId {
			c.JSON(http.StatusOK, map[string]interface{}{
				"app": app,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "应用未找到"})
}

// handleApplicationDelete 处理应用程序删除请求
// @Summary 删除应用程序
// @Description 终止并删除指定的应用程序
// @Tags applications
// @Param appId path string true "应用程序ID"
// @Success 200 {object} map[string]interface{} "删除成功"
// @Failure 404 {object} map[string]interface{} "应用程序未找到"
// @Router /cluster/apps/{appId} [delete]
func (s *GinServer) handleApplicationDelete(c *gin.Context) {
	appId := c.Param("appId")

	// 这里应该实现实际的应用程序终止逻辑
	s.logger.Info("Terminating application", zap.String("appId", appId))

	c.JSON(http.StatusOK, gin.H{"message": "Application terminated successfully"})
}

// handleNodesList 处理节点列表请求
// @Summary 获取节点列表
// @Description 获取集群中所有节点的列表和状态信息
// @Tags nodes
// @Produce json
// @Success 200 {object} map[string]interface{} "节点列表"
// @Router /cluster/nodes [get]
func (s *GinServer) handleNodesList(c *gin.Context) {
	nodes := s.rm.GetNodes()
	c.JSON(http.StatusOK, map[string]interface{}{
		"nodes": map[string]interface{}{
			"node": nodes,
		},
	})
}

// NodeRegistrationRequest 节点注册请求
type NodeRegistrationRequest struct {
	NodeID      common.NodeID   `json:"node_id" binding:"required"`
	Resource    common.Resource `json:"resource" binding:"required"`
	HTTPAddress string          `json:"http_address" binding:"required"`
}

// handleNodeRegistration 处理节点注册请求
// @Summary 注册节点
// @Description 向资源管理器注册新的节点管理器
// @Tags nodes
// @Accept json
// @Produce json
// @Param node body NodeRegistrationRequest true "节点注册信息"
// @Success 200 {object} map[string]interface{} "注册成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 500 {object} map[string]interface{} "内部服务器错误"
// @Router /cluster/nodes/register [post]
func (s *GinServer) handleNodeRegistration(c *gin.Context) {
	var req NodeRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Error("Invalid request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := s.rm.RegisterNode(req.NodeID, req.Resource, req.HTTPAddress)
	if err != nil {
		s.logger.Error("Failed to register node", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "registered"})
}

// NodeHeartbeatRequest 节点心跳请求
type NodeHeartbeatRequest struct {
	NodeID       common.NodeID       `json:"node_id" binding:"required"`
	UsedResource common.Resource     `json:"used_resource" binding:"required"`
	Containers   []*common.Container `json:"containers"`
}

// handleNodeHeartbeat 处理节点心跳请求
// @Summary 节点心跳
// @Description 节点管理器向资源管理器发送心跳信息
// @Tags nodes
// @Accept json
// @Produce json
// @Param heartbeat body NodeHeartbeatRequest true "心跳信息"
// @Success 200 {object} map[string]interface{} "心跳响应"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 500 {object} map[string]interface{} "内部服务器错误"
// @Router /cluster/nodes/heartbeat [post]
func (s *GinServer) handleNodeHeartbeat(c *gin.Context) {
	var req NodeHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Error("Invalid request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := s.rm.NodeHeartbeat(req.NodeID, req.UsedResource, req.Containers)
	if err != nil {
		s.logger.Error("Failed to process node heartbeat", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response_id": time.Now().Unix(),
		"status":      "ok",
	})
}

// handleNodeHealth 处理节点健康状态请求
// @Summary 获取节点健康状态
// @Description 获取集群中所有节点的健康状态统计
// @Tags nodes
// @Produce json
// @Success 200 {object} map[string]interface{} "节点健康状态"
// @Router /cluster/nodes/health [get]
func (s *GinServer) handleNodeHealth(c *gin.Context) {
	healthStatus := s.rm.GetNodeHealthStatus()
	c.JSON(http.StatusOK, gin.H{
		"nodeHealth": healthStatus,
		"timestamp":  time.Now().Unix(),
	})
}

// ginLoggerMiddleware 自定义日志中间件
func (s *GinServer) ginLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		s.logger.Debug("HTTP request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("remote_addr", c.ClientIP()))

		c.Next()

		duration := time.Since(start)
		s.logger.Debug("HTTP response",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Duration("duration", duration),
			zap.Int("status", c.Writer.Status()))
	}
}

// corsMiddleware CORS中间件
func (s *GinServer) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
