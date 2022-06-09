#!/bin/bash

# 构建 ApplicationMaster
set -e

echo "Building ApplicationMaster..."

# 确保在项目根目录
cd "$(dirname "$0")/.."

# 创建 bin 目录
mkdir -p bin

# 构建 ApplicationMaster
echo "Compiling ApplicationMaster..."
go build -o bin/applicationmaster ./cmd/applicationmaster/

echo "ApplicationMaster built successfully: bin/applicationmaster"

# 运行测试
echo "Running ApplicationMaster tests..."
go test ./internal/applicationmaster/... -v

echo "Build completed successfully!"
