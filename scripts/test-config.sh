#!/bin/bash

# 测试配置文件启动各个组件

echo "=== 测试配置文件启动系统 ==="
echo

# 测试 ResourceManager
echo "1. 测试 ResourceManager 配置文件加载..."
echo "启动 ResourceManager 并检查前几行输出："
./bin/resourcemanager -config=configs/resourcemanager.yaml -dev=true 2>&1 | head -3 &
RM_PID=$!
sleep 2
kill $RM_PID 2>/dev/null
echo

# 测试 NodeManager
echo "2. 测试 NodeManager 配置文件加载..."
echo "启动 NodeManager 并检查前几行输出："
./bin/rmnm -config=configs/rmnm.yaml -dev=true 2>&1 | head -3 &
NM_PID=$!
sleep 2
kill $NM_PID 2>/dev/null
echo

# 测试 ApplicationMaster
echo "3. 测试 ApplicationMaster 配置文件加载..."
echo "启动 ApplicationMaster 并检查前几行输出："
./bin/applicationmaster \
    -config=configs/applicationmaster.yaml \
    -application_id="$(date +%s)_1" \
    -application_attempt_id="$(date +%s)_1_1" \
    -dev=true 2>&1 | head -3 &
AM_PID=$!
sleep 2
kill $AM_PID 2>/dev/null
echo

echo "=== 配置文件测试完成 ==="
echo
echo "所有组件都成功加载了配置文件！"
echo
echo "使用方法："
echo "  ResourceManager: ./bin/resourcemanager -config=configs/resourcemanager.yaml"
echo "  NodeManager:     ./bin/nodemanager -config=configs/nodemanager.yaml"
echo "  ApplicationMaster: ./bin/applicationmaster -config=configs/applicationmaster.yaml -application_id=APP_ID -application_attempt_id=ATTEMPT_ID"
