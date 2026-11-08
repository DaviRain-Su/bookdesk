# 书桌（bookdesk）

一个只在本机运行的画布式 EPUB 阅读器：把书放到无限画布上阅读、划线、写卡片，并在所有书之间搜索。界面为中文，浅色，尽量克制。

一张书桌可以摊开好几块画布：一块读一本书，一块做主题笔记，互不打扰。书本身是全局的，同一本书可以出现在多块画布上。

## 技术构成

- **桌面壳**：myzig（Zig 宿主 + 系统 WKWebView）。不使用 Electron、Tauri，也不额外嵌入 Chromium。
- **前端**：React + Vite，画布用 `@xyflow/react`，阅读用 `epub.js`。
- **数据**：Zig 服务 + SQLite。书文件、元数据、划线、卡片、画布布局都在本地，不使用网页 IndexedDB，也不依赖联网后端。跨书搜索用 FTS5：中文按单字 + 二元组建索引，英文按词。
- **AI**：角色对话和查词交给本机已安装的 Agent：Pi、Codex 直接驱动它们自己的 CLI（原生协议），其他 Agent 走 ACP（Agent Client Protocol）；不装 Agent 时可以退回内置的 Anthropic API。

## 环境要求

- macOS（Apple Silicon 或 Intel）
- [Zig](https://ziglang.org/) 0.17.1
- Go 1.27 及以上
- [Bun](https://bun.sh/)
- myzig 框架的本地 clone（`egoist/mygo` 的 fork），默认放在本仓库旁边的 `../mygo`。

本仓库通过 `go.mod` 的 `replace` 和 `build.zig` 的 SDK 路径指向该 clone。若 clone 放在别处，需要同时修改这两处：

- `go.mod`：`replace github.com/egoist/mygo => ../mygo`
- `build.zig`：`root_source_file = b.path("../mygo/zig/sdk/mygo.zig")`

菜单需要 mygo 的「空 submenu 视为无 submenu」修复（commit `d3159399`，含 `menu_json_test.go`）；更早的 mygo 版本里，原生菜单项点击不会触发回调。

## 安装与启动

```bash
bun install
bun run dev
```

`bun run dev` 会先 `zig build` 编译 Zig 动态库，再由 mygo 启动 Vite 开发服务器和开发版 `.app`。只调前端时可以 `bun run dev:web` 单独起 Vite。

## 打包

```bash
bun run build
```

等价于 `zig build -Doptimize=ReleaseSmall && mygo build -skip-dmg .`，产物是 `build/` 下的 `.app`（开发用，未签名分发）。

## 数据目录

```
~/Library/Application Support/书桌/
├── bookdesk.db            # SQLite：书元数据、划线、卡片、人物卡、设置、画布布局
├── books/<id>/book.epub   # 导入时拷贝的书文件
├── images/                # 图片卡用到的图片
└── agent/                 # 外部 Agent 的工作目录（空）
```

旧版本（「阅读白板」）的数据目录和 `reading-canvas.db` 会在第一次启动时自动搬到新位置，书和笔记不会丢。

API Key 不在这个目录里：它由 myzig 的 `Secrets` 存进 macOS 钥匙串。

导入时会把 EPUB 复制进数据目录，原文件移动或删除不影响阅读。

## 如何重置

退出应用后删除整个数据目录即可：

```bash
rm -rf ~/Library/Application\ Support/书桌
```

这会清空所有书、划线、卡片和画布布局。

## 功能

- 导入 EPUB：顶栏按钮、把 `.epub` 拖进窗口，或 File 菜单「添加书籍…」（⌘O）。
- 无限画布：多本书并排、自由缩放平移，文字卡、图片卡、卡片间连线。
- 多画布：顶栏点画布名字切换。可以新建、重命名、删除；每块画布有自己的节点、连线和视口，互不影响。
- 阅读与划线：在书页里划选文字，生成高亮与引用卡；点击已有划线可以生成卡片或删除。
- 摘录：顶栏「摘录」按钮打开右侧面板，按书分组列出所有画布上的划线；点击一条跳回书里的位置，书不在当前画布时会自动添加，也可以在这里删除单条摘录。
- 他书卡片：选中一本书时，来自其他书的摘录卡会变成蓝色，和「这本书」的卡片区分开。
- 搜索：⌘F 当前书内搜索，⌘⇧F 跨书搜索；结果按书分组，点击跳到对应上下文并临时高亮。书不在当前画布上时会自动添加。
- 人物卡：划选书中的人名，点「人物」在书旁生成一张人物卡；双击可以改名字和简介。
- 角色对话：人物卡上的「和某某对话」打开右侧面板。回答可以流式出现，也可以中途点停止。面板里的「允许后续章节」默认关闭。
- 查词：划选英文单词或短语，点「查词」，结果出现在左下角。
- 设置：顶栏齿轮按钮。选 AI 引擎（外部 Agent 或内置 API）、从本机检测到的 Agent 里点选一个、检测连接。内置 API 的 Key 存钥匙串。
- File 菜单：添加书籍…、关闭窗口、退出。

## AI 配置

### 外部 Agent（默认）

角色对话和查词都交给本机已安装的 Agent。Pi 和 Codex 用它们自己的 CLI（原生协议，每次提问启动一次，不需要适配器）；其他 Agent 走 [Agent Client Protocol](https://agentclientprotocol.com/)，应用启动一个常驻子进程，用 JSON-RPC 2.0 通过 stdin/stdout 通信。换 Agent 不用改这个应用。

1. 点顶栏齿轮按钮，引擎选「外部 Agent」。
2. 设置页会检测本机装了哪些已知 Agent（Pi、Claude Code、Codex、Cursor、Gemini CLI、OpenCode、Devin、Grok、goose、Kimi、Qwen Code），点一个就能切换，不用输命令。PATH 里有可执行文件的直接可用；标「需适配器」的表示 CLI 已装、ACP 通过官方适配器转接（首次运行由 npx 拉取）；标「需下载」的也是第一次运行时由 npx 拉取。列表里没有的 Agent 可以在「自定义命令」里填。
3. 点「检测连接」。第一次运行前，先在它自己的终端里登录（例如先运行一次 `pi` 或 `codex`）。
4. Agent 的 Key 由它自己管理，不经过这个应用。

命令在登录 shell（`/bin/zsh -l -c`）里执行。应用第一次启动 Agent 前，会先问一次真正的登录 shell 要 PATH（包括 `.zshrc` 里由 nvm、fnm、mise 等加进去的目录），所以从 Finder 启动也能找到 `npx`、`agent` 这些命令；设置页的 Agent 列表也按这份 PATH 检测。Agent 的工作目录是数据目录下的 `agent/`。应用不声明文件系统与终端能力，Agent 拿不到你的书文件，需要权限的操作会被自动拒绝（优先回 `reject_once`，让 Agent 换个方式继续，而不是中止整轮）。

**Pi 和 Codex（原生）**：应用直接调用已安装的 `pi` / `codex`，不经过 ACP 适配器。

- Pi：`pi -p --mode json --no-tools --no-themes`，按行读 JSON 事件，回答逐字流式出现。会话 id 由应用生成，下一轮用 `--session-id` 续接。
- Codex：`codex exec --json`（续接用 `codex exec resume <thread_id>`），回答整段出现。线程 id 从第一行 `thread.started` 里取，存进本地设置。
- 两者都以 `--no-tools` / `sandbox_mode="read-only"` 运行，只能读、不能改你的文件。

**其他 Agent（ACP）**：如果 Agent 在 `initialize` 里声明支持 `sessionCapabilities.systemPrompt`，人物设定会作为会话的 system prompt 发送；否则会拼在每次提问的前面。同一个角色复用同一个 ACP 会话。

会话 id 会存进本地设置：应用重启后，下一次提问会接着上次的会话（ACP 用 `session/load`，Pi 用 `--session-id`，Codex 用 `exec resume`）。恢复失败（Agent 那边已经删掉这个会话）时会自动新建一个。查词不保存会话。

对话面板右上角的橡皮按钮可以「清除记忆」：删掉这个角色在本地保存的会话 id，并结束当前 Agent 里对应的会话，下一次提问会开一个全新的会话。点一次进入确认态，再点「确认清除」才执行。已经发给 Agent 的旧对话存在 Agent 自己的存储里，这个应用不会去删（也删不到）。

### 内置 API（回退）

不装 Agent 时，可以切到「内置 API」，直接调用 Anthropic Messages API：

1. 填入 API Key（`sk-ant-…`），点保存。Key 直接写入 macOS 钥匙串，不进数据库、不进仓库。
2. 模型默认 `claude-opus-5-5`，API 地址默认 `https://api.anthropic.com`；用代理或别的模型时在这里改。

### 发送范围

发送范围是硬性约束，不是提示词约定：

- 关闭「允许后续章节」时，只发送**当前可见页**的文字，以及**当前阅读位置之前**的章节里提到这个人物的片段（最多 4 段）。
- 打开后才会检索全书。
- 打开一本书不会自动生成摘要、人物列表或关系图；只有你主动提问时才发请求。

## 许可证

MIT，见 [LICENSE](LICENSE)。

## 版权与安全

- 只导入你自己拥有的 EPUB：你购买的、你有权使用的、公版书，或你自己生成的学习材料。
- 不破解 DRM，不下载盗版，也不拆微信读书、Kindle 等封闭客户端里的书。带 DRM 的书会明确提示不支持。
- 所有数据留在本机：不联网、不上传，也没有账号或云同步。仓库里不包含任何 `.epub`。
- AI 是可选功能，只有你主动提问时才会把上面说的那部分文字发给你配置的 Agent 或模型服务。内置 API 的 Key 存在 myzig 的 `Secrets`（Keychain），不写进仓库或明文配置文件；外部 Agent 的 Key 由 Agent 自己管理。
- 外部 Agent 以子进程运行，应用只给它一个空的工作目录。Pi / Codex 以无工具、只读沙箱模式启动；ACP Agent 不声明文件、终端和 MCP 能力，权限请求一律拒绝。

## 已知限制

- macOS WKWebView 的沙箱 iframe 中，epub.js 的选区事件不会冒泡到父页面，划选改用轮询实现。已有划线的点击由父页面的 SVG 覆盖层接收；代价是划选不能从一段已划线的文字上起手，需要从旁边开始。
- 目录页等非正文 HTML 也会进入索引，跨书搜索可能命中目录条目。
- 只支持 EPUB，不支持带 DRM 的书。
- 对话记录只保留在本次运行内，退出应用后不保存。角色对话的会话 id 存在本地设置里（ACP 用 `session/load`，Pi 用 `--session-id`，Codex 用 `exec resume`），重启后会接着上次的对话；对话内容存在 Agent 自己的存储里，不在这个应用里。「清除记忆」只清掉本地会话 id 和当前 Agent 里的会话，Agent 存储里的旧对话文件不会被删除。
- 查词目前只对英文选区显示；中文选区不会出现「查词」按钮。
- 查词用划选触发。沙箱 iframe 里拿不到悬停事件，所以不做悬停查词。
- 外部 Agent 自带 system prompt。即使应用把人物设定作为 system prompt 发过去，Agent 自己的指令仍可能稀释角色口吻。
- 同一时间只跑一轮对话。Pi 和 Codex 每次提问启动一个进程、结束就退出；其他 Agent 常驻，换 Agent 命令后下一次提问会重启进程。
- 点停止后，原生 Agent（Pi / Codex）会在 3 秒内结束进程；ACP Agent 若 12 秒内还不结束这一轮，应用也会结束它的进程。下一次提问会重新启动，会话仍按上面说的方式续接。
- 如果自定义命令用的是 Pi 的 ACP 适配器（`pi-acp`），它默认会在第一次回答前附带一段启动信息（版本、Skills、Extensions）。应用会在 Agent 工作目录里放一份 `quietStartup: true` 的项目级 Pi 设置把它关掉，不改用户自己的 `~/.pi` 配置。直接用原生 `pi` 时没有这个问题。
