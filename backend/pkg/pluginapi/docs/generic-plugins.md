# 通用插件公开契约

schema 2 支持纯前端、纯后端和组合插件。schema 1 Transport、服务名 `oauth_transport`、Transport API1、管理 Bridge1及旧Host API2保留。新后端用独立的 `BackendPlugin` / `backend` 服务和 `ServeBackend`，无需实现 `Forward`。完整字段见 [schema 2](../v1/manifest-v2.schema.json)、[进程协议](../v1/plugin.proto) 和 [可构建示例](../examples/README.md)。

## 清单、权限和配置

| 内容 | 必须声明 |
| --- | --- |
| 所有包 | schema_version=2、命名空间ID、语义版本、requires.sub2api、所有实际文件的SHA256 |
| 有后端 | 当前平台runtime；plugin_protocol=1、backend_api=1、host_service_api=3、transport_api=0；invoke/background至少一个能力 |
| 没有后端 | 不携runtime，以上四个协议版本为0，不声明后台/存储/用户观察能力 |
| 有前端 | ui_bridge=2；ui.entries包含id/label/path/mount/roles，path在已校验ui文件内 |
| 没有前端 | ui_bridge=0，无UI入口 |
| 配置页面 | 可选ui.entrypoint；没有配置页面时管理员用JSON对象编辑器 |
| 可调用方法 | backend.methods中声明name、role（user/admin）、access（read/write），最多32项 |

`mount=admin`只允许admin；`user`和`shell`显式声明user/admin角色。方法`role=user`允许所有登录角色，但包括管理员在内都使用调用者自己的KV空间；admin方法和后台工作使用插件共享空间。通用包不能声明旧OpenAI能力，也不创建其账号灰度绑定。未知字段、能力、版本和不完整类型组合会被拒绝。

能力白名单：`backend.invoke.v1`、`backend.background.v1`、`storage.read.v1`、`storage.write.v1`、`users.created.read.v1`、`users.email.read.v1`。write同时需要storage.read；email同时需要created.read。管理员安装可信签名包意味着允许其声明的能力，管理页列出权限。插件配置仍由宿主加密保存，校验/应用失败保持原值。

## 后端调用与宿主反向服务

Backend必须返回与清单完全一致的GetInfo和能力，Health为健康，InitHostServices确认Host API3 ready。Validate/Apply/Test不能启动后台业务，也没有业务授权。

Invoke收到宿主生成的request_id、method、payload_json、可信caller（user_id/role）及短时access_token。用户不能通过payload改写caller。请求/响应payload最多256KiB，新gRPC通道最多512KiB，Invoke最长10秒并传播取消。结果为payload_json或结构化error。写操作派发后超时/断连可能已经执行，宿主返回outcome_unknown，不自动重试。

HostService每次KV/观察调用都需要当前Invoke或RunOnce的access_token；授权绑定运行时、代次、调用者、方法读写和截止时间。生命周期、调用结束、停用或换包后的token无效。KV读需要storage.read，写还需要storage.write及本次write授权。跨插件和跨用户空间不能访问。

ListUserCreations只允许获授users.created.read的后台工作或管理员方法。邮箱还需要独立users.email.read；只返回当前未软删除用户邮箱，不保存邮箱副本。普通用户方法不能通过声明更多能力读取全站用户。

## 前端HTTP与Bridge

HTTP使用宿主原JWT、用户状态、角色、面板限制和审计；write额外复用二次验证。

| 入口 | 结果 |
| --- | --- |
| GET /api/v1/plugins/entries | 当前角色可见且已启用、兼容的入口数组 |
| POST /api/v1/plugins/:id/entries/:entry/session，body {} | url、bridge_token、ui_bridge_version=2、expires_at、可信entry（含授权methods） |
| POST /api/v1/plugins/:id/calls/:method，body {payload:{…}} | 只读方法；data为request_id/payload |
| POST /api/v1/plugins/:id/actions/:method，同上 | 写方法及二次验证；不能经calls绕过 |

`:id`为数值安装ID，plugin_key为清单ID，两者不可混用。业务资源能力URL绑定入口、包代次、用户角色/TokenVersion，30分钟有效；每次读取复核状态，不能用于管理配置。iframe不接收JWT。

Bridge2先建立文档专属MessagePort：SDK自身load前排队，load后发无凭证bridge.ready；宿主每次iframe load撤销旧连接后仅经window发送公开随机bridge.challenge。SDK携原fragment token、当前challenge及一个自己创建的channel端口发送bridge.connect；宿主验证frame/null origin/token/challenge/单端口，消耗challenge并只经port发bridge.connected。SDK收到ACK后才派发未执行队列，窗口不传业务请求、回复、context或授权端口，无window fallback。同一challenge不重复提交，已派发write不因重连自动重放。具体信封和显式版本选择见[UI Bridge](ui-bridge.md#bridge-2-文档连接)。

业务信封仅走已认证port：插件发送 `{source:"sub2api-plugin-ui",bridge_token,type,request_id,...参数}`；宿主回复同token/request_id、source=`sub2api-plugin-host`、type=`原type.result`及ok。校验绑定port、来源标识和token；宿主将回复绑定原connection/generation/pending对象。最多32个在途，30秒过期；重复ID不执行，重载后同ID也不能接收旧结果。路由变化取消请求、保留port并推送新context；身份/入口/代次变化、失败或卸载关闭端口并失效challenge。Bridge1配置保留原window合同。

业务操作：`plugin.call {method,payload}`、`context.get`、`ui.resize {height}`、`ui.notify {level,message}`。宿主从可信entry.methods决定read/write路径。context只含path/route_name/user_id/role，不含query、fragment、邮箱或凭据。失败结构含code/message/retryable/outcome_unknown；取消二次验证不会执行业务。

只有应用外壳辅助面板可用：`ui.close`、`ui.navigate {target:dashboard/profile/keys}`、`ui.anchor {target:navigation/content|null}`。宿主只绘制不可交互轮廓并返回矩形，不开放selector、任意DOM、脚本或API代理。一个时刻一个面板，关闭即卸载，可重开；切路由、注销、停用会清理请求、监听器、计时器和轮廓。它是合作入口，不是完整全站引导。

管理配置仍使用原config.load/save/test、plugin.status和二次验证。schema2配置页面协商Bridge2，只开放该配置子集；业务页面不能读写宿主管理配置。无效/停用/越权、RPC超时和写结果未知使用明确reason与request_id元数据。审计保留操作摘要，不保存任意payload或配置body。

辅助面板在不同的已登录AppLayout页面之间保留原iframe和port，路由变化仍取消旧请求/二次验证/轮廓并推送新context；公开及无AppLayout页面不加载。主站登录令牌续期会重新加载业务页面和辅助面板，schema2管理配置页会关闭；未保存的内容可能丢失。已经发出的写操作可能完成，请重新读取实际结果，勿自动重试。schema1配置保持原续期及30秒仅回复过期的行为，详见[UI Bridge](ui-bridge.md#管理配置方法)。

宿主HTTP客户端仍保留原来的每次请求30秒超时；它与包含TOTP等待时间的Bridge回复期限分别计时。

## 生命周期、恢复与安全限制

每副本可有本地运行时，后台只由宿主每5秒驱动RunOnce；期限5秒，独立LeaderLockCache锁TTL30秒，无协调或锁故障就跳过。超时撤权并杀死、等待进程退出后释放锁；进程启动/Health/配置页不驱动业务。插件仍应对外部副作用幂等。

停用先保存状态，关闭新业务入口和调用、撤权、回收进程；其他副本周期对齐。异常退出保留期望启用并有界恢复。Linux的通用子进程设置父线程终止信号以回收宿主强杀后的进程；这是创建线程的生命周期，可能早于整个宿主进程。其它平台沿用原进程管理，本轮没有验证其强杀回收。替换必须先停用，同ID保留配置，旧业务token不能访问新代次。包原件与持久状态支持不同本地目录的副本恢复。

创建观察是流程完成事实，准确边界和ID乱序补读见[示例说明](../examples/README.md#完成记录与恢复边界)。示例只记录测试事件，后续邮箱判断、提醒、奖励和引导内容由独立插件实现。超出公开能力时仍需新增宿主适配，尤其资金必须有鉴权、审计、幂等及账务事务接口。

本机子进程不是OS沙箱：它仍有服务用户的文件与网络权限，只安装可信原生代码。HostService权限限制宿主提供的接口，不保证限制恶意原生进程的全部行为。签名、解压路径/大小/哈希、配置加密、二次验证及旧Transport故障策略仍适用。
