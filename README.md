# Carrot - Go 语言实现的 Hadoop YARN

[![CI](https://github.com/hyfly233/carrot/workflows/CI/badge.svg)](https://github.com/hyfly233/carrot/actions)
[![Coverage](https://codecov.io/gh/hyfly233/carrot/branch/main/graph/badge.svg)](https://codecov.io/gh/hyfly233/carrot)
[![Go Report Card](https://goreportcard.com/badge/github.com/hyfly233/carrot)](https://goreportcard.com/report/github.com/hyfly233/carrot)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Carrot 是一个使用 Go 语言实现的 Hadoop YARN (Yet Another Resource Negotiator) 集群资源管理系统。它提供了 YARN
核心功能，包括资源管理、任务调度和容器管理。

## 🚀 功能特性

- ✅ **现代化架构** - 使用 Go 语言重新实现，具有更好的性能和可维护性
- ✅ **RESTful API** - 基于 HTTP REST API 的通信协议
- ✅ **多种调度策略** - 支持 FIFO、Capacity、Fair 调度器
- ✅ **容器管理** - 完整的容器生命周期管理
- ✅ **资源监控** - 实时资源监控和分配
- ✅ **高可用性** - 支持节点故障恢复和负载均衡
- ✅ **安全性** - 容器安全隔离和访问控制
- ✅ **可观测性** - 结构化日志和性能指标
- ✅ **云原生** - Docker 容器化部署支持

## 📋 系统要求

- Go 1.21 或更高版本
- Linux/macOS 系统
- Docker (可选，用于容器化部署)

## 🏗️ 架构概述

### 核心组件

1. **ResourceManager (RM)** - 资源管理器
    - 管理集群中的计算资源
    - 接收和调度应用程序
    - 监控 NodeManager 和应用程序状态
    - 提供 Web UI 和 REST API

2. **NodeManager (NM)** - 节点管理器
    - 管理单个节点上的资源
    - 启动和监控容器
    - 向 ResourceManager 发送心跳
    - 本地资源管理和清理

3. **ApplicationMaster (AM)** - 应用程序主控
    - 协调特定应用程序的执行
    - 请求和管理容器资源
    - 监控任务执行状态

4. **Client** - 客户端
    - 提交应用程序到集群
    - 监控应用程序状态
    - 管理应用程序生命周期

### 系统架构图

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Client      │◄──►│ ResourceManager │◄──►│   Web Console   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                               │
                               ▼
                    ┌─────────────────┐
                    │   Scheduler     │
                    └─────────────────┘
                               │
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
    ┌─────────────────┐┌─────────────────┐┌─────────────────┐
    │  NodeManager 1  ││  NodeManager 2  ││  NodeManager N  │
    └─────────────────┘└─────────────────┘└─────────────────┘
              │                │                │
              ▼                ▼                ▼
    ┌─────────────────┐┌─────────────────┐┌─────────────────┐
    │   Containers    ││   Containers    ││   Containers    │
    └─────────────────┘└─────────────────┘└─────────────────┘
```

## 快速开始

### 编译

```bash
# 编译所有组件
make build

# 或者分别编译
go build -o bin/resourcemanager cmd/resourcemanager/main.go
go build -o bin/nodemanager cmd/nodemanager/main.go
go build -o bin/client cmd/client/main.go
```

### 启动集群

1. **启动 ResourceManager**:

```bash
./bin/resourcemanager -port 8088
```

2. **启动 NodeManager**:

```bash
./bin/nodemanager -port 8042 -host localhost -rm-url http://localhost:8088 -memory 8192 -vcores 8
```

3. **提交应用程序**:

```bash
./bin/client -rm-url http://localhost:8088 -app-name "test-job" -command "echo 'Hello YARN!'"
```

### 使用 Docker

```bash
# 构建镜像
docker-compose -f deployments/docker/docker-compose.yml build

# 启动集群
docker-compose -f deployments/docker/docker-compose.yml up
```

## API 接口

### ResourceManager API

- `GET /ws/v1/cluster/info` - 获取集群信息
- `GET /ws/v1/cluster/apps` - 获取应用程序列表
- `POST /ws/v1/cluster/apps` - 提交新应用程序
- `POST /ws/v1/cluster/apps/new-application` - 获取新应用程序 ID
- `GET /ws/v1/cluster/nodes` - 获取节点列表

### NodeManager API

- `GET /ws/v1/node/containers` - 获取容器列表
- `POST /ws/v1/node/containers` - 启动新容器

## 配置

### ResourceManager 配置

```bash
./bin/resourcemanager -h
Usage of ./bin/resourcemanager:
  -port int
    	ResourceManager port (default 8088)
```

### NodeManager 配置

```bash
./bin/nodemanager -h
Usage of ./bin/nodemanager:
  -host string
    	NodeManager host (default "localhost")
  -memory int
    	Total memory in MB (default 8192)
  -port int
    	NodeManager port (default 8042)
  -rm-url string
    	ResourceManager URL (default "http://localhost:8088")
  -vcores int
    	Total virtual cores (default 8)
```

## 项目结构

```
carrot/
├── cmd/                          # 可执行文件入口
│   ├── resourcemanager/          # ResourceManager 主程序
│   ├── nodemanager/              # NodeManager 主程序
│   ├── client/                   # 客户端工具
│   └── applicationmaster/        # ApplicationMaster 主程序
├── internal/                     # 内部包
│   ├── common/                   # 公共类型和工具
│   ├── resourcemanager/          # ResourceManager 实现
│   └── nodemanager/              # NodeManager 实现
├── configs/                      # 配置文件
├── deployments/                  # 部署配置
├── scripts/                      # 脚本文件
└── README.md
```

## 开发指南

### 添加新的调度器

1. 实现 `Scheduler` 接口:

```go
type Scheduler interface {
Schedule(app *Application) ([]*common.Container, error)
AllocateContainers(requests []common.ContainerRequest) ([]*common.Container, error)
}
```

2. 在 ResourceManager 中注册新调度器

### 扩展 API

1. 在相应的服务器中添加新的 HTTP 处理器
2. 更新 API 文档

## 测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./internal/resourcemanager
```

## 性能调优

### ResourceManager 调优

- 调整心跳间隔
- 优化调度算法
- 配置适当的线程池大小

### NodeManager 调优

- 设置合适的资源限制
- 调整容器监控间隔
- 优化容器启动时间

## 监控

### 指标

- 集群资源利用率
- 应用程序完成时间
- 节点健康状态
- 容器成功/失败率

### 日志

所有组件都提供详细的日志输出，可以通过标准输出查看。

## 故障排除

### 常见问题

1. **NodeManager 无法连接到 ResourceManager**
    - 检查网络连接
    - 验证 ResourceManager URL 配置

2. **容器启动失败**
    - 检查资源是否充足
    - 验证命令和环境变量

3. **应用程序长时间处于 SUBMITTED 状态**
    - 检查调度器配置
    - 验证队列设置

## 贡献

欢迎提交 Pull Request 和 Issue。请确保：

1. 代码遵循 Go 语言规范
2. 添加适当的测试
3. 更新相关文档

## 许可证

本项目采用 MIT 许可证。详见 [LICENSE](LICENSE) 文件。

## 致谢

本项目参考了 Apache Hadoop YARN 的设计理念，感谢 Apache Hadoop 社区的贡献。
