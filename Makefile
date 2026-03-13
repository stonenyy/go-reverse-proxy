.PHONY: run build test mock-backend tidy

# 运行
run:
	go run ./cmd/main.go -config configs/config.yaml

# 编译
build:
	go build -o bin/proxy ./cmd/main.go

# 整理依赖
tidy:
	go mod tidy

# 启动模拟后端（用于测试）
mock-backend:
	@echo "启动模拟后端服务..."
	@go run ./tools/mockserver/main.go &
	@echo "后端服务已启动: 9001, 9002, 9003"

# 编译并运行
start: build
	./bin/proxy -config configs/config.yaml
