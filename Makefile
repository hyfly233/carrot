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

# 运行客户端示例
run-client:
	@echo "Submitting test application..."
	./bin/client -rm-url http://localhost:8088 -app-name "test-app" -command "echo 'Hello YARN from Go!'"

# 启动集群（后台运行）
start-cluster:
	@echo "Starting YARN cluster..."
	@./scripts/start-cluster.sh

# 停止集群
stop-cluster:
	@echo "Stopping YARN cluster..."
	@./scripts/stop-cluster.sh

# 格式化代码
fmt:
	@echo "Formatting code..."
	go fmt ./...

# 检查代码
lint:
	@echo "Linting code..."
	golangci-lint run

# 安装依赖
deps:
	@echo "Installing dependencies..."
	go mod tidy
	go mod download

# 构建 Docker 镜像
docker-build:
	@echo "Building Docker images..."
	docker-compose -f deployments/docker/docker-compose.yml build

# 启动 Docker 集群
docker-up:
	@echo "Starting Docker cluster..."
	docker-compose -f deployments/docker/docker-compose.yml up -d
