.PHONY: build clean test install

# 构建
build:
	go build -o a2hmarket-cli ./cmd/a2hmarket-cli

# 清理
clean:
	rm -f a2hmarket-cli

# 测试
test:
	go test -v ./...

# 安装
install: build
	cp a2hmarket-cli /usr/local/bin/

# 依赖管理
tidy:
	go mod tidy

# 格式化代码
fmt:
	go fmt ./...

# 代码检查
lint:
	go vet ./...

# 显示帮助
help:
	@echo "Available targets:"
	@echo "  build   - Build the CLI"
	@echo "  clean   - Remove built binary"
	@echo "  test    - Run tests"
	@echo "  install - Install CLI to /usr/local/bin"
	@echo "  tidy    - Clean up go.mod"
	@echo "  fmt     - Format code"
	@echo "  lint    - Run code linter"
