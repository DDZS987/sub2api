# [Feature/Bug] Gemini CLI 反代：剩余配额进度条仅统计请求次数，未纳入 token 用量，导致显示严重失真

## 使用场景

本问题发生在通过 sub2api **反代 Gemini CLI**（`oauth_type=google_one`，即以个人 Google One 账号授权的 Gemini CLI OAuth 账号）的场景下，与 Antigravity 反代无关。

Gemini CLI 反代的典型用法：将 Claude Code、Cursor 等工具的请求路由至 sub2api，sub2api 持有 Google OAuth token 并转发至 Gemini 上游，从而复用 Google One 订阅中包含的 Gemini 用量。

## 问题描述

管理后台账号列表中的 Gemini CLI 账号**剩余配额估算**（utilization 百分比 + 重置倒计时）仅以**请求次数**与 RPD 限额做比值，完全不考虑 token 消耗。

Gemini CLI 在反代长上下文编程任务时（如 Claude Code 调用 `gemini-3-pro-preview`），单次请求通常携带大量上下文，token 消耗远高于普通对话。实际配额可能已大量消耗，但进度条始终显示极低百分比（如 **1%** 或 **0%**），与真实使用情况严重背离，完全失去预警意义。

## 现象

以下数据来自实际 Gemini CLI 反代使用记录：

| 指标 | 显示值 | 实际情况 |
|------|--------|---------|
| 请求次数 | 25 次 | 25 次（统计准确） |
| Token 用量 | 850.8K | 850.8K（统计准确，但不参与进度计算） |
| 剩余配额进度条 | **1% · 18h 24m** | 实际配额已大量消耗，25 次后触发 429 |

进度条显示 1% 的计算过程：`25 次 / SharedRPD(1500) × 100 = 1.67%`，token 用量未被纳入。

## 核心问题拆解

该问题实际上由三个相互叠加的缺陷共同导致，单独修任何一个都无法完全解决：

1. **不同模型的配额上限各不相同**，但 sub2api 对 `google_ai_pro` tier 下所有模型统一使用 `SharedRPD=1500`，无法区分 `gemini-2.0-flash`（可能确实支持 1500 次/天）和 `gemini-3-pro-preview`（实测远低于此值）
2. **不同模型单次请求的 token 消耗差异极大**，Flash 系列远低于 Pro/Preview 系列，按次数统一计算会严重低估 Pro 类模型的配额消耗
3. **缺少 TPD（tokens-per-day）字段**，即便配额本质上是 token-based 的，当前结构也无法表达

> **注：** 由于 sub2api 是对 Gemini CLI 内部 API 的反代，Google 不会公开各模型对应的真实配额数据。相关数值需通过对其他开源反代项目的调研和实测积累来推断，存在一定的不确定性。

## 根因分析

### 1. `GeminiQuota` 结构体缺少模型级别的 TPD 字段

`gemini_quota.go` 中 `GeminiQuota` 只有请求维度的限额，无法表达 token 维度的日配额：

```go
type GeminiQuota struct {
    SharedRPD int64  // 每日共享请求数（对所有模型统一为 1500，无法区分模型差异）
    SharedRPM int64  // 每分钟共享请求数
    ProRPD    int64  // Pro 模型每日请求数
    ProRPM    int64  // Pro 模型每分钟请求数
    FlashRPD  int64  // Flash 模型每日请求数
    FlashRPM  int64  // Flash 模型每分钟请求数
    // 缺少 ProTPD / FlashTPD / SharedTPD（tokens-per-day）字段
    // 缺少对 Preview 模型（如 gemini-3-pro-preview）的独立配额支持
}
```

当前 `google_ai_pro` 默认值 `SharedRPD=1500` 可能对 `gemini-2.0-flash` 是合理的，但对 `gemini-3-pro-preview` 等 Preview 模型，实测远低于此值即触发 429，说明不同模型的真实配额上限差异显著，不能共用同一个 RPD 值。

### 2. `buildGeminiUsageProgress` 不使用 token 计算 utilization

`account_usage_service.go` 中 `buildGeminiUsageProgress` 签名已接收 `tokens` 参数，但仅用于填充 `window_stats`（统计展示），**不参与 `utilization` 百分比计算**：

```go
func buildGeminiUsageProgress(used, limit int64, resetAt time.Time, tokens int64, cost float64, now time.Time) *UsageProgress

// utilization 只看请求次数，与 tokens 无关
utilization := float64(used) / float64(limit) * 100
```

### 3. `PreCheckUsage` 同样只检查请求次数

`ratelimit_service.go` 的本地预检仅比较 `requests >= RPD`，不对 token 累计值做任何拦截，无法在耗尽 token 配额前提前阻止请求发出。

## 期望行为

### 短期：允许用户按模型手动配置 RPD 和 TPD

由于 Google 不公开 Gemini CLI 内部 API 的真实配额数据，短期内最可行的方案是在 `GEMINI_QUOTA_POLICY` 中支持**按模型分组**配置 RPD 和 TPD，由用户根据实测结果自行填写：

```json
{
  "quota_rules": {
    "google_ai_pro": {
      "shared_rpd": 1500,
      "shared_tpd": 1000000,
      "gemini_pro_preview": {
        "rpd": 25,
        "tpd": 500000
      }
    }
  }
}
```

### 中长期：在 `GeminiQuota` 中增加 TPD 字段并纳入计算

1. **`GeminiQuota` 结构体**：增加 `ProTPD`、`FlashTPD`、`SharedTPD` 字段
2. **`buildGeminiUsageProgress`**：当 TPD 配置有效时，以 `max(requests/RPD, tokens/TPD)` 作为 utilization，取较高者展示
3. **`PreCheckUsage`**：在每日预检中同时检查 token 累计值，超出 TPD 时跳过该账号，避免触发上游 429

### 数据来源建议

真实的各模型配额数值，建议通过以下途径积累：
- 参考 `gemini-cli`、`aistudio-proxy` 等开源项目的社区讨论和 issue
- 用户实测反馈（如本 issue 中 `gemini-3-pro-preview` 约 25 次/天触发 429）

## 背景 / 参考数据

Gemini CLI 反代典型 token 消耗（实测）：

- 单次请求平均 token 数：~30,000（含长上下文）
- 25 次请求累计 token：~850,000
- 25 次后触发 429，而进度条显示仍为 1%

若 Google 侧存在 token 维度的日配额（TPD），则当前进度条的 1% 显示与实际配额消耗情况可能相差数十倍，完全无法提前预警。

## 影响

- Gemini CLI 反代用户误判配额充裕，直到 Google 返回 429 才意识到限额已耗尽
- 无法通过进度条提前规划使用量或触发账号切换
- 与 Antigravity 账号的配额展示逻辑不一致（Antigravity 通过专用 API 获取实时余额）

## 环境

- sub2api 版本：latest（当前 v0.1.85+）
- 反代类型：**Gemini CLI**（非 Antigravity）
- 账号类型：`google_one` / `google_ai_pro`
- 使用场景：Claude Code 通过 sub2api 调用 `gemini-3-pro-preview` 长上下文编程
- 部署方式：Docker Compose
