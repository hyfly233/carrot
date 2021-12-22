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

# 清理构建文件
clean:
	@echo "Cleaning build files..."
	rm -rf bin/
	@echo "Clean completed!"

# 运行测试
test:
	@echo "Running tests..."
	go test ./...

# 运行 ResourceManager
run-rm:
	@echo "Starting ResourceManager..."
	./bin/resourcemanager -port 8088

# 运行 NodeManager
run-nm:
	@echo "Starting NodeManager..."
	./bin/nodemanager -port 8042 -host localhost -rm-url http://localhost:8088 -memory 8192 -vcores 8
