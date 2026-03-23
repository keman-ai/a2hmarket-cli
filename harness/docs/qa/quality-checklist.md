# 质量检查清单 - a2hmarket-cli

## 构建检查

- [ ] `make build` 编译通过，无报错
- [ ] `make test` 所有单元测试通过
- [ ] `make lint`（`go vet ./...`）无警告
- [ ] `make fmt`（`go fmt ./...`）无格式问题

## 命令输出检查

- [ ] 所有命令成功输出使用 `outputOK` 统一格式
- [ ] 所有命令错误输出使用 `outputError` 统一格式
- [ ] 输出 JSON 可被标准 JSON 解析器正确解析
- [ ] daemon 路径不使用 `fmt.Print*`

## 签名检查

- [ ] HTTP API 签名路径不含 query string
- [ ] A2A 信封签名的 canonicalize 函数递归排序所有 key
- [ ] 签名算法与 JS runtime 保持一致

## Outbox 完整性检查

新增 outbox 表时检查：
- [ ] store 层有 Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
- [ ] dispatcher 层有 Flush 函数
- [ ] listener.go flush ticker 中调用了 Flush
- [ ] flush 循环先清理 SENT 行再处理 PENDING/RETRY

## OpenClaw 交互检查

- [ ] 实现了双通道（gateway + CLI 回退）
- [ ] 需要飞书投递时使用 `agent` RPC（非 `chat.send`）
- [ ] `agent` RPC 调用时显式传入 `channel` 和 `replyTo`
- [ ] 解析 CLI 输出使用 `extractJSON()` 兼容非 JSON 前缀/后缀

## 安全检查

- [ ] `credentials.json` 写入权限为 `0600`
- [ ] 日志中不输出 agent_key
- [ ] `.gitignore` 排除敏感文件
- [ ] `works publish/update/delete` 必须带 `--confirm-human-reviewed`

## 多实例检查

- [ ] 只有 leader / standalone 角色订阅 MQTT
- [ ] 只有 leader / standalone 角色 flush outbox
- [ ] follower 通过 poll 检测 leader 过期后自动升级
- [ ] heartbeat revoked 时 leader 自动降级退出

## 跨平台检查

- [ ] update 命令的文件扩展名与 `.goreleaser.yaml` 一致（macOS/Windows=zip, Linux=tar.gz）
- [ ] reload 信号仅在 Unix 平台注册（SIGHUP）
- [ ] Windows 平台有 reload_signal_windows.go 空实现
- [ ] enrichedEnv() 正确探测 node 路径

## 数据库检查

- [ ] schema 迁移幂等（CREATE TABLE IF NOT EXISTS + ADD COLUMN IF NOT EXISTS）
- [ ] 入站消息通过 (peer_id, hash) 去重
- [ ] message_id 有 UNIQUE 索引
