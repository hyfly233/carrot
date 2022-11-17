#!/bin/bash

# 第三阶段 gRPC 通信测试脚本
# 测试 ApplicationMaster ↔ ResourceManager gRPC 通信

set -e

echo "=========================================="
echo "第三阶段 gRPC 测试: ApplicationMaster ↔ ResourceManager"
echo "=========================================="

# 构建所有组件
echo "构建项目组件..."
go build -o bin/resourcemanager ./cmd/resourcemanager
go build -o bin/nodemanager ./cmd/nodemanager
go build -o bin/applicationmaster ./cmd/applicationmaster

# 启动 ResourceManager
echo "启动 ResourceManager..."
./bin/resourcemanager --config=configs/resourcemanager.yaml &
RM_PID=$!
sleep 3

# 启动 NodeManager
echo "启动 NodeManager..."
./bin/nodemanager --config=configs/nodemanager.yaml &
NM_PID=$!
sleep 2

# 验证服务启动
echo "验证服务状态..."
echo "ResourceManager HTTP 端口 8088:"
curl -s http://localhost:8088/status | jq . || echo "HTTP服务未响应"

echo "ResourceManager NodeManager gRPC 端口 9088:"
netstat -an | grep 9088 | head -1 || echo "NodeManager gRPC端口未监听"

echo "ResourceManager ApplicationMaster gRPC 端口 9089:"
netstat -an | grep 9089 | head -1 || echo "ApplicationMaster gRPC端口未监听"

echo "NodeManager HTTP 端口 8080:"
curl -s http://localhost:8080/status | jq . || echo "NodeManager HTTP服务未响应"

echo "NodeManager gRPC 端口 9080:"
netstat -an | grep 9080 | head -1 || echo "NodeManager gRPC端口未监听"

# 测试gRPC连接
echo ""
echo "=========================================="
echo "测试第三阶段 gRPC 通信..."
echo "=========================================="

# 使用 grpcurl 测试 ApplicationMaster gRPC 服务 (如果安装了的话)
if command -v grpcurl &> /dev/null; then
    echo "使用 grpcurl 测试 ApplicationMaster gRPC 服务..."
    
    # 列出服务
    grpcurl -plaintext localhost:9089 list || echo "无法连接到 ApplicationMaster gRPC 服务"
    
    # 测试 GetClusterMetrics
    grpcurl -plaintext -d '{}' localhost:9089 applicationmaster.ApplicationMasterService/GetClusterMetrics || echo "GetClusterMetrics 调用失败"
else
    echo "grpcurl 未安装，跳过 gRPC 直接测试"
fi

# 使用 Go 客户端测试
echo ""
echo "创建 Go 测试客户端..."

cat > /tmp/test_am_grpc.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    ampb "carrot/api/proto/applicationmaster"
)

func main() {
    // 连接到 ResourceManager ApplicationMaster gRPC 服务
    conn, err := grpc.Dial("localhost:9089", grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatalf("连接失败: %v", err)
    }
    defer conn.Close()

    client := ampb.NewApplicationMasterServiceClient(conn)

    // 测试 GetClusterMetrics
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    req := &ampb.GetClusterMetricsRequest{}
    resp, err := client.GetClusterMetrics(ctx, req)
    if err != nil {
        log.Fatalf("GetClusterMetrics 失败: %v", err)
    }

    fmt.Printf("集群指标获取成功:\n")
    fmt.Printf("  运行中的应用: %d\n", resp.ClusterMetrics.AppsRunning)
    fmt.Printf("  活跃节点: %d\n", resp.ClusterMetrics.ActiveNodes)
    fmt.Printf("  总节点数: %d\n", resp.ClusterMetrics.TotalNodes)
    fmt.Printf("  可用虚拟核心: %d\n", resp.ClusterMetrics.AvailableVirtualCores)
    
    fmt.Println("✅ ApplicationMaster gRPC 通信测试成功!")
}
EOF

# 运行测试
echo "运行 gRPC 客户端测试..."
(cd /Users/flyhy/workspace/hyfly233/carrot && go run /tmp/test_am_grpc.go) || echo "❌ gRPC 客户端测试失败"

# 清理
echo ""
echo "清理进程..."
kill $RM_PID $NM_PID 2>/dev/null || true
wait 2>/dev/null || true

echo ""
echo "=========================================="
echo "第三阶段测试完成!"
echo "=========================================="
