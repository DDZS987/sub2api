# [P0] v0.1.85 Gemini 所有 OAuth 账号全量失效，无法刷新 Token 且授权链接生成返回 500

**关联 Issues：** #612 #613 #615

---

## 问题描述

升级到 v0.1.85 后，**所有 Gemini OAuth 账号（Antigravity 和 Gemini CLI 两种类型）同时失效**，影响面为全量 Gemini 用户，属于完全阻断性故障。

## 现象

1. 所有 Gemini 账号 Token 到期后无法自动刷新，持续报错
2. 管理后台点击"重新授权"，生成授权链接接口 `POST /api/v1/admin/gemini/oauth/auth-url` 返回 **HTTP 500**
3. 账号状态变为不可用，所有 Gemini 相关请求返回 **HTTP 401**

## 实际报错日志

Token 自动刷新失败（每隔 5 分钟循环报错）：

```
token_refresh.retry_attempt_failed  account_id=3  attempt=1/3
error: code=400 reason="ANTIGRAVITY_OAUTH_CLIENT_SECRET_MISSING"
message="missing antigravity oauth client_secret; set ANTIGRAVITY_OAUTH_CLIENT_SECRET"

token_refresh.retry_attempt_failed  account_id=2  attempt=1/3
error: code=400 reason="GEMINI_CLI_OAUTH_CLIENT_SECRET_MISSING"
message="built-in Gemini CLI OAuth client_secret is not configured; set GEMINI_CLI_OAUTH_CLIENT_SECRET or provide a custom OAuth client"

token_refresh.cycle_completed  total=6  oauth=6  needs_refresh=2  refreshed=0  failed=2
```

生成授权链接失败：

```
POST /api/v1/admin/gemini/oauth/auth-url  status=500  latency=1ms
```

## 根因分析

v0.1.85 引入的变更导致系统不再自动使用内置的 OAuth 客户端凭据，改为要求通过环境变量显式提供，但 `docker-compose.yml` 和 `.env.example` 均未新增对应变量，导致所有用户升级后直接 break。

具体涉及两条路径：

| 账号类型 | 缺失的环境变量 | 影响范围 |
| --- | --- | --- |
| Antigravity | `ANTIGRAVITY_OAUTH_CLIENT_SECRET` | 所有 Antigravity 账号 |
| Gemini CLI | `GEMINI_CLI_OAUTH_CLIENT_SECRET` | 所有 Gemini CLI 账号 |

## 建议修复方式

在 `docker-compose.yml` 补充这两个变量的传递，并在 `.env.example` 加入说明和默认值（内置凭据），使升级用户无需额外操作即可恢复正常。

这不是用户配置问题，是**升级过程中的 breaking change 未做向后兼容处理**。

---

## 临时修复教程（用户自救）

> **适用范围：** 使用 Docker Compose 部署、升级到 v0.1.85 后 Gemini 账号全部失效的用户。
> **操作时间：** 约 2 分钟，无需重新授权，已有账号数据不会丢失。

### 第一步：编辑 `.env` 文件

打开你的 `.env` 文件，在 `GEMINI_OAUTH_CLIENT_SECRET=` 那一行**下方**添加以下两行：

```env
GEMINI_CLI_OAUTH_CLIENT_SECRET=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
ANTIGRAVITY_OAUTH_CLIENT_SECRET=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
```

### 第二步：编辑 `docker-compose.yml`

找到 `environment:` 下 Gemini 相关的配置块（搜索 `GEMINI_OAUTH_CLIENT_SECRET`），在其**下方**追加两行：

```yaml
      - GEMINI_CLI_OAUTH_CLIENT_SECRET=${GEMINI_CLI_OAUTH_CLIENT_SECRET:-}
      - ANTIGRAVITY_OAUTH_CLIENT_SECRET=${ANTIGRAVITY_OAUTH_CLIENT_SECRET:-}
```

添加后该区域看起来像这样：

```yaml
      - GEMINI_OAUTH_CLIENT_ID=${GEMINI_OAUTH_CLIENT_ID:-}
      - GEMINI_OAUTH_CLIENT_SECRET=${GEMINI_OAUTH_CLIENT_SECRET:-}
      - GEMINI_OAUTH_SCOPES=${GEMINI_OAUTH_SCOPES:-}
      - GEMINI_QUOTA_POLICY=${GEMINI_QUOTA_POLICY:-}
      - GEMINI_CLI_OAUTH_CLIENT_SECRET=${GEMINI_CLI_OAUTH_CLIENT_SECRET:-}      # 新增
      - ANTIGRAVITY_OAUTH_CLIENT_SECRET=${ANTIGRAVITY_OAUTH_CLIENT_SECRET:-}    # 新增
```

### 第三步：重启容器

```bash
cd /path/to/your/deploy
docker compose up -d
```

### 验证修复

等待约 30 秒后执行：

```bash
docker logs sub2api --since=1m | grep -i "token_refresh\|refreshed"
```

看到以下输出即表示修复成功：

```
[TokenRefresh] Account X (xxx) refreshed successfully
[TokenRefresh] Cycle complete: ... refreshed=N, failed=0
```

### 说明

- 以上两个值是 Gemini CLI / Antigravity 工具的**公开内置凭据**，已包含在项目源码中，填写后与升级前行为完全一致。
- **不需要重新授权**，原有 refresh_token 仍然有效，账号数据不会受影响。
- 这是临时修复，等待官方在后续版本中将这两个变量补入 `docker-compose.yml` 和 `.env.example` 的默认配置。
