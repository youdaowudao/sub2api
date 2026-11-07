# 独立插件示例

示例只使用公开协议，宿主不识别这些示例的 ID。安装或替换插件不需要重新构建宿主。

| 目录 | 包内容与用途 |
| --- | --- |
| frontend | 静态页面、管理员/用户业务入口和一个辅助面板；没有可执行文件 |
| backend | 独立 Backend 进程；周期读取已完成建号记录，按稳定键保存测试事件 |
| combined | 静态业务页与独立 Backend；通过受控 Bridge 调用自己的方法 |
| legacy | schema 1 Transport 回归夹具；实际 HTTP 转发，与通用 Backend 分开 |

这些是开发示例，不判断邮箱、不发通知、不修改余额，也不迁移已有引导。`wait`、`crash` 和后台延迟设置仅用于隔离验收，勿在真实账户环境使用。

## 构建与签名

在 `backend` 模块目录运行，Go 版本以该模块的 `go.mod` 为准。输出和缓存可放项目内；不修改依赖版本。

```bash
mkdir -p ../.local/plugin-examples
go run ./pkg/pluginapi/examples/package --generate-test-key ../.local/plugin-examples/signing.json
go run ./pkg/pluginapi/examples/package --kind frontend --out-dir ../.local/plugin-examples --sign-key-file ../.local/plugin-examples/signing.json
go run ./pkg/pluginapi/examples/package --kind backend --out-dir ../.local/plugin-examples --sign-key-file ../.local/plugin-examples/signing.json
go run ./pkg/pluginapi/examples/package --kind combined --out-dir ../.local/plugin-examples --sign-key-file ../.local/plugin-examples/signing.json
go run ./pkg/pluginapi/examples/package --kind legacy --out-dir ../.local/plugin-examples --sign-key-file ../.local/plugin-examples/signing.json
```

生成密钥只创建新文件，不覆盖已有密钥，也不打印内容。在隔离宿主配置的 `plugins.trusted_publishers.local-example` 中配置密钥文件的 `public_key` 字段。只取公钥，私钥留在开发端；不要输出整个密钥文件。默认仍拒绝未签名或不受信任包。`--dev-unsigned` 只生成明确用于隔离开发的未签名包，不会改变宿主默认配置。

打包器编译示例进程、生成文件哈希并签名清单，不编译宿主。纯前端包不构建、携带或启动后台进程。打包后端时输出的 `example-*-runtime` 是插件产物，可执行文件已包含在 `.s2plugin` 中。生产发布应建立独立构建流程并如实填写已测试宿主版本。

## 安装与操作

1. 使用已构建的宿主，在“插件”管理页上传对应 `.s2plugin`，核对类型、权限、发布者和版本兼容性，再启用。未声明测试过当前宿主时，需要明确接受未测试状态。
2. `frontend` 的管理员/用户菜单会出现业务入口；登录后的应用布局还可打开、关闭、重新打开辅助面板。只有辅助面板允许导航和命名区域轮廓操作，业务页执行这些操作会被拒绝。
3. `combined` 中“查看调用身份”返回宿主确认的调用者。“保存我的记录”走写通道与原二次验证；“读取我的记录”只读。普通用户和管理员在用户方法下都使用各自空间。
4. `backend` 启用后无需打开配置页或产生转发流量。真实测试注册或管理员建号完成后，后台会读取并保存 `event.<user_id>`；重复读取覆盖同一键，不累计计数。管理员可调用 `event_list` 查看本插件最多100项事件；普通用户不能调用此方法。
5. 停用关闭业务入口及后续调用，并停止后台。配置管理仍按原约定可用。替换须先停用，同ID配置保留；旧业务会话不能读取新包。

纯后端包使用宿主的 JSON 配置编辑器，不需要虚假的配置 iframe。默认配置为 `{}`。可选测试字段：`background_delay_ms`（0–60000）、`ignore_cancel`、`crash_on_run`。它们只在 RunOnce 中生效，保存/校验/测试不执行后台工作。管理状态接口提供只读测试快照（本进程周期次数、正在执行数及进程ID），用于隔离环境核对多副本互斥；它不保存业务状态。

共用 `common/bridge.js` 显式选择协议：frontend/combined 的 script 标记为 `data-bridge-version="2"`，legacy 为 `"1"`，不隐式降级。Bridge2在自身load前排队，load后用公开challenge、fragment token和子文档创建的MessagePort完成连接，ACK后才发送业务；请求、结果和context只走端口。合法重载重新握手，已派发写操作不会自动重放；切路由保留端口并取消旧请求、更新context。完整消息与清理约定见[UI Bridge](../docs/ui-bridge.md#bridge-2-文档连接)。Bridge1继续原配置window协议。

## 完成记录与恢复边界

事件 `user.creation_completed:<user_id>` 表示本次新建流程的原必要步骤成功，且完成记录已经保存；覆盖邮箱、第三方首次完成注册和管理员建号。它不表示所有写入 users 的行，不回填历史用户或旧失败残留账号。记录之前宕机可能留下原账号，但没有完成事件，后续普通登录不补造事件；记录之后响应丢失或宿主重启，记录仍能恢复读取。

宿主完成表保存失败时不返回完成成功，账号保留，提示先尝试登录或核对；提交结果不明时不声称已回滚。插件是否安装、启用、在线及观察KV是否可用都不参与注册写入。

每轮读取固定一个用户ID上界，读完从0开始下一轮，避免低ID晚完成永久漏读。示例在所有事件写入成功后才保存本页进度；Redis KV丢失可重新补读，允许重放，不承诺恰好一次。`ttl=0` 的稳定事件键会保留，停用、删除或同ID恢复不会自动清除插件KV。

默认示例没有邮箱读取权限。构建后端/组合包时显式加 `--email-grant` 才声明 `users.email.read.v1`；宿主仅在该额外授权下返回当前未删除用户邮箱。完成表不复制邮箱。

替换示例可增加 `--version 1.0.1`；兼容性负例可使用 `--host-range '>=999.0.0'`。两者都只改变插件包。未来风险分析、通知、奖励和复杂引导需插件自己的实现；资金操作还需要新的可审计、可幂等宿主接口，当前不存在余额写能力。
