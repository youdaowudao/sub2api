# Sub2API 插件开发教程

本文面向希望为 Sub2API 开发、打包和发布插件的团队。插件以 `.s2plugin` 包交付：schema 1 保留原 OpenAI OAuth Transport；schema 2 支持纯前端、纯后端和组合插件，按实际需要携带静态 UI、独立进程或两者。

以下保留 schema 1 Transport 的开发顺序。开发通用插件时，先阅读[通用插件契约](../backend/pkg/pluginapi/docs/generic-plugins.md)和[四类可构建示例](../backend/pkg/pluginapi/examples/README.md)。示例源码和打包器已经随仓库提供，属于开发与回归材料，不代表已发布可用于生产的官方插件包。

## 1. 准备开发环境

建议使用以下环境：

- 构建本仓库 Go SDK 和示例时，使用 [`backend/go.mod`](../backend/go.mod) 声明的 Go 版本，当前为 1.27.2；
- Node.js（运行示例 JavaScript 语法检查时使用）；
- Git；
- 与目标部署环境一致的构建工具链。

协议定义和通用说明位于：

- `backend/pkg/pluginapi/v1/plugin.proto`：进程间消息和流式请求定义；
- `backend/pkg/pluginapi/v1/runtime.go`：插件进程启动入口；
- [`manifest.schema.json`](../backend/pkg/pluginapi/v1/manifest.schema.json)：schema 1 Transport 清单；
- [`manifest-v2.schema.json`](../backend/pkg/pluginapi/v1/manifest-v2.schema.json)：schema 2 通用插件清单；
- `backend/pkg/pluginapi/docs/`：开发、UI Bridge、包格式和安全边界说明。

现有示例位于 [`backend/pkg/pluginapi/examples/`](../backend/pkg/pluginapi/examples/README.md)：`frontend` 不携带进程，`backend` 不携带 UI，`combined` 通过 Bridge 调用自己的 Backend，`legacy` 保留 schema 1 HTTP 转发。公开契约以该 SDK 目录的协议、清单和文档为准。

## 2. 创建插件工程

自建 schema 1 Transport 工程时，可沿用以下结构；纯前端不需要 Go 入口或 Transport，通用后台使用 `ServeBackend`，可从现有示例开始：

```text
my-plugin/
├── cmd/<plugin>/main.go
├── internal/pluginconfig/
├── internal/transport/
├── ui/index.html
├── ui/assets/
├── tools/
├── manifest.source.json
└── build.sh
```

上面的 Transport 工程至少准备以下部分：

1. `manifest.source.json`：插件 ID、名称、版本、作者、能力和兼容的 Sub2API 版本；
2. `cmd/<plugin>/main.go`：启动入口和运行时版本注入，并同步打包器中的构建目标和二进制名称；
3. `internal/pluginconfig/`：配置结构、默认值、严格校验和规范化；
4. `internal/transport/`：HTTP 客户端、代理、请求头、请求体、网络连接参数、响应流和资源回收；
5. `ui/index.html` 与 `ui/assets/`：插件自己的配置界面；
6. 单元测试、进程集成测试和目标平台构建配置。

Transport 入口保持很小，只调用 `pluginv1.Serve`。通用 Backend 入口调用 `pluginv1.ServeBackend`，无需实现 `Forward`。纯前端只有清单与静态资源，不创建无用进程。实际业务放在可独立测试的包中。

## 3. 编写运行时

schema 1 运行时实现 `TransportPlugin`（服务名 `oauth_transport`），必须满足以下约定：

| 方法 | 要求 |
| --- | --- |
| `GetInfo` | 返回的插件 ID、版本、协议版本、传输 API 版本和能力必须与清单一致。 |
| `Health` | 快速返回进程是否可以接收新请求，不执行长时间网络探测。 |
| `ValidateConfig` | 严格解析 JSON，拒绝未知字段和非法范围，并返回完整的规范化配置。 |
| `ApplyConfig` | 成功后原子切换配置；失败时保留旧配置和旧连接。 |
| `TestConfig` | 针对已保存配置进行快速诊断，返回简短、可展示的结果。 |
| `Forward` | 按协议接收请求流，发出上游请求，再按顺序返回响应流。 |

请求帧顺序为 `start`、零到多个 `body_chunk`、`body_end`；响应帧顺序为 `start`、零到多个 `body_chunk`、`end`。不能继续处理时发送 `error` 帧。

`ForwardResponseError.request_sent` 必须准确：只有在能够确认尚未调用上游 HTTP Transport 时才返回 `false`；一旦已经调用，或无法确认上游是否收到请求，就返回 `true`。宿主会据此决定是否允许切换账号重试，避免重复执行同一个请求。

资源管理也属于运行时契约：复用 HTTP Transport 和连接池，配置切换时关闭旧空闲连接，沿用 gRPC stream 的 context 取消 DNS、连接、上传和响应读取，并始终关闭上游响应体。日志和错误消息不能包含 Token、代理凭据、完整请求体或敏感响应头。

schema 2 Backend 使用独立的 `BackendPlugin` / `backend` 服务，保留 GetInfo、Health 和配置生命周期，增加 `Invoke`、`RunOnce`。它必须声明 Backend API 1、Host Service API 3，完成 `InitHostServices` 并返回 `ready=true`；旧 Transport 的 Host API 2 仍可选，不能把这条兼容规则用于新 Backend。

Backend 的调用身份由宿主生成。每次 `Invoke` 或 `RunOnce` 得到短时 `access_token`，反向 KV/用户创建观察必须携带本次授权；调用结束或停用后失效。不要在启动、配置、Health 或常驻线程中自行开展后台业务。权限、读写分离与恢复规则见[通用契约](../backend/pkg/pluginapi/docs/generic-plugins.md)。

## 4. 设计插件配置

插件配置由插件定义，由 Sub2API 加密保存。推荐流程是：

1. 在 `internal/pluginconfig.Config` 中定义字段和默认值；
2. 使用 `json.Decoder.DisallowUnknownFields` 等严格方式解析；
3. 将空对象规范化为完整默认配置；
4. 在 `ValidateConfig` 和 `ApplyConfig` 中复用同一套校验；
5. 配置应用成功后再让宿主保存，保存失败时允许恢复旧配置。

JSON 字段统一使用 `snake_case`。敏感配置不要放入 URL、UI 通知、诊断结果或日志。Transport 的 UI 不应读取、刷新或持久化 OAuth Token。schema 2 无配置页面时，管理员使用宿主的 JSON 对象编辑器；纯前端配置只做对象校验和加密保存，不启动进程。

## 5. 实现插件自己的配置 UI

管理配置 UI 是插件包内的静态页面，由 `ui.entrypoint` 声明，仅管理员可打开。schema 1 使用 Bridge 1；schema 2 可选配置页协商 Bridge 2，但只开放原有配置子集。以下初始化流程用于管理配置页。

schema 2 的业务入口另由 `ui.entries` 声明，支持 admin、user 和 shell，使用独立业务会话和 Bridge 2。业务页不能调用配置消息；只有 shell 辅助面板允许固定导航和命名区域轮廓。完整消息见 [UI Bridge](../backend/pkg/pluginapi/docs/ui-bridge.md)。

Bridge 1 配置页初始化流程：

1. 加载包内 HTML、CSS 和 JavaScript；
2. 创建 Bridge 并注册 `message` 监听；
3. 发送 `sub2api.plugin.ready`；
4. 调用 `config.load` 渲染表单；
5. 编辑后调用 `config.save`；
6. 测试前先保存，再调用 `config.test`；
7. 页面卸载时清理监听器、计时器和等待响应。

Bridge 2 配置页与业务页先建立文档专属端口：SDK 自身 load 前排队并忽略 challenge，load 后发送无凭证 `bridge.ready`。宿主每次 iframe load 取消旧请求/关闭旧端口，只经 window 发送公开随机 `bridge.challenge`。SDK 用 fragment token、当前 challenge 和自己创建的 MessageChannel 端口证明，收到端口 ACK 后再派发未执行队列；业务请求、回复和 context 推送全部经该端口，无 window fallback。共享示例使用 `data-bridge-version="2"` 显式选择，legacy 使用 `"1"`。重载重新握手，不自动重放已派发写操作。

管理配置支持：

| 消息 | 用途 |
| --- | --- |
| `config.load` | 读取当前配置。 |
| `config.save` | 提交对象配置；有运行时时经其校验和应用，再由宿主加密保存，需要二次验证。 |
| `config.test` | 运行已保存配置的诊断，需要二次验证。 |
| `plugin.status` | 只读运行时状态，不触发测试或二次验证。 |
| `ui.resize` | 调整配置 iframe 高度。 |
| `ui.notify` | 显示成功、错误或提示消息。 |

需要响应的请求必须带非空且唯一的 `request_id`。宿主最多接受 32 个在途请求，30 秒过期；重载与卸载后旧结果失效。配置页的旧 ready、resize、notify 保留无响应行为。Bridge 1 校验双方窗口、宿主精确 origin/iframe opaque origin、来源标识、token 和请求 ID；Bridge 2 握手校验窗口来源，业务则验证绑定端口、来源标识、token 和请求 ID，不能沿用窗口 source/origin 判断端口事件。不要依赖 CDN、远程脚本、Cookie 或本地存储。页面需要兼容窄屏和明暗主题，并正确处理加载、保存、测试、超时和未保存状态。

详细握手与信封见 [UI Bridge](../backend/pkg/pluginapi/docs/ui-bridge.md)，示例共用封装位于 [`examples/common/bridge.js`](../backend/pkg/pluginapi/examples/common/bridge.js)。Bridge 2 早期初始化请求在握手 ACK 后正常执行，不要求 load 前派发；业务页也可在收到首次端口 `context.changed` 后初始化。路由更新取消旧请求、保留端口并推新 context；身份/入口/代次变化、失败或卸载使端口和挑战一同失效。

辅助面板在不同的已登录 AppLayout 页面之间保留原 iframe 和连接；公开及无 AppLayout 页面不加载。主站登录令牌续期会重新加载业务页面和辅助面板，schema 2 管理配置页会关闭；未保存的内容可能丢失。已经发出的写操作可能完成，请重新读取实际结果，勿自动重试。schema 1 管理配置保持原续期行为。

Bridge 1 配置请求的30秒计时只让回复过期：TOTP对话框保持，已发出的保存/测试继续，之后验证成功仍可操作，但过期结果不再回包；关闭、重载、卸载使旧文档操作失效。Bridge 2 超时仍取消请求和二次验证。取消HTTP请求不能证明保存没有发生，应先重新读取配置确认。

原HTTP客户端的每次请求30秒超时保持不变；Bridge回复计时包含等待二次验证的时间，HTTP计时则从实际派发时开始。

## 6. 编写包清单

自建打包流程可维护 `manifest.source.json`，由工具生成最终清单与哈希；现有示例打包器直接生成清单，不读取这个源文件。下面是 schema 1 模板，版本范围与已测试版本仅用于说明格式，发布时必须按实测填写：

```json
{
  "schema_version": 1,
  "id": "example.openai.transport",
  "name": "Example OpenAI Transport",
  "version": "0.1.0",
  "requires": {
    "sub2api": ">=0.1.179 <0.2.0",
    "recommended_sub2api_version": "0.1.179",
    "tested_sub2api_versions": ["0.1.179"],
    "plugin_protocol": 1,
    "transport_api": 1,
    "ui_bridge": 1
  },
  "capabilities": [
    {
      "id": "openai.oauth.outbound_transport.v1",
      "platform": "openai",
      "account_type": "oauth"
    }
  ],
  "runtimes": {},
  "ui": { "entrypoint": "ui/index.html" },
  "files": {}
}
```

最终清单必须填写真实目标平台运行时和每个文件的 SHA-256，不能直接安装上述空 `runtimes` / `files` 模板。`requires.sub2api` 是硬兼容范围，`tested_sub2api_versions` 只填实测版本，`recommended_sub2api_version` 用于管理页展示。

schema 1 只接受 `openai.oauth.outbound_transport.v1`。schema 2 采用[独立清单](../backend/pkg/pluginapi/v1/manifest-v2.schema.json)，不能混用 Transport 能力：有后台时 protocol/backend/host-service/transport 分别为 1/1/3/0，无后台时均为 0；有 UI 时 ui_bridge=2，否则为 0。它只接受当前白名单能力；新 Provider、任意 API 或资金写入不会因清单声明而自动获得支持。

## 7. 生成密钥并签名

生产包应始终签名，宿主默认拒绝未签名包。仓库提供用于隔离示例的 Ed25519 密钥生成和签名工具。从仓库根目录进入 `backend` 模块：

```bash
cd backend
mkdir -p ../.local/plugin-examples
go run ./pkg/pluginapi/examples/package --generate-test-key ../.local/plugin-examples/signing.json
```

工具只创建新文件，不覆盖已有密钥，也不打印内容。JSON 包含 `private_key` 和 `public_key`；只把公钥提供给隔离宿主，禁止输出整个文件或把私钥放入源码、插件包、部署服务器。生产发布应使用独立的受控签名流程，不复用示例测试密钥。

签名覆盖最终 `manifest.json` 的精确字节；清单中的文件哈希再覆盖运行时和 UI 文件。签名完成后不要重新格式化 `manifest.json`。

部署者在 Sub2API 配置文件中追加公钥：

```yaml
plugins:
  allow_unsigned: false
  trusted_publishers:
    local-example: "BASE64_ED25519_PUBLIC_KEY"
```

`trusted_publishers` 是在宿主内置官方公钥之外追加的信任来源，不能覆盖内置公钥。`signature.json` 中的 `key_id` 必须与配置键完全一致。密钥轮换时先发布包含新公钥的宿主配置或版本，再发布新签名包，最后再停用旧密钥。

开发阶段如需使用未签名包，只应在隔离的本地环境临时设置 `plugins.allow_unsigned: true`，测试完成后立即恢复为 `false`。

## 8. 构建、测试和安装

以下命令接着在 `backend` 模块目录执行。打包器只构建示例插件，不重建宿主：

```bash
go test ./pkg/pluginapi/v1 ./pkg/pluginapi/examples/... -count=1
node --check pkg/pluginapi/examples/common/bridge.js
for kind in frontend backend combined legacy; do
  go run ./pkg/pluginapi/examples/package --kind "$kind" \
    --out-dir ../.local/plugin-examples \
    --sign-key-file ../.local/plugin-examples/signing.json
done
unzip -t ../.local/plugin-examples/example-frontend-1.0.0.s2plugin
```

`--publisher` 默认是 `local-example`，须与隔离宿主公钥配置键一致。纯前端不构建或携带二进制，其余示例为当前平台构建运行时。构建与安装细节见[示例说明](../backend/pkg/pluginapi/examples/README.md)。这些命令是开发步骤，不表示文档已替你完成运行验收。

旧 `TestPluginRuntimeIntegration` 依赖特定可信 Transport 包的 UI 文件和配置合同，不能直接用新 `legacy` 示例替代。新示例应按其说明，在隔离宿主中安装后验证实际进程、业务页面、权限、停用和恢复。

Transport 最低测试集应覆盖配置默认值和边界值、插件身份、请求和响应分块、流式响应、上下文取消、插件退出、代理开关、包哈希、签名、路径安全、目标平台运行时以及配置 Bridge 的加载、保存、测试、错误和超时。通用插件还要验证 caller、方法角色与读写、短时授权、跨用户/插件隔离、后台超时和停止、业务会话代次，以及面板关闭重开与监听/计时器清理。

安装后先保持停用，核对兼容性、签名、实际权限和诊断结果。只有 schema 1 Transport 按账号灰度启用，API Key 和未命中的 OAuth 账号保留原路径；schema 2 直接按安装状态启停，不创建账号灰度绑定，后台启用后无需转发流量即可运行。

## 9. 发布前检查清单

- 有运行时时，插件版本、能力与 `GetInfo` 返回值一致；纯前端不需要伪造进程信息；
- `requires.sub2api` 覆盖范围经过验证，没有未经测试的破坏性版本；
- `tested_sub2api_versions` 与实际测试记录一致；
- 有运行时时，每个支持的平台和架构都有真实运行时文件；纯前端不携带二进制；
- schema 2 Backend 已完成 Host API 3 初始化，方法和宿主能力授权与实际用途一致；
- 生产包存在有效 `signature.json`，公钥已交付部署者；
- 包中没有私钥、源映射、测试数据、日志和临时文件；
- UI 不依赖外部资源，也不保存宿主会话信息；
- 配置切换、请求取消、响应关闭和错误重试语义经过测试；
- 发布说明包含升级、停用、回滚和兼容版本信息。

## 10. 常见问题

| 现象 | 排查方向 |
| --- | --- |
| 安装提示签名不受信任 | 检查 `signature.json.key_id`、Base64 公钥和配置键是否完全一致。 |
| 插件显示不兼容 | 检查 `requires.sub2api` 与该 schema 的协议组合；schema 2 还需检查 `backend_api`、`host_service_api`。 |
| 插件进程无法启动 | 检查目标系统和架构对应的运行时路径、可执行权限和运行用户权限。 |
| 配置页无法加载 | 有配置页时检查 `ui.entrypoint`、哈希和 Bridge 校验；schema 2 未声明配置页时使用 JSON 对象编辑器。 |
| 业务页被拒绝 | 核对启用状态、入口角色、数值安装 ID、包代次和当前登录状态；业务与配置会话不能混用。 |
| 保存后配置未生效 | 查看 `ValidateConfig`、`ApplyConfig` 返回的规范化配置和诊断信息。 |
| 请求失败后重复执行 | 检查 `ForwardResponseError.request_sent` 是否准确反映请求是否可能已发出。 |

## 11. 需要扩展能力时

如果新插件需要支持其他 Provider、其他账号类型或新的消息字段，应先扩展并版本化公开协议，再由宿主增加能力匹配和生命周期处理。不要仅通过清单声明一个宿主尚未实现的能力。这样可以让旧插件继续运行，也能让新宿主明确拒绝不兼容的插件。

已公开能力范围内可以独立安装插件；超出当前白名单或需要新的宿主数据、资金操作时，仍需先做最小宿主适配，并补齐鉴权、审计、幂等与验收。当前示例不判断邮箱、不发送提醒、不发放奖励，也不提供完整引导或余额写接口。

## 12. 当前示例

[示例目录](../backend/pkg/pluginapi/examples/README.md)包含 `frontend`、`backend`、`combined` 与 `legacy`，以及实际使用的打包器和共用 Bridge。后台示例只把“创建流程已完成”的事件保存到插件自己的 KV：不补造历史或失败建号事件，重复补读覆盖同一稳定键。准确记录边界、Redis 丢失后的重放和独立邮箱授权见[完成记录与恢复说明](../backend/pkg/pluginapi/examples/README.md#完成记录与恢复边界)。
