const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const types = @import("types.zig");
const db = @import("db.zig");

// 通过 ACP（Agent Client Protocol）接入外部 Agent：
// JSON-RPC 2.0，按行分隔，走子进程的 stdin/stdout。
// 协议版本 1；client 不声明文件系统与终端能力，Agent 只能拿到我们主动发送的文字。

pub const Config = struct {
    /// 在登录 shell 里执行的命令，例如 "npx -y pi-acp"。
    command: []const u8,
    /// Agent 的工作目录，默认是数据目录下空的 agent/，避免它去翻用户的文件。
    cwd: []const u8,
    /// 是否把会话 id 存进设置，下次启动时用 session/load 恢复（角色对话用；查词不用）。
    persist_session: bool = false,
};

const protocol_version: i64 = 1;
const max_line_bytes = 1024 * 1024;
const stderr_keep_bytes = 4096;
const init_timeout_ms: i32 = 180_000;
const prompt_timeout_ms: i32 = 600_000;
/// 点停止后等 Agent 收尾的上限；超过就结束进程，不让下一次提问被卡住。
const stop_timeout_ms: i64 = 12_000;
/// 单次 poll 的等待切片，用来在等消息时也能检查取消状态。
const read_slice_ms: i32 = 500;
/// 抓登录 shell 的 PATH 时的上限。
const path_capture_timeout_ms: i64 = 5_000;
/// session/load 要回放整段历史，比普通握手慢得多。
const session_load_timeout_ms: i32 = 120_000;
/// 会话 id 存在 settings 表里，键是「前缀 + 命令 + 会话键」。
const setting_session_prefix = "ai.acpSession";

const page_alloc = std.heap.page_allocator;

const Agent = struct {
    child: std.process.Child,
    write_mutex: std.Io.Mutex = .init,
    next_id: u64 = 1,
    name: []u8 = &.{},
    version: []u8 = &.{},
    command: []u8 = &.{},
    last_error: []u8 = &.{},
    supports_system_prompt: bool = false,
    supports_load_session: bool = false,
    supports_resume_session: bool = false,
    sessions: std.StringHashMapUnmanaged([]u8) = .empty,
    alive: bool = true,
    destroyed: bool = false,
    pending: std.ArrayList(u8) = .empty,
    stderr_buf: [stderr_keep_bytes]u8 = undefined,
    stderr_len: usize = 0,
    stderr_mutex: std.Io.Mutex = .init,
};

const TurnInfo = struct {
    active: bool = false,
    /// 前端点过停止；prompt 循环据此在 stop_timeout_ms 后收尾。
    cancelled: bool = false,
    turn_id: [96]u8 = undefined,
    turn_len: usize = 0,
    session_id: [160]u8 = undefined,
    session_len: usize = 0,
};

var active_agent: ?*Agent = null;
/// 串行化「启动 / 提问」。cancel 与 stop 不走这把锁，否则会被正在进行的提问卡住。
var agent_mutex: std.Io.Mutex = .init;
var kill_mutex: std.Io.Mutex = .init;
var turn_mutex: std.Io.Mutex = .init;
/// 保护 Agent.sessions 这张表，让「清除记忆」不必等一整轮对话结束。
var sessions_mutex: std.Io.Mutex = .init;
var active_turn: TurnInfo = .{};

/// 登录 shell 的 PATH，只抓一次（成功或失败都缓存）。
var login_path_state: enum { unknown, ok, failed } = .unknown;
var login_path: []u8 = &.{};
var login_path_mutex: std.Io.Mutex = .init;
/// Agent 进程死掉后再写它的 stdin 会触发 SIGPIPE；整个进程忽略一次即可。
var sigpipe_ignored = false;

fn asString(value: std.json.Value) ?[]const u8 {
    return switch (value) {
        .string => |s| s,
        else => null,
    };
}

fn asObject(value: std.json.Value) ?std.json.ObjectMap {
    return switch (value) {
        .object => |o| o,
        else => null,
    };
}

fn jsonIdEquals(value: std.json.Value, id: u64) bool {
    return switch (value) {
        .integer => |n| n >= 0 and @as(u64, @intCast(n)) == id,
        else => false,
    };
}

fn jsonErrorMessage(value: std.json.Value) ?[]const u8 {
    const obj = asObject(value) orelse return null;
    return asString(obj.get("message") orelse return null);
}

fn setLastError(agent: *Agent, message: []const u8) void {
    if (agent.last_error.len > 0) page_alloc.free(agent.last_error);
    agent.last_error = page_alloc.dupe(u8, message) catch &.{};
}

fn stderrTail(agent: *Agent, alloc: std.mem.Allocator) []const u8 {
    agent.stderr_mutex.lockUncancelable(util.io);
    defer agent.stderr_mutex.unlock(util.io);
    if (agent.stderr_len == 0) return "";
    return alloc.dupe(u8, agent.stderr_buf[0..agent.stderr_len]) catch "";
}

fn drainStderr(agent: *Agent) void {
    const file = agent.child.stderr orelse return;
    var buf: [4096]u8 = undefined;
    while (true) {
        const n = std.posix.read(file.handle, &buf) catch break;
        if (n == 0) break;
        agent.stderr_mutex.lockUncancelable(util.io);
        for (buf[0..n]) |c| {
            if (agent.stderr_len == stderr_keep_bytes) {
                std.mem.copyForwards(
                    u8,
                    agent.stderr_buf[0 .. stderr_keep_bytes - 1],
                    agent.stderr_buf[1..stderr_keep_bytes],
                );
                agent.stderr_len = stderr_keep_bytes - 1;
            }
            agent.stderr_buf[agent.stderr_len] = c;
            agent.stderr_len += 1;
        }
        agent.stderr_mutex.unlock(util.io);
    }
}

/// 把 Agent 打到 stdout 的非 JSON 行也留进诊断尾巴，进程退出时能一起显示。
fn appendDiagnostic(agent: *Agent, line: []const u8) void {
    const text = if (line.len > stderr_keep_bytes - 1)
        line[line.len - (stderr_keep_bytes - 1) ..]
    else
        line;
    agent.stderr_mutex.lockUncancelable(util.io);
    defer agent.stderr_mutex.unlock(util.io);
    const need = text.len + 1;
    if (agent.stderr_len + need > stderr_keep_bytes) {
        const drop = agent.stderr_len + need - stderr_keep_bytes;
        std.mem.copyForwards(
            u8,
            agent.stderr_buf[0 .. agent.stderr_len - drop],
            agent.stderr_buf[drop..agent.stderr_len],
        );
        agent.stderr_len -= drop;
    }
    @memcpy(agent.stderr_buf[agent.stderr_len..][0..text.len], text);
    agent.stderr_len += text.len;
    agent.stderr_buf[agent.stderr_len] = '\n';
    agent.stderr_len += 1;
}

fn writeLine(agent: *Agent, bytes: []const u8) !void {
    agent.write_mutex.lockUncancelable(util.io);
    defer agent.write_mutex.unlock(util.io);
    const file = agent.child.stdin orelse return error.WriteFailed;
    file.writeStreamingAll(util.io, bytes) catch return error.WriteFailed;
    file.writeStreamingAll(util.io, "\n") catch return error.WriteFailed;
}

fn sendRequest(agent: *Agent, alloc: std.mem.Allocator, method: []const u8, params: []const u8) !u64 {
    const id = agent.next_id;
    agent.next_id += 1;
    const line = try std.fmt.allocPrint(
        alloc,
        "{{\"jsonrpc\":\"2.0\",\"id\":{d},\"method\":\"{s}\",\"params\":{s}}}",
        .{ id, method, params },
    );
    try writeLine(agent, line);
    return id;
}

fn sendNotification(agent: *Agent, alloc: std.mem.Allocator, method: []const u8, params: []const u8) !void {
    const line = try std.fmt.allocPrint(
        alloc,
        "{{\"jsonrpc\":\"2.0\",\"method\":\"{s}\",\"params\":{s}}}",
        .{ method, params },
    );
    try writeLine(agent, line);
}

fn sendResult(agent: *Agent, alloc: std.mem.Allocator, id: std.json.Value, result_json: []const u8) !void {
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    try out.writer.writeAll("{\"jsonrpc\":\"2.0\",\"id\":");
    try std.json.Stringify.value(id, .{}, &out.writer);
    try out.writer.writeAll(",\"result\":");
    try out.writer.writeAll(result_json);
    try out.writer.writeAll("}");
    try writeLine(agent, out.written());
}

fn sendError(agent: *Agent, alloc: std.mem.Allocator, id: std.json.Value, code: i64, message: []const u8) !void {
    const Body = struct { code: i64, message: []const u8 };
    const body = try util.jsonString(alloc, Body{ .code = code, .message = message });
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    try out.writer.writeAll("{\"jsonrpc\":\"2.0\",\"id\":");
    try std.json.Stringify.value(id, .{}, &out.writer);
    try out.writer.writeAll(",\"error\":");
    try out.writer.writeAll(body);
    try out.writer.writeAll("}");
    try writeLine(agent, out.written());
}

/// 读一条完整的 JSON-RPC 消息。跳过 shell 启动脚本打印的噪声行。
fn readMessage(agent: *Agent, alloc: std.mem.Allocator, timeout_ms: i32) !std.json.Value {
    while (true) {
        if (std.mem.indexOfScalar(u8, agent.pending.items, '\n')) |idx| {
            const line = try alloc.dupe(u8, agent.pending.items[0..idx]);
            const rest_len = agent.pending.items.len - (idx + 1);
            std.mem.copyForwards(u8, agent.pending.items[0..rest_len], agent.pending.items[idx + 1 ..]);
            agent.pending.shrinkRetainingCapacity(rest_len);

            const trimmed = std.mem.trim(u8, line, " \t\r");
            if (trimmed.len == 0) continue;
            const parsed = std.json.parseFromSlice(std.json.Value, alloc, trimmed, .{}) catch {
                appendDiagnostic(agent, trimmed);
                continue;
            };
            return parsed.value;
        }

        if (agent.pending.items.len > max_line_bytes) return error.LineTooLong;

        const file = agent.child.stdout orelse return error.EndOfStream;
        var fds = [_]std.posix.pollfd{.{
            .fd = file.handle,
            .events = std.posix.POLL.IN,
            .revents = 0,
        }};
        const ready = std.posix.poll(&fds, timeout_ms) catch return error.ReadFailed;
        if (ready == 0) return error.Timeout;

        var buf: [8192]u8 = undefined;
        const n = std.posix.read(file.handle, &buf) catch return error.EndOfStream;
        if (n == 0) return error.EndOfStream;
        try agent.pending.appendSlice(page_alloc, buf[0..n]);
    }
}

fn waitResponse(
    agent: *Agent,
    alloc: std.mem.Allocator,
    id: u64,
    timeout_ms: i32,
) !std.json.Value {
    while (true) {
        const msg = try readMessage(agent, alloc, timeout_ms);
        const obj = asObject(msg) orelse continue;

        if (obj.get("method")) |method_value| {
            const method = asString(method_value) orelse continue;
            if (obj.get("id")) |req_id| {
                // 我们没有声明 fs / terminal / elicitation 能力，一律拒绝。
                sendError(agent, alloc, req_id, -32601, "书桌不支持这个请求。") catch {};
                _ = method;
            }
            continue;
        }

        const resp_id = obj.get("id") orelse continue;
        if (!jsonIdEquals(resp_id, id)) continue;

        if (obj.get("error")) |err_value| {
            setLastError(agent, jsonErrorMessage(err_value) orelse "外部 Agent 返回了错误。");
            return error.AgentError;
        }
        return msg;
    }
}

fn initialize(agent: *Agent, alloc: std.mem.Allocator) !void {
    const params = try std.fmt.allocPrint(
        alloc,
        "{{\"protocolVersion\":{d},\"clientCapabilities\":{{\"fs\":{{\"readTextFile\":false,\"writeTextFile\":false}},\"terminal\":false}},\"clientInfo\":{{\"name\":\"bookdesk\",\"version\":\"0.1.0\"}}}}",
        .{protocol_version},
    );
    const id = try sendRequest(agent, alloc, "initialize", params);
    const msg = try waitResponse(agent, alloc, id, init_timeout_ms);
    const obj = asObject(msg) orelse return error.BadResponse;
    const result = asObject(obj.get("result") orelse return error.BadResponse) orelse return error.BadResponse;

    var name: []const u8 = "";
    var version: []const u8 = "";
    if (result.get("agentInfo")) |info| {
        if (asObject(info)) |iobj| {
            if (iobj.get("title")) |t| name = asString(t) orelse name;
            if (name.len == 0) {
                if (iobj.get("name")) |n| name = asString(n) orelse name;
            }
            if (iobj.get("version")) |v| version = asString(v) orelse version;
        }
    }
    if (name.len > 0) agent.name = try page_alloc.dupe(u8, name);
    if (version.len > 0) agent.version = try page_alloc.dupe(u8, version);

    if (result.get("agentCapabilities")) |caps| {
        if (asObject(caps)) |cobj| {
            if (cobj.get("loadSession")) |ls| {
                switch (ls) {
                    .bool => |b| agent.supports_load_session = b,
                    else => {},
                }
            }
            if (cobj.get("sessionCapabilities")) |sc| {
                if (asObject(sc)) |scobj| {
                    if (scobj.get("systemPrompt")) |sp| {
                        switch (sp) {
                            .object => agent.supports_system_prompt = true,
                            else => {},
                        }
                    }
                    if (scobj.get("resume")) |rs| {
                        switch (rs) {
                            .object => agent.supports_resume_session = true,
                            else => {},
                        }
                    }
                }
            }
        }
    }
}

/// 进程里写一个已经死掉的 Agent 的 stdin 会触发 SIGPIPE，默认直接终止进程。
/// 忽略它，写失败会变成普通错误，由调用方处理。
fn ignoreSigpipe() void {
    if (sigpipe_ignored) return;
    sigpipe_ignored = true;
    var act: std.posix.Sigaction = .{
        .handler = .{ .handler = std.posix.SIG.IGN },
        .flags = 0,
        .mask = std.mem.zeroes(std.posix.sigset_t),
    };
    std.posix.sigaction(std.posix.SIG.PIPE, &act, null);
}

/// 从真正的登录 shell 里取 PATH。nvm、fnm、mise 常把 PATH 写在 .zshrc，
/// 只跑 `zsh -l -c` 读不到；先试交互式（-i），不行再退回非交互式。
pub fn loginShellPath() []const u8 {
    login_path_mutex.lockUncancelable(util.io);
    defer login_path_mutex.unlock(util.io);
    if (login_path_state == .unknown) {
        login_path_state = .failed;
        if (captureShellPath(true)) |path| {
            login_path = path;
            login_path_state = .ok;
        } else if (captureShellPath(false)) |path| {
            login_path = path;
            login_path_state = .ok;
        }
    }
    return login_path;
}

fn captureShellPath(interactive: bool) ?[]u8 {
    const script = "printf '\\n__RC_PATH_BEGIN__%s__RC_PATH_END__\\n' \"$PATH\"";
    const argv = if (interactive)
        &[_][]const u8{ "/bin/zsh", "-lic", script }
    else
        &[_][]const u8{ "/bin/zsh", "-lc", script };
    var child = std.process.spawn(util.io, .{
        .argv = argv,
        .stdin = .ignore,
        .stdout = .pipe,
        .stderr = .ignore,
        .pgid = 0,
    }) catch return null;
    defer {
        if (child.id) |pid| {
            std.posix.kill(-pid, std.posix.SIG.KILL) catch {
                std.posix.kill(pid, std.posix.SIG.KILL) catch {};
            };
            _ = child.wait(util.io) catch {};
        }
    }

    const file = child.stdout orelse return null;
    var buf: [8192]u8 = undefined;
    var used: usize = 0;
    const deadline = util.nowMillis() + path_capture_timeout_ms;
    while (used < buf.len) {
        const now = util.nowMillis();
        if (now >= deadline) break;
        var fds = [_]std.posix.pollfd{.{
            .fd = file.handle,
            .events = std.posix.POLL.IN,
            .revents = 0,
        }};
        const wait_ms: i32 = @intCast(@min(deadline - now, 1000));
        const ready = std.posix.poll(&fds, wait_ms) catch break;
        if (ready == 0) continue;
        const n = std.posix.read(file.handle, buf[used..]) catch break;
        if (n == 0) break;
        used += n;
        if (std.mem.indexOf(u8, buf[0..used], "__RC_PATH_END__") != null) break;
    }

    const text = buf[0..used];
    const begin_at = std.mem.indexOf(u8, text, "__RC_PATH_BEGIN__") orelse return null;
    const path_start = begin_at + "__RC_PATH_BEGIN__".len;
    const end_rel = std.mem.indexOf(u8, text[path_start..], "__RC_PATH_END__") orelse return null;
    const path = std.mem.trim(u8, text[path_start .. path_start + end_rel], " \t\r\n");
    if (path.len == 0) return null;
    return page_alloc.dupe(u8, path) catch null;
}

/// 把抓到的 PATH 放到命令前面，Agent 就能找到 nvm / fnm / mise 装的可执行文件。
fn shellCommand(alloc: std.mem.Allocator, command: []const u8) ![]const u8 {
    const path = loginShellPath();
    if (path.len == 0) return command;
    var quoted: std.Io.Writer.Allocating = .init(alloc);
    errdefer quoted.deinit();
    try quoted.writer.writeByte('\'');
    for (path) |c| {
        if (c == '\'') {
            try quoted.writer.writeAll("'\\''");
        } else {
            try quoted.writer.writeByte(c);
        }
    }
    try quoted.writer.writeByte('\'');
    return std.fmt.allocPrint(alloc, "export PATH={s}; {s}", .{ quoted.written(), command });
}

/// 设置页里可以点选的已知 ACP Agent。命令来自 ACP 官方 registry
/// （cdn.agentclientprotocol.com/registry/v1/latest/registry.json）。
const KnownAgent = struct {
    id: []const u8,
    name: []const u8,
    /// PATH 里的可执行文件名，按顺序找第一个。
    bins: []const []const u8,
    /// 跟在可执行文件后面的参数。
    args: []const u8 = "",
    /// 底层 CLI 的名字：它装了但 ACP 适配器没装时，仍算已安装（走 npx 适配器）。
    cli_bins: []const []const u8 = &.{},
    /// 找不到可执行文件时，用 npx 拉取的包（空表示没有 npm 分发）。
    npx_package: []const u8 = "",
    npx_args: []const u8 = "",
    hint: []const u8 = "",
    /// 这个 CLI 有自己的原生协议，应用直接驱动它，不需要 ACP 适配器。
    native: bool = false,
};

const known_agents = [_]KnownAgent{
    .{ .id = "pi", .name = "Pi", .bins = &.{"pi"}, .hint = "npm i -g @mariozechner/pi-coding-agent", .native = true },
    .{ .id = "claude", .name = "Claude Code", .bins = &.{ "claude-code-acp", "claude-agent-acp" }, .cli_bins = &.{"claude"}, .npx_package = "@agentclientprotocol/claude-agent-acp", .hint = "npm i -g @agentclientprotocol/claude-agent-acp" },
    .{ .id = "codex", .name = "Codex", .bins = &.{"codex"}, .hint = "npm i -g @openai/codex", .native = true },
    .{ .id = "cursor", .name = "Cursor", .bins = &.{ "cursor-agent", "agent" }, .args = "acp", .hint = "安装 Cursor CLI（cursor-agent）" },
    .{ .id = "gemini", .name = "Gemini CLI", .bins = &.{"gemini"}, .args = "--acp", .npx_package = "@google/gemini-cli", .npx_args = "--acp", .hint = "npm i -g @google/gemini-cli" },
    .{ .id = "opencode", .name = "OpenCode", .bins = &.{"opencode"}, .args = "acp", .hint = "安装 OpenCode CLI" },
    .{ .id = "devin", .name = "Devin", .bins = &.{"devin"}, .args = "acp", .hint = "安装 Devin CLI" },
    .{ .id = "grok", .name = "Grok", .bins = &.{"grok"}, .args = "agent stdio", .npx_package = "@xai-official/grok", .npx_args = "agent stdio", .hint = "npm i -g @xai-official/grok" },
    .{ .id = "goose", .name = "goose", .bins = &.{"goose"}, .args = "acp", .hint = "安装 goose CLI" },
    .{ .id = "kimi", .name = "Kimi CLI", .bins = &.{"kimi"}, .args = "acp", .hint = "安装 Kimi CLI" },
    .{ .id = "qwen", .name = "Qwen Code", .bins = &.{"qwen"}, .args = "--acp --experimental-skills", .npx_package = "@qwen-code/qwen-code", .npx_args = "--acp --experimental-skills", .hint = "npm i -g @qwen-code/qwen-code" },
};

/// 登录 shell 的 PATH 抓不到时的兜底目录（Finder 启动也能找到 Homebrew 命令）。
const fallback_path = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin";

fn executableOnPath(path_env: []const u8, name: []const u8) bool {
    var it = std.mem.splitScalar(u8, path_env, ':');
    while (it.next()) |dir| {
        if (dir.len == 0) continue;
        var buf: [std.fs.max_path_bytes]u8 = undefined;
        const full = std.fmt.bufPrint(&buf, "{s}/{s}", .{ dir, name }) catch continue;
        std.Io.Dir.cwd().access(util.io, full, .{ .execute = true }) catch continue;
        return true;
    }
    return false;
}

fn firstOnPath(path_env: []const u8, names: []const []const u8) ?[]const u8 {
    for (names) |name| {
        if (executableOnPath(path_env, name)) return name;
    }
    return null;
}

/// pi-acp 会在第一轮回答前附一段启动信息（版本、Skills、Extensions），
/// 混进回答里很难看。在 Agent 的工作目录里放一份项目级 Pi 设置让它安静启动；
/// 只影响这个目录下的会话，不动用户自己的 ~/.pi 配置。
fn quietPiStartup(cwd: []const u8) void {
    const pi_dir = std.fmt.allocPrint(page_alloc, "{s}/.pi", .{cwd}) catch return;
    defer page_alloc.free(pi_dir);
    util.ensureDir(pi_dir) catch return;
    const path = std.fmt.allocPrint(page_alloc, "{s}/settings.json", .{pi_dir}) catch return;
    defer page_alloc.free(path);
    if (util.fileExists(path)) return;
    const file = std.Io.Dir.cwd().createFile(util.io, path, .{}) catch return;
    defer file.close(util.io);
    file.writeStreamingAll(util.io, "{\"quietStartup\":true}\n") catch {};
}

/// 检测本机装了哪些已知 ACP Agent：看登录 shell 的 PATH 里有没有对应可执行文件，
/// 没有就看能不能用 npx 拉取。结果按「已安装 → 需下载 → 未安装」排序。
pub fn detectAgents(alloc: std.mem.Allocator) ![]types.AgentCandidate {
    const captured = loginShellPath();
    const path_env = if (captured.len > 0) captured else fallback_path;
    const npx_ok = executableOnPath(path_env, "npx");

    var installed: std.ArrayList(types.AgentCandidate) = .empty;
    var adapted: std.ArrayList(types.AgentCandidate) = .empty;
    var downloadable: std.ArrayList(types.AgentCandidate) = .empty;
    var missing: std.ArrayList(types.AgentCandidate) = .empty;

    for (known_agents) |known| {
        if (firstOnPath(path_env, known.bins)) |bin| {
            const command = if (known.args.len > 0)
                try std.fmt.allocPrint(alloc, "{s} {s}", .{ bin, known.args })
            else
                try alloc.dupe(u8, bin);
            try installed.append(alloc, .{
                .id = known.id,
                .name = known.name,
                .command = command,
                .state = "installed",
                .detail = if (known.native) "原生 CLI，按需启动" else bin,
            });
        } else if (known.npx_package.len > 0 and npx_ok) {
            const command = if (known.npx_args.len > 0)
                try std.fmt.allocPrint(alloc, "npx -y {s} {s}", .{ known.npx_package, known.npx_args })
            else
                try std.fmt.allocPrint(alloc, "npx -y {s}", .{known.npx_package});
            // 底层 CLI 装了、只是没有 ACP 适配器：仍算已安装，适配器由 npx 拉取。
            if (firstOnPath(path_env, known.cli_bins)) |cli_bin| {
                const detail = try std.fmt.allocPrint(
                    alloc,
                    "已装 {s} · 经 {s} 适配器",
                    .{ cli_bin, known.npx_package },
                );
                try adapted.append(alloc, .{
                    .id = known.id,
                    .name = known.name,
                    .command = command,
                    .state = "adapter",
                    .detail = detail,
                });
            } else {
                try downloadable.append(alloc, .{
                    .id = known.id,
                    .name = known.name,
                    .command = command,
                    .state = "download",
                    .detail = "首次运行会用 npx 下载",
                });
            }
        } else {
            try missing.append(alloc, .{
                .id = known.id,
                .name = known.name,
                .command = "",
                .state = "missing",
                .detail = known.hint,
            });
        }
    }

    var out: std.ArrayList(types.AgentCandidate) = .empty;
    try out.appendSlice(alloc, installed.items);
    try out.appendSlice(alloc, adapted.items);
    try out.appendSlice(alloc, downloadable.items);
    try out.appendSlice(alloc, missing.items);
    return out.items;
}

fn spawnAgent(alloc: std.mem.Allocator, config: Config) !*Agent {
    const command = std.mem.trim(u8, config.command, " \t\r\n");
    if (command.len == 0) return error.NoCommand;

    ignoreSigpipe();
    util.ensureDir(config.cwd) catch {};
    if (std.mem.indexOf(u8, command, "pi-acp") != null) quietPiStartup(config.cwd);
    const script = shellCommand(alloc, command) catch command;
    const argv = [_][]const u8{ "/bin/zsh", "-l", "-c", script };
    const child = std.process.spawn(util.io, .{
        .argv = &argv,
        .cwd = .{ .path = config.cwd },
        .stdin = .pipe,
        .stdout = .pipe,
        .stderr = .pipe,
        // 让 Agent 自成一个进程组，停止时能把 npx / node 一起杀掉。
        .pgid = 0,
    }) catch return error.SpawnFailed;

    const agent = try page_alloc.create(Agent);
    agent.* = .{ .child = child, .stderr_buf = undefined };
    agent.command = try page_alloc.dupe(u8, command);

    if (std.Thread.spawn(.{}, drainStderr, .{agent})) |thread| {
        thread.detach();
    } else |_| {}

    return agent;
}

fn destroyAgent(agent: *Agent) void {
    if (agent.destroyed) return;
    agent.destroyed = true;
    agent.alive = false;
    // 直接 SIGKILL：有的 Agent 收到 SIGTERM 会卡住，stop 不能跟着卡。
    if (agent.child.id) |pid| {
        std.posix.kill(-pid, std.posix.SIG.KILL) catch {
            std.posix.kill(pid, std.posix.SIG.KILL) catch {};
        };
        _ = agent.child.wait(util.io) catch {};
    }
}

fn killAgentLocked() void {
    kill_mutex.lockUncancelable(util.io);
    defer kill_mutex.unlock(util.io);
    const agent = active_agent orelse return;
    active_agent = null;
    destroyAgent(agent);
}

/// 提问途中发现 Agent 已经死了：清掉当前指针，下一次提问重新启动。
fn retireAgent(agent: *Agent) void {
    kill_mutex.lockUncancelable(util.io);
    defer kill_mutex.unlock(util.io);
    if (active_agent == agent) active_agent = null;
    destroyAgent(agent);
}

/// out_agent 让调用方在失败时还能拿到 Agent 指针（读 stderr、看错误信息）。
fn ensureAgent(alloc: std.mem.Allocator, config: Config, out_agent: *?*Agent) !*Agent {
    out_agent.* = null;
    const command = std.mem.trim(u8, config.command, " \t\r\n");
    if (active_agent) |agent| {
        if (agent.alive and std.mem.eql(u8, agent.command, command)) {
            out_agent.* = agent;
            return agent;
        }
        killAgentLocked();
    }

    const agent = try spawnAgent(alloc, config);
    active_agent = agent;
    out_agent.* = agent;
    initialize(agent, alloc) catch |err| {
        killAgentLocked();
        return err;
    };
    return agent;
}

fn sessionSettingKey(alloc: std.mem.Allocator, command: []const u8, key: []const u8) ![]u8 {
    return std.fmt.allocPrint(alloc, "{s}\x1f{s}\x1f{s}", .{ setting_session_prefix, command, key });
}

fn storedSessionId(alloc: std.mem.Allocator, command: []const u8, key: []const u8) ?[]const u8 {
    const setting_key = sessionSettingKey(alloc, command, key) catch return null;
    const stored = db.getSetting(alloc, setting_key) catch return null;
    const value = stored orelse return null;
    const trimmed = std.mem.trim(u8, value, " \t\r\n");
    return if (trimmed.len == 0) null else trimmed;
}

fn rememberSession(alloc: std.mem.Allocator, command: []const u8, key: []const u8, session_id: []const u8) void {
    const setting_key = sessionSettingKey(alloc, command, key) catch return;
    db.setSetting(setting_key, session_id) catch {};
}

fn forgetStoredSession(alloc: std.mem.Allocator, command: []const u8, key: []const u8) void {
    const setting_key = sessionSettingKey(alloc, command, key) catch return;
    db.deleteSetting(setting_key) catch {};
}

/// 用 session/load（或 session/resume）恢复上次的会话。
/// 返回 false 表示这个会话在 Agent 那边已经不存在，可以退回 session/new。
fn restoreSession(
    agent: *Agent,
    alloc: std.mem.Allocator,
    session_id: []const u8,
    cwd: []const u8,
) !bool {
    const method = if (agent.supports_load_session) "session/load" else "session/resume";
    const sid_json = util.jsonString(alloc, session_id) catch return false;
    const cwd_json = util.jsonString(alloc, cwd) catch return false;
    const params = std.fmt.allocPrint(
        alloc,
        "{{\"sessionId\":{s},\"cwd\":{s},\"mcpServers\":[]}}",
        .{ sid_json, cwd_json },
    ) catch return false;
    const id = sendRequest(agent, alloc, method, params) catch |err| {
        if (err == error.WriteFailed) retireAgent(agent);
        return err;
    };
    // 回放的历史会以 session/update 通知的形式到来，waitResponse 直接忽略。
    _ = waitResponse(agent, alloc, id, session_load_timeout_ms) catch |err| switch (err) {
        error.AgentError => return false,
        error.Timeout => {
            retireAgent(agent);
            return error.Timeout;
        },
        else => {
            retireAgent(agent);
            return err;
        },
    };
    return true;
}

fn cachedSession(agent: *Agent, key: []const u8) ?[]const u8 {
    sessions_mutex.lockUncancelable(util.io);
    defer sessions_mutex.unlock(util.io);
    return agent.sessions.get(key);
}

fn cacheSession(agent: *Agent, key: []const u8, session_id: []const u8) !void {
    const owned_key = try page_alloc.dupe(u8, key);
    errdefer page_alloc.free(owned_key);
    const owned_session = try page_alloc.dupe(u8, session_id);
    errdefer page_alloc.free(owned_session);
    sessions_mutex.lockUncancelable(util.io);
    defer sessions_mutex.unlock(util.io);
    try agent.sessions.put(page_alloc, owned_key, owned_session);
}

fn getOrCreateSession(
    agent: *Agent,
    alloc: std.mem.Allocator,
    key: []const u8,
    system_prompt: []const u8,
    cwd: []const u8,
    persist: bool,
) ![]const u8 {
    if (cachedSession(agent, key)) |session_id| return session_id;

    if (persist and (agent.supports_load_session or agent.supports_resume_session)) {
        if (storedSessionId(alloc, agent.command, key)) |stored| {
            const restored = restoreSession(agent, alloc, stored, cwd) catch |err| {
                forgetStoredSession(alloc, agent.command, key);
                return err;
            };
            if (restored) {
                try cacheSession(agent, key, stored);
                return cachedSession(agent, key) orelse error.BadResponse;
            }
            // 会话在 Agent 那边已经不存在了，退回新建。
            forgetStoredSession(alloc, agent.command, key);
        }
    }

    const cwd_json = try util.jsonString(alloc, cwd);
    var params: std.Io.Writer.Allocating = .init(alloc);
    errdefer params.deinit();
    try params.writer.print("{{\"cwd\":{s},\"mcpServers\":[]", .{cwd_json});
    if (agent.supports_system_prompt and system_prompt.len > 0) {
        const prompt_json = try util.jsonString(alloc, system_prompt);
        try params.writer.print(
            ",\"systemPrompt\":{{\"mode\":\"append\",\"content\":[{{\"type\":\"text\",\"text\":{s}}}]}}",
            .{prompt_json},
        );
    }
    try params.writer.writeAll("}");

    const id = sendRequest(agent, alloc, "session/new", params.written()) catch |err| {
        if (err == error.WriteFailed) retireAgent(agent);
        return err;
    };
    const msg = try waitResponse(agent, alloc, id, init_timeout_ms);
    const obj = asObject(msg) orelse return error.BadResponse;
    const result = asObject(obj.get("result") orelse return error.BadResponse) orelse return error.BadResponse;
    const session_id = asString(result.get("sessionId") orelse return error.BadResponse) orelse
        return error.BadResponse;

    try cacheSession(agent, key, session_id);
    if (persist) rememberSession(alloc, agent.command, key, session_id);
    return cachedSession(agent, key) orelse error.BadResponse;
}

fn setActiveTurn(turn_id: []const u8, session_id: []const u8) void {
    turn_mutex.lockUncancelable(util.io);
    defer turn_mutex.unlock(util.io);
    active_turn = .{};
    const t = turn_id[0..@min(turn_id.len, active_turn.turn_id.len)];
    @memcpy(active_turn.turn_id[0..t.len], t);
    active_turn.turn_len = t.len;
    const s = session_id[0..@min(session_id.len, active_turn.session_id.len)];
    @memcpy(active_turn.session_id[0..s.len], s);
    active_turn.session_len = s.len;
    active_turn.active = true;
}

fn clearActiveTurn() void {
    turn_mutex.lockUncancelable(util.io);
    defer turn_mutex.unlock(util.io);
    active_turn.active = false;
}

fn handleUpdate(
    win: mygo.Window,
    turn_id: []const u8,
    session_id: []const u8,
    msg: std.json.Value,
    text_out: *std.Io.Writer.Allocating,
) void {
    const obj = asObject(msg) orelse return;
    const params = asObject(obj.get("params") orelse return) orelse return;
    // 同一个 Agent 进程里有多个会话（每个角色一个、查词一个），
    // 别把别的会话的片段混进这一轮。
    if (params.get("sessionId")) |sid_value| {
        if (asString(sid_value)) |sid| {
            if (sid.len > 0 and !std.mem.eql(u8, sid, session_id)) return;
        }
    }
    const update = asObject(params.get("update") orelse return) orelse return;
    const kind = asString(update.get("sessionUpdate") orelse return) orelse return;

    if (std.mem.eql(u8, kind, "agent_message_chunk")) {
        const content = asObject(update.get("content") orelse return) orelse return;
        const ctype = asString(content.get("type") orelse return) orelse return;
        if (!std.mem.eql(u8, ctype, "text")) return;
        const chunk = asString(content.get("text") orelse return) orelse return;
        if (chunk.len == 0) return;
        text_out.writer.writeAll(chunk) catch return;
        win.emit("acp:update", types.AcpUpdate{
            .turnId = turn_id,
            .kind = "text",
            .text = chunk,
        }) catch {};
        return;
    }

    if (std.mem.eql(u8, kind, "agent_thought_chunk")) {
        const content = asObject(update.get("content") orelse return) orelse return;
        const ctype = asString(content.get("type") orelse return) orelse return;
        if (!std.mem.eql(u8, ctype, "text")) return;
        const chunk = asString(content.get("text") orelse return) orelse return;
        if (chunk.len == 0) return;
        win.emit("acp:update", types.AcpUpdate{
            .turnId = turn_id,
            .kind = "thought",
            .text = chunk,
        }) catch {};
        return;
    }

    if (std.mem.eql(u8, kind, "tool_call")) {
        const title = asString(update.get("title") orelse return) orelse "工具调用";
        win.emit("acp:update", types.AcpUpdate{
            .turnId = turn_id,
            .kind = "tool",
            .text = title,
        }) catch {};
    }
}

fn agentErrorMessage(agent: ?*Agent, alloc: std.mem.Allocator, err: anyerror) []const u8 {
    return switch (err) {
        error.NoCommand => "还没有配置外部 Agent 的命令。打开设置，填上例如 npx -y pi-acp。",
        error.SpawnFailed => "启动外部 Agent 失败。请检查设置里的命令，确认它在终端里能直接运行。",
        error.Timeout => "外部 Agent 长时间没有响应，已经停止它。",
        error.EndOfStream, error.ReadFailed, error.WriteFailed => blk: {
            const tail = if (agent) |a| stderrTail(a, alloc) else "";
            if (tail.len > 0) {
                break :blk std.fmt.allocPrint(
                    alloc,
                    "外部 Agent 进程已退出。\n{s}",
                    .{std.mem.trim(u8, tail, " \t\r\n")},
                ) catch "外部 Agent 进程已退出。";
            }
            break :blk "外部 Agent 进程已退出。请检查设置里的命令，或先在这个 Agent 里登录。";
        },
        error.AgentError => blk: {
            if (agent) |a| {
                if (a.last_error.len > 0) break :blk a.last_error;
            }
            break :blk "外部 Agent 返回了错误。";
        },
        error.BadResponse => "外部 Agent 返回了无法理解的响应。",
        error.LineTooLong => "外部 Agent 返回了过长的消息。",
        else => "外部 Agent 出错了。",
    };
}

/// 阅读场景不需要文件、命令或终端权限，一律拒绝。
/// ACP 里回 `cancelled` 表示「这次请求被取消」，有的 Agent 会因此中止整轮；
/// 所以优先选 reject_once / reject_always，让 Agent 换个方式继续。
fn permissionReply(
    alloc: std.mem.Allocator,
    params_value: ?std.json.Value,
    session_id: []const u8,
) []const u8 {
    const cancelled = "{\"outcome\":{\"outcome\":\"cancelled\"}}";
    const params = asObject(params_value orelse return cancelled) orelse return cancelled;
    if (params.get("sessionId")) |sid_value| {
        if (asString(sid_value)) |sid| {
            if (sid.len > 0 and !std.mem.eql(u8, sid, session_id)) return cancelled;
        }
    }
    const options = params.get("options") orelse return cancelled;
    const arr = switch (options) {
        .array => |a| a,
        else => return cancelled,
    };
    var reject_once: ?[]const u8 = null;
    var reject_always: ?[]const u8 = null;
    for (arr.items) |item| {
        const option = asObject(item) orelse continue;
        const kind = asString(option.get("kind") orelse continue) orelse continue;
        const option_id = asString(option.get("optionId") orelse continue) orelse continue;
        if (reject_once == null and std.mem.eql(u8, kind, "reject_once")) reject_once = option_id;
        if (reject_always == null and std.mem.eql(u8, kind, "reject_always")) reject_always = option_id;
    }
    const chosen = reject_once orelse reject_always orelse return cancelled;
    const id_json = util.jsonString(alloc, chosen) catch return cancelled;
    return std.fmt.allocPrint(
        alloc,
        "{{\"outcome\":{{\"outcome\":\"selected\",\"optionId\":{s}}}}}",
        .{id_json},
    ) catch cancelled;
}

fn turnCancelled() bool {
    turn_mutex.lockUncancelable(util.io);
    defer turn_mutex.unlock(util.io);
    return active_turn.active and active_turn.cancelled;
}

fn stoppedReply(alloc: std.mem.Allocator, raw_text: []const u8) types.ChatReply {
    const text = std.mem.trim(u8, raw_text, " \t\r\n");
    if (text.len == 0) return .{ .ok = false, .message = "已停止。" };
    return .{
        .ok = true,
        .text = std.fmt.allocPrint(alloc, "{s}\n\n（已停止）", .{text}) catch text,
    };
}

/// 发送一次提问并等待这一轮结束。流式片段通过 acp:update 事件发给前端。
pub fn prompt(
    ctx: mygo.CallContext,
    config: Config,
    session_key: []const u8,
    turn_id: []const u8,
    system_prompt: []const u8,
    user_text: []const u8,
) types.ChatReply {
    const alloc = ctx.allocator;
    agent_mutex.lockUncancelable(util.io);
    defer agent_mutex.unlock(util.io);

    var failed_agent: ?*Agent = null;
    const agent = ensureAgent(alloc, config, &failed_agent) catch |err| {
        return .{ .ok = false, .message = agentErrorMessage(failed_agent, alloc, err) };
    };
    const session_id = getOrCreateSession(agent, alloc, session_key, system_prompt, config.cwd, config.persist_session) catch |err| {
        return .{ .ok = false, .message = agentErrorMessage(agent, alloc, err) };
    };

    const prompt_text = if (agent.supports_system_prompt or system_prompt.len == 0)
        user_text
    else
        std.fmt.allocPrint(alloc, "{s}\n\n{s}", .{ system_prompt, user_text }) catch user_text;

    const session_json = util.jsonString(alloc, session_id) catch "";
    const text_json = util.jsonString(alloc, prompt_text) catch "";
    const params = std.fmt.allocPrint(
        alloc,
        "{{\"sessionId\":{s},\"prompt\":[{{\"type\":\"text\",\"text\":{s}}}]}}",
        .{ session_json, text_json },
    ) catch return .{ .ok = false, .message = "准备提问内容失败。" };

    setActiveTurn(turn_id, session_id);
    defer clearActiveTurn();

    const id = sendRequest(agent, alloc, "session/prompt", params) catch |err| {
        if (err == error.WriteFailed) retireAgent(agent);
        return .{ .ok = false, .message = agentErrorMessage(agent, alloc, err) };
    };

    var text_out: std.Io.Writer.Allocating = .init(alloc);
    defer text_out.deinit();
    var stop_reason: []const u8 = "";

    const overall_deadline = util.nowMillis() + prompt_timeout_ms;
    // 点过停止后，Agent 迟迟不结束的兜底时间。
    var stop_deadline: i64 = 0;

    while (true) {
        const now = util.nowMillis();
        // 取消可能在消息之间到达；每轮都看一眼，别等下一次读超时才记时。
        if (stop_deadline == 0 and turnCancelled()) {
            stop_deadline = now + stop_timeout_ms;
        }
        if (stop_deadline > 0 and now >= stop_deadline) {
            // Agent 没有在取消后收尾：结束它，别让下一次提问被卡住。
            retireAgent(agent);
            return stoppedReply(alloc, text_out.written());
        }
        if (now >= overall_deadline) {
            retireAgent(agent);
            return .{ .ok = false, .message = agentErrorMessage(agent, alloc, error.Timeout) };
        }
        const wait_ms: i32 = @intCast(@min(@as(i64, read_slice_ms), overall_deadline - now));

        const msg = readMessage(agent, alloc, wait_ms) catch |err| {
            if (err == error.Timeout) continue;
            if (err == error.EndOfStream or err == error.ReadFailed or err == error.LineTooLong) {
                retireAgent(agent);
            }
            return .{ .ok = false, .message = agentErrorMessage(agent, alloc, err) };
        };
        const obj = asObject(msg) orelse continue;

        if (obj.get("method")) |method_value| {
            const method = asString(method_value) orelse continue;
            if (std.mem.eql(u8, method, "session/update")) {
                handleUpdate(ctx.window, turn_id, session_id, msg, &text_out);
                continue;
            }
            if (std.mem.eql(u8, method, "session/request_permission")) {
                if (obj.get("id")) |req_id| {
                    const reply = permissionReply(alloc, obj.get("params"), session_id);
                    sendResult(agent, alloc, req_id, reply) catch {};
                }
                continue;
            }
            if (obj.get("id")) |req_id| {
                sendError(agent, alloc, req_id, -32601, "书桌不支持这个请求。") catch {};
            }
            continue;
        }

        const resp_id = obj.get("id") orelse continue;
        if (!jsonIdEquals(resp_id, id)) continue;

        if (obj.get("error")) |err_value| {
            const message = jsonErrorMessage(err_value) orelse "外部 Agent 返回了错误。";
            return .{ .ok = false, .message = alloc.dupe(u8, message) catch message };
        }
        if (obj.get("result")) |result| {
            if (asObject(result)) |robj| {
                if (robj.get("stopReason")) |sr| stop_reason = asString(sr) orelse stop_reason;
            }
        }
        break;
    }

    const text = std.mem.trim(u8, text_out.written(), " \t\r\n");
    if (std.mem.eql(u8, stop_reason, "cancelled")) {
        return stoppedReply(alloc, text);
    }
    if (text.len == 0) {
        if (std.mem.eql(u8, stop_reason, "refusal")) {
            return .{ .ok = false, .message = "外部 Agent 拒绝回答这个问题。" };
        }
        return .{ .ok = false, .message = "外部 Agent 没有返回文字。" };
    }
    if (std.mem.eql(u8, stop_reason, "max_tokens")) {
        return .{
            .ok = true,
            .text = std.fmt.allocPrint(alloc, "{s}\n\n（回答被截断）", .{text}) catch text,
        };
    }
    return .{ .ok = true, .text = alloc.dupe(u8, text) catch text };
}

/// 前端点「停止」：只发 session/cancel，不抢 agent_mutex。
pub fn cancel(turn_id: []const u8) void {
    const agent = active_agent orelse return;
    if (!agent.alive) return;

    turn_mutex.lockUncancelable(util.io);
    var session_buf: [160]u8 = undefined;
    var session_len: usize = 0;
    var matches = false;
    if (active_turn.active) {
        const active_id = active_turn.turn_id[0..active_turn.turn_len];
        matches = turn_id.len == 0 or std.mem.eql(u8, active_id, turn_id);
        if (matches) {
            active_turn.cancelled = true;
            session_len = active_turn.session_len;
            @memcpy(session_buf[0..session_len], active_turn.session_id[0..session_len]);
        }
    }
    turn_mutex.unlock(util.io);
    if (!matches) return;

    const alloc = page_alloc;
    const session_json = util.jsonString(alloc, session_buf[0..session_len]) catch return;
    defer alloc.free(session_json);
    const params = std.fmt.allocPrint(alloc, "{{\"sessionId\":{s}}}", .{session_json}) catch return;
    defer alloc.free(params);
    sendNotification(agent, alloc, "session/cancel", params) catch {};
}

/// 清除一个会话键的记忆：本地存的会话 id（所有 Agent 命令下的），以及内存里缓存的会话。
/// 下一次提问会新建会话；Agent 自己存的旧对话不在这个应用的管理范围。
pub fn forgetSession(session_key: []const u8) void {
    const key = std.mem.trim(u8, session_key, " \t\r\n");
    if (key.len == 0) return;

    // 键是「前缀 \x1f 命令 \x1f 会话键」，用后缀匹配删掉所有命令下的记录。
    var pattern: std.Io.Writer.Allocating = .init(page_alloc);
    defer pattern.deinit();
    pattern.writer.writeAll("%") catch return;
    pattern.writer.writeByte('\x1f') catch return;
    for (key) |c| {
        switch (c) {
            '%' => pattern.writer.writeAll("\\%") catch return,
            '_' => pattern.writer.writeAll("\\_") catch return,
            '\\' => pattern.writer.writeAll("\\\\") catch return,
            else => pattern.writer.writeByte(c) catch return,
        }
    }
    db.deleteSettingsLike(pattern.written()) catch {};

    sessions_mutex.lockUncancelable(util.io);
    defer sessions_mutex.unlock(util.io);
    if (active_agent) |agent| {
        if (agent.sessions.fetchRemove(key)) |kv| {
            page_alloc.free(kv.key);
            page_alloc.free(kv.value);
        }
    }
}

/// 设置页的「检测」：启动并初始化 Agent，返回它的名字。
pub fn start(ctx: mygo.CallContext, config: Config) types.AgentTest {
    const alloc = ctx.allocator;
    agent_mutex.lockUncancelable(util.io);
    defer agent_mutex.unlock(util.io);

    killAgentLocked();
    const agent = spawnAgent(alloc, config) catch |err| {
        return .{ .ok = false, .message = agentErrorMessage(null, alloc, err) };
    };
    active_agent = agent;
    initialize(agent, alloc) catch |err| {
        const message = agentErrorMessage(agent, alloc, err);
        killAgentLocked();
        return .{ .ok = false, .message = alloc.dupe(u8, message) catch "外部 Agent 初始化失败。" };
    };
    return .{ .ok = true, .name = alloc.dupe(u8, agent.name) catch "" };
}

pub fn stop() void {
    killAgentLocked();
}

pub fn runningName() ?[]const u8 {
    const agent = active_agent orelse return null;
    if (!agent.alive) return null;
    return agent.name;
}
