#!/bin/bash

# 测试新的日志系统
echo "=== 测试 Carrot YARN 新日志系统 ==="

# 清理旧日志
echo "清理旧日志文件..."
rm -rf logs/

# 编译项目
echo "编译项目..."
go build -o bin/resourcemanager ./cmd/resourcemanager/
go build -o bin/rmnm ./cmd/rmnm/
go build -o bin/applicationmaster ./cmd/applicationmaster/

if [ $? -ne 0 ]; then
    echo "编译失败！"
    exit 1
fi

echo "编译成功！"

# 测试 ResourceManager 日志
echo ""
echo "=== 测试 ResourceManager 日志系统 ==="
echo "启动 ResourceManager（5秒后停止）..."

./bin/resourcemanager -config=configs/resourcemanager.yaml &
RM_PID=$!

sleep 5
kill $RM_PID 2>/dev/null || true
wait $RM_PID 2>/dev/null || true

# 检查日志文件
echo "检查 ResourceManager 日志文件..."
if [ -d "logs/resourcemanager" ]; then
    echo "✅ 日志目录创建成功: logs/resourcemanager"
    ls -la logs/resourcemanager/

    # 检查日志内容
    if [ -f logs/resourcemanager/carrot-$(date +%Y-%m-%d-%H).log ]; then
        echo "✅ 小时日志文件创建成功"
        echo "日志文件内容样本:"
        head -5 logs/resourcemanager/carrot-$(date +%Y-%m-%d-%H).log
    else
        echo "❌ 小时日志文件未创建"
    fi
else
    echo "❌ 日志目录未创建"
fi

# 测试 NodeManager 日志
echo ""
echo "=== 测试 NodeManager 日志系统 ==="
echo "启动 NodeManager（5秒后停止）..."

./bin/rmnm -config=configs/rmnm.yaml &
NM_PID=$!

sleep 5
kill $NM_PID 2>/dev/null || true
wait $NM_PID 2>/dev/null || true

# 检查日志文件
echo "检查 NodeManager 日志文件..."
if [ -d "logs/nodemanager" ]; then
    echo "✅ 日志目录创建成功: logs/nodemanager"
    ls -la logs/rmnm/

    # 检查日志内容
    if [ -f logs/rmnm/carrot-$(date +%Y-%m-%d-%H).log ]; then
        echo "✅ 小时日志文件创建成功"
        echo "日志文件内容样本:"
        head -5 logs/rmnm/carrot-$(date +%Y-%m-%d-%H).log
    else
        echo "❌ 小时日志文件未创建"
    fi
else
    echo "❌ 日志目录未创建"
fi

# 测试 ApplicationMaster 日志
echo ""
echo "=== 测试 ApplicationMaster 日志系统 ==="
echo "启动 ApplicationMaster（5秒后停止）..."

./bin/applicationmaster -config=configs/applicationmaster.yaml -application_id=app_$(date +%s)_001 -application_attempt_id=appattempt_$(date +%s)_001_000001 &
AM_PID=$!

sleep 5
kill $AM_PID 2>/dev/null || true
wait $AM_PID 2>/dev/null || true

# 检查日志文件
echo "检查 ApplicationMaster 日志文件..."
if [ -d "logs/applicationmaster" ]; then
    echo "✅ 日志目录创建成功: logs/applicationmaster"
    ls -la logs/applicationmaster/

    # 检查日志内容
    if [ -f logs/applicationmaster/carrot-$(date +%Y-%m-%d-%H).log ]; then
        echo "✅ 小时日志文件创建成功"
        echo "日志文件内容样本:"
        head -5 logs/applicationmaster/carrot-$(date +%Y-%m-%d-%H).log
    else
        echo "❌ 小时日志文件未创建"
    fi
else
    echo "❌ 日志目录未创建"
fi

# 测试日志格式
echo ""
echo "=== 验证日志格式 ==="
echo "检查 JSON 格式..."

for component in resourcemanager rmnm applicationmaster; do
    log_file="logs/${component}/carrot-$(date +%Y-%m-%d-%H).log"
    if [ -f "$log_file" ]; then
        echo "检查 ${component} 日志格式:"
        if head -1 "$log_file" | jq . > /dev/null 2>&1; then
            echo "✅ ${component} 日志格式正确（JSON）"
        else
            echo "❌ ${component} 日志格式错误"
            echo "日志内容:"
            head -1 "$log_file"
        fi
    fi
done

# 总结
echo ""
echo "=== 测试总结 ==="
echo "日志目录结构:"
find logs/ -type f -name "*.log" 2>/dev/null | head -10

echo ""
echo "日志文件大小:"
find logs/ -type f -name "*.log" -exec ls -lh {} \; 2>/dev/null

echo ""
echo "=== 日志系统测试完成 ==="

# 清理进程
pkill -f "bin/resourcemanager"
pkill -f "bin/nodemanager"
pkill -f "bin/applicationmaster"
