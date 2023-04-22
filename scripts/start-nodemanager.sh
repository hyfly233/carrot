#!/bin/bash

# 启动 NodeManager

echo "Starting NodeManager..."

# 默认配置文件路径
CONFIG_FILE="configs/nodemanager.yaml"

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Configuration file $CONFIG_FILE not found!"
    exit 1
fi

# 启动 NodeManager
./bin/rmnm -config="$CONFIG_FILE" -dev=false

echo "NodeManager stopped."
