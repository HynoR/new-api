# LeapCore 无人机器客户端对接与开通流程

本文面向运维、客服和客户端开发。目标是让一台全新的无人机器在上电后，可以自动向 new-api 请求并拿到可用的 `sk-` key。

## 角色分工

客服负责收集机器码，并确认客户机器是否需要开通。客服不需要接触 `LeapCoreHelperKey`。

运维负责在 new-api 面板中预置机器用户、配置 LeapCore helper key、排查注册失败。运维需要把 helper key 当作服务端与客户端之间的共享密钥管理。

客户端开发负责把客户端的注册地址、请求路径和 helper key 配到机器程序里，并确保机器上电时调用 `FetchChannelKey(ctx)`，拿到 key 后写入客户端自己的配置或运行时存储。

## 服务端一次性配置

1. 管理员登录 new-api 面板。
2. 进入“系统设置 / 认证 / LeapCore”。
3. 设置 `LeapCoreHelperKey`。

`LeapCoreHelperKey` 是客户端请求 `POST /api/leapcore/register` 时放在 `X-Helper-Key` 请求头里的共享密钥。未配置时，注册接口返回 HTTP 503；客户端传错或没传时，注册接口返回 HTTP 401。

## LeapCoreHelperKey 前端配置位置

运维在 new-api 前端这样配置：

```text
系统设置 -> 认证 -> LeapCore -> Helper Key
```

填入的值会保存到系统设置项 `LeapCoreHelperKey`。这个设置项属于敏感 key，前端只负责写入，不需要在页面上明文回显旧值；需要更换时直接输入新值并保存。

相关前端代码位置：

| 文件 | 作用 |
| --- | --- |
| `web/default/src/features/system-settings/auth/leapcore-section.tsx` | `LeapCoreHelperKey` 输入框和保存逻辑 |
| `web/default/src/features/system-settings/auth/section-registry.tsx` | 把 LeapCore 配置区挂到“系统设置 / 认证”分组 |
| `web/default/src/features/system-settings/auth/index.tsx` | 系统设置默认值里声明 `LeapCoreHelperKey` |
| `web/default/src/features/system-settings/types.ts` | 系统设置类型里声明 `LeapCoreHelperKey` |

## 新机器开通流程

1. 客户端或客服工具读取机器指纹 `fp`。
2. 按客户端实际规则生成机器码：
   ```text
   machine_id = FGLC_ + base64(fp)
   ```
3. 客服把原始 fingerprint 或完整 `machine_id` 记录到工单。也可以记录 `base64(machine_id)`，方便复制传输。
4. 运维登录 new-api 面板，进入“用户”页面，点击“注册机器”。
5. 运维粘贴原始 fingerprint、完整 `machine_id` 或 `base64(machine_id)`。
6. 面板自动创建或复用对应机器用户，并返回：
   - `username`
   - 初始 `password`
   - `remark`
7. 运维确认 `remark` 等于完整 `machine_id`。这个字段是机器绑定依据，不能改成备注文本。
8. 机器上电后调用注册接口。new-api 会为该机器用户创建或复用名为 `leapcore-machine` 的 token，并返回 `data.key`。

如果机器先上电、后预置用户，第一次请求会返回 HTTP 403。运维预置机器用户后，让客户重启机器或触发客户端重试即可。

## 面板预置规则

面板“注册机器”支持三种输入。

原始 fingerprint：

```text
4c4c45444d0032108058c4c04f4d5332/CN1296378H00AB
```

完整 `machine_id`：

```text
FGLC_<base64 fingerprint>
```

`base64(machine_id)`：

```text
base64(FGLC_<base64 fingerprint>)
```

输入原始 fingerprint 时，后台会按客户端规则自动转换成完整 `machine_id`：

```text
FGLC_ + base64(fingerprint)
```

面板会按固定规则生成用户：

```text
username = FGLC_ + sha256(machine_id) 的 hex 前 15 位
password = sha256(machine_id) 的 hex 前 15 位
remark   = machine_id
role     = 普通用户
status   = 启用
```

客户端取 key 时，new-api 会再次按同样规则计算用户名，并检查用户 `remark` 是否等于完整 `machine_id`。因此人工修改用户名或 remark 都会导致取 key 失败。

## 客户端需要改的代码位置

客户端现有函数主体不需要改。需要确认或调整的是函数周边的几个变量和请求配置：

```go
func FetchChannelKey(ctx context.Context) (string, error) {
    fp, err := util.GetFingerprint()
    if err != nil {
       return "", fmt.Errorf("%w: %w", ErrGetFingerprint, err)
    }

    client := request.NewClient(
       defaultRegisterHost,
       request.WithHeader("X-Helper-Key", helperKey),
    )

    payload := map[string]string{
       "machine_id": "FGLC_" + base64.StdEncoding.EncodeToString([]byte(fp)),
    }

    resp, err := client.PostJSON(ctx, "/api/leapcore/register", payload)
    // ...
}
```

客户端开发需要在客户端代码里查找并配置：

| 配置 | 要求 |
| --- | --- |
| `defaultRegisterHost` | 指向 new-api 外网可访问地址，例如 `https://api.example.com`，不要带 `/api/leapcore/register` 路径 |
| `helperKey` | 与 new-api 面板“系统设置 / 认证 / LeapCore”里的 `LeapCoreHelperKey` 完全一致 |
| `client.PostJSON` path | 保持 `/api/leapcore/register` |
| `machine_id` 生成 | 保持 `FGLC_ + base64(fp)` |
| 响应解析 | 读取 `data.key`，key 为空时按失败处理 |

如果客户端项目把这些值放在构建参数、配置文件或环境注入里，只需要改对应配置源。如果路径不是变量而是硬编码，必须确认硬编码路径就是 `/api/leapcore/register`。

## 客户端请求协议

请求：

```http
POST /api/leapcore/register
X-Helper-Key: <LeapCoreHelperKey>
Content-Type: application/json

{
  "machine_id": "FGLC_<base64 fingerprint>"
}
```

成功响应：

```json
{
  "success": true,
  "message": "",
  "data": {
    "key": "sk-..."
  }
}
```

成功必须是 HTTP 200。客户端只应该依赖 `data.key`。

## token 行为

注册接口只处理机器用户下名为 `leapcore-machine` 的 token：

- 已有可用 `leapcore-machine` token：直接返回现有 key。
- 没有同名 token：自动创建启用、永不过期、无限额度 token。
- 已有同名 token 但被禁用、过期或额度不可用：返回 HTTP 403，不自动绕过人工禁用。

这意味着运维可以通过禁用 `leapcore-machine` token 来阻止某台机器继续自动取 key。

## 常见状态码

| 状态码 | 含义 | 处理方式 |
| --- | --- | --- |
| 200 | 成功，`data.key` 可用 | 客户端保存并使用 key |
| 400 | 请求体或 `machine_id` 格式错误 | 客户端检查机器码生成逻辑 |
| 401 | `X-Helper-Key` 缺失或错误 | 客户端和运维核对 helper key |
| 403 | 机器未预置、用户禁用、remark 不匹配、角色不允许或 token 不可用 | 运维检查用户和 token 状态 |
| 503 | 服务端未配置 `LeapCoreHelperKey` | 运维到系统设置中配置 helper key |
| 500 | 服务端数据库或 token 创建异常 | 运维查看 new-api 日志 |

## 运维与客服协作建议

客服工单至少记录：

- 客户名称或设备归属。
- 原始 fingerprint、完整 `machine_id`，或 `base64(machine_id)`。
- 机器上电时间和客户端版本。
- 如果取 key 失败，记录 HTTP 状态码和响应 `message`。

运维处理工单时：

1. 确认服务端已配置 `LeapCoreHelperKey`。
2. 用面板“注册机器”预置机器用户。
3. 确认返回的 `remark` 是完整 `machine_id`。
4. 如果机器仍失败，根据状态码检查 helper key、用户状态、角色、remark 和 `leapcore-machine` token。
5. 不要把机器用户初始密码或 helper key 发给客户。客户端只需要自动拿到返回的 `sk-` key。

## curl 联调示例

```bash
machine_id="FGLC_$(printf '%s' 'fingerprint-value' | base64)"
helper_key="value-from-system-settings"

curl -sS \
  -X POST 'https://your-new-api.example.com/api/leapcore/register' \
  -H "X-Helper-Key: ${helper_key}" \
  -H 'Content-Type: application/json' \
  -d "{\"machine_id\":\"${machine_id}\"}"
```

如果需要测试面板支持的 `base64(machine_id)` 输入，可以这样生成：

```bash
printf '%s' "${machine_id}" | base64
```


TEST:
NEW_API_HOST="https://your-new-api.example.com"
HELPER_KEY="你的 LeapCoreHelperKey"
FINGERPRINT="test-fingerprint-001"

MACHINE_ID="FGLC_$(printf '%s' "$FINGERPRINT" | base64)"

curl -i -sS \
  -X POST "${NEW_API_HOST}/api/leapcore/register" \
  -H "X-Helper-Key: ${HELPER_KEY}" \
  -H "Content-Type: application/json" \
  -d "{\"machine_id\":\"${MACHINE_ID}\"}"
