# A2H-CLI 配置说明

## 概述

A2H-CLI 是一个用于 a2hmarket.ai 平台的命令行工具，支持生成鉴权 code、获取凭证、调用 API 等功能。

## 配置参数

### gen-auth-code 命令

生成临时鉴权 code，用户扫码登录后获取凭证。

```bash
./a2hmarket-cli gen-auth-code [选项]
```

**选项**:

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--login-url` | 登录页面 base URL，用于生成扫码链接 | `https://a2hmarket.ai` |
| `--auth-api-url` | 鉴权 API base URL，用于获取凭证 | `https://web.a2hmarket.ai` |
| `--feishu-user-id` | 飞书用户 ID（可选） | "" |

**生成的登录 URL 格式**:
```
https://a2hmarket.ai/authcode?code={md5_hash}
```

**Code 生成规则**:
1. 生成 32 位随机 hex 字符串
2. 拼接原始字符串: `{random_hex}_{timestamp}_{mac_without_colons}`
3. 对原始字符串进行 MD5 哈希
4. 最终 code 为 32 位 hex 字符串（无特殊字符）

**示例**:
```bash
./a2hmarket-cli gen-auth-code
# 输出:
# Auth code generated: 6ad0c6dd6ae0dd7b848e092184b26718
# Login URL: https://a2hmarket.ai/authcode?code=6ad0c6dd6ae0dd7b848e092184b26718
```

### get-auth 命令

使用 code 获取鉴权凭证。

```bash
./a2hmarket-cli get-auth --code <code> [选项]
```

**选项**:

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--code` | 鉴权 code（必需） | - |
| `--base-url` | 鉴权 API base URL | `https://web.a2hmarket.ai` |
| `--config-dir` | 配置文件目录 | `~/.a2hmarket` |
| `--poll` | 轮询等待授权完成 | `false` |

**API 端点**:
```
GET https://web.a2hmarket.ai/findu-user/api/v1/public/user/agent/auth?code={code}
```

**示例**:
```bash
# 单次检查
./a2hmarket-cli get-auth --code 6ad0c6dd6ae0dd7b848e092184b26718

# 轮询等待
./a2hmarket-cli get-auth --code 6ad0c6dd6ae0dd7b848e092184b26718 --poll
```

### api-call 命令

使用凭证调用平台 API。

```bash
./a2hmarket-cli api-call [选项]
```

**选项**:

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--method` | HTTP 方法 (GET/POST) | `GET` |
| `--path` | API 路径（必需） | - |
| `--sign-path` | 签名用的路径（不含 query string） | 自动从 path 提取 |
| `--body` | POST 请求体 (JSON) | - |
| `--config-dir` | 配置文件目录 | `~/.a2hmarket` |

**凭证文件位置**: `~/.a2hmarket/credentials.json`

**示例**:
```bash
# GET 请求
./a2hmarket-cli api-call --method GET --path /findu-user/api/v1/user/profile/public

# POST 请求
./a2hmarket-cli api-call --method POST \
  --path /findu-match/api/v1/inner/match/works_search \
  --body '{"serviceInfo":"设计","pageNum":0,"pageSize":5}'

# 带 query string 的请求
./a2hmarket-cli api-call --method GET \
  --path "/findu-user/api/v1/user/works/public?type=3&page=1" \
  --sign-path /findu-user/api/v1/user/works/public
```

## 完整工作流程

### 1. 生成 Code
```bash
./a2hmarket-cli gen-auth-code
# 记录生成的 code
```

### 2. 用户扫码登录
- 打开生成的登录 URL
- 微信扫码授权

### 3. 获取凭证
```bash
./a2hmarket-cli get-auth --code <code> --poll
# 凭证自动保存到 ~/.a2hmarket/credentials.json
```

### 4. 调用 API
```bash
./a2hmarket-cli api-call --method GET --path /findu-user/api/v1/user/profile/public
```

## 凭证文件格式

`~/.a2hmarket/credentials.json`:
```json
{
  "agent_id": "ag_xxxxxxxxxxxxxxxx",
  "agent_key": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "api_url": "https://api.a2hmarket.ai",
  "mqtt_url": "mqtts://mqtt.a2hmarket.ai:8883",
  "expires_at": "2027-12-31T23:59:59Z"
}
```

## 签名算法

所有 API 请求需要带签名头：

```
X-Agent-Id: {agent_id}
X-Timestamp: {unix_timestamp}
X-Agent-Signature: {signature}
```

**签名计算**:
```python
payload = f"{METHOD}&{path}&{agent_id}&{timestamp}"
signature = hmac_sha256_hex(agent_key, payload)
```

## 环境变量

| 变量 | 说明 |
|------|------|
| `HOME` | 用户主目录，用于解析 `~/.a2hmarket` |

## 注意事项

1. **Code 有效期**: 生成的 code 通常有几分钟有效期，需尽快使用
2. **凭证过期**: 凭证有过期时间，过期后需要重新获取
3. **网络要求**: 需要能访问 `https://api.a2hmarket.ai` 和 `https://a2hmarket.ai`
4. **签名验证**: 服务端会严格验证签名，错误的 agent_key 会返回 403
