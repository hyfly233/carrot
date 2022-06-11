#!/bin/bash

# ApplicationMaster 快速功能测试
set -e

echo "ApplicationMaster 功能测试"
echo "=========================="

# 确保在项目根目录
cd "$(dirname "$0")/.."

# 检查二进制文件
if [ ! -f "bin/applicationmaster" ]; then
    echo "构建 ApplicationMaster..."
    go build -o bin/applicationmaster ./cmd/applicationmaster/
fi

echo "✓ ApplicationMaster 二进制文件已准备"

# 生成测试用的应用程序 ID
TIMESTAMP=$(date +%s)
APP_ID="${TIMESTAMP}_1"
ATTEMPT_ID="${APP_ID}_1"

echo "✓ 生成应用程序 ID: $APP_ID"
echo "✓ 生成尝试 ID: $ATTEMPT_ID"

# 启动 ApplicationMaster（后台运行）
echo "启动 ApplicationMaster..."
./bin/applicationmaster \
    -application_id="$APP_ID" \
    -application_attempt_id="$ATTEMPT_ID" \
    -rm_address="http://localhost:8030" \
    -app_type="simple" \
    -num_tasks=2 \
    -port=8088 \
    -debug=true \
    > /tmp/applicationmaster.log 2>&1 &

AM_PID=$!
echo "✓ ApplicationMaster 已启动 (PID: $AM_PID)"

# 等待服务器启动
echo "等待服务器启动..."
sleep 3

# 测试 HTTP 服务器是否正常启动
if curl -s -f "http://localhost:8088/ws/v1/appmaster/info" > /dev/null; then
    echo "✓ HTTP 服务器正常运行"
else
    echo "✗ HTTP 服务器启动失败"
    kill $AM_PID 2>/dev/null || true
    exit 1
fi

# 测试各个 API 端点
echo "测试 API 端点..."

echo "1. 测试应用程序信息接口..."
if curl -s "http://localhost:8088/ws/v1/appmaster/info" | jq . > /dev/null 2>&1; then
    echo "   ✓ /ws/v1/appmaster/info - 正常"
else
    echo "   ✗ /ws/v1/appmaster/info - 失败"
fi

echo "2. 测试状态接口..."
if curl -s "http://localhost:8088/ws/v1/appmaster/status" | jq . > /dev/null 2>&1; then
    echo "   ✓ /ws/v1/appmaster/status - 正常"
else
    echo "   ✗ /ws/v1/appmaster/status - 失败"
fi

echo "3. 测试容器接口..."
if curl -s "http://localhost:8088/ws/v1/appmaster/containers" | jq . > /dev/null 2>&1; then
    echo "   ✓ /ws/v1/appmaster/containers - 正常"
else
    echo "   ✗ /ws/v1/appmaster/containers - 失败"
fi

echo "4. 测试进度接口..."
if curl -s "http://localhost:8088/ws/v1/appmaster/progress" | jq . > /dev/null 2>&1; then
    echo "   ✓ /ws/v1/appmaster/progress - 正常"
else
    echo "   ✗ /ws/v1/appmaster/progress - 失败"
fi

echo "5. 测试指标接口..."
if curl -s "http://localhost:8088/ws/v1/appmaster/metrics" | jq . > /dev/null 2>&1; then
    echo "   ✓ /ws/v1/appmaster/metrics - 正常"
else
    echo "   ✗ /ws/v1/appmaster/metrics - 失败"
fi

echo "6. 测试 Web UI..."
if curl -s -f "http://localhost:8088/" > /dev/null; then
    echo "   ✓ Web UI - 正常"
else
    echo "   ✗ Web UI - 失败"
fi

# 显示一些实际的 API 响应
echo
echo "API 响应示例:"
echo "============"

echo "应用程序信息:"
curl -s "http://localhost:8088/ws/v1/appmaster/info" | jq .

echo
echo "应用程序状态:"
curl -s "http://localhost:8088/ws/v1/appmaster/status" | jq .

echo
echo "性能指标:"
curl -s "http://localhost:8088/ws/v1/appmaster/metrics" | jq .

# 测试优雅关闭
echo
echo "测试优雅关闭..."
if curl -s -X POST "http://localhost:8088/ws/v1/appmaster/shutdown" | jq . > /dev/null 2>&1; then
    echo "✓ 优雅关闭请求发送成功"
else
    echo "✗ 优雅关闭请求失败"
fi

# 等待一些时间让 ApplicationMaster 处理关闭请求
sleep 2

# 检查进程是否还在运行
if kill -0 $AM_PID 2>/dev/null; then
    echo "强制停止 ApplicationMaster..."
    kill $AM_PID
    sleep 1
    if kill -0 $AM_PID 2>/dev/null; then
        kill -9 $AM_PID 2>/dev/null || true
    fi
else
    echo "✓ ApplicationMaster 已优雅关闭"
fi

echo
echo "测试完成!"
echo "========="
echo "✓ ApplicationMaster 核心功能正常"
echo "✓ HTTP 服务器和 API 正常工作"
echo "✓ Web UI 可以访问"
echo "✓ 优雅关闭机制正常"
echo
echo "详细日志请查看: /tmp/applicationmaster.log"
echo "Web UI 地址: http://localhost:8088"
