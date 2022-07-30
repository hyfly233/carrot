# Carrot YARN API 参考

本文档提供 Carrot YARN 所有组件的完整 REST API 参考。

## 📋 目录

-   [概述](#概述)
-   [认证](#认证)
-   [ResourceManager API](#resourcemanager-api)
-   [NodeManager API](#nodemanager-api)
-   [ApplicationMaster API](#applicationmaster-api)
-   [Client API](#client-api)
-   [错误处理](#错误处理)
-   [SDK 示例](#sdk-示例)

## 🌐 概述

### API 版本

当前 API 版本: `v1`

所有 API 端点都以 `/api/v1` 为前缀。

### 内容类型

-   **请求**: `application/json`
-   **响应**: `application/json`

### 基础 URL

```
http://<host>:<port>/api/v1
```

### 通用响应格式

```json
{
    "success": true,
    "data": {
        // 响应数据
    },
    "error": null,
    "timestamp": "2025-08-26T10:00:00Z"
}
```

错误响应格式：

```json
{
    "success": false,
    "data": null,
    "error": {
        "code": "ERROR_CODE",
        "message": "错误描述",
        "details": {}
    },
    "timestamp": "2025-08-26T10:00:00Z"
}
```

## 🔐 认证

### JWT Token 认证

在请求头中包含 JWT token：

```
Authorization: Bearer <jwt_token>
```

### 获取 Token

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "password"
}
```

响应：

```json
{
    "success": true,
    "data": {
        "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
        "expires_at": "2025-08-27T10:00:00Z",
        "user": {
            "username": "admin",
            "roles": ["admin"]
        }
    }
}
```

### 刷新 Token

```http
POST /api/v1/auth/refresh
Authorization: Bearer <current_token>
```

## 🏢 ResourceManager API

### 集群信息

#### 获取集群信息

```http
GET /api/v1/cluster/info
```

响应：

```json
{
    "success": true,
    "data": {
        "cluster_id": "carrot-cluster-001",
        "cluster_name": "Production Cluster",
        "start_time": "2025-08-26T08:00:00Z",
        "state": "STARTED",
        "resource_manager_version": "1.0.0",
        "resource_manager_build_date": "2025-08-26",
        "resource_manager_build_version": "1.0.0-SNAPSHOT",
        "hadoop_version_built_on": "unknown",
        "hadoop_build_version": "unknown",
        "hadoop_version": "unknown"
    }
}
```

#### 获取集群指标

```http
GET /api/v1/cluster/metrics
```

响应：

```json
{
    "success": true,
    "data": {
        "apps_submitted": 1000,
        "apps_completed": 950,
        "apps_pending": 30,
        "apps_running": 20,
        "apps_failed": 15,
        "apps_killed": 15,
        "memory_total": 1048576,
        "memory_available": 524288,
        "memory_allocated": 524288,
        "memory_reserved": 0,
        "vcores_total": 256,
        "vcores_available": 128,
        "vcores_allocated": 128,
        "vcores_reserved": 0,
        "nodes_total": 32,
        "nodes_lost": 0,
        "nodes_unhealthy": 0,
        "nodes_decommissioned": 0,
        "nodes_decommissioning": 0,
        "nodes_rebooted": 0,
        "nodes_active": 32
    }
}
```

### 应用程序管理

#### 提交应用程序

```http
POST /api/v1/applications
Content-Type: application/json

{
  "application_name": "MyApp",
  "application_type": "simple",
  "queue": "default",
  "priority": 0,
  "am_container_spec": {
    "resource": {
      "memory": 1024,
      "vcores": 1
    },
    "commands": ["./my-app"]
  },
  "max_app_attempts": 3,
  "keep_containers_across_application_attempts": false,
  "application_tags": ["tag1", "tag2"],
  "log_aggregation_context": {
    "log_include_pattern": "*.log",
    "log_exclude_pattern": "*.tmp"
  }
}
```

响应：

```json
{
    "success": true,
    "data": {
        "application_id": "application_1629974400000_0001",
        "application_name": "MyApp",
        "application_type": "simple",
        "user": "admin",
        "queue": "default",
        "state": "SUBMITTED",
        "final_status": "UNDEFINED",
        "progress": 0,
        "tracking_url": "http://localhost:8080",
        "diagnostics": "",
        "cluster_id": "carrot-cluster-001",
        "start_time": "2025-08-26T10:00:00Z",
        "finish_time": null,
        "elapsed_time": 0,
        "am_container_logs": "http://localhost:8042/node/containerlogs/...",
        "am_host_http_address": "localhost:8080",
        "allocated_memory": 0,
        "allocated_vcores": 0,
        "running_containers": 0,
        "memory_seconds": 0,
        "vcore_seconds": 0,
        "preempted_resource_memory": 0,
        "preempted_resource_vcores": 0,
        "num_non_am_container_preempted": 0,
        "num_am_container_preempted": 0
    }
}
```

#### 获取应用程序列表

```http
GET /api/v1/applications?state=RUNNING&queue=default&limit=10&offset=0
```

查询参数：

| 参数     | 类型   | 描述         | 默认值 |
| -------- | ------ | ------------ | ------ |
| `state`  | string | 应用程序状态 | all    |
| `queue`  | string | 队列名称     | all    |
| `user`   | string | 用户名       | all    |
| `limit`  | int    | 返回数量限制 | 100    |
| `offset` | int    | 偏移量       | 0      |

#### 获取应用程序详情

```http
GET /api/v1/applications/{application_id}
```

#### 终止应用程序

```http
DELETE /api/v1/applications/{application_id}
```

#### 获取应用程序尝试列表

```http
GET /api/v1/applications/{application_id}/attempts
```

#### 获取应用程序容器列表

```http
GET /api/v1/applications/{application_id}/containers
```

### 节点管理

#### 获取节点列表

```http
GET /api/v1/nodes?state=RUNNING&healthy=true
```

查询参数：

| 参数      | 类型    | 描述     | 默认值 |
| --------- | ------- | -------- | ------ |
| `state`   | string  | 节点状态 | all    |
| `healthy` | boolean | 健康状态 | all    |

响应：

```json
{
    "success": true,
    "data": {
        "nodes": [
            {
                "node_id": "node1.example.com:8042",
                "node_host_name": "node1.example.com",
                "node_http_address": "node1.example.com:8042",
                "last_health_update": "2025-08-26T10:00:00Z",
                "health_report": "Healthy",
                "num_containers": 5,
                "used_memory": 2048,
                "available_memory": 6144,
                "used_vcores": 3,
                "available_vcores": 5,
                "version": "1.0.0",
                "state": "RUNNING"
            }
        ],
        "total": 1
    }
}
```

#### 获取节点详情

```http
GET /api/v1/nodes/{node_id}
```

### 队列管理

#### 获取调度器信息

```http
GET /api/v1/scheduler
```

响应：

```json
{
    "success": true,
    "data": {
        "type": "capacity",
        "scheduler_info": {
            "queue_name": "root",
            "capacity": 100.0,
            "used_capacity": 45.5,
            "max_capacity": 100.0,
            "absolute_capacity": 100.0,
            "absolute_max_capacity": 100.0,
            "absolute_used_capacity": 45.5,
            "num_applications": 25,
            "queues": [
                {
                    "queue_name": "default",
                    "capacity": 60.0,
                    "used_capacity": 50.0,
                    "max_capacity": 100.0,
                    "num_applications": 15
                },
                {
                    "queue_name": "production",
                    "capacity": 40.0,
                    "used_capacity": 75.0,
                    "max_capacity": 80.0,
                    "num_applications": 10
                }
            ]
        }
    }
}
```

## 🖥️ NodeManager API

### 节点信息

#### 获取节点信息

```http
GET /api/v1/node/info
```

响应：

```json
{
    "success": true,
    "data": {
        "node_id": "node1.example.com:8042",
        "node_host_name": "node1.example.com",
        "node_manager_version": "1.0.0",
        "node_manager_build_date": "2025-08-26",
        "node_manager_version_built_on": "1.0.0-SNAPSHOT",
        "hadoop_version": "unknown",
        "hadoop_build_version": "unknown",
        "hadoop_version_built_on": "unknown",
        "total_memory": 8192,
        "total_vcores": 8,
        "last_node_update_time": "2025-08-26T10:00:00Z",
        "health_report": "Healthy",
        "node_healthy": true,
        "node_manager_start_time": "2025-08-26T08:00:00Z"
    }
}
```

### 容器管理

#### 获取容器列表

```http
GET /api/v1/containers
```

响应：

```json
{
    "success": true,
    "data": {
        "containers": [
            {
                "container_id": "container_1629974400000_0001_01_000001",
                "state": "RUNNING",
                "exit_code": null,
                "diagnostics": "",
                "user": "admin",
                "total_memory_needed": 1024,
                "total_vcores_needed": 1,
                "container_logs_link": "http://node1.example.com:8042/node/containerlogs/..."
            }
        ]
    }
}
```

#### 获取容器详情

```http
GET /api/v1/containers/{container_id}
```

#### 获取容器日志

```http
GET /api/v1/containers/{container_id}/logs?start=0&end=1000
```

查询参数：

| 参数    | 类型 | 描述         | 默认值    |
| ------- | ---- | ------------ | --------- |
| `start` | int  | 起始字节位置 | 0         |
| `end`   | int  | 结束字节位置 | -1 (全部) |

### 健康检查

#### 获取节点健康状态

```http
GET /api/v1/node/health
```

响应：

```json
{
    "success": true,
    "data": {
        "healthy": true,
        "health_report": "Healthy",
        "last_health_update": "2025-08-26T10:00:00Z",
        "node_utilization": {
            "memory_utilization": 45.5,
            "cpu_utilization": 65.2,
            "disk_utilization": 35.8
        }
    }
}
```

## 📱 ApplicationMaster API

### 应用程序管理

#### 注册 ApplicationMaster

```http
POST /api/v1/application/register
Content-Type: application/json

{
  "host": "am.example.com",
  "rpc_port": 8080,
  "tracking_url": "http://am.example.com:8080"
}
```

#### 资源请求

```http
POST /api/v1/application/allocate
Content-Type: application/json

{
  "response_id": 1,
  "progress": 0.5,
  "ask": [
    {
      "priority": 1,
      "resource_name": "*",
      "capability": {
        "memory": 1024,
        "vcores": 1
      },
      "num_containers": 3,
      "relax_locality": true,
      "node_label_expression": ""
    }
  ],
  "release": [
    "container_1629974400000_0001_01_000002"
  ],
  "blacklist_request": {
    "blacklist_additions": ["badnode.example.com"],
    "blacklist_removals": []
  }
}
```

响应：

```json
{
    "success": true,
    "data": {
        "response_id": 2,
        "allocated_containers": [
            {
                "container_id": "container_1629974400000_0001_01_000003",
                "node_id": "node1.example.com:8042",
                "node_http_address": "node1.example.com:8042",
                "resource": {
                    "memory": 1024,
                    "vcores": 1
                },
                "priority": 1,
                "container_token": "..."
            }
        ],
        "completed_containers": [],
        "limit": 1000,
        "updated_nodes": [],
        "num_cluster_nodes": 32,
        "preempt": [],
        "nm_tokens": [],
        "am_rm_token": null
    }
}
```

#### 完成 ApplicationMaster

```http
POST /api/v1/application/finish
Content-Type: application/json

{
  "final_application_status": "SUCCEEDED",
  "diagnostics": "Application completed successfully",
  "tracking_url": "http://am.example.com:8080/final"
}
```

## 👥 Client API

### 应用程序操作

#### 提交应用程序 (简化版)

```http
POST /api/v1/client/applications/submit
Content-Type: application/json

{
  "name": "MySimpleApp",
  "type": "simple",
  "queue": "default",
  "command": "./my-script.sh",
  "memory": 1024,
  "vcores": 1
}
```

#### 获取应用程序状态

```http
GET /api/v1/client/applications/{application_id}/status
```

#### 终止应用程序

```http
POST /api/v1/client/applications/{application_id}/kill
```

### 集群状态

#### 获取集群概览

```http
GET /api/v1/client/cluster/overview
```

响应：

```json
{
    "success": true,
    "data": {
        "cluster_name": "Production Cluster",
        "total_nodes": 32,
        "active_nodes": 32,
        "total_memory": "256 GB",
        "available_memory": "128 GB",
        "total_vcores": 256,
        "available_vcores": 128,
        "running_applications": 25,
        "pending_applications": 5,
        "completed_applications": 1000
    }
}
```

## ❌ 错误处理

### 错误代码

| 错误代码              | HTTP 状态码 | 描述           |
| --------------------- | ----------- | -------------- |
| `INVALID_REQUEST`     | 400         | 请求参数无效   |
| `UNAUTHORIZED`        | 401         | 未授权访问     |
| `FORBIDDEN`           | 403         | 权限不足       |
| `NOT_FOUND`           | 404         | 资源不存在     |
| `CONFLICT`            | 409         | 资源冲突       |
| `INTERNAL_ERROR`      | 500         | 内部服务器错误 |
| `SERVICE_UNAVAILABLE` | 503         | 服务不可用     |

### 错误响应示例

```json
{
    "success": false,
    "data": null,
    "error": {
        "code": "INVALID_REQUEST",
        "message": "Missing required parameter: application_name",
        "details": {
            "field": "application_name",
            "required": true
        }
    },
    "timestamp": "2025-08-26T10:00:00Z"
}
```

## 🛠️ SDK 示例

### Go SDK

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/hyfly233/carrot/pkg/client"
)

func main() {
    // 创建客户端
    config := &client.Config{
        ResourceManagerURL: "http://localhost:8088",
        Token: "your-jwt-token",
    }

    c := client.New(config)

    // 提交应用程序
    app := &client.Application{
        Name: "MyApp",
        Type: "simple",
        Queue: "default",
        AMContainerSpec: &client.ContainerSpec{
            Resource: &client.Resource{
                Memory: 1024,
                VCores: 1,
            },
            Commands: []string{"./my-app"},
        },
    }

    ctx := context.Background()
    result, err := c.SubmitApplication(ctx, app)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Application submitted: %s\n", result.ApplicationID)

    // 查询应用程序状态
    status, err := c.GetApplicationStatus(ctx, result.ApplicationID)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Application state: %s\n", status.State)
}
```

### Python SDK

```python
from carrot_client import CarrotClient

# 创建客户端
client = CarrotClient(
    rm_url="http://localhost:8088",
    token="your-jwt-token"
)

# 提交应用程序
app_spec = {
    "application_name": "MyPythonApp",
    "application_type": "simple",
    "queue": "default",
    "am_container_spec": {
        "resource": {
            "memory": 1024,
            "vcores": 1
        },
        "commands": ["python", "my_script.py"]
    }
}

try:
    result = client.submit_application(app_spec)
    app_id = result["application_id"]
    print(f"Application submitted: {app_id}")

    # 等待应用程序完成
    status = client.wait_for_completion(app_id, timeout=300)
    print(f"Application finished with status: {status}")

except Exception as e:
    print(f"Error: {e}")
```

### JavaScript SDK

```javascript
const { CarrotClient } = require("carrot-yarn-client");

// 创建客户端
const client = new CarrotClient({
    rmUrl: "http://localhost:8088",
    token: "your-jwt-token",
});

async function submitApplication() {
    try {
        const appSpec = {
            application_name: "MyJSApp",
            application_type: "simple",
            queue: "default",
            am_container_spec: {
                resource: {
                    memory: 1024,
                    vcores: 1,
                },
                commands: ["node", "app.js"],
            },
        };

        const result = await client.submitApplication(appSpec);
        console.log(`Application submitted: ${result.application_id}`);

        // 监控应用程序状态
        const status = await client.getApplicationStatus(result.application_id);
        console.log(`Application state: ${status.state}`);
    } catch (error) {
        console.error("Error:", error);
    }
}

submitApplication();
```

### curl 示例

```bash
# 获取集群信息
curl -X GET \
  "http://localhost:8088/api/v1/cluster/info" \
  -H "Authorization: Bearer your-jwt-token" \
  -H "Content-Type: application/json"

# 提交应用程序
curl -X POST \
  "http://localhost:8088/api/v1/applications" \
  -H "Authorization: Bearer your-jwt-token" \
  -H "Content-Type: application/json" \
  -d '{
    "application_name": "CurlApp",
    "application_type": "simple",
    "queue": "default",
    "am_container_spec": {
      "resource": {
        "memory": 1024,
        "vcores": 1
      },
      "commands": ["./my-app"]
    }
  }'

# 获取应用程序列表
curl -X GET \
  "http://localhost:8088/api/v1/applications?state=RUNNING&limit=10" \
  -H "Authorization: Bearer your-jwt-token"

# 终止应用程序
curl -X DELETE \
  "http://localhost:8088/api/v1/applications/application_1629974400000_0001" \
  -H "Authorization: Bearer your-jwt-token"
```

## 📊 API 限制

### 速率限制

| 端点类型 | 限制          | 窗口期   |
| -------- | ------------- | -------- |
| 读取操作 | 1000 requests | 1 minute |
| 写入操作 | 100 requests  | 1 minute |
| 认证操作 | 10 requests   | 1 minute |

### 分页限制

-   最大页面大小: 1000
-   默认页面大小: 100

### 超时设置

-   连接超时: 30 秒
-   读取超时: 30 秒
-   写入超时: 30 秒

## 📚 相关文档

-   [🏗️ 系统架构](./系统架构.md) - 理解系统设计
-   [🔧 开发指南](./开发指南.md) - 开发环境搭建
-   [⚙️ 配置参考](./配置参考.md) - 配置 API 参数
-   [📖 核心概念](./核心概念.md) - 理解 API 概念
