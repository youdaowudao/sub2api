# 插件 UI Bridge

schema 1 管理配置保留 Bridge 1。schema 2 有 UI 时声明 Bridge 2：可选管理配置页只开放原配置子集，业务页使用独立消息集。完整入口与 HTTP 合同见[通用插件契约](generic-plugins.md)，共用示例封装见 [`examples/common/bridge.js`](../examples/common/bridge.js)。

## 加载方式

管理员配置页通过 `ui.entrypoint` 加载，宿主为每次打开创建短时 UI 会话：

```text
/api/v1/plugin-ui/<asset-token>/index.html#bridge_token=<bridge-token>
```

资源 Token 用于读取包内 `ui/` 文件，初始 Bridge Token 放在 URL fragment，不随资源 HTTP 请求发送到服务器。iframe 使用 `sandbox="allow-scripts"`，不授予 `allow-same-origin`。

UI 只能加载包内、已在清单声明的资源。CSP 禁止外部网络连接、表单提交和外部 frame。

schema 2 业务页由 `ui.entries` 声明，父组件先用 JWT 请求 `POST /api/v1/plugins/:id/entries/:entry/session`，再直接加载返回的静态资源 URL。iframe 不接收 JWT；资源能力绑定用途、入口、安装代次、用户/角色与 TokenVersion，有效 30 分钟，每次读取重新检查状态。业务与管理配置资源会话不能混用。

## Bridge 2 文档连接

Bridge 1 配置继续使用原 window 消息。Bridge 2 的配置、业务页和辅助面板必须建立文档专属 MessagePort，不能回退到 window 业务请求或回复。窗口代理在导航后仍可能相同，不能把它当作文档身份。

| 步骤 | 传输和信封 | 行为 |
| --- | --- | --- |
| 就绪 | UI → parent window：`{source:"sub2api-plugin-ui",type:"bridge.ready"}` | SDK 自身 document load 前仅排队请求、忽略 challenge 且不发 ready；load 后才发，晚安装时用 readyState=complete |
| 挑战 | 宿主 → iframe window：`{source:"sub2api-plugin-host",type:"bridge.challenge",challenge}` | 每次 iframe load 先撤销旧请求并关闭旧端口，再生成安全随机 challenge；ready 只重发当前尚未使用的挑战。窗口消息不含 token、context、结果或端口 |
| 证明 | UI → parent window：`{source:"sub2api-plugin-ui",type:"bridge.connect",bridge_token,challenge}`，转移恰好一个子文档创建的 MessageChannel 端口 | 宿主验证当前 iframe 窗口、opaque origin、来源标识、fragment token、当前 challenge 和单端口；成功即消耗 challenge，重复连接不替换活动连接 |
| 确认 | 仅 port：`{source:"sub2api-plugin-host",bridge_token,type:"bridge.connected"}` | SDK 收到正确 ACK 后才派发未执行队列；随后请求、响应和 context.changed 全部使用该 port |

SDK 校验窗口挑战的 `event.source === parent` 和精确宿主 origin；同一 challenge 只提交一次 channel。重载重新握手，端口绑定原文档；旧异步结果不能转投新端口。路由变化取消当前请求、二次验证和轮廓，保留端口并推送新 context；身份、入口、代次变化或失败、关闭、卸载使端口和挑战同时失效。连接加载受 30 秒界限约束。

页面与辅助面板可同时存在：每个宿主实例先检查消息是否来自自己的 iframe。其它 iframe 的消息直接忽略，不关闭其端口；只有当前 iframe 的无效握手才关闭所转移的无用端口。

共用示例 SDK 必须显式选择协议，例如 schema 2 使用 `<script src="bridge.js" data-bridge-version="2"></script>`，legacy 使用 `data-bridge-version="1"`。`bridge.request` 可以早于 load 调用，早期请求在 ACK 后正常执行；已派发操作不会因新连接自动重放，写操作断连后可能已经执行。

## 消息信封

以下业务信封在 Bridge 1 经 window 传输，在 Bridge 2 仅经已认证 port 传输。UI 到宿主：

```json
{
  "source": "sub2api-plugin-ui",
  "bridge_token": "TOKEN",
  "type": "config.load",
  "request_id": "UNIQUE_ID"
}
```

宿主到 UI：

```json
{
  "source": "sub2api-plugin-host",
  "bridge_token": "TOKEN",
  "type": "config.load.result",
  "request_id": "UNIQUE_ID",
  "ok": true,
  "config": {}
}
```

## 管理配置方法

| `type` | UI 参数 | 成功响应 |
|---|---|---|
| `sub2api.plugin.ready` | 无 | 无响应 |
| `config.load` | 无 | `config` |
| `config.save` | `config` 对象 | 规范化后的 `config` |
| `config.test` | 无 | `result` |
| `plugin.status` | 无 | `result`（`Health`：`{healthy, message, status_json}`） |
| `ui.resize` | `height` | 无响应 |
| `ui.notify` | `level`、`message` | 无响应 |

`config.test` 在 v1 中测试已保存配置（需二次验证，可产生副作用）。UI 若要测试当前表单，应先调用 `config.save`。

`plugin.status` 是只读运行时状态通道：无副作用、免二次验证、不弹宿主提示，供状态面板轮询。它映射到插件 `Health`，`result.status_json` 是插件自定义的不透明 JSON 快照。带状态展示的插件应使用它，而不是把 `config.test` 当作状态轮询。

`config.save` 同样经过二次验证。纯前端配置只校验对象并加密保存，测试不启动进程，状态返回启用情况与无需进程的说明。schema 2 管理配置即使协商 Bridge 2，也不因此获得业务或 shell 操作；没有配置页时由宿主提供 JSON 对象编辑器。

Bridge 1 管理配置的30秒计时只让回复过期，不中止已发出的保存/测试，也不自动关闭正在等待的二次验证；之后验证成功仍可继续操作，但过期结果不会发回 iframe。关闭、重载或卸载仍使旧文档操作失效。Bridge 2 在30秒后取消请求及等待中的二次验证。HTTP中止不能证明服务端没有保存，应重新读取实际结果，避免自动重试。

HTTP客户端原有的单次请求30秒超时继续保留；它从每次HTTP派发时开始，与包含等待二次验证时间的Bridge回复期限分别计时。

## 业务 Bridge 2

业务页与辅助面板的下列操作都需要唯一 `request_id`，成功响应带 `ok:true`：

| `type` | 参数 | 成功结果与限制 |
| --- | --- | --- |
| `plugin.call` | `method`、对象 `payload` | `result:{request_id,payload}`；父组件从可信 session.entry.methods 选 calls/read 或 actions/write，write 复用二次验证 |
| `context.get` | 无 | `context:{path,route_name,user_id,role}`；无 query、fragment、邮箱或凭据 |
| `ui.resize` | 数字 `height` | `ok:true`；普通业务页 160–960，shell 240–720 |
| `ui.notify` | `level:info/success/error`、`message` | 宿主 toast，文字最多 500 字符 |
| `ui.close` | 无，仅 shell | 先响应，再销毁 iframe 与轮廓；保留宿主重开按钮 |
| `ui.navigate` | `target:dashboard/profile/keys`，仅 shell | 仅导航到宿主固定目标，拒绝 URL、path 和其他路由名 |
| `ui.anchor` | `target:navigation/content` 或 `null`，仅 shell | 返回有界 `rect:{x,y,width,height}` 或 null；宿主绘制不可交互轮廓，不接受 selector |

失败响应是 `ok:false,error:{code,message,retryable,outcome_unknown}`；用户取消二次验证为 `CANCELLED`、`outcome_unknown=false`，业务尚未执行。写操作派发后超时或断连可能已执行，不要自动重试。业务页没有 config.load/save/test，也没有任意 API、DOM 或脚本代理。

路由变化经已认证 port 发送 `context.changed`，结构与 `context.get` 相同；连接 ACK 后也会发送当前 context。首次 load 前调用的初始化请求先排队，握手后正常返回；重载取消旧请求，复用同 ID 也不能接到旧代次结果。一个时刻只挂载一个辅助面板，关闭、切路由、注销、停用和卸载会清理相应请求、计时器和轮廓；销毁时同时移除监听器。entries 每 5 秒刷新，相同入口与代次保留 iframe 和在途请求；停用后页面和面板收起，服务端立即拒绝后续调用与资源。

辅助面板由应用根部常驻宿主持有，在不同的已登录 AppLayout 页面间导航时保留原 iframe、业务会话和 port；取消旧请求、二次验证和轮廓，再通过原 port 更新 context。公开页面和没有 AppLayout 的页面不加载面板。

主站登录令牌续期会重新加载业务页面和辅助面板，schema 2 管理配置页会关闭；未保存的内容可能丢失。已经发出的写操作可能完成，请重新读取实际结果，勿自动重试。schema 1 管理配置保持原续期行为。

## 必须执行的校验

Bridge 1 的 UI 接收窗口消息时验证 `event.source === parent`、精确宿主 origin、来源标识、Bridge Token 和等待中的 `request_id`；宿主验证 iframe 窗口、`event.origin === "null"` 与 token。Bridge 2 仅握手验证窗口和 origin，业务校验绑定的 port、来源标识、token 和 request_id；MessagePort 事件不能沿用窗口的 source/origin 判断。宿主回复还必须匹配原连接、请求代次与 pending 对象，不能异步读取当前 contentWindow 回包。context.changed 是无请求 ID 的宿主通知。

所有需响应消息使用非空、唯一的 request_id，最多 32 个在途，宿主 30 秒过期；重复 ID 和第 33 个在途请求不执行。配置 ready/resize/notify 保留旧无响应行为。每个等待请求都要有超时与卸载清理，迟到结果不能串到重载后的新文档。

宿主不会向 iframe 提供管理员 Token。插件 UI 不得尝试访问管理 API、Cookie、父页面 DOM 或浏览器存储中的宿主数据。
