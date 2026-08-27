# codex-tg：通过 Telegram 管理本地 Codex 会话

`codex-tg` 是一个用 Go 编写的本地 Codex 控制平面。它在本机连接
OpenAI Codex App Server，把 Telegram 变成查看任务进度、切换会话、
发送后续指令、处理 Plan 输入、审批操作和接收最终结果的远程控制入口。

当前 fork 版本：[`v0.5.0-amadeus.1`](https://github.com/Amadeus1P048596/codex-tg/releases/tag/v0.5.0-amadeus.1)，
基于上游 `v0.5.0`，目前以源码形式发布。

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
- 原始工具调用和输出仍可通过 Details、Tools file 与完整日志按需查看。

卡片开头的 emoji 用来标识会话，不表示状态；状态由 `处理中`、`需要输入`、
`已完成`、`失败` 和 `已取消` 等文字明确表达。

### 前台会话、首页与收件箱

每个 Telegram chat/topic 只有一个前台 Codex 会话：

- `/home`：打开会话首页，查看当前会话和后台待处理数量。
- `/current`：确认当前前台会话及其状态。
- `/threads`：以可点击标题列出当前 Telegram App Server runtime 中的真实会话。
- `/inbox`：保存后台已完成、失败、取消或需要输入的会话，重启后仍然存在。
- 切换会话时会清理旧的非终态卡片，并显示新前台会话的当前状态。
- 后台任务的过程更新保持静默，只在终态或需要输入时提供一次“切换至该会话”入口。

`/threads` 不会把隔离的 Codex Desktop 历史缓存伪装成当前 runtime 中可以操作的会话。

### 会话创建与管理

- `/newchat`：依次输入标题和首条提示词，在 `Documents/Codex/<日期>/<标题>` 下创建 Chat。
- `/newthread`：使用相同的“标题 → 提示词”流程创建普通会话，但不创建 Chat 文件夹。
- `/newchat <提示词>` 与 `/newthread <提示词>`：保留单行快捷形式。
- `/cancel`：取消正在等待标题或首条提示词的新建流程。
- `/title <标题>`：重命名当前会话，并防止后续自动刷新覆盖用户设置的标题。
- `/archive`：仅归档当前空闲会话，执行前需要按钮确认。
- `/unarchive`：按每页 10 条列出真实归档会话并恢复。

### Telegram 写入权交接

同一个 Codex thread 可以被多个客户端读取，但活跃的 App Server 连接会持有写入权。
本 fork 对这件事做了显式处理：

- `/show` 和观察模式只读，不会主动抢占会话。
- `/bind` 或卡片上的 `由 TG 接管` 会让 Telegram 获取并保持写入权。
- 如果 Codex Desktop 等其他客户端正在持有写入权，Telegram 会报告冲突，不会排队消息，
  也不会偷偷创建并行 turn。
- `/release` 或 `释放 TG 控制` 会在所有相关会话都安全空闲时释放当前 Telegram
  live session 持有的写入权。
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
- 临时图片在 App Server 接受后删除，不会另外产生“已收到图片”卡片。

媒体组，以及在 `/newchat` 或 `/newthread` 等待输入阶段直接用图片创建会话，当前不在支持范围内。

## 工作方式

```text
Telegram
   │  Bot API：消息、按钮、图片、通知
   ▼
codex-tg Go daemon
   ├── SQLite：路由、绑定、卡片、收件箱和投递状态
   ├── Telegram adapter：渲染与输入
   └── Codex control / App Server client
                          │ 本机 stdio
                          ▼
                   codex app-server
                          │
                          ▼
                 本地 thread / turn / workspace
```

关键约束：

- Codex App Server 是 thread、turn、审批和实时事件的权威来源。
- `threadId` 是持久身份；Telegram chat/topic 只是输入和展示表面。
- SQLite 保存路由、绑定、回调、卡片、观察目标、收件箱和投递元数据。
- App Server 默认通过本机 stdio 启动，不应直接暴露到公网。

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
`~/.codex-tg/config.env`。也可以通过 `CTR_GO_CONFIG` 指定其他配置路径；显式环境变量的
优先级高于配置文件。

如果更喜欢直接使用环境变量，PowerShell 示例为：

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
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
| 首页与导航 | `/start`、`/home` | 打开当前会话首页 |
| 首页与导航 | `/current`、`/inbox` | 查看当前会话或后台待处理会话 |
| 首页与导航 | `/threads`、`/projects` | 切换 runtime 会话或从项目视图导航 |
| 新建会话 | `/newchat`、`/newthread`、`/new` | 创建 Chat、普通会话或指定项目会话 |
| 会话管理 | `/title`、`/archive`、`/unarchive`、`/cancel` | 重命名、归档、恢复或取消待输入流程 |
| 路由与输入 | `/show`、`/bind`、`/reply` | 查看、接管或向指定会话发送内容 |
| Plan Mode | `/plan`、`/reply --plan` | 在路由会话中启动 Plan Mode |
| 模型设置 | `/settings`、`/model`、`/effort` | 查看并修改 Telegram 发起任务使用的模型设置 |
| 观察与诊断 | `/observe all\|off`、`/context`、`/status` | 管理全局观察目标并查看路由/运行状态 |
| 写入权与维护 | `/release`、`/repair`、`/stop` | 释放写入权、重建 App Server 会话或停止任务 |
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
| `CTR_GO_CODEX_HOME` | 仅传给子 App Server 的隔离 `CODEX_HOME` |
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

## 隔离 Codex Desktop 与 Telegram runtime

设置 `CTR_GO_CODEX_HOME` 后，只有 `codex-tg` 启动的 App Server 子进程会收到这个
`CODEX_HOME`；父进程、daemon 和 Codex Desktop 不会被修改。

```powershell
$env:CTR_GO_CODEX_HOME = "C:\Users\you\.codex-tg\codex-home"
```

建议：

- 为 Telegram runtime 使用独立的状态数据库、thread 历史和 writer lock。
- 可以通过文件系统链接共享静态资源，例如 `skills`、plugins 和全局说明。
- 不要在 Desktop 与 Telegram home 之间复制或链接 `state_*.sqlite`、session 数据库、
  thread 历史、writer lock 或运行时缓存。

隔离模式下，`/threads` 只显示该 Telegram runtime 真正能够访问的会话，这是预期行为。

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
