#!/bin/bash

# ApplicationMaster 使用示例
set -e

echo "ApplicationMaster Usage Examples"
echo "================================"

# 确保在项目根目录
cd "$(dirname "$0")/.."

# 检查二进制文件是否存在
if [ ! -f "bin/applicationmaster" ]; then
    echo "ApplicationMaster binary not found. Building..."
    ./scripts/build-applicationmaster.sh
fi

echo
echo "1. Simple Application Example:"
echo "------------------------------"
echo "Running a simple application with 3 tasks..."

# 生成应用程序 ID
TIMESTAMP=$(date +%s)
APP_ID="${TIMESTAMP}_1"
ATTEMPT_ID="${APP_ID}_1"

echo "Application ID: $APP_ID"
echo "Attempt ID: $ATTEMPT_ID"

# 运行简单应用程序（在后台运行，10秒后停止）
echo "Starting ApplicationMaster for simple application..."
timeout 30s ./bin/applicationmaster \
    -application_id="$APP_ID" \
    -application_attempt_id="$ATTEMPT_ID" \
    -rm_address="http://localhost:8030" \
    -app_type="simple" \
    -num_tasks=3 \
    -port=8088 \
    -debug=true \
    &

AM_PID=$!
echo "ApplicationMaster started with PID: $AM_PID"

# 等待一些时间让 ApplicationMaster 启动
sleep 2

echo
echo "2. Testing ApplicationMaster Web UI:"
echo "------------------------------------"
echo "You can access the ApplicationMaster Web UI at: http://localhost:8088"
echo "API endpoints:"
echo "  - Application info: http://localhost:8088/ws/v1/appmaster/info"
echo "  - Status: http://localhost:8088/ws/v1/appmaster/status"
echo "  - Containers: http://localhost:8088/ws/v1/appmaster/containers"
echo "  - Progress: http://localhost:8088/ws/v1/appmaster/progress"
echo "  - Metrics: http://localhost:8088/ws/v1/appmaster/metrics"

# 检查 API 是否可用
echo
echo "Testing API endpoints..."

# 等待服务器启动
sleep 3

# 测试应用程序信息
echo "Testing application info endpoint..."
curl -s "http://localhost:8088/ws/v1/appmaster/info" | python3 -m json.tool || echo "API not ready yet"

echo
echo "Testing status endpoint..."
curl -s "http://localhost:8088/ws/v1/appmaster/status" | python3 -m json.tool || echo "API not ready yet"

echo
echo "ApplicationMaster will run for 30 seconds..."
echo "You can:"
echo "1. Open http://localhost:8088 in your browser to see the Web UI"
echo "2. Test the API endpoints manually"
echo "3. Check the logs for ApplicationMaster activity"

# 等待 ApplicationMaster 完成
wait $AM_PID || echo "ApplicationMaster finished"

echo
echo "3. Distributed Application Example:"
echo "----------------------------------"
echo "Here's how to run a distributed application:"

# 生成新的应用程序 ID
TIMESTAMP=$(date +%s)
APP_ID="${TIMESTAMP}_2"
ATTEMPT_ID="${APP_ID}_1"

echo "Application ID: $APP_ID"
echo "Attempt ID: $ATTEMPT_ID"

echo "Command to run distributed application:"
echo "./bin/applicationmaster \\"
echo "    -application_id=\"$APP_ID\" \\"
echo "    -application_attempt_id=\"$ATTEMPT_ID\" \\"
echo "    -rm_address=\"http://localhost:8030\" \\"
echo "    -app_type=\"distributed\" \\"
echo "    -num_workers=3 \\"
echo "    -port=8089 \\"
echo "    -debug=true"

echo
echo "4. Configuration Options:"
echo "------------------------"
echo "Available command line options:"
echo "  -application_id: Application ID (required)"
echo "  -application_attempt_id: Application Attempt ID (required)" 
echo "  -rm_address: ResourceManager address (default: http://localhost:8030)"
echo "  -tracking_url: Application tracking URL (default: auto-generated)"
echo "  -port: HTTP server port (default: 8088)"
echo "  -app_type: Application type - simple|distributed (default: simple)"
echo "  -num_tasks: Number of tasks for simple application (default: 3)"
echo "  -num_workers: Number of workers for distributed application (default: 2)"
echo "  -heartbeat_interval: Heartbeat interval (default: 10s)"
echo "  -max_retries: Maximum container retries (default: 3)"
echo "  -debug: Enable debug logging (default: false)"

echo
echo "5. Integration with ResourceManager:"
echo "-----------------------------------"
echo "To use ApplicationMaster with ResourceManager:"
echo "1. Start ResourceManager: ./bin/resourcemanager"
echo "2. Start NodeManager(s): ./bin/nodemanager"
echo "3. Submit application through ResourceManager Web UI or API"
echo "4. ResourceManager will launch ApplicationMaster automatically"

echo
echo "Examples completed!"
echo "Check the logs and Web UI for more details."
