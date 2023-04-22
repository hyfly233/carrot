#!/bin/bash

# 简单的集成测试脚本

echo "=== Carrot YARN 集成测试 ==="

# 设置测试环境
TEST_DIR="/tmp/carrot-test"
mkdir -p $TEST_DIR/logs

# 启动 ResourceManager
echo "启动 ResourceManager..."
./bin/resourcemanager -port 8088 > $TEST_DIR/logs/rm.log 2>&1 &
RM_PID=$!
echo "ResourceManager PID: $RM_PID"

# 等待 RM 启动
sleep 3

# 检查 RM 是否启动成功
if curl -s http://localhost:8088/ws/v1/cluster/info > /dev/null; then
    echo "✓ ResourceManager 启动成功"
else
    echo "✗ ResourceManager 启动失败"
    kill $RM_PID 2>/dev/null
    exit 1
fi

# 启动 NodeManager
echo "启动 NodeManager..."
./bin/rmnm -port 8042 -host localhost -rm-url http://localhost:8088 -memory 4096 -vcores 4 > $TEST_DIR/logs/nm.log 2>&1 &
NM_PID=$!
echo "NodeManager PID: $NM_PID"

# 等待 NM 启动并注册
sleep 5

# 检查节点是否注册成功
NODE_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/nodes | jq -r '.nodes.node | length' 2>/dev/null || echo "0")
if [ "$NODE_COUNT" -gt "0" ]; then
    echo "✓ NodeManager 注册成功，节点数量: $NODE_COUNT"
else
    echo "✗ NodeManager 注册失败"
    kill $RM_PID $NM_PID 2>/dev/null
    exit 1
fi

# 提交测试应用程序
echo "提交测试应用程序..."
./bin/client -rm-url http://localhost:8088 -app-name "integration-test" -command "echo 'Hello from Carrot YARN!'; sleep 10"

# 等待应用程序执行
sleep 3

# 检查应用程序状态
APP_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/apps | jq -r '.apps.app | length' 2>/dev/null || echo "0")
if [ "$APP_COUNT" -gt "0" ]; then
    echo "✓ 应用程序提交成功，应用数量: $APP_COUNT"
else
    echo "✗ 应用程序提交失败"
fi

echo ""
echo "=== 集群状态 ==="
echo "ResourceManager: http://localhost:8088/ws/v1/cluster/info"
echo "节点信息: http://localhost:8088/ws/v1/cluster/nodes"
echo "应用程序: http://localhost:8088/ws/v1/cluster/apps"

echo ""
echo "测试完成！"
echo "清理进程..."
kill $RM_PID $NM_PID 2>/dev/null

echo "日志文件位于: $TEST_DIR/logs/"
echo "可以查看详细日志来排查问题"
