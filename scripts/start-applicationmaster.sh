#!/bin/bash

# 启动 ApplicationMaster

echo "Starting ApplicationMaster..."

# 默认配置文件路径
CONFIG_FILE="configs/applicationmaster.yaml"

# 应用程序 ID 和尝试 ID（通常由 ResourceManager 提供）
APP_ID=${1:-"$(date +%s)_1"}
ATTEMPT_ID=${2:-"${APP_ID}_1"}

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Configuration file $CONFIG_FILE not found!"
    exit 1
fi

# 启动 ApplicationMaster
./bin/applicationmaster \
    -config="$CONFIG_FILE" \
    -application_id="$APP_ID" \
    -application_attempt_id="$ATTEMPT_ID" \
    -dev=false

echo "ApplicationMaster stopped."
