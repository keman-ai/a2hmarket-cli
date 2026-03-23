# 开发者角色 - a2hmarket-cli

## 角色职责

作为 a2hmarket-cli 的开发者，你负责功能编码实现、单元测试编写和代码规范遵循。

## 必读文档

1. `CLAUDE.md` — 项目配置说明
2. `AGENT.md` — 开发经验与踩坑记录（必读）
3. `harness/docs/rd/dev-conventions.md` — 开发规范
4. `harness/docs/rd/pitfalls.md` — 开发陷阱
5. `harness/docs/arch/invariants.md` — 架构约束

## 开发流程

### 1. 新增 CLI 命令

```
1. 在 cmd/a2hmarket-cli/ 创建命令文件（如 feature.go）
2. 定义 featureCommand() 函数返回 *cli.Command
3. 在 main.go 的 Commands 列表中注册
4. 实现 Action 函数：加载凭据 → 调用内部逻辑 → outputOK/outputError
5. 编写对应的单元测试
```

### 2. 新增平台 API 调用

```go
creds, err := loadCreds(configDir)
if err != nil {
    return err
}
client := buildAPIClient(creds)

// GET 请求
var data interface{}
if err := client.GetJSON(apiPath, signPath, &data); err != nil {
    return outputError("action", err)
}

// POST 请求
body := map[string]interface{}{...}
if err := client.PostJSON(apiPath, body, &data); err != nil {
    return outputError("action", err)
}

return outputOK("action", data)
```

### 3. 新增 Outbox 表

按以下 checklist 完成（缺少任何一步都会导致消息积压）：

- [ ] `internal/store/schema.go`：添加 CREATE TABLE 和索引
- [ ] `internal/store/xxx_outbox.go`：Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
- [ ] `internal/dispatcher/xxx_outbox.go`：Flush 函数
- [ ] `cmd/a2hmarket-cli/listener.go`：flush ticker 中调用 Flush

### 4. OpenClaw 交互

```go
// 1. 优先 gateway
err := openclaw.GatewayAgentSend(sessionKey, message, true, channel, target)
if err == nil {
    return nil
}

// 2. 回退 CLI
bin, err := findOpenclawBinary()
// ...
```

## 关键注意事项

1. **daemon 不用 fmt.Print**：listener run 中所有输出用 `common.Infof`
2. **签名路径不含 query**：带 `?` 的 apiPath 需传 signPath
3. **agent RPC 传 channel/replyTo**：不传会报错
4. **push_outbox 先清 SENT**：flush 循环先 Phase 1 清理 SENT → ACKED
5. **凭据权限 0600**：`os.WriteFile(path, data, 0600)`
6. **CLI 输出兼容**：解析 openclaw 输出用 `extractJSON()` + `json.Decoder`

## 常用命令

```bash
make build    # 构建
make test     # 运行测试
make lint     # 代码检查
make fmt      # 格式化
make tidy     # 依赖管理

# 启动 Mock Server 进行集成测试
go run test/mock-server/main.go -port 18080 -status authorized

# 运行特定包测试
go test -v ./internal/api/...
go test -v ./internal/config/...
go test -v ./internal/protocol/...
```
