# Master 端 /v1/chat/completions 非流式链路约束契约

> 用于 Rust Slave 端字节级一致镜像

---

## 1. 路由入口

- `router/relay-router.go:96`
  ```go
  httpRouter.POST("/chat/completions", controller.Relay)
  ```
- `controller/relay.go:67`
  ```go
  func Relay(c *gin.Context, relayFormat types.RelayFormat)
  ```
- 非流式最终进入 `relay.TextHelper(c, info)`

---

## 2. 请求 DTO

**文件**: `dto/openai_request.go:29` — `GeneralOpenAIRequest`

关键字段：
```go
Model               string            `json:"model,omitempty"`
Messages            []Message         `json:"messages,omitempty"`
Stream              *bool             `json:"stream,omitempty"`
StreamOptions       *StreamOptions    `json:"stream_options,omitempty"`
MaxTokens           *uint             `json:"max_tokens,omitempty"`
MaxCompletionTokens *uint             `json:"max_completion_tokens,omitempty"`
Temperature         *float64          `json:"temperature,omitempty"`
TopP                *float64          `json:"top_p,omitempty"`
TopK                *int              `json:"top_k,omitempty"`
N                   *int              `json:"n,omitempty"`
Tools               []ToolCallRequest `json:"tools,omitempty"`
ToolChoice          any               `json:"tool_choice,omitempty"`
```

**约束契约（Rule 6）**：
- 可选标量字段必须使用**指针类型** + `omitempty`
- `nil` → 序列化时**省略**
- 显式 `0` / `false` → 非 nil 指针 → **必须保留并透传给上游**

---

## 3. 响应 DTO

**文件**: `dto/openai_response.go:40/223`

```go
type OpenAITextResponse struct {
    Id      string                     `json:"id"`
    Model   string                     `json:"model"`
    Object  string                     `json:"object"`
    Created any                        `json:"created"`
    Choices []OpenAITextResponseChoice `json:"choices"`
    Error   any                        `json:"error,omitempty"`
    Usage   `json:"usage"`
}

type Usage struct {
    PromptTokens         int `json:"prompt_tokens"`
    CompletionTokens     int `json:"completion_tokens"`
    TotalTokens          int `json:"total_tokens"`
    PromptTokensDetails    InputTokenDetails  `json:"prompt_tokens_details"`
    CompletionTokenDetails OutputTokenDetails `json:"completion_tokens_details"`
}
```

---

## 4. 预扣费

**文件**: `service/pre_consume_quota.go:33` — `PreConsumeQuota`

逻辑：
1. `model.GetUserQuota(userId)` 查余额
2. 若 `userQuota > trustQuota`（默认 `500000`）且 token 额度充足 → `preConsumedQuota = 0`（**信任免预扣**）
3. 否则执行：
   - `model.DecreaseUserQuota(userId, quota)` — 扣用户余额
   - `PreConsumeTokenQuota(relayInfo, quota)` → `model.DecreaseTokenQuota(tokenId, key, quota)` — 扣 token 剩余额度

**预扣额度公式**（`relay/helper/price.go:89-121`）：

- 按量计费：
  ```
  preConsumedTokens = max(promptTokens, PreConsumedQuota) + MaxTokens
  preConsumedQuota  = int(float64(preConsumedTokens) * modelRatio * groupRatio)
  ```
- 按次计费：
  ```
  preConsumedQuota = int(modelPrice * QuotaPerUnit * groupRatio)
  ```

---

## 5. 结算（核心）

**文件**: `service/text_quota.go:320` — `PostTextConsumeQuota` → `calculateTextQuotaSummary`

### 精确 Quota 公式

使用 `shopspring/decimal` 进行运算，最终 `Round(0).IntPart()`：

```
ratio = modelRatio * groupRatio

prompt = promptTokens - cacheTokens - imageTokens - audioTokens

promptQuota = prompt
            + cacheTokens * cacheRatio
            + imageTokens * imageRatio
            + cacheCreationTokens * cacheCreationRatio

completionQuota = completionTokens * completionRatio

quota = (promptQuota + completionQuota) * ratio
      + toolSurcharge
      + audioInputQuota
```

若存在 `OtherRatios`，依次连乘。

### Rounding 规则

- **HALF_AWAY_FROM_ZERO**（四舍五入）到整数
- Rust 端请使用 `rust_decimal` 的 `round_dp_with_strategy(0, MidpointAwayFromZero)` 或等效实现

### 兜底契约

| 条件 | 结果 |
|------|------|
| `ratio != 0 && quota <= 0` | `quota = 1` |
| `totalTokens == 0` | `quota = 0` |

### 扣费 SQL 顺序

1. **用户余额** — `model/user.go:928`
   ```sql
   UPDATE users SET quota = quota - ? WHERE id = ?
   ```
2. **已用额度** — `model/user.go:972`
   ```sql
   UPDATE users SET used_quota = used_quota + ?, request_count = request_count + ? WHERE id = ?
   ```
3. **渠道额度** — `model/channel.go:765`
   ```sql
   UPDATE channels SET used_quota = used_quota + ? WHERE id = ?
   ```
4. **Token 额度** — `model/token.go:425`
   ```sql
   UPDATE tokens SET remain_quota = remain_quota - ?, used_quota = used_quota + ? WHERE id = ?
   ```

### 结算方式

- 若 `relayInfo.Billing != nil`（BillingSession）：`delta = actualQuota - preConsumedQuota`，调用 `session.Settle(delta)`
- 否则旧路径：`PostConsumeQuota(relayInfo, quotaDelta, preConsumedQuota, true)`

---

## 6. 倍率查询

| 函数 | 文件 | 说明 |
|------|------|------|
| `GetModelRatio(name)` | `setting/ratio_setting/model_ratio.go:403` | 默认 `37.5`；未配置且非自用模式报错 |
| `GetCompletionRatio(name)` | `setting/ratio_setting/model_ratio.go:443` | 硬编码 fallback，如 gpt-4o 默认 `4` |
| `GetGroupRatio(name)` | `setting/ratio_setting/group_ratio.go:84` | 分组倍率 |

---

## 7. 写日志

**文件**: `model/log.go:204` — `RecordConsumeLog`

写入 `logs` 表的字段顺序（GORM `Create`）：

```
user_id, created_at, type=2, content,
prompt_tokens, completion_tokens, token_name, model_name,
quota, channel_id, token_id, use_time,
is_stream, group, ip, request_id, other
```

`other` 字段为 **JSON 字符串**，典型键：
```json
{
  "model_ratio": 1.0,
  "group_ratio": 1.0,
  "completion_ratio": 4.0,
  "cache_tokens": 0,
  "cache_ratio": 1.0,
  "usage_semantic": "openai"
}
```

---

## 8. 上游调用

**文件**: `relay/channel/openai/adaptor.go`

- `GetRequestURL`：默认 `base_url + info.RequestURLPath`（即 `/v1/chat/completions`）；Azure 特殊拼接 `/openai/deployments/{model}/chat/completions`
- `SetupRequestHeader`：标准 `Authorization: Bearer {api_key}`；Azure 用 `api-key` 头
- `ConvertOpenAIRequest`：
  - 非 OpenAI/Azure 渠道强制 `request.StreamOptions = nil`
  - o/gpt-5 系列会改写 `MaxCompletionTokens`、`Temperature` 等字段

---

## 9. 错误响应

**文件**: `controller/relay.go:88-106` + `types/error.go:180`

上游错误统一在 `defer` 中返回：
```go
c.JSON(status, gin.H{"error": newAPIError.ToOpenAIError()})
```

`OpenAIError` 结构：
```go
{
  "message": string,
  "type":    string,
  "param":   string,
  "code":    any
}
```

---

## 10. Token 用量

**文件**: `relay/channel/openai/relay-openai.go:195` — `OpenaiHandler`

- **优先信任上游**：`response.usage.prompt_tokens` / `completion_tokens`
- **缺失时本地补齐**：
  - `PromptTokens = info.GetEstimatePromptTokens()`（本地估算）
  - `CompletionTokens = CountTextToken(choices文本, model)`（本地计算）

---

## Rust 端镜像约束清单

1. **指针零值语义**：`stream` / `max_tokens` / `temperature` 等可选字段必须区分“缺失”与“显式零值”，缺失时 omit，零值时保留透传。
2. **Decimal Rounding**：所有 quota 计算使用 HALF_AWAY_FROM_ZERO（四舍五入）到整数，严禁直接用 float `ceil` / `floor`。
3. **兜底规则**：非零 ratio 下 quota 最小为 `1`；`totalTokens == 0` 时 quota 必须为 `0`。
4. **SQL 顺序**：先扣 `users.quota`（余额），再增 `users.used_quota` / `request_count` 和 `channels.used_quota`；token 同步更新 `remain_quota` + `used_quota`。
5. **日志字段**：`other` 必须序列化为 JSON 字符串，键名与 Go 端完全一致。
