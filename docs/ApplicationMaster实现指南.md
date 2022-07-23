# ApplicationMaster 实现文档

## 概述

ApplicationMaster (AM) 是 YARN 架构中的核心组件，负责协调特定应用程序的执行。本文档详细介绍了 Carrot 项目中 ApplicationMaster 的实现。

## 架构设计

### 核心组件

1. **ApplicationMaster** - 主控制器
   - 管理应用程序生命周期
   - 协调容器资源请求和释放
   - 监控任务执行状态
   - 提供 Web UI 和 REST API

2. **ResourceManager Client** - RM 通信客户端
   - 注册/注销 ApplicationMaster
   - 发送心跳和资源请求
   - 接收容器分配信息

3. **NodeManager Client** - NM 通信客户端
   - 启动和停止容器
   - 获取容器状态
   - 获取容器日志

4. **HTTP Server** - Web 服务器
   - 提供 REST API
   - 展示应用程序状态
   - 实时监控界面

### 应用程序类型

#### 1. 简单应用程序 (SimpleApplication)

适用于独立任务执行场景：

```go
app := applicationmaster.NewSimpleApplication(am, 3)
err := app.Run(ctx)
```

特点：
- 每个任务在独立容器中运行
- 任务之间无依赖关系
- 适合批处理作业

#### 2. 分布式应用程序 (DistributedApplication)

适用于主从架构应用：

```go
app := applicationmaster.NewDistributedApplication(am, 3)
err := app.Run(ctx)
```

特点：
- 包含一个主任务和多个工作任务
- 主任务协调工作任务执行
- 适合分布式计算框架

## 核心功能

### 1. 生命周期管理

```
ApplicationMaster 生命周期：

1. 启动 → 2. 注册RM → 3. 请求容器 → 4. 启动容器 → 5. 监控执行 → 6. 清理资源 → 7. 注销RM → 8. 停止
     ↑                                                                                                ↓
     └─────────────────────────────── 心跳通信 ──────────────────────────────────────────────────────┘
```

### 2. 容器管理

- **容器请求**: 根据应用需求向 ResourceManager 请求容器
- **容器分配**: 处理 ResourceManager 分配的容器
- **容器启动**: 通过 NodeManager 启动容器
- **容器监控**: 定期检查容器状态
- **容器清理**: 释放不再需要的容器

### 3. 状态监控

ApplicationMaster 维护以下状态信息：

```go
type ApplicationMaster struct {
    allocatedContainers  map[string]*common.Container  // 已分配容器
    completedContainers  map[string]*common.Container  // 已完成容器
    failedContainers     map[string]*common.Container  // 失败容器
    pendingRequests      []*common.ContainerRequest    // 待处理请求
    progress             float32                       // 执行进度
    applicationState     string                        // 应用状态
}
```

### 4. Web UI

ApplicationMaster 提供了直观的 Web 界面：

- **应用程序概览**: 显示基本信息和状态
- **容器管理**: 实时显示容器分配情况
- **资源监控**: 展示内存和 CPU 使用情况
- **自动刷新**: 每 5 秒自动更新状态

访问地址: `http://localhost:8088`

## API 接口

### REST API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/ws/v1/appmaster/info` | GET | 获取应用程序信息 |
| `/ws/v1/appmaster/status` | GET | 获取运行状态 |
| `/ws/v1/appmaster/containers` | GET | 获取容器列表 |
| `/ws/v1/appmaster/progress` | GET | 获取执行进度 |
| `/ws/v1/appmaster/metrics` | GET | 获取性能指标 |
| `/ws/v1/appmaster/shutdown` | POST | 关闭应用程序 |

### API 响应示例

#### 应用程序信息
```json
{
  "appmaster": {
    "applicationId": {
      "cluster_timestamp": 1692969600,
      "id": 1
    },
    "state": "RUNNING",
    "progress": 0.67,
    "trackingUrl": "http://localhost:8088"
  }
}
```

#### 容器状态
```json
{
  "containers": [
    {
      "container": {
        "id": {
          "application_attempt_id": {
            "application_id": {"cluster_timestamp": 1692969600, "id": 1},
            "attempt_id": 1
          },
          "container_id": 1
        },
        "node_id": {"host": "worker-1", "port": 8042},
        "resource": {"memory": 1024, "vcores": 1},
        "state": "RUNNING"
      },
      "status": "ALLOCATED"
    }
  ]
}
```

## 配置参数

### 命令行参数

```bash
./bin/applicationmaster \
    -application_id="1692969600_1" \
    -application_attempt_id="1692969600_1_1" \
    -rm_address="http://localhost:8030" \
    -app_type="simple" \
    -num_tasks=3 \
    -port=8088 \
    -heartbeat_interval=10s \
    -max_retries=3 \
    -debug=true
```

### 参数说明

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `application_id` | string | 必需 | 应用程序 ID |
| `application_attempt_id` | string | 必需 | 应用程序尝试 ID |
| `rm_address` | string | `http://localhost:8030` | ResourceManager 地址 |
| `app_type` | string | `simple` | 应用程序类型 |
| `num_tasks` | int | 3 | 简单应用任务数 |
| `num_workers` | int | 2 | 分布式应用工作节点数 |
| `port` | int | 8088 | HTTP 服务端口 |
| `heartbeat_interval` | duration | 10s | 心跳间隔 |
| `max_retries` | int | 3 | 最大重试次数 |
| `debug` | bool | false | 调试模式 |

## 使用示例

### 1. 启动简单应用程序

```bash
# 生成应用程序 ID
APP_ID="$(date +%s)_1"
ATTEMPT_ID="${APP_ID}_1"

# 启动 ApplicationMaster
./bin/applicationmaster \
    -application_id="$APP_ID" \
    -application_attempt_id="$ATTEMPT_ID" \
    -app_type="simple" \
    -num_tasks=5 \
    -debug=true
```

### 2. 启动分布式应用程序

```bash
# 生成应用程序 ID
APP_ID="$(date +%s)_2"
ATTEMPT_ID="${APP_ID}_1"

# 启动 ApplicationMaster
./bin/applicationmaster \
    -application_id="$APP_ID" \
    -application_attempt_id="$ATTEMPT_ID" \
    -app_type="distributed" \
    -num_workers=4 \
    -port=8089 \
    -debug=true
```

### 3. 通过 ResourceManager 自动启动

通常情况下，ApplicationMaster 由 ResourceManager 自动启动：

```bash
# 1. 提交应用程序到 ResourceManager
curl -X POST http://localhost:8030/ws/v1/cluster/apps \
    -H "Content-Type: application/json" \
    -d '{
        "application_name": "test-app",
        "application_type": "YARN",
        "queue": "default",
        "am_container_spec": {
            "commands": ["./bin/applicationmaster", "-app_type=simple"]
        }
    }'

# 2. ResourceManager 会自动分配容器并启动 ApplicationMaster
```

## 监控和调试

### 1. 日志监控

ApplicationMaster 使用结构化日志记录重要事件：

```go
logger.Info("Container allocated",
    zap.String("container_id", containerKey),
    zap.String("node", nodeHost),
    zap.Int64("memory", resource.Memory),
    zap.Int32("vcores", resource.VCores))
```

### 2. 性能指标

通过 `/ws/v1/appmaster/metrics` 端点获取性能指标：

- 容器分配统计
- 内存和 CPU 使用情况
- 应用程序执行进度
- 错误和重试次数

### 3. Web UI 监控

Web UI 提供实时监控界面：

- 自动刷新状态信息
- 容器状态可视化
- 资源使用图表
- 日志查看功能

## 错误处理

### 1. 容器失败处理

```go
func (am *ApplicationMaster) handleFailedContainer(containerID common.ContainerID) {
    // 记录失败信息
    am.logger.Error("Container failed", zap.String("container_id", containerKey))
    
    // 移动到失败列表
    am.failedContainers[containerKey] = container
    
    // 根据重试策略决定是否重新请求容器
    if retryCount < am.maxContainerRetries {
        am.RequestContainers([]*common.ContainerRequest{originalRequest})
    }
}
```

### 2. 网络异常处理

- ResourceManager 连接失败时的重试机制
- NodeManager 通信异常的处理
- 心跳超时的恢复策略

### 3. 资源不足处理

- 容器请求被拒绝时的等待策略
- 资源竞争的处理机制
- 降级执行策略

## 扩展开发

### 1. 自定义应用程序类型

```go
type CustomApplication struct {
    am     *ApplicationMaster
    logger *zap.Logger
    // 自定义字段
}

func (app *CustomApplication) Run(ctx context.Context) error {
    // 实现自定义应用程序逻辑
    return nil
}
```

### 2. 自定义调度策略

```go
func (am *ApplicationMaster) customScheduleLogic() {
    // 根据应用特性实现自定义调度
    // 例如：优先级调度、位置感知调度等
}
```

### 3. 监控扩展

```go
func (am *ApplicationMaster) registerCustomMetrics() {
    // 注册自定义性能指标
    // 集成 Prometheus 等监控系统
}
```

## 最佳实践

### 1. 资源管理

- 合理设置容器资源大小
- 避免资源浪费和不足
- 实现动态资源调整

### 2. 容错设计

- 实现容器失败重试机制
- 设计应用程序检查点
- 处理节点故障场景

### 3. 性能优化

- 优化容器启动时间
- 减少网络通信开销
- 实现任务并行执行

### 4. 监控告警

- 设置关键指标告警
- 实现日志聚合分析
- 监控资源使用趋势

## 故障排查

### 常见问题

1. **ApplicationMaster 启动失败**
   - 检查应用程序 ID 格式
   - 验证 ResourceManager 连接
   - 查看启动日志错误信息

2. **容器分配失败**
   - 检查集群资源是否充足
   - 验证资源请求是否合理
   - 查看调度器日志

3. **容器启动失败**
   - 检查 NodeManager 状态
   - 验证容器启动命令
   - 查看容器日志

4. **性能问题**
   - 分析资源使用情况
   - 检查网络延迟
   - 优化任务并行度

### 调试工具

- 使用 `-debug=true` 启用详细日志
- 通过 Web UI 监控实时状态
- 使用 API 获取详细信息
- 查看 ResourceManager 和 NodeManager 日志

## 总结

ApplicationMaster 是 YARN 架构中连接资源管理和应用执行的关键组件。本实现提供了：

- 完整的应用程序生命周期管理
- 灵活的应用程序开发框架
- 丰富的监控和调试功能
- 良好的扩展性和可维护性

通过合理使用 ApplicationMaster，可以构建高效、可靠的分布式应用程序。
