# 架构不变量 - a2hmarket-cli

## 1. 消息投递三通道完整性

系统有三条独立的消息投递通道，每条通道必须同时具备入队（store）和消费（dispatcher + flush）逻辑：

| 通道 | 表 | 方向 | flush 位置 |
|------|-----|------|-----------|
| A2A 出站 | `a2a_outbox` | CLI → 对方 Agent（MQTT） | `dispatcher.FlushA2AOutbox` |
| Push 推送 | `push_outbox` | 入站消息 → OpenClaw session | `dispatcher.FlushPushOutbox` |
| 外部通知 | `media_outbox` | ack 摘要 → 飞书等外部渠道 | `dispatcher.FlushMediaOutbox` |

**不变量**：每张 outbox 表必须有对应的 flush 逻辑。添加新 outbox 时必须完成：
- store 层：Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
- dispatcher 层：Flush 函数
- listener.go：flush ticker 中调用 Flush

## 2. OpenClaw 交互双通道

所有 OpenClaw 操作必须实现双通道：
1. **优先**：WebSocket gateway（`ws://127.0.0.1:{port}`）
2. **回退**：CLI 子进程（`exec openclaw ...`）

gateway 不可用时自动回退到 CLI，确保功能在 gateway 未运行时也可用。

## 3. OpenClaw RPC 方法选择

- **chat.send**：仅用于内部推送（不投递外部渠道），因 client mode 限制
- **agent**：用于需要 AI 处理且投递到外部渠道的场景，必须显式传 `channel` 和 `replyTo`
- **send**：用于绕过 AI 直接发到外部渠道的场景

**不变量**：要让 AI 处理后投递飞书，必须用 `agent` RPC + `deliver=true` + 显式 channel/replyTo

## 4. 签名算法一致性

HTTP API 签名：`HMAC-SHA256(agentKey, "{METHOD}&{path}&{agentId}&{timestamp}")`

A2A 信封签名：先计算 payload_hash（SHA-256 of canonicalized payload），再对整个信封（去除 signature 字段）做 HMAC-SHA256

**不变量**：签名路径（signPath）必须是不含 query string 的纯路径

## 5. Daemon 与交互式输出分离

- **daemon 路径**（listener listen / listener run）：所有输出必须走 `common.Infof / common.Debugf`（zerolog JSON → stderr + 日志文件）
- **交互式命令**（status、update、auth 等）：可以使用 `fmt.Printf` 写 stdout

**不变量**：daemon 路径中禁止使用 `fmt.Print*` 写 stdout

## 6. 统一输出格式

所有 CLI 命令的成功/失败输出必须使用 `outputOK / outputError` 函数，产生如下 JSON 信封：
```json
{"ok": true/false, "action": "command.name", "data": {...}}
```

## 7. Schema 迁移幂等性

SQLite schema 迁移采用 `CREATE TABLE IF NOT EXISTS` + 列级 `ALTER TABLE ADD COLUMN IF NOT EXISTS`，保证可在已有数据库上无损升级。

**不变量**：`applySchema` 函数必须幂等，不能依赖外部迁移工具

## 8. 凭据安全

- `credentials.json` 文件权限必须为 `0600`
- 凭据中的 `agent_key` 不得出现在日志输出中
- `.gitignore` 必须排除 `credentials.json` 和 `.env.release.local`

## 9. 多实例租约协调

- 同一 agent_id 可在多台机器运行，通过 lease API 选举 leader
- leader：订阅 MQTT + 处理消息 + flush outbox
- follower：待机，定期 poll，检测到 leader 过期时自动升级
- standalone：控制层不可达时的降级模式

**不变量**：只有 leader 或 standalone 角色才能订阅 MQTT 和 flush outbox

## 10. 去重机制

入站消息通过 `(peer_id, hash)` UNIQUE 约束去重。hash 计算：
- 有 messageID 时：`SHA-256("id:" + messageID)`
- 无 messageID 时：`SHA-256("{peerID}|{messageTs}|{messageText}")`

**不变量**：相同消息不会在 message_event 表中出现两次
