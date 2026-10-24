---
title: 快速开始
---

# 快速开始

第一次使用 Windows 版 Codex App 时，按下面步骤配置。配置完成后，Codex 会通过 UseGoodAI 使用 AI 模型。

开始前确认：

- 已安装 ChatGPT / Codex App；还没安装先看 [Codex 接入](/clients/codex)。
- 已打开 UseGoodAI 管理后台，方便下一步创建 API 密钥。
- 账号有余额。

## 1. 创建本中转站的 API 密钥

进入管理后台 **API 密钥**，点击 **创建密钥**。名称写用途，分组选择当前要用的模型档位；创建后先确认列表中有这把密钥，等第 2 步出现输入 API Key 的提示时再回来复制。

API Key 用来让 Codex 识别你的账户并记录用量，后面脚本会要求你粘贴这个 Key。

<details>
<summary>查看创建和复制截图</summary>

<a class="doc-image-link" href="/images/quick-start/create-api-key-1.jpg" target="_blank" rel="noopener">
  <img src="/images/quick-start/create-api-key-1.jpg" alt="进入 API 密钥页面并点击创建密钥">
</a>

<a class="doc-image-link" href="/images/quick-start/create-api-key-2.jpg" target="_blank" rel="noopener">
  <img src="/images/quick-start/create-api-key-2.jpg" alt="填写密钥名称并选择正确分组">
</a>

<a class="doc-image-link" href="/images/quick-start/create-api-key-3.jpg" target="_blank" rel="noopener">
  <img src="/images/quick-start/create-api-key-3.jpg" alt="在 API 密钥列表中复制 API Key">
</a>

</details>

<section data-onboarding="windows-step">

## 2. 打开 PowerShell 运行脚本

<div data-onboarding="windows-run">

Windows 按 `Win + X`，点击 **终端** 或 **Windows PowerShell**。粘贴已复制的命令并按回车，看到“输入 API Key”的提示后，先回网页复制密钥。

</div>

双击下面的命令行选中整条命令，再按 **Ctrl+C** 手动复制，回到本机 PowerShell 粘贴并运行：

<div data-onboarding="windows-command">

```powershell
irm https://docs.usegoodai.com/install/codex.ps1 | iex
```

</div>

命令运行到“输入 API Key”的提示时，先停在那里，回到管理后台的 **API 密钥** 列表。

核对要使用的那一行分组，点击密钥旁的复制按钮；复制命令后，剪贴板里已经不是之前的 Key。

再回到刚才的 PowerShell，在 API Key 提示处粘贴并按回车。等脚本执行结束，再回本页继续第 3 步。

运行前先保存任务。脚本写入配置后会自动关闭正在运行的 ChatGPT 软件和 Codex，正在运行的任务会中断。

<details>
<summary>Mac 用户看这里</summary>

打开终端运行：

```bash
curl -fsSL https://docs.usegoodai.com/install/codex.sh | bash
```

看到输入 API Key 的提示时，也要先回管理后台核对分组并复制密钥，再回终端粘贴、按回车；脚本结束后再继续。

</details>

</section>

<section data-onboarding="codex-test">

## 3. 重新打开 Codex

脚本已经自动关闭 Codex App。重新打开并新建任务。

发送 `测试`。

能正常回复，就接入完成。还是不能回复，先看下面的脚本排查。

</section>

<section data-onboarding="models">

## 4. 查看模型价格

接入完成后，看 [模型价格](/models) 了解价格和分组。实际扣费以后台使用记录为准。

</section>

<section data-onboarding="problems">

## 运行脚本时遇到问题

| 遇到的问题 | 先做这个 | 还是不行 |
| --- | --- | --- |
| PowerShell 提示无法下载脚本 | 检查网络后重新运行第 2 步命令 | 仍然失败时，改用下面的 CC Switch 接入 |
| 脚本提示未检测到 Codex App | 先安装 Codex App，再回到本页重新运行脚本 | 看 [Codex 接入](/clients/codex) |
| Codex 提示 `INSUFFICIENT_BALANCE` 或 `Insufficient account balance` | 登录后台充值，充值后重新测试 | 仍然提示余额不足时，确认 Codex 使用的是当前账号的 API Key |
| 脚本提示未能自动关闭，或脚本完成后 Codex 还是不能回复 | 重启电脑，再重新打开 Codex 并新建任务 | 看 [报错与踩坑](/errors/) |

脚本已经完成，但 Codex 返回具体错误时，直接看 [报错与踩坑](/errors/)。

</section>

<section data-onboarding="support">

需要帮助时，打开 [联系客服及新用户福利](/contact)，按页面说明联系。福利内容和领取规则以客服页为准。

</section>

<details>
<summary>脚本安全吗？</summary>

脚本内容公开，运行前可查看具体操作。

这个脚本只做两件事：写入 Codex 配置；写入后关闭正在运行的 ChatGPT 软件和 Codex，让新配置生效。不会修改系统设置，也不会安装或改动其他软件。不放心就用下面的 CC Switch 或手动配置。

| 文件 | 作用 |
| --- | --- |
| `C:\Users\你的用户名\.codex\config.toml` | 写入 UseGoodAI 接口地址、模型和 Responses 配置 |
| `C:\Users\你的用户名\.codex\auth.json` | 写入 UseGoodAI API Key |

写入前会自动备份旧文件。

Mac 用户对应路径是 `~/.codex/config.toml` 和 `~/.codex/auth.json`。

</details>

<details>
<summary>移除中转配置，恢复官方账号登录</summary>

Windows 打开 PowerShell，运行：

```powershell
irm https://docs.usegoodai.com/install/codex-restore.ps1 | iex
```

macOS / Linux 打开终端，运行：

```bash
curl -fsSL https://docs.usegoodai.com/install/codex-restore.sh | bash
```

脚本会清空登录凭据，并删除 `config.toml` 中的模型端点配置；MCP 和其它配置保持不变。完成后彻底退出 Codex，重新打开并使用官方账号登录。

</details>

## 不用脚本的方式

| 顺序 | 方式 | 入口 |
| --- | --- | --- |
| 第二种 | CC Switch 接入 | [Codex CC Switch 接入](/codex-cc-switch) |
| 第三种 | 手动配置 | [Codex 手动接入](/clients/codex-manual-config) |

## 安装生图工具

Codex 已经可以正常对话后，再为图片模型额外创建一个 **专门画图分组** 的 API Key。按照[安装生图工具](/image-video-group-image)运行一键安装命令，之后可以直接在 Codex 对话中生成、修改并显示图片。
