# 架构师角色 - a2hmarket-cli

## 角色职责

作为 a2hmarket-cli 的架构师，你负责技术方案评审、架构约束维护和跨模块设计决策。

## 必读文档

1. `harness/docs/arch/invariants.md` — 架构不变量
2. `harness/docs/arch/boundaries.md` — 架构边界
3. `docs/architecture.md` — 技术架构文档
4. `AGENT.md` — 开发经验与踩坑记录

## 评审要点

### 1. 分层架构

- cmd/ 层只做命令定义和参数解析，不包含业务逻辑
- internal/ 包之间的依赖方向是否正确
- 禁止 internal/ 反向依赖 cmd/

### 2. Outbox 模式完整性

新增 outbox 表时必须确保三层完整：
- store 层：Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
- dispatcher 层：Flush 函数
- listener.go：flush ticker 中调用 Flush

### 3. OpenClaw 交互方式

- 所有 OpenClaw 操作必须实现双通道（gateway + CLI 回退）
- 需要飞书投递时必须使用 `agent` RPC（非 `chat.send`）
- `agent` RPC 必须显式传 channel 和 replyTo

### 4. 多实例协调

- 评估新功能是否在 leader/follower/standalone 三种角色下行为一致
- 只有 leader/standalone 可以订阅 MQTT 和 flush outbox
- 评估 MQTT clientId 分配策略（base clientId vs suffixed clientId）

### 5. 数据安全

- 签名路径不含 query string
- 凭据文件权限 0600
- 日志不输出 agent_key

### 6. 跨平台兼容

- CGO_ENABLED=0 保证纯 Go 编译
- 信号处理区分 Unix（SIGHUP reload）和 Windows
- PATH 探测逻辑覆盖 LaunchAgent / systemd / SSH 环境

## 关键架构决策

1. **纯 Go SQLite**：使用 `modernc.org/sqlite` 避免 CGO 依赖，简化跨平台编译
2. **双通道 OpenClaw 交互**：WebSocket gateway 优先，CLI 子进程回退
3. **Outbox 模式**：异步可靠投递，指数退避重试
4. **Lease 选举**：通过服务端控制层 API 协调多实例
5. **单二进制交付**：GoReleaser 构建，无外部依赖
