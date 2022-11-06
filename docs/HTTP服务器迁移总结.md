# HTTP 服务器迁移总结

## 迁移概述

已成功将 ApplicationMaster、ResourceManager 和 NodeManager 的 HTTP 相关代码迁移到对应的 `server` 目录下的 `http_server.go` 中，并预留了 gRPC、TCP 等其他通信协议的逻辑。

## 迁移的文件结构

### ApplicationMaster
```
internal/applicationmaster/server/
├── http_server.go    # HTTP 服务器实现
├── server.go         # 通用服务器接口和管理器
├── errors.go         # 错误定义
└── example.go        # 使用示例和适配器
```

### ResourceManager
```
internal/resourcemanager/server/
├── http_server.go    # HTTP 服务器实现
├── server.go         # 通用服务器接口和管理器
└── errors.go         # 错误定义
```

### NodeManager
```
internal/nodemanager/server/
├── http_server.go    # HTTP 服务器实现
├── server.go         # 通用服务器接口和管理器
└── errors.go         # 错误定义
```

## 主要特性

### 1. 模块化设计
- 每个组件的 HTTP 服务器代码都独立封装在各自的 `server` 包中
- 通过接口定义实现了松耦合设计
- 支持依赖注入和接口替换

### 2. 统一的服务器管理
- `ServerManager` 提供统一的服务器管理功能
- 支持多种服务器类型的注册、启动和停止
- 提供批量操作和状态查询功能

### 3. 预留的扩展性
- 支持 HTTP、gRPC、TCP、UDP 多种通信协议
- 预留了 gRPC、TCP、UDP 服务器的接口和基础实现框架
- 便于未来添加新的通信协议支持

### 4. 完善的错误处理
- 定义了统一的错误类型
- 提供了清晰的错误信息
- 支持错误传播和处理

## 迁移的功能

### ApplicationMaster HTTP 服务器
- 应用程序信息查询 (`/ws/v1/appmaster/info`)
- 应用程序状态查询 (`/ws/v1/appmaster/status`)
- 容器管理 (`/ws/v1/appmaster/containers`)
- 进度查询 (`/ws/v1/appmaster/progress`)
- 指标查询 (`/ws/v1/appmaster/metrics`)
- 关闭控制 (`/ws/v1/appmaster/shutdown`)
- Web UI 界面

### ResourceManager HTTP 服务器
- 集群信息 (`/ws/v1/cluster/info`)
- 应用程序管理 (`/ws/v1/cluster/apps`)
- 节点管理 (`/ws/v1/cluster/nodes`)
- 节点注册 (`/ws/v1/cluster/nodes/register`)
- 节点心跳 (`/ws/v1/cluster/nodes/heartbeat`)
- 节点健康状态 (`/ws/v1/cluster/nodes/health`)

### NodeManager HTTP 服务器
- 容器管理 (`/ws/v1/node/containers`)
- 单个容器操作 (`/ws/v1/node/containers/{containerId}`)
- 节点信息 (`/ws/v1/node/info`)
- 节点状态 (`/ws/v1/node/status`)

## 使用方式

### 1. 基本使用
```go
// 创建 HTTP 服务器
httpServer := server.NewHTTPServer(component, logger)

// 启动服务器
err := httpServer.Start(8080)

// 停止服务器
err := httpServer.Stop()
```

### 2. 使用服务器管理器
```go
// 创建服务器管理器
serverManager := server.NewServerManager(logger)

// 注册服务器
serverManager.RegisterServer(server.ServerTypeHTTP, httpServer)

// 启动所有服务器
ports := map[server.ServerType]int{
    server.ServerTypeHTTP: 8080,
    server.ServerTypeGRPC: 8081,
}
err := serverManager.StartAllServers(ports)

// 停止所有服务器
err := serverManager.StopAllServers()
```

### 3. 适配现有代码
通过适配器模式，可以将现有的组件适配到新的接口：
```go
// 创建适配器
adapter := NewApplicationMasterAdapter(existingAM)

// 使用适配器创建服务器
httpServer := server.NewHTTPServer(adapter, logger)
```

## 预留的扩展功能

### gRPC 服务器 (预留)
```go
// 创建 gRPC 服务器
grpcServer := server.NewGRPCServer(logger)

// 注册服务
grpcServer.RegisterService("MyService", serviceImpl)

// 启动服务器
err := grpcServer.Start(8081)
```

### TCP 服务器 (预留)
```go
// 创建 TCP 服务器
tcpServer := server.NewTCPServer(logger)

// 设置连接处理器
tcpServer.SetConnectionHandler(func(conn net.Conn) {
    // 处理连接逻辑
})

// 启动服务器
err := tcpServer.Start(8082)
```

### UDP 服务器 (预留)
```go
// 创建 UDP 服务器
udpServer := server.NewUDPServer(logger)

// 设置数据包处理器
udpServer.SetPacketHandler(func(data []byte, addr net.Addr) {
    // 处理数据包逻辑
})

// 启动服务器
err := udpServer.Start(8083)
```

## 迁移的优势

1. **代码组织更清晰**：HTTP 相关代码集中管理，便于维护
2. **职责分离**：业务逻辑与网络通信分离
3. **扩展性强**：支持多种通信协议，便于未来扩展
4. **测试友好**：通过接口定义，便于编写单元测试
5. **配置灵活**：支持动态配置和管理多个服务器
6. **向后兼容**：通过适配器模式保持与现有代码的兼容性

## 后续建议

1. **实现 gRPC 支持**：根据需要实现完整的 gRPC 服务器功能
2. **添加认证和授权**：为 HTTP 服务器添加安全机制
3. **性能监控**：添加服务器性能监控和指标收集
4. **配置管理**：将服务器配置外部化，支持动态配置
5. **负载均衡**：在需要时添加负载均衡支持
6. **服务发现**：集成服务发现机制

## 注意事项

- 现有的 HTTP 服务器代码（如 `internal/applicationmaster/http_server.go`）可以保留作为过渡，但建议逐步迁移到新的架构
- 新的接口定义需要与现有的数据结构保持兼容
- 在生产环境使用前，建议充分测试各种场景
