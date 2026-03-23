# 已知问题 - a2hmarket-cli

## 活跃问题

### 1. pkg/utils 包未被使用

**描述**：`pkg/utils/utils.go` 中的 `GetEnv` 函数始终返回空字符串，且整个 pkg/utils 包在项目中未被任何代码引用。

**影响**：无功能影响，但代码冗余。

**状态**：待清理

### 2. TLS InsecureSkipVerify

**描述**：MQTT transport 中使用 `InsecureSkipVerify: true`，跳过了 TLS 证书验证。

**位置**：`internal/mqtt/transport.go` `buildMQTTOptions` 函数

**影响**：可能存在中间人攻击风险。

**状态**：待评估（可能是阿里云 MQTT 证书链问题的临时方案）

### 3. 文件下载大小限制硬编码

**描述**：`internal/openclaw/download.go` 中文件下载大小上限硬编码为 50MB。

**影响**：超过 50MB 的附件无法通过 push_outbox 下载到本地并推送到飞书。

**状态**：TODO - 考虑可配置化

## 已解决问题

### 1. HTTPS TLS 证书问题（已修复）

**描述**：`api.a2hmarket.ai` 的 SSL 证书未包含该域名。

**解决**：服务端已更新证书。

### 2. media_outbox 积压（已修复）

**描述**：media_outbox 表只有入队没有消费，消息永远积压。

**解决**：在 listener.go flush ticker 中添加 `FlushMediaOutbox` 调用。

### 3. push_outbox SENT 行永久积压（已修复）

**描述**：消息推送成功后标记为 SENT 但不清理。

**解决**：FlushPushOutbox 先执行 Phase 1 清理 SENT → ACKED，再处理 PENDING/RETRY。

### 4. chat.send 不投递飞书（已修复）

**描述**：chat.send 的 deliver=true 因 client mode 限制不生效。

**解决**：改用 agent RPC 方法 + 显式传入 channel/replyTo。

### 5. update 命令 macOS 下载 404（已修复）

**描述**：update 命令对 macOS 使用 .tar.gz 但实际产物是 .zip。

**解决**：update.go 中根据 GOOS 正确选择扩展名。
