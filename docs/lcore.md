# LeapCore 无人机器取 Key 对接

LeapCore 客户端在机器通电时会调用 new-api 获取可用的 API key。客户端请求形态固定，new-api 端只负责校验预置机器用户，并在该用户还没有专用 token 时创建 `leapcore-machine` token。

## 客户端请求

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

成功必须返回 HTTP 200。客户端只读取 `data.key`，所以接口响应结构不能变。

## 服务端配置

在管理后台进入“系统设置 / 认证 / LeapCore”，配置 `LeapCoreHelperKey`。

如果未配置该系统设置，接口返回 HTTP 503。请求头 `X-Helper-Key` 缺失或不匹配时返回 HTTP 401。

## 机器用户预置规则

new-api 不会在客户端注册请求中自动创建机器用户。管理员可以在管理后台的用户页面点击“注册机器”，或由外部 provisioning 流程提前创建用户：

1. 计算客户端会提交的 `machine_id`：
   ```text
   FGLC_ + base64(raw fingerprint)
   ```
2. 计算用户名：
   ```text
   FGLC_ + first_15_hex_chars(sha256(machine_id))
   ```
3. 创建普通用户，字段要求：
   - `username`：上一步计算出的用户名。
   - `display_name`：建议与 `username` 相同。
   - `role`：普通用户。
   - `status`：启用。
   - `remark`：完整 `machine_id`。

缺少对应用户、用户禁用、角色不是普通用户、或 `remark` 与 `machine_id` 不一致时，接口返回 HTTP 403。

管理后台支持输入原始 fingerprint、完整 `machine_id` 或 `base64(machine_id)`。输入原始 fingerprint 时，后台会按客户端规则自动转换成 `FGLC_ + base64(fingerprint)`。创建出的初始密码为 `sha256(machine_id)` 的 hex 字符串前 15 位。

## Token 行为

接口只管理预置机器用户下名为 `leapcore-machine` 的 token：

- 已有可用 token：直接返回该 token 的 key。
- 没有同名 token：创建一个启用、永不过期、无限额度的 token。
- 已有同名 token 但禁用、过期或额度不可用：返回 HTTP 403，不创建新 token。

如果系统启用了 `DefaultUseAutoGroup`，新建 token 的 `group` 会设置为 `auto`。

## curl 示例

```bash
machine_id="FGLC_$(printf '%s' 'fingerprint-value' | base64)"
helper_key="value-from-system-settings"

curl -sS \
  -X POST 'https://your-new-api.example.com/api/leapcore/register' \
  -H "X-Helper-Key: ${helper_key}" \
  -H 'Content-Type: application/json' \
  -d "{\"machine_id\":\"${machine_id}\"}"
```
