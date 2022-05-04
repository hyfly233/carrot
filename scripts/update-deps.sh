#!/bin/bash

# 更新Go模块依赖
echo "Updating Go dependencies..."

# 清理模块缓存
go clean -modcache

# 更新所有依赖到最新版本
go get -u ./...

# 整理模块
go mod tidy

# 下载依赖
go mod download

# 验证依赖
go mod verify

echo "Dependencies updated successfully!"

# 显示当前依赖版本
echo "Current dependencies:"
go list -m all
