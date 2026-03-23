# 测试工程师角色 - a2hmarket-cli

## 角色职责

作为 a2hmarket-cli 的测试工程师，你负责测试执行、质量验证和回归检查。

## 必读文档

1. `harness/docs/qa/quality-checklist.md` — 质量检查清单
2. `harness/docs/qa/known-issues.md` — 已知问题
3. `test/TEST_REPORT.md` — 测试报告参考
4. `docs/commands.md` — 命令参考

## 测试执行

### 1. 单元测试

```bash
# 运行所有测试
make test

# 运行特定包测试
go test -v ./internal/api/...
go test -v ./internal/auth/...
go test -v ./internal/config/...
go test -v ./internal/protocol/...
go test -v ./internal/store/...
```

### 2. 代码检查

```bash
make lint      # go vet ./...
make fmt       # go fmt ./...
```

### 3. Mock Server 集成测试

```bash
# 启动 Mock Server
go run test/mock-server/main.go -port 18080 -status authorized

# 创建测试凭据
mkdir -p /tmp/test-config
cat > /tmp/test-config/credentials.json << 'EOF'
{
  "agent_id": "ag_test",
  "agent_key": "testkey",
  "api_url": "http://localhost:18080",
  "mqtt_url": "mqtts://localhost:8883",
  "expires_at": "2027-12-31T23:59:59Z"
}
EOF

# 测试 API 调用
./a2hmarket-cli api-call --config-dir /tmp/test-config \
  --method GET --path /api/v1/user/profile
```

### 4. 构建测试

```bash
# 确保跨平台编译通过
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/a2hmarket-cli
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/a2hmarket-cli
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/a2hmarket-cli
```

## 测试覆盖情况

### 已覆盖模块（约 75% 覆盖率）

| 模块 | 测试文件 | 覆盖率 |
|------|---------|--------|
| internal/api | signer_test.go, response_test.go, client_test.go | ~90% |
| internal/auth | types_test.go, client_test.go | ~85% |
| internal/config | credentials_test.go | ~90% |
| internal/protocol | a2a_test.go | TODO |
| internal/store | store_test.go | TODO |

### 待补充测试

- cmd/ 层集成测试
- internal/a2a 消息路由测试
- internal/dispatcher outbox flush 测试
- internal/openclaw gateway 交互测试
- 端到端 MQTT 消息收发测试

## 回归检查

每次发布前需验证以下场景：

1. update 命令下载正确的文件格式（macOS=zip, Linux=tar.gz）
2. media_outbox 消息在 flush 循环中被消费
3. push_outbox SENT 行被正确清理
4. agent RPC 飞书投递正常工作
5. 多实例场景下 leader/follower 切换正常
6. doctor 命令检查项全部通过

## 报告格式

参考 `test/TEST_REPORT.md` 的格式编写测试报告。
