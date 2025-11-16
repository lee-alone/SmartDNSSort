.PHONY: help build build-linux build-arm build-windows build-all run test clean deps release

# 变量
BINARY_NAME=SmartDNSSort
MAIN_PATH=./cmd/main.go
BIN_DIR=./bin
VERSION?=v1.0

help:
	@echo "SmartDNSSort Makefile"
	@echo ""
	@echo "可用命令:"
	@echo "  make deps           - 下载依赖"
	@echo "  make build          - 编译（当前平台）"
	@echo "  make build-windows  - 编译 Windows（x86和x64）"
	@echo "  make build-linux    - 编译 Linux（x86和ARM）"
	@echo "  make build-all      - 编译所有平台"
	@echo "  make run            - 运行服务器"
	@echo "  make test           - 运行测试"
	@echo "  make clean          - 清理编译文件"
	@echo "  make release        - 打包发布版本"
	@echo "  make test-dns       - DNS 查询测试"

deps:
	@echo "下载依赖..."
	go mod download
	go mod tidy

# 创建 bin 目录
$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# Windows x64
build-windows-x64: deps $(BIN_DIR)
	@echo "编译 Windows x64..."
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-windows-x64.exe $(MAIN_PATH)
	@echo "✓ 完成: $(BIN_DIR)/$(BINARY_NAME)-windows-x64.exe"

# Windows x86
build-windows-x86: deps $(BIN_DIR)
	@echo "编译 Windows x86..."
	GOOS=windows GOARCH=386 go build -o $(BIN_DIR)/$(BINARY_NAME)-windows-x86.exe $(MAIN_PATH)
	@echo "✓ 完成: $(BIN_DIR)/$(BINARY_NAME)-windows-x86.exe"

# Linux x86
build-linux-x86: deps $(BIN_DIR)
	@echo "编译 Debian x86..."
	GOOS=linux GOARCH=386 go build -o $(BIN_DIR)/$(BINARY_NAME)-debian-x86 $(MAIN_PATH)
	@echo "✓ 完成: $(BIN_DIR)/$(BINARY_NAME)-debian-x86"

# Linux x64
build-linux-x64: deps $(BIN_DIR)
	@echo "编译 Debian x64..."
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-debian-x64 $(MAIN_PATH)
	@echo "✓ 完成: $(BIN_DIR)/$(BINARY_NAME)-debian-x64"

# Linux ARM
build-linux-arm: deps $(BIN_DIR)
	@echo "编译 Debian ARM64..."
	GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/$(BINARY_NAME)-debian-arm64 $(MAIN_PATH)
	@echo "✓ 完成: $(BIN_DIR)/$(BINARY_NAME)-debian-arm64"

# 简化别名
build-windows: build-windows-x86 build-windows-x64
build-linux: build-linux-x86 build-linux-x64 build-linux-arm

# 当前平台编译
build: deps $(BIN_DIR)
	@echo "编译当前平台..."
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "✓ 编译完成"

# 所有平台编译
build-all: build-windows build-linux
	@echo "✓ 全平台编译完成"
	@echo ""
	@echo "输出文件位置: $(BIN_DIR)/"
	@ls -lh $(BIN_DIR)/

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
	rm -rf $(BIN_DIR)
	go clean
	@echo "✓ 清理完成"

# 发布打包
release: clean build-all
	@echo "✓ 发布版本已生成"
	@echo ""
	@echo "生成的二进制文件:"
	@ls -lh $(BIN_DIR)/
	@echo ""
	@echo "请将 $(BIN_DIR)/ 中的文件上传到 GitHub Releases"

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
