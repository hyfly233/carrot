.PHONY: build clean test lint fmt vet run-rm run-nm run-client docker-build

# 默认目标
all: fmt vet lint test build

# Go相关变量
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# 构建信息
VERSION := $(shell git describe --tags --always --dirty)
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse HEAD)

LDFLAGS := -ldflags "\
	-X main.Version=$(VERSION) \
	-X main.BuildTime=$(BUILD_TIME) \
	-X main.GitCommit=$(GIT_COMMIT)"

# 编译所有组件
build:
	@echo "Building YARN components..."
	@mkdir -p bin
	$(GOBUILD) $(LDFLAGS) -o bin/resourcemanager cmd/resourcemanager/main.go
	$(GOBUILD) $(LDFLAGS) -o bin/nodemanager cmd/nodemanager/main.go
	$(GOBUILD) $(LDFLAGS) -o bin/client cmd/client/main.go
	$(GOBUILD) $(LDFLAGS) -o bin/applicationmaster cmd/applicationmaster/main.go
	@echo "Build completed!"

# 交叉编译
build-linux:
	@echo "Building for Linux..."
	@mkdir -p bin/linux
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/linux/resourcemanager cmd/resourcemanager/main.go
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/linux/nodemanager cmd/nodemanager/main.go
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/linux/client cmd/client/main.go

# 清理构建文件
clean:
	@echo "Cleaning build files..."
	$(GOCLEAN)
	rm -rf bin/
	@echo "Clean completed!"

# 格式化代码
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

# 代码检查
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

# 静态分析 (需要安装golangci-lint)
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Please install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# 运行测试
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

# 运行测试并生成覆盖率报告
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# 性能测试
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# 更新依赖
deps:
	@echo "Updating dependencies..."
	$(GOMOD) tidy
	$(GOMOD) download

# 运行 ResourceManager
run-rm:
	@echo "Starting ResourceManager..."
	./bin/resourcemanager -port 8088 -dev

# 运行 NodeManager
run-nm:
	@echo "Starting NodeManager..."
	./bin/nodemanager -port 8042 -host localhost -rm-url http://localhost:8088 -memory 8192 -vcores 8

# 运行客户端示例
run-client:
	@echo "Submitting test application..."
	./bin/client -rm-url http://localhost:8088 -app-name "test-app" -command "echo 'Hello YARN from Go!'"

# Docker构建
docker-build:
	@echo "Building Docker images..."
	docker-compose -f deployments/docker/docker-compose.yml build
	docker build -f deployments/docker/Dockerfile.rm -t carrot-resourcemanager:$(VERSION) .
	docker build -f deployments/docker/Dockerfile.nm -t carrot-nodemanager:$(VERSION) .

# 启动集群（后台运行）
start-cluster:
	@echo "Starting YARN cluster..."
	@./scripts/start-cluster.sh

# 停止集群
stop-cluster:
	@echo "Stopping YARN cluster..."
	@./scripts/stop-cluster.sh

# 安装开发工具
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest

# 帮助信息
help:
	@echo "Available targets:"
	@echo "  build          - Build all components"
	@echo "  build-linux    - Build for Linux"
	@echo "  clean          - Clean build files"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run linter"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage"
	@echo "  benchmark      - Run benchmarks"
	@echo "  deps           - Update dependencies"
	@echo "  docker-build   - Build Docker images"
	@echo "  start-cluster  - Start YARN cluster"
	@echo "  stop-cluster   - Stop YARN cluster"
	@echo "  install-tools  - Install development tools"

# 启动 Docker 集群
docker-up:
	@echo "Starting Docker cluster..."
	docker-compose -f deployments/docker/docker-compose.yml up -d

# 停止 Docker 集群
docker-down:
	@echo "Stopping Docker cluster..."
	docker-compose -f deployments/docker/docker-compose.yml down

# 查看 Docker 日志
docker-logs:
	docker-compose -f deployments/docker/docker-compose.yml logs -f

# 开发环境设置
dev-setup: deps build
	@echo "Development environment setup completed!"

# 发布构建
release: clean test build
	@echo "Release build completed!"
