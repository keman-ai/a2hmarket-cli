# 开发规范 - a2hmarket-cli

## 项目结构

```
cmd/a2hmarket-cli/    CLI 入口层，每个命令一个文件
internal/             业务逻辑层（不对外暴露）
  api/                平台 HTTP 客户端（带签名）
  auth/               授权流程类型
  config/             配置与凭据管理
  common/             日志与错误码
  protocol/           A2A 协议信封
  mqtt/               MQTT 传输层
  a2a/                A2A 消息处理
  store/              SQLite 事件存储
  dispatcher/         出站消息调度器
  openclaw/           OpenClaw 交互
  lease/              多实例租约
  oss/                OSS 文件上传
pkg/utils/            公共工具
scripts/              构建与发布脚本
test/                 集成测试与 mock server
```

## 命令开发规范

### 新增命令步骤

1. 在 `cmd/a2hmarket-cli/` 创建命令文件（如 `feature.go`）
2. 定义 `featureCommand()` 函数返回 `*cli.Command`
3. 在 `main.go` 的 `Commands` 列表中注册
4. 命令输出使用 `outputOK / outputError` 统一格式

### 输出格式

所有命令必须使用统一的 JSON 信封格式：

```go
// 成功
return outputOK("action.name", map[string]interface{}{...})

// 失败
return outputError("action.name", fmt.Errorf("error message"))
```

### 日志规范

- **daemon 路径**（listener listen / listener run）：使用 `common.Infof / common.Debugf / common.Warnf`
- **交互式命令**：可使用 `fmt.Printf` 写 stdout
- 禁止在 daemon 路径使用 `fmt.Print*`，否则裸文本和 zerolog JSON 日志混在一起

### 凭据加载

```go
configDir := expandHome(c.String("config-dir"))
creds, err := loadCreds(configDir)
if err != nil {
    return err
}
```

### API 客户端

```go
client := buildAPIClient(creds)
var data interface{}
if err := client.GetJSON(apiPath, signPath, &data); err != nil {
    return outputError("action", err)
}
```

## 签名规范

### HTTP API 签名

```
payload = "{METHOD}&{path}&{agentId}&{timestamp}"
signature = HMAC-SHA256(agentKey, payload).hex()
```

- signPath 必须是不含 query string 的纯路径
- 带 query string 时需显式传入 signPath 参数

### A2A 信封签名

1. 递归排序 payload 所有 key → canonicalize → SHA-256 → payload_hash
2. 去除 signature 字段 → canonicalize 整个信封 → HMAC-SHA256(agentKey, ...)

## 编码规范

### Go 代码风格

- 代码格式化：`go fmt ./...`
- 代码检查：`go vet ./...`
- 构建：`CGO_ENABLED=0 go build -o a2hmarket-cli ./cmd/a2hmarket-cli`

### 错误处理

- 使用 `fmt.Errorf` 包装错误，保留错误链：`fmt.Errorf("context: %w", err)`
- 对外输出使用 `outputError`，内部使用 `return fmt.Errorf(...)`
- `common` 包提供类型化错误：`ConfigError / CredentialError / MQTTError` 等

### 环境变量

| 变量 | 用途 |
|------|------|
| `HOME` | 默认配置目录基础路径 |
| `A2H_BASE_URL` | 覆盖默认 API base URL |
| `A2H_DEBUG` | 开启 debug 模式 |
| `OPENCLAW_STATE_DIR` | OpenClaw 状态目录 |
| `OPENCLAW_HOME` | OpenClaw 主目录 |
| `OPENCLAW_CONFIG_PATH` | OpenClaw 配置文件路径 |

## 构建与测试

```bash
make build    # 构建
make test     # 运行测试
make lint     # 代码检查（go vet）
make fmt      # 格式化
make tidy     # 依赖管理
```

## 版本管理

- 版本号通过 ldflags 注入：`-X main.version={{.Version}}`
- 本地构建默认版本为 `"dev"`
- 正式版本由 GoReleaser 在 CI 中注入
