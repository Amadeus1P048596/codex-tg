# codex-tg：通过 Telegram 管理本地 Codex 会话

`codex-tg` 是一个用 Go 编写的本地 Codex 控制平面。它为 Telegram 启动自己的
OpenAI Codex App Server，把手机变成查看任务进度、切换会话、发送后续指令、
处理 Plan 输入、审批操作和接收最终结果的远程控制入口。

当前模式是“**独立会话，共享记忆与 Skills**”：Telegram 不复用 Windows Codex
客户端的 App Server、会话目录或运行时数据库；在 Codex runtime 层，两端只通过明确配置的
资源链接共享 Skills、plugins、全局规则等能力，并通过独立的应用级记忆库共享用户明确批准的
持久信息。项目 workspace 仍可按配置指向同一批本地目录；Scheduled tasks 也使用 TG 私有的
定义目录、调度器和运行会话，不再依赖或写入 Codex Desktop 的任务目录。

最近发布标签：[`v0.5.0-amadeus.1`](https://github.com/Amadeus1P048596/codex-tg/releases/tag/v0.5.0-amadeus.1)，
基于上游 `v0.5.0`。当前分支还包含 `Unreleased` 修正，目前均以源码形式提供。

> **Fork 与致谢**
>
> 本仓库是 [`mideco-tech/codex-tg`](https://github.com/mideco-tech/codex-tg)
> 的社区 fork。感谢原项目维护者和所有贡献者完成的架构、实现、测试与文档工作。
> 本 fork 保留上游 Git 历史、`LICENSE` 和 `NOTICE`，并继续使用 Apache-2.0
> 许可证。详细改动与归属说明见 [FORK_NOTES.md](FORK_NOTES.md)。

## 它解决什么问题

Codex App Server 运行在本机，Telegram 只承担经过权限限制的交互与通知。
因此你可以在不把 App Server 暴露到公网的情况下：

- 从手机查看本机 Codex 正在处理什么，以及任务是否需要输入。
- 回复现有会话、启动普通任务或 Plan Mode，并停止当前任务。
- 在多个 Codex 会话之间切换，同时避免后台任务刷屏。
- 处理审批、结构化选择、图片输入、Details 和完整日志导出。
- 保留本地优先的数据边界：工作区、Codex 会话、SQLite 状态和配置文件都留在自己的机器上。

`codex-tg` 不以替代 Codex 官方 Remote Connections 为目标。它更偏向一个可扩展的
本地控制层：Telegram 是当前生产适配器，内部 `control` 接口则为未来的路由 Agent、
语音入口、托盘应用或其他私有适配器提供基础。

## 当前 fork 的主要功能

### 单活动卡片

每个 Codex turn 通常只对应一张 Telegram 活动卡：

```text
❤️ 修复消息重复通知
Codex · 处理中 · 38s

进度
✓ 搜索通知实现
✓ 查看 internal/daemon/panels.go
● 修改活动卡生命周期

9 次操作 · T:01a03706
```

- 前 4 秒只显示 Telegram `typing`，较快完成的任务不会先产生临时卡片。
- 长任务创建一张活动卡，非终态编辑最短间隔为 4 秒。
- 搜索、读取、修改、构建和测试等原始事件会聚合为用户可读的最近活动。
- 完成、失败、取消或等待输入时直接更新同一张卡片。
- 较长结果可继续发送为独立的 `Codex · 结果` 消息。
- Codex 输出的 Markdown pipe table 会自动转换成适合窄屏的 Telegram 记录列表：
  第一列作为加粗标题，其余列纵向显示为带标签字段；代码块里的表格示例保持原样。
- 原始工具调用和输出仍可通过 Details、Tools file 与完整日志按需查看。

卡片开头的 emoji 用来标识会话，不表示状态；状态由 `处理中`、`需要输入`、
`已完成`、`失败` 和 `已取消` 等文字明确表达。

### 前台会话、首页与收件箱

每个 Telegram chat/topic 只有一个前台 Codex 会话：

- `/home`：打开会话首页，同时查看当前会话、后台运行中的会话及待处理数量；
  后台运行项会显示状态和已运行时间，可直接点开对应会话卡片。
- `/current`：确认当前前台会话及其状态。
- `/threads`：以可点击标题列出当前 Telegram App Server runtime 中的真实会话。
- `/inbox`：保存后台已完成、失败、取消或需要输入的会话，重启后仍然存在。
- 切换会话时会清理旧的非终态卡片，并显示新前台会话的当前状态。
- 后台任务不会持续推送过程卡片，但其运行状态会汇总到 `/home`；终态或需要输入时
  仍只提供一次“切换至该会话”入口，避免多个进度卡同时刷新造成刷屏。

`/threads` 只列出 Telegram 独立 runtime 中的真实会话，不会混入 Windows Codex
客户端的历史。这两个入口可以同时工作，但不会看到或写入彼此的 thread；如果配置到同一
workspace，磁盘上的源码改动仍然是双方可见的。

### 会话创建与管理

- `/newchat`：先输入纯文本标题，再发送首条提示词（可附带图片），在
  `Documents/Codex/<日期>/<标题>` 下创建 Chat。
- `/newthread`：使用相同的“纯文本标题 → 可带图片的提示词”流程创建普通会话，但不创建
  Chat 文件夹。
- `/newchat <提示词>` 与 `/newthread <提示词>`：保留单行快捷形式。
- `/cancel`：取消正在等待标题或首条提示词的新建流程。
- `/title <标题>`：重命名当前会话，并防止后续自动刷新覆盖用户设置的标题。
- `/archive`：仅归档当前空闲会话，执行前需要按钮确认。
- `/unarchive`：按每页 10 条列出真实归档会话并恢复。

### Telegram 独立 runtime 内的写入会话

Telegram daemon 在自己的 `CODEX_HOME` 中维护一条可写的 live App Server 会话和一条
只读轮询会话。这里的写入权管理只发生在 Telegram 独立 runtime 内，不是在 Telegram
与 Windows Codex 客户端之间交接会话：

- `/show` 和观察模式通过只读轮询查看状态，不会加载 live writer。
- `/bind` 或卡片上的 `在 TG 中继续` 会切换前台路由，并让 TG live App Server 加载该会话。
- 若隔离 runtime 内意外存在另一个 App Server writer，Telegram 会报告冲突；输入不会排队，
  也不会偷偷创建并行 turn。这个检查是 runtime 内的防御措施，不代表 Desktop 正在共享会话。
- `/release` 或 `释放空闲写入权` 会在所有相关会话都安全空闲时重建 live App Server，
  只读轮询保持连接。
- 连续 5 分钟没有允许用户的 Telegram 消息或按钮操作时，守护进程会尝试同样的安全释放；
  活跃任务、审批或等待输入会推迟释放。

### Plan Mode、审批与图片

- `/plan <内容>` 或 `/reply --plan ...` 通过 App Server 的
  `collaborationMode.mode = plan` 真正启动 Plan Mode。
- 结构化选择会显示为可点击按钮，并保持与原 thread/turn 的精确路由关系。
- `/approve`、`/deny` 或卡片按钮可处理待确认操作。
- 可以向已经路由的会话发送 Telegram 图片；机器人选择最大尺寸的 photo，下载上限为
  20 MiB，并以 App Server `localImage` 输入启动或 steer 当前 turn。
- 图片可带说明文字；没有说明时会使用一个简短的默认图片分析提示。
- App Server 接受请求后，权限为私有的临时图片会保留 30 分钟，避免异步读取时文件已经被删；
  随后自动删除，启动时和后续下载前也会清理超过 24 小时的遗留文件。
- 图片输入不会另外产生“已收到图片”卡片。

记录标题后，可以把纯图片或“说明文字＋图片”作为新会话的首条提示词；若仍在等待标题，
图片不会误投当前会话，机器人会提示先发送纯文本标题。媒体组、用图片充当标题，以及给单行
`/newchat <提示词>` 或 `/newthread <提示词>` 命令附图，当前不在支持范围内。

### 定时任务

可以直接在 Telegram 会话里用自然语言要求 Codex 创建、查看、修改或删除 Scheduled tasks。
codex-tg 会在新建和恢复会话时注入本地 `automation_update` 工具，把任务写入 TG 私有目录；
daemon 每 15 秒检查一次到期任务，并在 TG 私有 App Server 中为每次运行创建一个独立后台会话。
任务的运行、需要输入、失败和最终结果沿用现有 observer 与 `/home`、`/inbox` 链路回到 Telegram。

- Telegram 只能创建独立运行的 `cron` 任务。绑定既有会话续跑的 `heartbeat` 不受支持；每次运行
  都是新的 thread，避免把旧对话状态、writer 所有权或失败上下文带进下一次计划执行。
- Desktop 与 TG 的任务目录和调度器完全隔离，因此 Desktop 是否运行不会影响 TG 定时任务，也不会
  因两个客户端同时加载同一份定义而重复执行。
- 每个任务可保存独立的 model、reasoning effort 和可选 `cwd`；未设置 `cwd` 时使用 daemon 默认目录。
- 到期时先在 SQLite 中持久领取该时间槽；daemon 重启后不会重复启动同一时间槽。任务在 App Server
  暂未就绪时保持等待，连接恢复后再执行。
- 任务提示词以明文保存在本机任务目录，不应包含 token、密码或其他秘密。

默认目录是 `~/.codex-tg/automations`，可用 `CTR_GO_AUTOMATIONS_DIR` 改到另一个 TG 专用目录。
不要把它指向 `~/.codex/automations` 或任何 Desktop runtime 目录。

## 工作方式

```text
Windows Codex 客户端                         Telegram
   │ 自有 App Server                            │ Bot API
   ▼                                            ▼
Desktop CODEX_HOME                        codex-tg Go daemon
   ├── Desktop sessions                     ├── daemon SQLite（含任务时间槽领取）
   ├── state / writer locks                 ├── TG automations + 调度循环
   ├── runtime cache                        ├── live App Server（写入/定时执行）
   └── Desktop automations                  └── poll App Server（只读校验）
                                                  │ 本机 stdio
                                                  ▼
                                          Telegram CODEX_HOME
                                             ├── TG sessions
                                             ├── state / writer locks
                                             └── runtime cache

runtime 与定时任务均不共享可变状态；只显式链接 Skills / plugins / packages / 全局规则，
并可独立共享应用级持久记忆
项目层可按配置使用同一 workspace
```

关键约束：

- Codex App Server 是 thread、turn、审批和实时事件的权威来源。
- `threadId` 是持久身份；Telegram chat/topic 只是输入和展示表面。
- SQLite 保存路由、绑定、回调、卡片、观察目标、收件箱和投递元数据。
- live 与 poll App Server 默认都通过本机 stdio 启动，不应直接暴露到公网。
- Windows Codex 客户端与 Telegram daemon 各自拥有 App Server 生命周期和可变状态。

## 快速开始

### 前置条件

- Go 1.26 或更高版本。
- 已安装并登录 OpenAI Codex CLI，且本机支持 `codex app-server`。
- 从 Telegram 的 BotFather 创建一个机器人并取得 token。
- 你的 Telegram 数字 user id；如需限制群组或 topic，还应准备 chat id。

当前 fork 没有附带预编译二进制，请从源码运行或自行构建。

### 1. 克隆 fork

```powershell
git clone https://github.com/Amadeus1P048596/codex-tg.git
cd codex-tg
```

默认分支已经指向当前 fork 发布分支。上游仓库仍保留在
[`mideco-tech/codex-tg`](https://github.com/mideco-tech/codex-tg)。

### 2. 初始化配置

```powershell
go run ./cmd/ctr-go init
go run ./cmd/ctr-go doctor
```

`init` 会引导填写 Telegram token、允许的用户、默认工作目录等信息，并默认写入
`~/.codex-tg/config.env`。新配置会把 `CTR_GO_CODEX_HOME` 默认设为
`~/.codex-tg/codex-home`，从一开始就与 Windows Codex 客户端隔离；Scheduled tasks
目录也默认使用 `~/.codex-tg/automations`。也可以通过
`CTR_GO_CONFIG` 指定其他配置路径；显式环境变量的优先级高于配置文件。

隔离 home 需要自己的 Codex 授权文件。首次使用时，请把 `CODEX_HOME` 临时指向该目录，
按本机 Codex CLI 的登录流程完成授权，然后再启动 daemon。不要从 Desktop home 复制
或链接 session、SQLite、writer lock 和缓存文件。

如果更喜欢直接使用环境变量，PowerShell 示例为：

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
$env:CTR_GO_CODEX_HOME = "C:\Users\you\.codex-tg\codex-home"
$env:CTR_GO_AUTOMATIONS_DIR = "C:\Users\you\.codex-tg\automations"
```

### 3. 启动守护进程

```powershell
go run ./cmd/ctr-go daemon run
```

也可以先构建：

```powershell
go build -buildvcs=false -o ctr-go.exe ./cmd/ctr-go
.\ctr-go.exe doctor
.\ctr-go.exe daemon run
```

macOS 还继承了上游的用户级 LaunchAgent 服务安装器：

```powershell
go run ./cmd/ctr-go service install --start --start-at-login
go run ./cmd/ctr-go service status
```

Windows 和 Linux 当前建议使用自己的进程管理方式运行 `ctr-go daemon run`。

### 4. 在 Telegram 中开始使用

```text
/start
/home
/threads
/observe all
```

选择或创建一个会话后，可以直接回复路由卡片，也可以向当前绑定会话发送普通文本。

## 常用 Telegram 命令

| 类别 | 命令 | 作用 |
| --- | --- | --- |
| 首页与导航 | `/start`、`/home` | 查看前台会话、后台运行状态与待处理摘要 |
| 首页与导航 | `/current`、`/inbox` | 查看当前会话或后台待处理会话 |
| 首页与导航 | `/threads`、`/projects` | 切换 runtime 会话或从项目视图导航 |
| 新建会话 | `/newchat`、`/newthread`、`/new` | 创建 Chat、普通会话或指定项目会话 |
| 会话管理 | `/title`、`/archive`、`/unarchive`、`/cancel` | 重命名、归档、恢复或取消待输入流程 |
| 路由与输入 | `/show`、`/bind`、`/reply` | 查看、在 TG 中继续或向指定会话发送内容 |
| Plan Mode | `/plan`、`/reply --plan` | 在路由会话中启动 Plan Mode |
| 模型设置 | `/settings`、`/model`、`/effort` | 查看并修改 Telegram 发起任务使用的模型设置 |
| 观察与诊断 | `/observe all\|off`、`/context`、`/status` | 管理全局观察目标并查看路由/运行状态 |
| 写入权与维护 | `/release`、`/repair`、`/stop` | 释放空闲 live 写入权、重建 App Server 会话或停止任务 |
| 审批 | `/approve`、`/deny` | 接受或拒绝待审批请求 |
| 卡片兼容 | `/panelmode per_run\|stable` | 切换每 turn 卡片或稳定复用卡片模式 |

回复路由优先级为：显式 thread id → 回复消息绑定 → 一次性 steer/answer 状态 → 当前 chat/topic 绑定。

## 本地命令

```text
ctr-go init [--force]
ctr-go service install [flags]
ctr-go service start|stop|restart|status|enable-login|disable-login|uninstall
ctr-go daemon run
ctr-go status
ctr-go doctor
ctr-go repair
ctr-go version
```

从源码运行时，把 `ctr-go` 替换为 `go run ./cmd/ctr-go` 即可。

## 重要配置

| 环境变量 | 说明 |
| --- | --- |
| `CTR_GO_TELEGRAM_BOT_TOKEN` | Telegram Bot API token，必填且不得提交到 Git |
| `CTR_GO_ALLOWED_USER_IDS` | 允许操作机器人的 Telegram user id 列表 |
| `CTR_GO_ALLOWED_CHAT_IDS` | 可选的 chat id 白名单 |
| `CTR_GO_DEFAULT_CWD` | Codex 默认工作目录 |
| `CTR_GO_CODEX_CHATS_ROOT` | Codex UI Chat 根目录，默认 `~/Documents/Codex` |
| `CTR_GO_CODEX_BIN` | Codex CLI 可执行文件路径或命令名 |
| `CTR_GO_CODEX_HOME` | 仅传给 TG 子 App Server 的独立 `CODEX_HOME`；新配置默认 `~/.codex-tg/codex-home` |
| `CTR_GO_AUTOMATIONS_DIR` | TG 私有 Scheduled tasks 定义目录，默认 `~/.codex-tg/automations`；不得与 Desktop 共享 |
| `CTR_GO_CONFIG` | 自定义 `config.env` 路径 |
| `CTR_GO_HOME` | 自定义 daemon 数据、日志和 SQLite 根目录 |
| `CTR_GO_PANEL_MODE` | `per_run`（默认）或 `stable` |
| `CTR_GO_NOTIFY_NEW_RUN` | 是否为新 run 使用普通通知 |
| `CTR_GO_LOG_ENABLED` | 是否保存 daemon 标准日志 |
| `CTR_GO_DIAGNOSTIC_LOGS` | 是否输出结构化诊断事件 |
| `CTR_GO_OBSERVER_POLL_SECONDS` | observer 轮询间隔，默认 5 秒 |
| `CTR_GO_REQUEST_TIMEOUT_SECONDS` | App Server 请求超时，默认 30 秒 |
| `CTR_GO_FULL_ACCESS` | 让子 App Server 使用 `approval_policy=never` 与 `danger-full-access`，默认关闭 |

`CTR_GO_FULL_ACCESS=true` 会显著扩大 Codex 对本机的操作权限，只应在理解风险且访问机器人
受到严格白名单保护时启用。

完整的性能、项目预览和投递重试配置可在
[`internal/config/config.go`](internal/config/config.go) 中查看；最小示例见
[`.env.example`](.env.example)。兼容变量 `CTR_TELEGRAM_BOT_TOKEN`、
`CTR_ALLOWED_USER_IDS` 和 `CTR_ALLOWED_CHAT_IDS` 仍然可用。

## 独立会话，共享记忆与 Skills

`CTR_GO_CODEX_HOME` 只传给 `codex-tg` 启动的 live/poll App Server 子进程；父进程、
daemon 和 Windows Codex 客户端不会被修改。新安装由 `ctr-go init` 和 macOS
`service install` 默认写入独立路径，已有配置不会被静默迁移。

```powershell
$env:CTR_GO_CODEX_HOME = "C:\Users\you\.codex-tg\codex-home"
```

### 为什么这样设计

- App Server 会持有 thread writer，并维护 session、SQLite、锁和缓存；两个客户端共享这些
  可变文件容易产生写入冲突、锁竞争和状态漂移。
- Desktop 与 Telegram 可以独立启动、修复、升级和清理；一端崩溃或迁移数据库时不会阻塞
  或损坏另一端。
- `/threads` 的含义保持确定：它展示 Telegram runtime 真正能够继续的会话，而不是一个
  看得见却无法安全写入的 Desktop 历史列表。
- 能力与长期偏好仍然可以一致，因此隔离 runtime 不会变成一套完全陌生的 Codex 环境。

### 共享边界

可以通过明确的文件系统链接共享 `skills`、`plugins`、`packages`、`AGENTS.md` 和经过审查的
全局配置。若安装了本机 `shared-memory` Skill，两端还可以使用
`~/.codex-shared/memory/memory.sqlite`（或 `CODEX_SHARED_MEMORY_PATH`）共享用户明确批准的
稳定事实、偏好和协作约定。

`CTR_GO_AUTOMATIONS_DIR` 不再是共享边界。它属于 codex-tg，受限的本地 MCP 工具负责管理定义，
daemon 负责领取时间槽，TG 私有 App Server 负责执行。这样设计是因为 Desktop 的 Scheduled
tasks 由 Desktop 宿主注册和管理：单纯写入其目录并不能可靠触发宿主调度，也无法把结果路由回
Telegram。彻底隔离定义和执行后，任务是否运行只取决于 codex-tg 自身，Desktop 升级、退出或
重载任务都不会影响 TG，也消除了两个客户端竞争同一任务定义的重复执行风险。

共享记忆不等于共享聊天记录：conversation、thread/run id、临时任务状态、凭据和秘密都不应
写入该记忆库。Desktop 与 Telegram 之间也绝不能复制或链接 `sessions`、
`archived_sessions`、`state_*.sqlite`、`memories_*.sqlite`、writer lock 或运行时缓存。

`ctr-go init` 只负责写入隔离路径，不会自动复制运行时文件或创建上述共享链接；共享项应由
操作者逐项配置和审查。

项目 workspace 不属于 `CODEX_HOME` runtime 状态。两端可以有意指向同一仓库，但独立会话
并不能阻止它们同时修改同一个文件；这种情况下仍应使用 Git 分支、工作树或人工协调来避免
源码层面的冲突。

## 验证与开发

提交前的基本检查：

```powershell
go test ./...
go build -buildvcs=false ./...
```

Telegram 实际交互还需要真实 Bot API 回读验证。公开安全的 live E2E 说明位于
[`tests/live_e2e/README.md`](tests/live_e2e/README.md)，默认不会随 `go test ./...` 执行。

当前 fork 发布候选已在 Windows、Go 1.26.7 下通过完整单元测试、构建、格式与敏感信息扫描。
部分 Telegram 交互路径仍标记为需要完整 live readback，详情见
[`docs/testing/validation-notes.md`](docs/testing/validation-notes.md)。

主要目录：

- `cmd/ctr-go/`：命令行入口和 daemon 启动。
- `internal/appserver/`：Codex App Server JSON-RPC 客户端与快照规范化。
- `internal/control/`：适配器无关的控制平面接口。
- `internal/daemon/`：路由、活动卡、前台会话、线程管理和生命周期编排。
- `internal/storage/`：SQLite schema 与持久化。
- `internal/telegram/`：Telegram Bot API 传输层。
- `internal/tgformat/`：Telegram HTML/Markdown 渲染。
- `docs/`：ADR、功能 brief、wiki、验证和发布说明。

## 安全注意事项

- 不要把 Codex App Server 监听地址暴露到公网。
- 必须配置 `CTR_GO_ALLOWED_USER_IDS`，必要时同时限制 `CTR_GO_ALLOWED_CHAT_IDS`。
- 不要提交 bot token、`.env`、Telegram session、SQLite 数据库、日志、私密截图或本地会话文件。
- Telegram long polling 出现 `409 Conflict` 通常表示另一个进程正在消费同一个 bot token。
- 群组使用前应重新审查访问控制、topic 路由和消息可见范围。
- 发现安全问题时请优先使用 GitHub Security Advisory；公开 issue 中不要包含凭据或私密日志。

## 文档

- [Fork 改动与归属](FORK_NOTES.md)
- [版本说明](docs/release/v0.5.0-amadeus.1.md)
- [Telegram UX](docs/wiki/Telegram-UX.md)
- [Quickstart](docs/wiki/Quickstart.md)
- [Plan Mode](docs/wiki/Plan-Mode.md)
- [Control Plane](docs/wiki/Control-Plane.md)
- [Architecture](docs/wiki/Architecture.md)
- [Operations](docs/wiki/Operations.md)
- [Security](docs/wiki/Security.md)
- [变更日志](CHANGELOG.md)
- [回归测试地图](docs/testing/regression-map.md)
- [验证记录](docs/testing/validation-notes.md)
- [架构决策记录](docs/adr/)

## 许可证与原项目致谢

本项目继续使用 [Apache License 2.0](LICENSE)。上游版权声明保留在 [NOTICE](NOTICE)
中，fork 修改说明记录在 [FORK_NOTES.md](FORK_NOTES.md) 和 Git 历史中。

再次感谢 [`mideco-tech/codex-tg`](https://github.com/mideco-tech/codex-tg)
维护者与贡献者公开原项目。本 fork 的控制平面、App Server 集成和大量基础功能都建立在他们的工作之上。
