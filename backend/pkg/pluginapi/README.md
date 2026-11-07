# Sub2API 本地插件协议

本目录是插件开发者可以依赖的公开契约。`v1/plugin.proto` 和 `v1/runtime.go` 定义进程协议，`v1/manifest.schema.json` 和 `v1/manifest-v2.schema.json` 分别定义旧 Transport 与通用插件清单，`docs/` 记录开发和发布规范。Provider 私有实现不应放入本目录。

## 开发文档

- [开发指南](docs/development.md)：保留 Transport 流程，并说明通用 Backend 的接入区别。
- [通用插件契约](docs/generic-plugins.md)：schema 2、Backend、短时宿主授权、业务入口和后台周期。
- [可构建示例](examples/README.md)：纯前端、纯后端、组合与 legacy Transport，以及真实打包命令。
- [UI Bridge](docs/ui-bridge.md)：管理配置 Bridge 1 与业务 Bridge 2 的消息和安全要求。
- [包格式](docs/package-format.md)：清单、文件哈希、签名和版本规则。
- [安全边界](docs/security.md)：进程权限、敏感数据和故障策略。

## 实体与运行方式

插件的交付实体是 `.s2plugin` ZIP 包，包含清单、生产签名及类型所需文件。schema 1 Transport 同时携带运行时与配置 UI；schema 2 可仅有静态 UI、仅有 Backend 运行时，或两者都有。管理员在独立的插件管理页手动上传，Sub2API 不从网络自动下载插件，也不要求 Docker。

有运行时时，宿主启动当前平台二进制并通过本机 gRPC 协作；纯前端启用不启动进程。旧 Transport 继续使用原转发和排空流程。通用 Backend 启用后独立于转发流量运行，后台只由宿主周期调用 `RunOnce`；停用先关闭新入口/调用，再撤销授权、取消任务并回收进程。

多实例部署不要求共享插件目录。宿主会在数据库保存已验签的原始插件包，各实例缺少本地文件时会重新验签和解包，并周期性对齐启用状态和加密配置；账号灰度只适用于旧 Transport。所有实例必须连接同一数据库并使用相同的加密密钥。

独立进程是代码和发布边界，不是操作系统安全沙箱。插件拥有 Sub2API 服务用户所拥有的文件和网络权限，因此只应安装可信发布者的签名包。闭源二进制可提高源码分发门槛，但不能承诺无法反编译。

## schema 1 Transport 能力边界

schema 1 仍只接受 `openai.oauth.outbound_transport.v1`：

- 仅匹配 `platform=openai` 且 `account_type=oauth` 的上游 HTTP 请求。
- API Key 账号、其他 provider、OAuth 登录与 Token 刷新流程不进入插件。
- 插件建立真实的上游 HTTP/TLS 连接并返回原始 HTTP 响应。
- 命中插件的 OAuth WebSocket 账号会使用 Sub2API 现有 HTTP Bridge，不直接建立上游 WebSocket，避免绕过 v1 HTTP 插件协议。
- Sub2API 继续负责响应状态处理、SSE 解析、错误映射、用量统计、计费和下游输出。
- 灰度比例以账号 ID 稳定分桶，未命中的 OAuth 账号继续使用原有内置路径。

## schema 2 三类插件

| 类型 | 文件与行为 |
| --- | --- |
| 纯前端 | 静态 UI 和业务入口，无 runtime，不启动进程 |
| 纯后端 | Backend runtime，无 UI；管理员使用 JSON 对象编辑配置 |
| 组合 | 静态业务页与 Backend，通过受控 Bridge 调用自己的声明方法 |

通用 Backend 使用 `BackendPlugin`、服务名 `backend` 和 `ServeBackend`，不实现 `Forward`。有后台时 `plugin_protocol=1`、`backend_api=1`、`host_service_api=3`、`transport_api=0`；无后台时四者为 0。有 UI 时 `ui_bridge=2`，否则为 0。新旧能力不能混装，通用插件不创建账号灰度绑定。

`ui.entrypoint` 仍只表示管理员配置页；`ui.entries` 声明 admin/user/shell 业务入口与明确角色。`backend.methods` 声明最多 32 个方法的名称、角色和 read/write；宿主构造可信 caller，写操作经二次验证。完整规则见[通用契约](docs/generic-plugins.md)。

## 宿主服务（HostService）

HostService 经 go-plugin broker 反向提供，连接随运行时回收。插件通过 `HostBrokerReceiver` 获得 broker，再在 `InitHostServices` 中拨回宿主。

- **schema 1**：旧 Host API 2 保持可选。未实现 `InitHostServices` 的旧 Transport 返回 `Unimplemented` 时仍可转发；既有 KV 和范围内账号目录/出站身份行为保留。
- **schema 2 Backend**：Host API 3 初始化是必需步骤。必须实现 `InitHostServices` 并确认 `ready=true`；可使用 `DialGenericHostServices` 创建有界反向连接，保存到运行时结束再关闭。初始化失败不能降级放行。

KV 由 Redis 支撑，保留既有 namespace/key 格式、单值 256 KiB、TTL 和列表数量限制。插件身份由当前运行时注入，不能通过参数选择其他插件；持久性取决于现有 Redis 部署，不承诺丢失后不重放。

schema 2 还必须声明 `storage.read.v1` / `storage.write.v1`，且每次 KV 调用携带当前 `Invoke` 或 `RunOnce` 的短时 `access_token`。授权绑定运行时、包代次、调用者、读写和截止时间，结束或停用即撤销。用户方法始终使用调用者自己的 KV 空间，包括管理员调用 user 方法；admin 方法和后台使用插件共享空间。read 方法不能借 storage.write 声明写入。

`ListUserCreations` 需要 `users.created.read.v1`，且只准后台或管理员方法调用；读取当前邮箱还需独立 `users.email.read.v1`。纯前端不能申请这些后台能力，普通用户方法不能读取全站用户，通用 Backend 也不能使用旧账号目录或 OAuth 凭据服务。更多字段与完成记录边界见[通用契约](docs/generic-plugins.md)和[示例说明](examples/README.md)。

扩展 HostService 时应同时补齐协议版本、宿主实现、能力匹配与安全校验；追加 RPC 本身不会自动授予插件新权限。

## 包结构

以下为 schema 1 或组合插件的示意布局；schema 2 应省略不需要的运行时或 UI，运行时路径以清单为准：

```text
manifest.json
signature.json                 # 生产包必需
runtimes/linux-amd64/plugin
runtimes/linux-arm64/plugin
runtimes/windows-amd64/plugin.exe
ui/index.html
ui/assets/...
```

`manifest.json` 必须声明所有运行时和 UI 文件的 SHA-256。`signature.json` 使用受信任发布者的 Ed25519 私钥对 `manifest.json` 原始字节签名。官方 OpenAI Transport 公钥由宿主内置，第三方发布者公钥由部署者追加到 `plugins.trusted_publishers`。文件哈希由已签名清单保护。

插件默认保持停用。未签名包默认拒绝安装；`plugins.allow_unsigned` 只应用于开发者自己构建的本地调试包。

## 兼容性

清单的兼容字段包括：

- `requires.sub2api`：允许的 Sub2API 语义化版本范围。
- `requires.recommended_sub2api_version`：可选的建议宿主版本。
- `requires.tested_sub2api_versions`：可选的实际验证过的宿主版本列表。
- `requires.plugin_protocol`、`transport_api`、`ui_bridge`：独立协议版本；schema 2 还必须声明 `backend_api`、`host_service_api`，按类型使用有效版本或 0。

构建本仓库 Go SDK 和示例的 Go 版本以 [`backend/go.mod`](../../go.mod) 为准；进程协议不要求独立插件与宿主共享编译器或内存 ABI。

宿主版本超出范围时，插件可以安装并查看，但保持“不兼容”状态且不能启用。版本在范围内但未列入已测试版本时，管理员必须再次确认才能启用。

## UI 隔离与 Bridge

插件 UI 由包内静态文件实现，宿主使用只有 `allow-scripts` 权限的 sandbox iframe 加载。iframe 不接收主站 JWT，也不能直接访问管理 API。宿主使用资源能力 URL 与独立 Bridge Token，校验 frame window、opaque origin 和 token。业务资源会话还绑定入口、包代次、可信用户/角色和 TokenVersion，每次读取复核启用和身份状态，不能用于读取管理配置。

管理配置 UI（schema 1 Bridge 1、schema 2 Bridge 2 的配置子集）可发送以下消息。消息按语义分层，鉴权与副作用一致对应（读操作免二次验证，写/主动测试需要二次验证），任何插件都可复用，不针对具体插件定制：

| 消息 | 语义 | 二次验证 | 映射的插件 RPC |
| --- | --- | --- | --- |
| `config.load` | 读取已保存配置 | 否 | 宿主数据库 |
| `config.save` | 写入配置 | 是 | `ValidateConfig` + `ApplyConfig` |
| `config.test` | 主动测试配置/连通性（可产生副作用） | 是 | `TestConfig` |
| `plugin.status` | 读取运行时状态（无副作用） | 否 | `Health`（`status_json`） |
| `ui.resize` / `ui.notify` | 仅 UI 交互 | — | — |

需要响应的消息带唯一 `request_id`，宿主以 `<type>.result` 返回结果，最多 32 个在途，30 秒失效。首次 load 前的初始化请求保留；重载和卸载后旧结果不能发给新文档。

- 配置整体使用 Sub2API 的密钥加密后存入数据库；有运行时时先验证和应用，数据库写入失败时恢复旧配置。纯前端仅校验对象并加密保存，不调用表中的生命周期 RPC，也不启动进程。
- `config.test` 的结果由插件 UI 自行展示（内联或经 `ui.notify`），宿主不再对成功结果强制弹出提示，避免插件把它当作轻量状态轮询时刷屏。
- `plugin.status` 是通用的**只读**状态通道：宿主经 `GET /admin/plugins/:id/status` 调用运行中插件的 `Health`，返回 `{healthy, message, status_json}`。`status_json` 是插件自定义的**不透明** JSON 快照（宿主不解析、不参与健康判定），插件必须以无副作用方式生成（不得应用配置、访问上游或触发探测），因此该端点只读、免二次验证。有运行时的插件未运行时返回 `healthy=false` 且不含 `status_json`；纯前端直接返回启用状态及无需进程的说明。这样带状态面板的插件无需滥用 `config.test` 即可展示实时状态。

业务页使用 Bridge 2 的 `plugin.call`、`context.get`、`ui.resize`、`ui.notify`，不能发送 config 消息。shell 另可关闭面板、导航到 dashboard/profile/keys、请求 navigation/content 的不可交互轮廓。context 不含 query、fragment、邮箱或凭据。动态菜单和直接访问都按服务端入口与角色判断，停用后页面关闭。详细信封及边界见 [UI Bridge](docs/ui-bridge.md)。

## 协议源码

- `v1/plugin.proto`：稳定的进程间消息定义。
- `v1/runtime.go`：Go 插件进程启动入口和宿主客户端声明。
- [`v1/manifest.schema.json`](v1/manifest.schema.json)：schema 1 清单。
- [`v1/manifest-v2.schema.json`](v1/manifest-v2.schema.json)：schema 2 清单。

插件通过进程协议协作，不使用 Go 动态链接，也不要求插件与 Sub2API 使用相同编译器或共享内存 ABI。
