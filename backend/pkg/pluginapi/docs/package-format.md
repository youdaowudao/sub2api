# `.s2plugin` 包格式

`.s2plugin` 是 ZIP 文件，根目录必须包含 `manifest.json`，生产包还必须包含 `signature.json`。

## 标准布局

以下为 schema 1 Transport 的示意布局；运行时实际路径由 `runtimes.<goos>-<goarch>.path` 指定，不要求使用固定目录名：

```text
manifest.json
signature.json
runtimes/<goos>-<goarch>/<binary>
ui/index.html
ui/assets/...
```

所有运行时和 UI 文件必须出现在 `manifest.files`，值为小写十六进制 SHA-256。清单和签名文件自身不写入 `files`。

schema 2 按类型省略不存在的部分：纯前端只携静态 UI，不携运行时或后台声明（runtimes 可省略或为空对象）；纯后端只携运行时，无 UI；组合两者都有。`ui.entrypoint` 是可选管理员配置页，`ui.entries` 是业务页和辅助面板，不需要为纯后台伪造配置页面。[示例打包器](../examples/README.md)使用 `bin/plugin` 作为运行时路径，纯前端不会构建二进制。

包不允许绝对路径、父目录跳转、重复路径、符号链接、未声明文件或缺失文件。宿主还限制上传大小、解压后大小和文件数量。

## 清单

schema 1 见 [`manifest.schema.json`](../v1/manifest.schema.json)，schema 2 见 [`manifest-v2.schema.json`](../v1/manifest-v2.schema.json)和[通用契约](generic-plugins.md)。版本字段含义：

- `version`：插件自身语义化版本。
- `requires.sub2api`：宿主硬兼容范围。
- `requires.recommended_sub2api_version`：可选的建议宿主版本。
- `requires.tested_sub2api_versions`：可选的真实验证过的版本列表。
- `requires.plugin_protocol`：进程握手协议。
- `requires.transport_api`：旧 Transport 请求和响应帧协议。
- `requires.ui_bridge`：管理配置或业务 UI 消息协议。
- schema 2 的 `requires.backend_api` / `requires.host_service_api`：Backend 及其必需反向宿主服务的协议。

| 类型 | plugin_protocol | transport_api | backend_api | host_service_api | ui_bridge |
| --- | --- | --- | --- | --- | --- |
| schema 1 Transport | 1 | 1 | 不声明 | 不声明（旧 Host API 2 可选协商） | 1 |
| schema 2 纯前端 | 0 | 0 | 0 | 0 | 2 |
| schema 2 纯后端 | 1 | 0 | 1 | 3 | 0 |
| schema 2 组合 | 1 | 0 | 1 | 3 | 2 |

schema 2 的六个 requires 字段（含 sub2api）均需声明；不适用的协议明确写 0。通用能力项只用 id，不使用旧 platform/account_type，不允许混入 OpenAI Transport 能力。未知字段、重复/未知能力、版本和类型组合不匹配会被拒绝。

有 Backend 时至少声明 backend.invoke.v1 或 backend.background.v1；调用能力要求声明 backend.methods 的 name/role/access。UI 业务入口声明 id/label/path/mount/roles，路径必须位于已校验的 ui/ 文件内。存储与用户观察的依赖和角色规则见[通用契约](generic-plugins.md)，不能只靠添加任意能力名授予未实现的宿主接口。

## 签名

`signature.json`：

```json
{
  "algorithm": "ed25519",
  "key_id": "publisher-key-id",
  "signature": "BASE64_SIGNATURE"
}
```

签名对象是 `manifest.json` 的精确原始字节。发布者私钥不得进入插件包、源码仓库或 Sub2API 运行环境。部署者只配置 Base64 Ed25519 公钥。

默认生产配置拒绝未签名包。官方 OpenAI Transport 使用宿主内置公钥验签，不需要配置；其他发布者仍需配置 `trusted_publishers`。`allow_unsigned` 只用于开发者自己构建的本地包。
