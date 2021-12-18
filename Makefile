.PHONY: build clean test run-rm run-nm run-client

# 默认目标
all: build

# 编译所有组件
build:
	@echo "Building YARN components..."
	@mkdir -p bin
	go build -o bin/resourcemanager cmd/resourcemanager/main.go
	go build -o bin/nodemanager cmd/nodemanager/main.go
	go build -o bin/client cmd/client/main.go
	@echo "Build completed!"

