# 开发陷阱 - a2hmarket-cli

本文档记录在开发过程中踩过的坑，从代码中的 AGENT.md 和实际实现中提取。

## 1. daemon 进程中 fmt.Print 写 stdout

**问题**：`listener run` 是后台 daemon，stdout 被 LaunchAgent 重定向到日志文件。`fmt.Printf` 输出的裸文本和 zerolog 的 JSON 日志混在一起，日志解析器无法处理。

**规则**：daemon 路径（`listener listen` / `listener run`）里的所有输出必须走 `common.Infof` / `common.Debugf`（zerolog → stderr + 日志文件）。只有交互式命令（`status`、`update`、`auth`）可以用 `fmt.Printf` 写 stdout。

## 2. update 命令下载 404

**问题**：macOS release 产物是 `.zip`，但 `update.go` 里 macOS 用的是 `.tar.gz`，拼出来的文件名不存在 → 404。

**规则**：`update` 命令的文件扩展名必须与 `.goreleaser.yaml` 中的 `format_overrides` 一致。当前：macOS/Windows = `.zip`，Linux = `.tar.gz`。

## 3. nginx 代理只配了 API 没配下载

**问题**：`a2hmarket.ai/github/api/` 代理到 `api.github.com`（获取 release 信息），但 `/github/{owner}/{repo}/releases/download/` 没有代理到 `github.com`（下载二进制）。代理路径 404，回退 GitHub 直连国内又访问不了。

**规则**：nginx 代理需要同时覆盖 API 查询和 release 下载两个路径。

## 4. media_outbox 只有入队没有消费

**问题**：`inbox ack --notify-external` 写入 `media_outbox` 表，但 listener daemon 的 flush 循环里只处理 `push_outbox` 和 `a2a_outbox`，没有 `media_outbox`。消息永远积压。

**规则**：每张 outbox 表必须有对应的 flush 逻辑。添加新 outbox 时，checklist：
- [ ] store 层：Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
- [ ] dispatcher 层：Flush 函数
- [ ] listener.go：flush ticker 里调用 Flush

## 5. push_outbox SENT 行永久积压

**问题**：消息成功推送后标记为 `SENT`，但没有人转为 `ACKED`。`pending_push_count` 持续增长。

**根因**：Go 版 `FlushPushOutbox` 只处理 `PENDING`/`RETRY` 行，不清理 `SENT` 行。JS 版有 `push_once` 逻辑在每次 flush 时清理 SENT → ACKED。

**规则**：flush 循环必须先扫描 `SENT` 行并标记 `ACKED`（push_once 模式），再处理 `PENDING`/`RETRY`。

## 6. chat.send deliver=true 不投递飞书

**问题**：`chat.send` 带 `deliver=true` 调用成功（无报错），但 AI 回复不路由到飞书，只留在 webchat。

**根因**：OpenClaw 的 `resolveChatSendOriginatingRoute` 检查 client mode。`"backend"` mode 不满足条件。

**解决**：改用 `agent` RPC 方法（`resolveAgentDeliveryPlan`，无 client mode 限制），并显式传 `channel` 和 `replyTo` 参数。

## 7. agent RPC 缺少 channel/replyTo 导致飞书投递失败

**问题**：`agent` RPC 带 `deliver=true` 但不传 `channel` 和 `replyTo`，报错 `Delivering to Feishu requires target`。

**规则**：调用 `agent` RPC 时，始终从 session key 解析 channel 和 target 并显式传入：
```go
channel, target := ParseSessionKey(sessionKey)
GatewayAgentSend(sessionKey, message, true, channel, target)
```

## 8. openclaw sessions --json 输出格式不稳定

**问题**：CLI 输出里 JSON 后面跟了 `[plugins] ...` 行，`json.Unmarshal` 报 `invalid character '[' after top-level value`。而且不同版本可能返回 `{"sessions":[...]}` 或直接 `[...]`。

**规则**：解析 openclaw CLI 输出时，用 `extractJSON()` + `json.Decoder` 提取第一个完整 JSON 值，同时兼容 wrapper 和 array 两种格式。

## 9. LaunchAgent / SSH 环境下 PATH 最小化

**问题**：LaunchAgent daemon 和 SSH 非交互环境下，PATH 是最小化的，node 和 openclaw 可能找不到。

**规则**：`enrichedEnv()` 会自动探测 node 路径并注入 PATH。`findOpenclawBinary()` 会在常见位置搜索。但新增外部依赖时需确保也有探测逻辑。

## 10. 签名路径包含 query string

**问题**：`GetJSON` 传入的 apiPath 含 query string（如 `?type=3&page=1`），但签名路径应不含 query。如果不显式传 signPath，签名会使用含 query 的完整路径导致 403。

**规则**：带 query string 的 API 调用必须显式传入不含 query 的 signPath，或者 API 客户端的 `doRequest` 会自动截取 `?` 前的部分。

## 11. 飞书投递目标推导失败

**问题**：`inbox ack --notify-external` 写入 `media_outbox` 时，如果 `event.TargetSessionKey` 为空（MQTT 入站时通常不带），则无法推断投递目标。

**规则**：飞书投递目标推导链：显式参数 → source-session-key 解析 → event.TargetSessionKey → 实时查 OpenClaw sessions 取最活跃的飞书会话。最后一步是最可靠的兜底。

## 12. gateway 认证 token 来源不一致

**问题**：gateway 认证 token 从 `device-auth.json` 读取，但 gateway 进程本身从 `openclaw.json` 读取，两者可能不一致。

**规则**：优先从 `~/.openclaw/openclaw.json` 的 `gateway.auth.token` 读取（与 gateway 同一份配置），回退到 `device-auth.json`。
