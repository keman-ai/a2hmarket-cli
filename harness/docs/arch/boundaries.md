# 架构边界 - a2hmarket-cli

## 模块边界

### cmd/a2hmarket-cli — CLI 入口层

只负责命令定义、参数解析和输出格式化。不包含业务逻辑。

| 文件 | 职责 |
|------|------|
| `main.go` | App 定义，注册所有命令 |
| `auth.go` | gen-auth-code, get-auth, status, api-call |
| `send.go` | send（直接 MQTT 发布） |
| `listener.go` | listen, listener run/role/takeover/reload |
| `listener_autostart.go` | 自动启动 listener（launchd / systemd / 子进程） |
| `inbox.go` | inbox pull/ack/peek/get/check |
| `inbox_history.go` | inbox history（服务端 API 查询） |
| `profile.go` | profile get/upload-qrcode/delete-qrcode |
| `works.go` | works search/publish/update/delete/list |
| `order.go` | order create/confirm/reject/cancel/... |
| `file.go` | file upload |
| `sync.go` | sync（同步 profile + works 到本地缓存） |
| `update.go` | update（CLI 自动更新） |
| `update_skill.go` | update-skill（OpenClaw skill 更新） |
| `doctor.go` | doctor（前置条件检查） |
| `helpers.go` | 共享工具函数 |

### internal/ — 业务逻辑层

| 包 | 职责 | 对外依赖 |
|-----|------|---------|
| `internal/api` | 平台 HTTP 客户端（带签名） | api.a2hmarket.ai |
| `internal/auth` | 授权流程类型定义 | web.a2hmarket.ai |
| `internal/config` | 配置加载与凭据管理 | 文件系统 |
| `internal/common` | 日志与错误码 | 无 |
| `internal/protocol` | A2A 协议信封格式 | 无 |
| `internal/mqtt` | MQTT 传输层（token + transport） | MQTT Broker, api |
| `internal/a2a` | A2A 消息处理（路由、去重、preview） | protocol, store, mqtt |
| `internal/store` | SQLite 事件存储 | modernc.org/sqlite |
| `internal/dispatcher` | 出站消息调度器 | store, openclaw |
| `internal/openclaw` | OpenClaw 交互（gateway + CLI 回退） | WebSocket, exec |
| `internal/lease` | 多实例租约协调 | api.a2hmarket.ai |
| `internal/oss` | OSS 文件上传 | api |

### pkg/utils — 公共工具

通用工具函数（目前较少使用）。

## 依赖方向

```
cmd/a2hmarket-cli
  ├── internal/config      （加载凭据）
  ├── internal/api         （业务 API 调用）
  ├── internal/auth        （授权类型）
  ├── internal/mqtt        （消息传输）
  ├── internal/a2a         （A2A 消息处理）
  ├── internal/store       （事件存储）
  ├── internal/dispatcher  （出站调度）
  ├── internal/openclaw    （OpenClaw 交互）
  ├── internal/lease       （租约协调）
  ├── internal/oss         （文件上传）
  ├── internal/protocol    （协议定义）
  └── internal/common      （日志/错误）

internal/api ← internal/mqtt（签名函数复用）
internal/api ← internal/lease（签名函数复用）
internal/a2a → internal/protocol, internal/store, internal/mqtt
internal/dispatcher → internal/store, internal/openclaw
internal/oss → internal/api, internal/config
```

**禁止**：internal/ 包不得反向依赖 cmd/ 层。

## 外部服务边界

| 外部服务 | 接入方式 | 认证方式 |
|---------|---------|---------|
| a2hmarket API | HTTPS + HMAC-SHA256 签名 | X-Agent-Id / X-Timestamp / X-Agent-Signature |
| MQTT Broker | TLS + Token 认证 | 从 /mqtt-token/api/v1/token 获取临时凭据 |
| OpenClaw Gateway | WebSocket + Ed25519 签名 | 本地 device.json 私钥签名 challenge |
| OSS | 2 步预签名直传 | 通过 API 获取签名 URL，直接 PUT 到 OSS |
| GitHub API | HTTPS（无认证） | 匿名访问 + a2hmarket.ai 代理加速 |

## 平台与进程边界

| 平台 | LaunchAgent (macOS) | systemd (Linux) | 特殊注意 |
|------|-------|---------|---------|
| daemon 启动 | launchctl load plist | systemctl --user start | PATH 最小化，需 enrichedEnv() |
| 信号处理 | SIGINT / SIGTERM | 同左 | SIGHUP 用于 reload（仅 Unix） |
| 自动启动 | ensureListenerRunning | 同左 | 优先使用 service manager，回退到子进程 |
