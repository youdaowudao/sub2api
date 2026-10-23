# 插件开发指南

## 稳定边界

schema 1 Transport 仍只支持 `openai.oauth.outbound_transport.v1`。它负责建立实际上游 HTTP/TLS 连接，Sub2API 负责账号选择、OAuth Token 生命周期、下游协议、响应解析、SSE、错误映射、用量统计和计费。

插件不应修改 API Key 路径，也不应自行刷新或持久化 OAuth Token。

schema 2 支持纯前端、纯后端和组合插件。新开发先对照[通用插件契约](generic-plugins.md)、[schema 2 清单](../v1/manifest-v2.schema.json)和[可构建示例](../examples/README.md)。通用 Backend 不承担 OAuth 转发，也不能取得旧账号目录或出站凭据；新能力必须在公开白名单内。

## 推荐结构

```text
plugin/
├── cmd/<plugin>/main.go
├── internal/config/
├── internal/transport/
├── ui/index.html
├── ui/assets/
├── tools/packager/
├── manifest.source.json
└── README.md
```

以上是 Transport 的建议结构，入口调用 `pluginv1.Serve`。通用 Backend 调用 `pluginv1.ServeBackend`；纯前端只有清单和静态资源，无 Go 入口和二进制。配置解析和业务实现放入独立包，以便不启动子进程就能单元测试。

构建仓库内 Go SDK 和示例时，版本以 [`backend/go.mod`](../../../go.mod) 为准。示例工具从 `backend` 模块目录运行，不需要未提供的外部示例仓库：

```bash
mkdir -p ../.local/plugin-examples
go run ./pkg/pluginapi/examples/package --generate-test-key ../.local/plugin-examples/signing.json
go run ./pkg/pluginapi/examples/package --kind combined \
  --out-dir ../.local/plugin-examples \
  --sign-key-file ../.local/plugin-examples/signing.json
```

生成器不覆盖已有密钥，不输出其内容；只把 `public_key` 配入隔离宿主的 `plugins.trusted_publishers.local-example`，私钥留在开发端。类型可选 frontend/backend/combined/legacy；平台、签名和安装步骤见[示例说明](../examples/README.md)。

## 运行时方法

下表和流式帧顺序适用于 schema 1 Transport：

| 方法 | 要求 |
|---|---|
| `GetInfo` | ID、版本、协议和能力必须与清单一致 |
| `Health` | 返回进程是否可以接受新请求，不执行昂贵探测 |
| `ValidateConfig` | 严格解析并返回完整规范化 JSON |
| `ApplyConfig` | 原子应用配置；失败时保留旧配置 |
| `TestConfig` | 验证当前环境和已保存配置，返回简短诊断 |
| `Forward` | 双向流式传输请求与原始 HTTP 响应 |

请求帧顺序：`start`、零到多个 `body_chunk`、`body_end`。响应帧顺序：`start`、零到多个 `body_chunk`、`end`。不能继续处理的错误使用 `error` 帧。

`request_sent` 必须如实表示请求是否可能已经到达上游。值为 `true` 时宿主禁止自动切换账号重放；只有能确认尚未调用上游 Transport 时才能返回 `false`。

schema 2 的 `BackendPlugin` 使用服务名 `backend`，无需 `Forward`；GetInfo、Health 和配置生命周期外，提供 `Invoke` 与 `RunOnce`。有后台时必须声明 protocol/backend/host-service/transport=1/1/3/0，并完成 Host API 3 的 `InitHostServices`、返回 `ready=true`。schema 1 的 Host API 2 仍可选，不能据此跳过新 Backend 初始化。

`Invoke` 携带宿主确认的 caller、已授权方法和本次短时 `access_token`。反向 KV/用户观察必须同时满足清单能力、本次 read/write 和当前调用授权；user 方法的 KV 总是按调用者隔离，admin/background 才使用插件共享空间。保存 broker 连接到进程结束，但不能保存已结束调用的授权继续使用。详见[通用契约](generic-plugins.md)。

后台只在宿主每 5 秒驱动的 `RunOnce` 中执行，遵守 5 秒期限、取消和幂等；不在启动、Health、配置校验或测试中启动业务。写调用已派发后的超时/断连可能已执行，返回结果未知，不能自动重试。

## 配置

- JSON 字段统一使用 `snake_case`。
- 拒绝未知字段、非法范围和受保护请求头。
- 默认配置必须完整，空对象应规范化为所有默认字段。
- 保存时由插件先验证和应用，再由宿主加密写入数据库。
- 数据库写入失败时宿主会尝试恢复旧配置，插件必须允许重复应用。
- schema 2 的 `ui.entrypoint` 可选；缺失时管理员用 JSON 对象编辑器，不能打开不存在的 iframe。纯前端保存对象配置不启动进程。

业务页面使用 `ui.entries` 与 [Bridge 2](ui-bridge.md)，按 admin/user/shell 与角色挂载，不能读取管理配置；只有 shell 可用固定导航与命名区域轮廓。

## 资源管理

以下 HTTP 资源规则用于 Transport；Backend 同样需要传播取消并回收自己创建的资源：

- 复用 HTTP Transport 和连接池，不要为每个请求创建新连接池。
- 配置切换后关闭旧空闲连接。
- 使用 stream context 取消 DNS、连接、上传和响应读取。
- 始终关闭上游响应体。
- 不在插件内无限缓存按账号区分的客户端。

## 最低测试集

- 配置默认值、未知字段、边界值和深复制。
- 插件身份及协议版本。
- 请求体分块、无请求体、固定 Content-Length。
- 响应状态、重复请求头、流式响应和响应读取错误。
- 上下文取消、插件退出和超时。
- 代理开启与禁用。
- 包哈希、签名、路径穿越和目标平台运行时。
- UI Bridge 加载、保存、测试、错误和超时。
- 通用插件的 caller 与方法角色、读写端点、短时授权撤销、跨插件/用户隔离。
- 后台无转发流量时执行、锁故障关闭、超时/停用回收、重启和重复补读。
- 业务页与配置会话隔离、包代次、菜单/直访权限，以及 shell 关闭重开和请求/监听/计时器清理。

发布前还应在隔离宿主中用真实包验证安装、页面和运行行为。旧 `TestPluginRuntimeIntegration` 依赖特定 Transport 包的 UI/配置与可信发布者，不能直接套用到新 legacy 示例；按[示例说明](../examples/README.md)验证新包。
