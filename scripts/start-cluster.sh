#!/bin/bash

# 启动 Carrot YARN 集群（使用配置文件）

set -e  # 遇到错误时退出

echo "Starting Carrot YARN Cluster..."

# 检查二进制文件是否存在
if [ ! -f "bin/resourcemanager" ]; then
    echo "Building ResourceManager..."
    go build -o bin/resourcemanager cmd/resourcemanager/main.go
fi

if [ ! -f "bin/nodemanager" ]; then
    echo "Building NodeManager..."
    go build -o bin/nodemanager cmd/nodemanager/main.go
fi

# 检查配置文件是否存在
if [ ! -f "configs/resourcemanager.yaml" ]; then
    echo "ResourceManager configuration file not found!"
    exit 1
fi

if [ ! -f "configs/nodemanager.yaml" ]; then
    echo "NodeManager configuration file not found!"
    exit 1
fi

# 创建日志目录
mkdir -p logs

# 启动 ResourceManager
echo "Starting ResourceManager..."
nohup ./bin/resourcemanager -config="configs/resourcemanager.yaml" > logs/resourcemanager.log 2>&1 &
RM_PID=$!
echo $RM_PID > logs/resourcemanager.pid
echo "ResourceManager started with PID: $RM_PID"

# 等待 ResourceManager 启动
sleep 3

# 启动 NodeManager
echo "Starting NodeManager..."
nohup ./bin/nodemanager -config="configs/nodemanager.yaml" > logs/nodemanager.log 2>&1 &
NM_PID=$!
echo $NM_PID > logs/nodemanager.pid
echo "NodeManager started with PID: $NM_PID"

# 等待服务稳定
sleep 2

echo "Carrot YARN 集群启动成功!"
echo "ResourceManager: http://localhost:8088"
echo "NodeManager: http://localhost:8042"
echo ""
echo "查看日志:"
echo "  ResourceManager: tail -f logs/resourcemanager.log"
echo "  NodeManager: tail -f logs/nodemanager.log"
echo ""
echo "检查状态:"
echo "  curl http://localhost:8088/ws/v1/cluster/info"
echo "  curl http://localhost:8088/ws/v1/cluster/nodes"
echo ""
echo "停止集群: ./scripts/stop-cluster.sh"
