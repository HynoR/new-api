# Phase 3 Chat Completions 契约

1. 入口：`router/relay-router.go:96-98` 注册 `POST("/chat/completions", Relay(OpenAI))`；`controller/relay.go:67 Relay` -> `relay/compatible_handler.go:26 TextHelper`。

2. 请求：`dto/openai_request.go:29-109` 只有 `GeneralOpenAIRequest`，无 `ChatCompletionsRequest`。关键字段：`Model`、`Messages`、`Stream *bool`、`StreamOptions *StreamOptions`、`MaxTokens/MaxCompletionTokens *uint`、`Temperature/TopP *float64`、`TopK/N *int`、`Tools`。指针+`omitempty`：缺省省略，显式 `0/false` 保留。`StreamOptions` 在 `:243-248`。

3. 响应：`dto/openai_response.go:40-48` `OpenAITextResponse{...,Error,Usage}`；`Usage` `:223-243` 含 `prompt_tokens/completion_tokens/total_tokens` 和 cache/audio/image details。

4. 预扣：`controller/relay.go:152-164` 当前走 `PreConsumeBilling`，legacy `PreConsumeQuota` 非主链路。`relay/helper/price.go:88-120`：`pre=max(prompt,500)+max_tokens`；倍率 `int(pre*modelRatio*groupRatio)`，价格 `int(modelPrice*500000*groupRatio)`，均截断。`service/billing_session.go:198-207` 先 token 后 wallet/subscription；Redis：`HINCRBY user:{id} Quota`、`HINCRBY token:{hmac} RemainQuota`，TTL>0 才写。

5. 结算：`service/text_quota.go:157-307`：`base=prompt-cache-cache_create-image`；`promptQ=base+cache*cacheRatio+cacheCreate*createRatio+image*imageRatio`；`completionQ=completion*completionRatio`；`quota=Round((promptQ+completionQ)*modelRatio*groupRatio+toolSurcharge+audioInput)`。`Round`=`decimal.Round(0)`，不是 ceil，正数 `.5` 远离 0；`total=0=>0`，否则 `ratio!=0 && quota==0=>1`。`:363-373` 先更新 `user.used_quota/request_count`、`channel.used_quota`，再 `SettleBilling`。fallback `service/quota.go:404-448 PostConsumeQuota` 按 delta 正负调 wallet/subscription 与 token。

6. 倍率：`GetModelRatio` `setting/ratio_setting/model_ratio.go:403-417` miss 返回 `37.5, SelfUseModeEnabled`；`GetCompletionRatio` `:443-459`：`/` 模型 map 优先，否则 hardcoded -> map -> 默认；`GetGroupRatio` `group_ratio.go:84-91` miss=1；特殊用户组倍率见 `relay/helper/price.go:39-65`。

7. 日志：`model/log.go:189-242 RecordConsumeLog` 字段序：`user_id,username,created_at,type=2,content,prompt_tokens,completion_tokens,token_name,model_name,quota,channel_id,token_id,use_time,is_stream,group,ip,request_id,other`。`other` 来自 `service/log_info_generate.go:36-82`，含 `model_ratio/group_ratio/completion_ratio/cache_tokens/cache_ratio/model_price/user_group_ratio/frt/admin_info/request_path/billing_source`；JSON 为 Go `encoding/json.Marshal`。

8. 上游：`relay/channel/openai/adaptor.go:97-172` 默认 URL=`baseURL+RequestURLPath`；Azure 改 `/openai/deployments/{model}/chat/completions?api-version=...`。`:175-226` 复制 `Content-Type/Accept`，设 `Authorization: Bearer key`/组织头，override 最后覆盖。`:229-349` 可能删 `stream_options`、注入 OpenRouter `usage`、将 `o*/gpt-5* max_tokens` 改 `max_completion_tokens` 并清温度等。

9. 错误：非 200：`relay/compatible_handler.go:193-197` -> `service/error.go:86-124 RelayErrorHandler`；可解析 `error` object 则保留。最终 `controller/relay.go:100-103` 输出 `{"error": ToOpenAIError()}`。

10. usage：非流 `relay/channel/openai/relay-openai.go:195-299 OpenaiHandler`。若上游 `usage.prompt_tokens!=0` 用上游；否则本地 `prompt=estimatePromptTokens`，`completion` 用上游 completion 或响应文本估算，并可能重写 body。
