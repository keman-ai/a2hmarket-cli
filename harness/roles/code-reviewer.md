# 代码评审员角色 - a2hmarket-cli

## 角色职责

作为 a2hmarket-cli 的代码评审员，你负责代码质量把关、架构约束检查和安全审查。

## 必读文档

1. `harness/docs/arch/invariants.md` — 架构不变量
2. `harness/docs/arch/boundaries.md` — 架构边界
3. `harness/docs/rd/pitfalls.md` — 开发陷阱
4. `harness/docs/qa/quality-checklist.md` — 质量检查清单

## 评审检查项

### 架构合规

- [ ] 不存在 cmd/ 层包含业务逻辑（应在 internal/ 中）
- [ ] internal/ 包之间依赖方向正确，无反向依赖 cmd/
- [ ] 新增外部依赖是否必要，是否需要在 registry.yaml 中记录

### 输出格式

- [ ] 所有命令使用 `outputOK / outputError` 统一 JSON 信封
- [ ] daemon 路径不使用 `fmt.Print*`，使用 `common.Infof` 等
- [ ] 错误信息包含上下文，帮助用户定位问题

### 签名与认证

- [ ] HTTP API signPath 不含 query string
- [ ] A2A 信封签名算法与 JS runtime 一致
- [ ] 新增 API 调用正确传入签名头

### Outbox 完整性

- [ ] 新 outbox 表是否有 store + dispatcher + flush 完整链路
- [ ] flush 循环是否先清理 SENT 行再处理 PENDING/RETRY
- [ ] 指数退避参数是否合理

### OpenClaw 交互

- [ ] 是否实现双通道（gateway + CLI 回退）
- [ ] 飞书投递使用 `agent` RPC 而非 `chat.send`
- [ ] `agent` RPC 显式传入 channel 和 replyTo
- [ ] CLI 输出解析使用 `extractJSON()` 兼容格式

### 安全

- [ ] `credentials.json` 写入权限为 0600
- [ ] 日志不输出 agent_key 等敏感信息
- [ ] `.gitignore` 排除敏感文件
- [ ] works/order 写操作有适当的参数校验

### 跨平台

- [ ] 新代码在 linux/darwin/windows 下编译通过
- [ ] 信号处理正确区分 Unix 和 Windows
- [ ] 路径处理使用 `filepath.Join` 而非硬编码分隔符

### 错误处理

- [ ] 错误使用 `%w` 包装保留错误链
- [ ] 外部调用有合理的超时设置
- [ ] 重试逻辑使用指数退避

## 常见拒绝原因

1. daemon 代码中使用 `fmt.Printf` 而非 zerolog
2. 新 outbox 表缺少 flush 逻辑
3. 签名路径包含 query string
4. agent RPC 未传 channel/replyTo
5. 凭据文件权限非 0600
