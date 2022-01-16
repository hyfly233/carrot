#!/bin/bash

# Start YARN cluster components

echo "Starting YARN cluster..."

# Create logs directory
mkdir -p logs

# Start ResourceManager
echo "Starting ResourceManager..."
./bin/resourcemanager -port 8088 > logs/resourcemanager.log 2>&1 &
RM_PID=$!
echo $RM_PID > logs/resourcemanager.pid
echo "ResourceManager started with PID: $RM_PID"

# Wait for ResourceManager to start
sleep 3

# Start NodeManager
echo "Starting NodeManager..."
./bin/nodemanager -port 8042 -host localhost -rm-url http://localhost:8088 -memory 8192 -vcores 8 > logs/nodemanager.log 2>&1 &
NM_PID=$!
echo $NM_PID > logs/nodemanager.pid
echo "NodeManager started with PID: $NM_PID"

# Wait for services to stabilize
sleep 2

echo "YARN cluster started successfully!"
echo "ResourceManager: http://localhost:8088"
echo "NodeManager: http://localhost:8042"
echo ""
echo "To check status:"
echo "  curl http://localhost:8088/ws/v1/cluster/info"
echo "  curl http://localhost:8088/ws/v1/cluster/nodes"
echo ""
echo "To submit a test application:"
echo "  ./bin/client -rm-url http://localhost:8088 -app-name 'test-job' -command 'echo Hello YARN!'"
