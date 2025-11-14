.PHONY: help build run test clean deps

# 变量
BINARY_NAME=smartdnssort
MAIN_PATH=./cmd/main.go

help:
	@echo "SmartDNSSort Makefile"
	@echo ""
	@echo "可用命令:"
	@echo "  make deps       - 下载依赖"
	@echo "  make build      - 编译（Windows）"
	@echo "  make run        - 运行服务器"
	@echo "  make test       - 运行测试"
	@echo "  make clean      - 清理编译文件"
	@echo "  make test-dns   - DNS 查询测试"

deps:
	@echo "下载依赖..."
	go mod download
	go mod tidy

build: deps
	@echo "编译 $(BINARY_NAME)..."
	go build -o $(BINARY_NAME).exe $(MAIN_PATH)
	@echo "编译完成：$(BINARY_NAME).exe"

run: deps
	@echo "运行 SmartDNSSort..."
	go run $(MAIN_PATH)

test: deps
	@echo "运行测试..."
	go test -v ./...

test-verbose: deps
	@echo "详细测试..."
	go test -v -race ./...

clean:
	@echo "清理编译文件..."
	if exist $(BINARY_NAME).exe del $(BINARY_NAME).exe
	go clean

# 跨平台编译
build-linux: deps
	@echo "编译 Linux 版本..."
	$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o $(BINARY_NAME) $(MAIN_PATH)

build-macos: deps
	@echo "编译 macOS 版本..."
	$env:GOOS="darwin"; $env:GOARCH="amd64"; go build -o $(BINARY_NAME)-darwin $(MAIN_PATH)

build-all: build build-linux build-macos
	@echo "全平台编译完成"

# 代码质量检查
lint:
	@echo "运行 golangci-lint..."
	golangci-lint run ./...

fmt:
	@echo "代码格式化..."
	go fmt ./...

vet:
	@echo "运行 go vet..."
	go vet ./...

# Docker 相关（可选）
docker-build:
	docker build -t smartdnssort:latest .

docker-run:
	docker run -d --name smartdnssort -p 53:53/udp -p 53:53/tcp smartdnssort:latest
