# [Bug] Gemini CLI 反代：google_one 账号触发 429 时被封至 PST 午夜而非使用 tier 的 Cooldown 配置

## 使用场景

本问题发生在通过 sub2api **反代 Gemini CLI**（`oauth_type=google_one`，即以个人 Google One 账号授权的 Gemini CLI OAuth 账号）的场景下，与 Antigravity 反代无关。

Gemini CLI 通过 sub2api 代理时，客户端（如 Claude Code、Cursor 等调用 Gemini CLI 的工具）将请求发送至 sub2api，sub2api 持有 Google OAuth token 并转发至 `cloudcode-pa.googleapis.com`。

## 问题描述

`google_one` 类型的 Gemini CLI 账号收到上游 429 响应后，sub2api 会将账号封禁到 PST 午夜（最长 24 小时），完全忽略该账号 tier 中配置的 `Cooldown` 时间。

封禁期间所有后续请求因调度器找不到可用账号而返回 **503**——客户端看不到来自 Google 的 429，而是收到 sub2api 自身的 503 拦截。手动在管理后台清除限流后可立刻恢复正常，说明 Google 侧的实际限流窗口远短于 24 小时（可能为 RPM 级别的短暂限制）。

## 账号信息

| 字段 | 值 |
|------|-----|
| 账号类型 | Gemini CLI OAuth（非 Antigravity） |
| `oauth_type` | `google_one` |
| `tier_id` | `google_ai_pro` |
| 该 tier 配置的 Cooldown | `5 * time.Minute` |

## 复现步骤

1. 在 sub2api 中添加通过 Gemini CLI OAuth 授权的 `google_one` 账号
2. 通过 sub2api 反代 Gemini CLI，发送请求直至上游返回 429
3. 观察后续请求返回 **503**（非 Google 的 429，而是 sub2api 的系统拦截）
4. 在管理后台手动清除限流 → 立即可以正常使用

步骤 4 证明 Google 侧限流已解除，sub2api 的封禁时长与实际情况严重不符。

## 根因分析

`gemini_messages_compat_service.go` 的 `handleGeminiUpstreamError` 中，`google_one` 账号错误地落入了 `else` 分支：

```go
// 当前逻辑（有问题）
if isCodeAssist {
    // Code Assist (GCP)：正确使用 tier 的 Cooldown 作为 fallback
    cooldown := geminiCooldownForTier(tierID)
    if s.rateLimitService != nil {
        cooldown = s.rateLimitService.GeminiCooldown(ctx, account)
    }
    ra = time.Now().Add(cooldown)
    log.Printf("[Gemini 429] Account %d (Code Assist, ...) rate limited, cooldown=%v", ...)
} else {
    // ← google_one (Gemini CLI) 账号也落入此分支 ← BUG
    // 注释写的是 "API Key / AI Studio OAuth"，但 google_one 同样被路由到这里
    if ts := nextGeminiDailyResetUnix(); ts != nil {
        ra = time.Unix(*ts, 0)
        log.Printf("[Gemini 429] Account %d (API Key/AI Studio, type=%s) rate limited, reset at PST midnight (%v)", ...)
    }
}
```

`google_one` 账号的 `IsGeminiCodeAssist()` 返回 `false`（`oauth_type != "code_assist"`），因此落入 `else` 分支，被封至 PST 午夜。

`google_ai_pro` tier 明确配置了 `Cooldown: 5 * time.Minute`（见 `gemini_quota.go`），但对 `google_one` 账号，这个配置在 429 处理路径中从未被读取。

日志佐证：429 触发后日志打印的标签是 `"API Key/AI Studio, type=oauth"`，但实际账号均为 `google_one` 类型的 Gemini CLI 账号：

```
[Gemini 429] Account 4 (API Key/AI Studio, type=oauth) rate limited, reset at PST midnight (2026-02-25 16:00:00 +0800 CST)
[Gemini 429] Account 2 (API Key/AI Studio, type=oauth) rate limited, reset at PST midnight (2026-02-26 16:00:00 +0800 CST)
```

## 期望行为

`google_one`（Gemini CLI）账号在 429 fallback 时，应与 Code Assist 账号相同，优先读取 `geminiQuotaService.CooldownForAccount()` 返回的 tier cooldown，而不是硬编码跳转至 PST 午夜。

## 建议修复

在 `handleGeminiUpstreamError` 中为 `google_one` 单独增加一个分支：

```go
if isCodeAssist {
    cooldown := geminiCooldownForTier(tierID)
    if s.rateLimitService != nil {
        cooldown = s.rateLimitService.GeminiCooldown(ctx, account)
    }
    ra = time.Now().Add(cooldown)
    log.Printf("[Gemini 429] Account %d (Code Assist, tier=%s, project=%s) rate limited, cooldown=%v",
        account.ID, tierID, projectID, time.Until(ra).Truncate(time.Second))
} else if oauthType == "google_one" {
    // Gemini CLI (Google One)：同样使用 tier cooldown，不强制封到 PST 午夜
    cooldown := 5 * time.Minute
    if s.rateLimitService != nil {
        cooldown = s.rateLimitService.GeminiCooldown(ctx, account)
    }
    ra = time.Now().Add(cooldown)
    log.Printf("[Gemini 429] Account %d (Gemini CLI / Google One, tier=%s) rate limited, cooldown=%v",
        account.ID, tierID, time.Until(ra).Truncate(time.Second))
} else {
    // AI Studio / API Key：保持原有 PST 午夜逻辑（日配额耗尽场景）
    if ts := nextGeminiDailyResetUnix(); ts != nil {
        ra = time.Unix(*ts, 0)
        log.Printf("[Gemini 429] Account %d (API Key/AI Studio, type=%s) rate limited, reset at PST midnight (%v)",
            account.ID, account.Type, ra)
    } else {
        ra = time.Now().Add(5 * time.Minute)
        log.Printf("[Gemini 429] Account %d rate limited, fallback to 5min", account.ID)
    }
}
```

## 环境

- sub2api 版本：latest（当前 v0.1.85+）
- 反代类型：**Gemini CLI**（非 Antigravity）
- 账号类型：`google_one` / `google_ai_pro`
- 部署方式：Docker Compose
