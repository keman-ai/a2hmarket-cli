# A2H-CLI 开发经验与踩坑记录

## 架构要点

### 消息投递有三条独立通道

| 通道 | 表 | 方向 | flush 位置 |
|------|-----|------|-----------|
| A2A 出站 | `a2a_outbox` | CLI → 对方 Agent（MQTT） | `dispatcher.FlushA2AOutbox` |
| Push 推送 | `push_outbox` | 入站消息 → OpenClaw session | `dispatcher.FlushPushOutbox` |
| 外部通知 | `media_outbox` | ack 摘要 → 飞书等外部渠道 | `dispatcher.FlushMediaOutbox` |

**教训**：添加新的 outbox 表后，必须在 listener daemon 的 flush ticker 里加对应的 flush 循环，否则消息入队了但永远不会被消费。`media_outbox` 曾因缺少 flush 循环导致飞书通知全部积压。

### OpenClaw Gateway RPC 方法选择

与 OpenClaw gateway 交互有三种 RPC 方法，用途完全不同：

| RPC 方法 | 用途 | 是否经过 AI | 能否投递飞书 |
|----------|------|------------|------------|
| `chat.send` | 向 AI session 注入消息 | 是，AI 处理 | **不能**（client mode 限制） |
| `agent` | 向 AI session 注入消息 + deliver | 是，AI 处理 | **能**（需传 channel + replyTo） |
| `send` | 直接发消息到外部渠道 | 否，绕过 AI | 能（raw send） |

**核心教训**：

1. **要让 AI 处理后投递飞书，必须用 `agent` RPC + `deliver=true`**，不能用 `chat.send`。
   - `chat.send` 的 `deliver=true` 有 client mode 限制：`resolveChatSendOriginatingRoute` 检查 client mode，`"backend"` mode 被拒绝继承 session 的外部路由。
   - `agent` RPC 用 `resolveAgentDeliveryPlan`，没有 client mode 限制。
   - 这和 `openclaw agent --session-id <id> --message <msg> --deliver` CLI 是同一条路径。

2. **`agent` RPC 投递飞书时必须显式传 `channel` 和 `replyTo`**。
   - 不传会报错：`Delivering to Feishu requires target <chatId|user:openId|chat:chatId>`
   - 从 session key 解析：`agent:main:feishu:direct:ou_xxx` → `channel="feishu"`, `replyTo="ou_xxx"`

3. **`send` RPC 是 raw send，直接到飞书，不经过 AI**。
   - 可靠但消息没有 AI 处理和格式化
   - 作为 `agent` RPC 失败时的兜底

### OpenClaw 交互双通道

所有 OpenClaw 操作都走双通道：
1. **优先**：WebSocket gateway（`ws://127.0.0.1:{port}`），快
2. **回退**：CLI 子进程（`exec openclaw ...`），慢但稳

**教训**：
- gateway 认证 token 优先从 `~/.openclaw/openclaw.json` 读取（与 gateway 同一份配置），不要依赖 `device-auth.json`，两者容易不一致。
- CLI 回退时 `openclaw sessions --json` 的 stdout 可能混入 `[plugins]` 日志行，JSON 解析必须容忍非 JSON 前缀/后缀。用 `json.Decoder` 提取第一个完整 JSON 值。

### Session 路由策略

`ResolvePushSession` 从 OpenClaw sessions 列表中选最佳 session，优先级：
1. feishu channel session（`updatedAt` 最新的）
2. 任何非 main session
3. 任何有 sessionId 的 session

**教训**：push_outbox 之前用 `GetMostRecentSession` 取第一个 session（可能是 `agent:main:main` 系统 session），消息到了 AI 但 AI 回复留在 webchat。必须优先选飞书 session。

### 飞书投递目标的推导链

`inbox ack --notify-external` 需要知道推送到哪个飞书聊天，推导顺序：
1. 显式 `--channel` / `--to` 参数
2. `--source-session-key` 解析
3. `event.TargetSessionKey` 解析
4. **兜底**：查 OpenClaw sessions，找 `updatedAt` 最大且 key 含可投递 channel 的 session

**教训**：事件入库时 `target_session_key` 通常为空（MQTT 入站时不带），不能依赖它。最可靠的方式是实时查 OpenClaw sessions 取最活跃的飞书会话。

---

## 踩过的坑

### 1. daemon 进程中 fmt.Print 写 stdout

**问题**：`listener run` 是后台 daemon，stdout 被 LaunchAgent 重定向到日志文件。`fmt.Printf` 输出的裸文本和 zerolog 的 JSON 日志混在一起，日志解析器无法处理。

**规则**：daemon 路径（`listener listen` / `listener run`）里的所有输出必须走 `common.Infof` / `common.Debugf`（zerolog → stderr + 日志文件）。只有交互式命令（`status`、`update`、`auth`）可以用 `fmt.Printf` 写 stdout。

### 2. update 命令下载 404

**问题**：macOS release 产物是 `.zip`，但 `update.go` 里 macOS 用的是 `.tar.gz`，拼出来的文件名不存在 → 404。

**规则**：`update` 命令的文件扩展名必须与 CI 打包格式一致。当前：macOS/Windows = `.zip`，Linux = `.tar.gz`。

### 3. nginx 代理只配了 API 没配下载

**问题**：`a2hmarket.ai/github/api/` 代理到 `api.github.com`（获取 release 信息），但 `/github/{owner}/{repo}/releases/download/` 没有代理到 `github.com`（下载二进制）。代理路径 404，回退 GitHub 直连国内又访问不了。

**规则**：nginx 代理需要同时覆盖 API 查询和 release 下载两个路径。GitHub release 下载会 302 到 `objects.githubusercontent.com`，nginx 需配 `proxy_redirect` 或额外的 location 跟随重定向。

### 4. media_outbox 只有入队没有消费

**问题**：`inbox ack --notify-external` 写入 `media_outbox` 表（`summary_enqueued: true`），但 listener daemon 的 flush 循环里只处理 `push_outbox` 和 `a2a_outbox`，没有 `media_outbox`。消息永远积压。

**规则**：每张 outbox 表必须有对应的 flush 逻辑。添加新 outbox 时，checklist：
- [ ] store 层：Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
- [ ] dispatcher 层：Flush 函数
- [ ] listener.go：flush ticker 里调用 Flush

### 5. push_outbox SENT 行永久积压

**问题**：消息成功 `push: delivered` 后标记为 `SENT`，但没有人转为 `ACKED`。`pending_push_count` 持续增长。

**根因**：Go 版 `FlushPushOutbox` 只处理 `PENDING`/`RETRY` 行，不清理 `SENT` 行。JS 版有 `push_once` 逻辑在每次 flush 时清理 SENT → ACKED。

**规则**：flush 循环必须先扫描 `SENT` 行并标记 `ACKED`（push_once 模式），再处理 `PENDING`/`RETRY`。

### 6. chat.send deliver=true 不投递飞书

**问题**：`chat.send` 带 `deliver=true` 调用成功（无报错），但 AI 回复不路由到飞书，只留在 webchat。

**根因**：OpenClaw 的 `resolveChatSendOriginatingRoute` 检查 client mode。我们的 `"backend"` mode 不满足条件（只有 `"cli"` mode 或无 metadata 的客户端才能继承 session 外部路由）。

**解决**：改用 `agent` RPC 方法（`resolveAgentDeliveryPlan`，无 client mode 限制），并显式传 `channel` 和 `replyTo` 参数。

### 7. agent RPC 缺少 channel/replyTo 导致飞书投递失败

**问题**：`agent` RPC 带 `deliver=true` 但不传 `channel` 和 `replyTo`，报错 `Delivering to Feishu requires target`。

**根因**：`resolveAgentDeliveryPlan` 能从 session entry 的 `lastChannel`/`lastTo` 推断投递目标，但如果 session entry 没有这些字段（首次投递或字段未持久化），就需要显式传。

**规则**：调用 `agent` RPC 时，始终从 session key 解析 channel 和 target 并显式传入：
```go
channel, target := ParseSessionKey(sessionKey)
GatewayAgentSend(sessionKey, message, true, channel, target)
```

### 8. `openclaw sessions --json` 输出格式不稳定（原 #5）

**问题**：CLI 输出里 JSON 后面跟了 `[plugins] ...` 行，`json.Unmarshal` 报 `invalid character '[' after top-level value`。而且不同版本可能返回 `{"sessions":[...]}` 或直接 `[...]`。

**规则**：解析 openclaw CLI 输出时，用 `extractJSON()` + `json.Decoder` 提取第一个完整 JSON 值，同时兼容 wrapper 和 array 两种格式。

---

## 部署相关

### 环境差异

| 场景 | PATH | node | openclaw |
|------|------|------|----------|
| 用户终端 | 完整 | nvm 加载 | 在 PATH 中 |
| LaunchAgent daemon | 最小化 | 可能找不到 | 可能找不到 |
| SSH 非交互 | 最小化 | 可能找不到 | 需要绝对路径 |

`enrichedEnv()` 会自动探测 node 路径并注入 PATH。`findOpenclawBinary()` 会在常见位置搜索。但 LaunchAgent 和 SSH 环境下仍需注意。

### 远程调试

```bash
# SSH 到目标机器执行 CLI（需要设置 PATH）
ssh user@host "export PATH=/opt/homebrew/bin:~/bin:\$PATH && a2hmarket-cli inbox check"

# 查看 listener 日志
ssh user@host "tail -50 ~/.a2hmarket/store/listener.log"

# 查看 OpenClaw sessions
ssh user@host "export PATH=/opt/homebrew/bin:\$PATH && openclaw sessions --json"
```

### skill 更新

```bash
# 更新 a2hmarket skill 到最新版
a2hmarket-cli update-skill

# 首次安装（目录不存在时）
a2hmarket-cli update-skill --force
```

Skill 包从 `https://a2hmarket.ai/github` 代理下载，解压到 `~/.openclaw/workspace/skills/a2hmarket/`。
