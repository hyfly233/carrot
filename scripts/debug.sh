#!/bin/bash

# Carrot YARN 项目调试脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 调试目录
DEBUG_DIR="/tmp/carrot-debug-$(date +%Y%m%d-%H%M%S)"
mkdir -p $DEBUG_DIR/logs

echo -e "${BLUE}🔧 Carrot YARN 调试工具${NC}"
echo "=========================="
echo "调试目录: $DEBUG_DIR"
echo ""

# 设置调试环境变量
export LOG_LEVEL=DEBUG
export CARROT_DEBUG=true
export GODEBUG=gctrace=1

# 函数：清理进程
cleanup() {
    echo -e "\n${YELLOW}清理进程...${NC}"
    if [ -f $DEBUG_DIR/rm.pid ]; then
        RM_PID=$(cat $DEBUG_DIR/rm.pid)
        kill $RM_PID 2>/dev/null || true
        echo "已停止 ResourceManager (PID: $RM_PID)"
    fi

    if [ -f $DEBUG_DIR/nm.pid ]; then
        NM_PID=$(cat $DEBUG_DIR/nm.pid)
        kill $NM_PID 2>/dev/null || true
        echo "已停止 NodeManager (PID: $NM_PID)"
    fi
}

# 捕获退出信号
trap cleanup EXIT INT TERM

# 函数：检查命令是否存在
check_command() {
    if ! command -v $1 &> /dev/null; then
        echo -e "${RED}✗ $1 命令未找到${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ $1 已安装${NC}"
    return 0
}

# 函数：检查端口是否被占用
check_port() {
    if lsof -Pi :$1 -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo -e "${RED}✗ 端口 $1 已被占用${NC}"
        echo "占用进程:"
        lsof -Pi :$1 -sTCP:LISTEN
        return 1
    fi
    echo -e "${GREEN}✓ 端口 $1 可用${NC}"
    return 0
}

# 检查前置条件
echo -e "${BLUE}检查前置条件...${NC}"
check_command "go" || exit 1
check_command "curl" || exit 1
check_command "jq" || echo -e "${YELLOW}警告: jq 未安装，JSON 输出将不会格式化${NC}"

# 检查端口
echo -e "\n${BLUE}检查端口可用性...${NC}"
check_port 8088 || exit 1
check_port 8042 || exit 1

# 确保项目已编译
echo -e "\n${BLUE}编译项目...${NC}"
if ! make build; then
    echo -e "${RED}✗ 编译失败${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 编译成功${NC}"

# 启动 ResourceManager
echo -e "\n${BLUE}启动 ResourceManager...${NC}"
./bin/resourcemanager -port 8088 > $DEBUG_DIR/logs/rm.log 2>&1 &
RM_PID=$!
echo $RM_PID > $DEBUG_DIR/rm.pid
echo "ResourceManager PID: $RM_PID"

# 等待 RM 启动
echo "等待 ResourceManager 启动..."
for i in {1..10}; do
    if curl -f -s http://localhost:8088/ws/v1/cluster/info > /dev/null 2>&1; then
        echo -e "${GREEN}✓ ResourceManager 启动成功${NC}"
        break
    fi
    if [ $i -eq 10 ]; then
        echo -e "${RED}✗ ResourceManager 启动超时${NC}"
        echo "ResourceManager 日志:"
        tail -20 $DEBUG_DIR/logs/rm.log
        exit 1
    fi
    sleep 1
done

# 获取集群信息
echo -e "\n${BLUE}获取集群信息...${NC}"
curl -s http://localhost:8088/ws/v1/cluster/info > $DEBUG_DIR/cluster-info.json
if command -v jq &> /dev/null; then
    cat $DEBUG_DIR/cluster-info.json | jq .
else
    cat $DEBUG_DIR/cluster-info.json
fi

# 启动 NodeManager
echo -e "\n${BLUE}启动 NodeManager...${NC}"
./bin/rmnm -port 8042 -host localhost -rm-url http://localhost:8088 -memory 4096 -vcores 4 > $DEBUG_DIR/logs/nm.log 2>&1 &
NM_PID=$!
echo $NM_PID > $DEBUG_DIR/nm.pid
echo "NodeManager PID: $NM_PID"

# 等待节点注册
echo "等待节点注册..."
for i in {1..15}; do
    NODE_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/nodes | jq -r '.nodes.node | length' 2>/dev/null || echo "0")
    if [ "$NODE_COUNT" -gt "0" ]; then
        echo -e "${GREEN}✓ 节点注册成功，节点数量: $NODE_COUNT${NC}"
        break
    fi
    if [ $i -eq 15 ]; then
        echo -e "${RED}✗ 节点注册超时${NC}"
        echo "NodeManager 日志:"
        tail -20 $DEBUG_DIR/logs/nm.log
        exit 1
    fi
    sleep 1
done

# 获取节点信息
echo -e "\n${BLUE}获取节点信息...${NC}"
curl -s http://localhost:8088/ws/v1/cluster/nodes > $DEBUG_DIR/nodes.json
if command -v jq &> /dev/null; then
    cat $DEBUG_DIR/nodes.json | jq .
else
    cat $DEBUG_DIR/nodes.json
fi

# 提交测试应用
echo -e "\n${BLUE}提交测试应用...${NC}"
./bin/client -rm-url http://localhost:8088 -app-name "debug-test-$(date +%s)" -command "echo 'Debug test started'; sleep 5; echo 'Debug test completed'" > $DEBUG_DIR/client.log 2>&1 &
CLIENT_PID=$!

# 等待应用提交
sleep 2

# 获取应用信息
echo -e "\n${BLUE}获取应用信息...${NC}"
curl -s http://localhost:8088/ws/v1/cluster/apps > $DEBUG_DIR/apps.json
if command -v jq &> /dev/null; then
    cat $DEBUG_DIR/apps.json | jq .
else
    cat $DEBUG_DIR/apps.json
fi

echo -e "\n${GREEN}✓ 调试环境启动完成！${NC}"
echo ""
echo "=== 调试信息 ==="
echo "ResourceManager PID: $RM_PID"
echo "NodeManager PID: $NM_PID"
echo "调试目录: $DEBUG_DIR"
echo ""
echo "=== 日志文件 ==="
echo "ResourceManager: $DEBUG_DIR/logs/rm.log"
echo "NodeManager: $DEBUG_DIR/logs/nm.log"
echo "Client: $DEBUG_DIR/client.log"
echo ""
echo "=== API 端点 ==="
echo "集群信息: http://localhost:8088/ws/v1/cluster/info"
echo "节点列表: http://localhost:8088/ws/v1/cluster/nodes"
echo "应用列表: http://localhost:8088/ws/v1/cluster/apps"
echo "容器列表: http://localhost:8042/ws/v1/node/containers"
echo ""
echo "=== 调试命令 ==="
echo "查看实时日志: tail -f $DEBUG_DIR/logs/*.log"
echo "发送请求: curl -s http://localhost:8088/ws/v1/cluster/info | jq ."
echo "检查进程: ps aux | grep -E 'resourcemanager|nodemanager'"
echo "网络连接: netstat -tlnp | grep -E '8088|8042'"
echo ""
echo "=== Delve 调试 ==="
echo "调试 ResourceManager: dlv attach $RM_PID"
echo "调试 NodeManager: dlv attach $NM_PID"
echo ""
echo "按 Ctrl+C 退出并清理进程..."

# 保持脚本运行，显示实时状态
while true; do
    sleep 5

    # 检查进程是否还在运行
    if ! kill -0 $RM_PID 2>/dev/null; then
        echo -e "${RED}⚠️  ResourceManager 进程已退出${NC}"
        break
    fi

    if ! kill -0 $NM_PID 2>/dev/null; then
        echo -e "${RED}⚠️  NodeManager 进程已退出${NC}"
        break
    fi

    # 显示简要状态
    NODE_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/nodes | jq -r '.nodes.node | length' 2>/dev/null || echo "0")
    APP_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/apps | jq -r '.apps.app | length' 2>/dev/null || echo "0")
    echo -e "${GREEN}[$(date '+%H:%M:%S')] 状态: $NODE_COUNT 个节点, $APP_COUNT 个应用${NC}"
done
