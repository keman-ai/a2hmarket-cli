# 工作流 - a2hmarket-cli 端到端协作流程

## 阶段一：需求分析

**负责角色**：产品经理

1. 明确需求目标和验收标准
2. 确认功能属于哪个命令模块（auth / send / inbox / listener / profile / works / order / file）
3. 确认是否涉及新的外部服务交互（API / MQTT / OpenClaw gateway）
4. 确认输出格式是否符合 `outputOK / outputError` 的 JSON 信封规范
5. 输出：需求文档或 Issue

## 阶段二：架构评审

**负责角色**：架构师

1. 评估需求对现有分层架构（cmd → internal）的影响
2. 确认是否需要新增 outbox 表（若需要，必须同时规划 store / dispatcher / listener flush 三层）
3. 检查 OpenClaw 交互方式（优先 WebSocket gateway，回退 CLI 子进程）
4. 评估是否涉及新的 MQTT topic 或 RPC 方法
5. 确认多实例场景下的行为（leader / follower / standalone）
6. 输出：技术方案或架构决策记录

## 阶段三：编码实现

**负责角色**：开发者

1. 在 `cmd/a2hmarket-cli/` 添加命令文件，注册到 `main.go`
2. 在 `internal/` 相应包实现业务逻辑
3. 若涉及新 outbox 表，按 checklist 完成：
   - store 层：Enqueue / ListPending / MarkSent / MarkRetry / MarkFailed
   - dispatcher 层：Flush 函数
   - listener.go：flush ticker 中调用 Flush
4. 若涉及 OpenClaw 交互，实现双通道（gateway + CLI 回退）
5. daemon 路径输出走 `common.Infof`，交互式命令用 `fmt.Printf`
6. 编写单元测试
7. Commit message 必须使用英文（release notes 从 commit 自动生成，需保持英文一致）
8. 输出：代码提交

## 阶段四：代码评审

**负责角色**：代码评审员

1. 检查是否遵循 cmd → internal 分层，无跨层依赖
2. 检查所有命令输出是否使用 `outputOK / outputError` 统一格式
3. 检查 daemon 路径是否使用 zerolog 而非 fmt.Printf
4. 检查新 outbox 表是否有完整的 store + dispatcher + flush 链路
5. 检查 OpenClaw 交互是否实现双通道回退
6. 检查签名路径是否正确（不含 query string）
7. 检查 credentials.json 文件权限是否为 0600
8. 输出：评审意见

## 阶段五：测试验证

**负责角色**：测试工程师

1. 执行单元测试：`make test`
2. 执行代码检查：`make lint`（`go vet ./...`）
3. 验证 CLI 命令输出格式是否符合 JSON 信封规范
4. 使用 mock-server 进行集成测试
5. 验证 update 命令的文件扩展名与 CI 打包格式一致
6. 检查已知问题清单中的回归项
7. 输出：测试报告

## 阶段六：构建发布

**负责角色**：开发者

1. 本地构建：`make build`
2. 编写 `RELEASE_NOTES.md`（见下方规则）
3. 推送 `v*` 标签触发 GitHub Actions Release
4. CI 使用 GoReleaser 构建多平台二进制（linux/darwin/windows x amd64/arm64）
5. macOS/Windows 产物为 `.zip`，Linux 为 `.tar.gz`
6. 用户通过 `a2hmarket-cli update` 或 `install.sh` 安装

> **强制规则：功能合入 main 后必须立即打 tag 触发 Release。**
> 用户通过 GitHub Release 下载二进制，若不打 tag 则新功能无法到达用户手中。
> 版本号规则：除非特殊指定，否则只递增最后一位（patch），例如 `v1.1.35` → `v1.1.36`。

> **Release Notes 规则：打 tag 前必须生成 `RELEASE_NOTES.md` 并提交。**
> CI 优先读取此文件作为 GitHub Release 描述；若不存在则 fallback 到自动生成。
> 编写要求：
> 1. 英文，面向用户，说人话，不罗列 commit log
> 2. 按类别分组：New Features / Improvements / Bug Fixes（没有的类别省略）
> 3. 同一类事情合并为一条，只保留用户关心的变更，过滤掉 CI/chore/harness 等内部变更
> 4. 每条用一句话说清楚对用户的影响

## 检查点

每个阶段完成后，执行 `./scripts/lint-all.sh` 确保代码质量基线。
