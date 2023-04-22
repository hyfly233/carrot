#!/bin/bash

# Stop YARN cluster components

echo "Stopping YARN cluster..."

# Stop ResourceManager
if [ -f logs/resourcemanager.pid ]; then
    RM_PID=$(cat logs/resourcemanager.pid)
    if kill -0 $RM_PID 2>/dev/null; then
        echo "Stopping ResourceManager (PID: $RM_PID)..."
        kill $RM_PID
        rm logs/resourcemanager.pid
    else
        echo "ResourceManager not running"
        rm -f logs/resourcemanager.pid
    fi
else
    echo "ResourceManager PID file not found"
fi

# Stop NodeManager
if [ -f logs/rmnm.pid ]; then
    NM_PID=$(cat logs/rmnm.pid)
    if kill -0 $NM_PID 2>/dev/null; then
        echo "Stopping NodeManager (PID: $NM_PID)..."
        kill $NM_PID
        rm logs/rmnm.pid
    else
        echo "NodeManager not running"
        rm -f logs/rmnm.pid
    fi
else
    echo "NodeManager PID file not found"
fi

# Clean up container directories
echo "Cleaning up container directories..."
rm -rf /tmp/yarn-containers/*

echo "YARN cluster stopped successfully!"
