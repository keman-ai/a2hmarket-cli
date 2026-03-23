# AGENTS.md - a2hmarket-cli

## 项目简介

a2hmarket-cli 是 A2H Market 平台的 Go 语言命令行工具，负责 Agent 身份认证、A2A（Agent-to-Agent）消息收发、本地消息持久化及外部通知推送。

## 快速开始

```bash
make build    # 构建
make test     # 测试
make lint     # 代码检查
```

## 关键约束

1. **Outbox 完整性**：每张 outbox 表必须同时有 store 层、dispatcher 层和 listener flush 三层实现
2. **OpenClaw 双通道**：所有 OpenClaw 操作必须实现 WebSocket gateway 优先 + CLI 子进程回退
3. **飞书投递用 agent RPC**：不能用 chat.send（client mode 限制），必须显式传 channel 和 replyTo
4. **daemon 不用 fmt.Print**：listener run 中所有输出用 zerolog（common.Infof 等）
5. **签名路径不含 query**：HTTP API signPath 必须是不含 query string 的纯路径
6. **凭据权限 0600**：credentials.json 写入权限必须为 0600

## 目录结构

```
cmd/a2hmarket-cli/    CLI 入口层
internal/             业务逻辑层
  api/                平台 HTTP 客户端（带签名）
  auth/               授权流程类型
  config/             配置与凭据管理
  common/             日志与错误码
  protocol/           A2A 协议信封
  mqtt/               MQTT 传输层
  a2a/                A2A 消息处理
  store/              SQLite 事件存储
  dispatcher/         出站消息调度器
  openclaw/           OpenClaw 交互
  lease/              多实例租约
  oss/                OSS 文件上传
harness/              工程规范文档
```

## Harness 文档索引

| 文档 | 路径 | 用途 |
|------|------|------|
| 注册表 | `harness/registry.yaml` | 项目元信息与技术栈 |
| 工作流 | `harness/workflow.md` | 端到端协作流程 |
| 架构不变量 | `harness/docs/arch/invariants.md` | 必须遵守的架构规则 |
| 架构边界 | `harness/docs/arch/boundaries.md` | 模块划分与依赖方向 |
| 产品概述 | `harness/docs/pm/product-overview.md` | 产品功能与业务流程 |
| 开发规范 | `harness/docs/rd/dev-conventions.md` | 编码与项目结构规范 |
| 开发陷阱 | `harness/docs/rd/pitfalls.md` | 踩坑记录 |
| 质量清单 | `harness/docs/qa/quality-checklist.md` | 代码评审检查项 |
| 已知问题 | `harness/docs/qa/known-issues.md` | 当前已知的问题 |

## 角色文档

| 角色 | 路径 |
|------|------|
| 产品经理 | `harness/roles/product-manager.md` |
| 架构师 | `harness/roles/architect.md` |
| 开发者 | `harness/roles/coder.md` |
| 代码评审员 | `harness/roles/code-reviewer.md` |
| 测试工程师 | `harness/roles/qa.md` |
