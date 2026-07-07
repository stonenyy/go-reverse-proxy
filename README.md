# Go 反向代理服务

基于 Go 标准库实现的高性能反向代理，支持多后端负载均衡、健康检查、限流等功能。

## 功能特性

- ✅ **多路由规则** - 按路径前缀匹配，精确路径优先
- ✅ **负载均衡** - 支持轮询 / 加权轮询 / 随机三种策略
- ✅ **健康检查** - 后台自动检测后端节点存活状态
- ✅ **限流** - 令牌桶算法，支持突发流量
- ✅ **中间件** - 日志、Panic 恢复、CORS
- ✅ **优雅退出** - 接收 SIGINT/SIGTERM 信号平滑关闭
- ✅ **Header 透传** - X-Forwarded-For / X-Real-IP
- ✅ **指标收集** - 请求总数、错误数、限流数
- ✅ **TLS 支持** - 配置证书即可启用 HTTPS
- ✅ **管理接口** - 查看节点状态和统计信息

## 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 启动模拟后端（用于测试）

```bash
# 终端 1：启动三个模拟后端
go run tools/mockserver/main.go
```

### 3. 启动反向代理

```bash
# 终端 2：启动代理
go run cmd/main.go -config configs/config.yaml
```

### 4. 手动功能测试

```bash
# 测试 API 路由（会在 9001 和 9002 之间负载均衡）
curl http://localhost:8080/api/users

# 测试静态资源路由（转发到 9003）
curl http://localhost:8080/static/style.css

# 测试默认路由
curl http://localhost:8080/

# 查看节点状态
curl http://localhost:8080/__proxy/status

# 查看节点统计
curl http://localhost:8080/__proxy/stats

# 查看运行指标
curl http://localhost:8080/__proxy/metrics

# 代理自身健康检查
curl http://localhost:8080/__proxy/health
```

### 5. 单元测试

```bash
# 使用 Makefile
make test

# 或者直接运行 Go 测试
go test ./...
```

## 配置说明

编辑 `configs/config.yaml`：

```yaml
server:
  port: 8080

routes:
  - path: /api/
    backends:
      - url: http://localhost:9001
        weight: 2          # 权重为 2，分配 2/3 流量
      - url: http://localhost:9002
        weight: 1
    load_balance: weighted # weighted / roundrobin / random

rate_limit:
  enabled: true
  requests_per_second: 100
  burst: 200

health_check:
  enabled: true
  interval: 10   # 秒
  timeout: 5
  path: /health
```

## 编译

```bash
make build
./bin/proxy -config configs/config.yaml
```

## 管理接口

| 接口 | 说明 |
|------|------|
| `GET /__proxy/health` | 代理自身健康状态 |
| `GET /__proxy/status` | 所有后端节点状态 |
| `GET /__proxy/stats`  | 健康检查统计数据 |
| `GET /__proxy/metrics` | 运行指标，包括请求总数、错误数和限流数 |

## 目录结构

```
.
├── cmd/
│   └── main.go                  # 程序入口
├── internal/
│   ├── config/config.go         # 配置解析
│   ├── metrics/metrics.go       # 运行时指标
│   ├── proxy/
│   │   ├── router.go            # 路由 & 反向代理核心
│   │   └── balancer.go          # 负载均衡策略
│   ├── middleware/middleware.go  # 中间件
│   └── healthcheck/checker.go   # 健康检查
├── tools/
│   └── mockserver/main.go       # 模拟后端（测试用）
├── configs/
│   └── config.yaml              # 配置文件
├── go.mod
└── Makefile
```
