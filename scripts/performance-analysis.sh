#!/bin/bash

# Carrot YARN 性能分析工具

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROFILE_DIR="/tmp/carrot-profile-$(date +%Y%m%d-%H%M%S)"
mkdir -p $PROFILE_DIR

echo -e "${BLUE}🔍 Carrot YARN 性能分析工具${NC}"
echo "=============================="
echo "分析结果将保存到: $PROFILE_DIR"
echo ""

# 函数：检查 pprof 工具
check_pprof() {
    if ! command -v go &> /dev/null; then
        echo -e "${RED}✗ Go 未安装${NC}"
        exit 1
    fi

    if ! go tool pprof -h &> /dev/null; then
        echo -e "${RED}✗ pprof 工具不可用${NC}"
        exit 1
    fi

    echo -e "${GREEN}✓ pprof 工具可用${NC}"
}

# 函数：启动带性能分析的服务
start_profiling_server() {
    local component=$1
    local port=$2
    local args=$3

    echo -e "${BLUE}启动 $component (端口 $port)...${NC}"

    # 修改代码以启用 pprof
    local temp_main=$(mktemp)
    cat > $temp_main << EOF
package main

import (
    "flag"
    "log"
    "net/http"
    _ "net/http/pprof"
    "runtime"

    "carrot/internal/$component"
)

func init() {
    // 设置 CPU 核心数以提高性能
    runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
    // 启动 pprof 服务器
    go func() {
        log.Printf("pprof server starting on :6060")
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()

    // 原始的 main 逻辑
    var (
        port = flag.Int("port", $port, "$component port")
    )
    flag.Parse()

    log.Println("Starting $component with profiling enabled...")

    // 这里需要根据实际组件调整
    // 由于我们不能修改原始代码，这个脚本主要用于演示
    panic("This is a template - actual implementation would import and start the component")
}
EOF

    echo "生成的性能分析版本已保存到: $temp_main"
    echo "要启用性能分析，需要在原代码中添加 pprof 支持"
}

# 函数：运行性能测试
run_performance_tests() {
    echo -e "${BLUE}运行性能测试...${NC}"

    # CPU 性能测试
    echo "运行 CPU 基准测试..."
    go test -bench=. -benchmem -cpuprofile=$PROFILE_DIR/cpu.prof ./internal/resourcemanager > $PROFILE_DIR/bench_rm.txt 2>&1 || true
    go test -bench=. -benchmem -cpuprofile=$PROFILE_DIR/cpu_nm.prof ./internal/nodemanager > $PROFILE_DIR/bench_nm.txt 2>&1 || true

    # 内存性能测试
    echo "运行内存基准测试..."
    go test -bench=. -benchmem -memprofile=$PROFILE_DIR/mem.prof ./internal/resourcemanager > $PROFILE_DIR/mem_rm.txt 2>&1 || true
    go test -bench=. -benchmem -memprofile=$PROFILE_DIR/mem_nm.prof ./internal/nodemanager > $PROFILE_DIR/mem_nm.txt 2>&1 || true

    echo -e "${GREEN}✓ 性能测试完成${NC}"
}

# 函数：分析性能数据
analyze_performance() {
    echo -e "${BLUE}分析性能数据...${NC}"

    # 分析 CPU 性能
    if [ -f $PROFILE_DIR/cpu.prof ]; then
        echo "生成 CPU 性能报告..."
        go tool pprof -text $PROFILE_DIR/cpu.prof > $PROFILE_DIR/cpu_analysis.txt 2>&1 || true
        go tool pprof -top $PROFILE_DIR/cpu.prof > $PROFILE_DIR/cpu_top.txt 2>&1 || true
    fi

    # 分析内存使用
    if [ -f $PROFILE_DIR/mem.prof ]; then
        echo "生成内存使用报告..."
        go tool pprof -text $PROFILE_DIR/mem.prof > $PROFILE_DIR/mem_analysis.txt 2>&1 || true
        go tool pprof -top $PROFILE_DIR/mem.prof > $PROFILE_DIR/mem_top.txt 2>&1 || true
    fi

    echo -e "${GREEN}✓ 性能分析完成${NC}"
}

# 函数：生成负载测试
run_load_test() {
    echo -e "${BLUE}运行负载测试...${NC}"

    # 确保集群正在运行
    if ! curl -f -s http://localhost:8088/ws/v1/cluster/info > /dev/null 2>&1; then
        echo -e "${YELLOW}启动集群用于负载测试...${NC}"
        ./scripts/start-cluster.sh
        sleep 5
    fi

    echo "提交多个测试应用..."

    # 并发提交多个应用
    for i in {1..10}; do
        ./bin/client -rm-url http://localhost:8088 \
            -app-name "load-test-$i" \
            -command "echo 'Load test $i'; sleep $((RANDOM % 10 + 5))" &
    done

    # 等待所有应用提交完成
    wait

    echo "监控集群状态..."
    for i in {1..30}; do
        APP_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/apps | jq -r '.apps.app | length' 2>/dev/null || echo "0")
        NODE_COUNT=$(curl -s http://localhost:8088/ws/v1/cluster/nodes | jq -r '.nodes.node | length' 2>/dev/null || echo "0")

        echo "[$(date '+%H:%M:%S')] 应用数: $APP_COUNT, 节点数: $NODE_COUNT"

        # 保存详细状态
        curl -s http://localhost:8088/ws/v1/cluster/apps > $PROFILE_DIR/apps_$i.json
        curl -s http://localhost:8088/ws/v1/cluster/nodes > $PROFILE_DIR/nodes_$i.json

        sleep 2
    done

    echo -e "${GREEN}✓ 负载测试完成${NC}"
}

# 函数：系统资源监控
monitor_system_resources() {
    echo -e "${BLUE}监控系统资源...${NC}"

    # 监控 CPU 和内存使用
    {
        echo "时间,CPU使用率,内存使用率,负载平均"
        for i in {1..60}; do
            CPU=$(top -l 1 | grep "CPU usage" | awk '{print $3}' | sed 's/%//' 2>/dev/null || echo "0")
            MEM=$(top -l 1 | grep "PhysMem" | awk '{print $2}' | sed 's/M//' 2>/dev/null || echo "0")
            LOAD=$(uptime | awk -F'load averages:' '{print $2}' | awk '{print $1}' 2>/dev/null || echo "0")

            echo "$(date '+%H:%M:%S'),$CPU,$MEM,$LOAD"
            sleep 1
        done
    } > $PROFILE_DIR/system_resources.csv

    echo -e "${GREEN}✓ 系统资源监控完成${NC}"
}

# 函数：生成报告
generate_report() {
    echo -e "${BLUE}生成性能报告...${NC}"

    cat > $PROFILE_DIR/performance_report.md << EOF
# Carrot YARN 性能分析报告

生成时间: $(date)
分析目录: $PROFILE_DIR

## 基准测试结果

### ResourceManager 性能
\`\`\`
$(cat $PROFILE_DIR/bench_rm.txt 2>/dev/null || echo "未找到 ResourceManager 基准测试结果")
\`\`\`

### NodeManager 性能
\`\`\`
$(cat $PROFILE_DIR/bench_nm.txt 2>/dev/null || echo "未找到 NodeManager 基准测试结果")
\`\`\`

## CPU 性能分析

### 热点函数 (ResourceManager)
\`\`\`
$(cat $PROFILE_DIR/cpu_top.txt 2>/dev/null || echo "未找到 CPU 分析结果")
\`\`\`

## 内存使用分析

### 内存热点 (ResourceManager)
\`\`\`
$(cat $PROFILE_DIR/mem_top.txt 2>/dev/null || echo "未找到内存分析结果")
\`\`\`

## 系统资源使用

系统资源监控数据保存在: system_resources.csv

## 建议优化方向

1. **CPU 优化**:
   - 检查 CPU 热点函数
   - 优化锁竞争
   - 减少不必要的计算

2. **内存优化**:
   - 检查内存泄漏
   - 优化对象池使用
   - 减少内存分配

3. **并发优化**:
   - 优化 goroutine 使用
   - 减少锁竞争
   - 改进并发设计

4. **网络优化**:
   - 使用连接池
   - 优化序列化
   - 减少网络调用

## 文件说明

- bench_*.txt: 基准测试结果
- cpu*.prof: CPU 性能分析文件
- mem*.prof: 内存性能分析文件
- *_analysis.txt: 详细分析报告
- apps_*.json: 应用状态快照
- nodes_*.json: 节点状态快照
- system_resources.csv: 系统资源使用情况
EOF

    echo -e "${GREEN}✓ 性能报告已生成: $PROFILE_DIR/performance_report.md${NC}"
}

# 主要执行流程
main() {
    echo "选择要执行的性能分析:"
    echo "1. 运行基准测试"
    echo "2. 运行负载测试"
    echo "3. 监控系统资源"
    echo "4. 生成完整报告"
    echo "5. 全部执行"

    read -p "请输入选择 (1-5): " choice

    case $choice in
        1)
            check_pprof
            run_performance_tests
            analyze_performance
            ;;
        2)
            run_load_test
            ;;
        3)
            monitor_system_resources &
            MONITOR_PID=$!
            echo "系统资源监控已启动 (PID: $MONITOR_PID)"
            echo "按 Ctrl+C 停止监控..."
            wait $MONITOR_PID
            ;;
        4)
            generate_report
            ;;
        5)
            check_pprof
            run_performance_tests
            analyze_performance
            run_load_test &
            LOAD_PID=$!
            monitor_system_resources &
            MONITOR_PID=$!

            wait $LOAD_PID
            kill $MONITOR_PID 2>/dev/null || true

            generate_report
            ;;
        *)
            echo "无效选择"
            exit 1
            ;;
    esac

    echo ""
    echo "=== 性能分析完成 ==="
    echo "结果保存在: $PROFILE_DIR"
    echo ""
    echo "查看报告: cat $PROFILE_DIR/performance_report.md"
    echo "查看基准测试: cat $PROFILE_DIR/bench_*.txt"
    echo "分析 CPU 性能: go tool pprof $PROFILE_DIR/cpu.prof"
    echo "分析内存使用: go tool pprof $PROFILE_DIR/mem.prof"
}

# 捕获退出信号
trap 'echo -e "\n${YELLOW}性能分析被中断${NC}"; exit 0' INT TERM

main "$@"
