const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const types = @import("types.zig");
const db = @import("db.zig");
const acp = @import("acp.zig");

// Pi 和 Codex 有自己的原生协议，不走 ACP：
// - Pi：`pi -p --mode json`，按行输出 JSON 事件。
// - Codex：`codex exec --json`，按行输出 JSON 事件。
// 每次提问启动一个进程，结束就退出；会话 id 存在本地设置里，下一轮接着用。

pub const Engine = enum { pi, codex };

const max_line_bytes = 1024 * 1024;
const prompt_timeout_ms: i64 = 600_000;
const read_slice_ms: i32 = 500;
/// 取消后给进程收尾的时间，超过就 SIGKILL。
const stop_timeout_ms: i64 = 3_000;
/// 一轮结束后等进程自己退出的时间。
const exit_grace_ms: i64 = 3_000;
const stderr_keep_bytes = 4096;
const setting_pi_prefix = "ai.piSession";
const setting_codex_prefix = "ai.codexSession";

const page_alloc = std.heap.page_allocator;

const Run = struct {
    child: std.process.Child,
    pending: std.ArrayList(u8) = .empty,
    stderr_buf: [stderr_keep_bytes]u8 = undefined,
    stderr_len: usize = 0,
    stderr_mutex: std.Io.Mutex = .init,
    settled: bool = false,
    failed: bool = false,
    error_message: []const u8 = "",
    /// Codex 的 thread id（从 thread.started 里拿）。
    thread_id: []const u8 = "",
};

var run_mutex: std.Io.Mutex = .init;
var run_active = false;
var run_cancelled = false;
var run_pid: std.posix.pid_t = 0;
var run_turn: [96]u8 = undefined;
var run_turn_len: usize = 0;

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

fn eql(a: []const u8, b: []const u8) bool {
    return std.mem.eql(u8, a, b);
}

/// 从设置里的命令判断是不是原生引擎：命令的第一个词是 pi / codex。
pub fn engineForCommand(command: []const u8) ?Engine {
    var parts = std.mem.tokenizeAny(u8, command, " \t");
    const exe = parts.next() orelse return null;
    const base = std.fs.path.basename(exe);
    if (eql(base, "pi")) return .pi;
    if (eql(base, "codex")) return .codex;
    return null;
}

fn sessionSettingKey(alloc: std.mem.Allocator, engine: Engine, key: []const u8) ![]u8 {
    const prefix = switch (engine) {
        .pi => setting_pi_prefix,
        .codex => setting_codex_prefix,
    };
    return std.fmt.allocPrint(alloc, "{s}\x1f{s}", .{ prefix, key });
}

fn storedSessionId(alloc: std.mem.Allocator, engine: Engine, key: []const u8) ?[]const u8 {
    const setting_key = sessionSettingKey(alloc, engine, key) catch return null;
    defer alloc.free(setting_key);
    const stored = db.getSetting(alloc, setting_key) catch return null;
    const value = stored orelse return null;
    const trimmed = std.mem.trim(u8, value, " \t\r\n");
    if (trimmed.len == 0) return null;
    return trimmed;
}

fn rememberSession(alloc: std.mem.Allocator, engine: Engine, key: []const u8, id: []const u8) void {
    const setting_key = sessionSettingKey(alloc, engine, key) catch return;
    defer alloc.free(setting_key);
    db.setSetting(setting_key, id) catch {};
}

fn forgetSession(alloc: std.mem.Allocator, engine: Engine, key: []const u8) void {
    const setting_key = sessionSettingKey(alloc, engine, key) catch return;
    defer alloc.free(setting_key);
    db.deleteSetting(setting_key) catch {};
}

fn newUuid(alloc: std.mem.Allocator) ![]u8 {
    var b: [16]u8 = undefined;
    util.io.random(&b);
    b[6] = (b[6] & 0x0f) | 0x40;
    b[8] = (b[8] & 0x3f) | 0x80;
    return std.fmt.allocPrint(
        alloc,
        "{x:0>2}{x:0>2}{x:0>2}{x:0>2}-{x:0>2}{x:0>2}-{x:0>2}{x:0>2}-{x:0>2}{x:0>2}-{x:0>2}{x:0>2}{x:0>2}{x:0>2}{x:0>2}{x:0>2}",
        .{ b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15] },
    );
}

fn drainStderr(run: *Run) void {
    const file = run.child.stderr orelse return;
    var buf: [4096]u8 = undefined;
    while (true) {
        const n = std.posix.read(file.handle, &buf) catch break;
        if (n == 0) break;
        run.stderr_mutex.lockUncancelable(util.io);
        for (buf[0..n]) |c| {
            if (run.stderr_len == stderr_keep_bytes) {
                std.mem.copyForwards(
                    u8,
                    run.stderr_buf[0 .. stderr_keep_bytes - 1],
                    run.stderr_buf[1..stderr_keep_bytes],
                );
                run.stderr_len = stderr_keep_bytes - 1;
            }
            run.stderr_buf[run.stderr_len] = c;
            run.stderr_len += 1;
        }
        run.stderr_mutex.unlock(util.io);
    }
}

fn stderrTail(run: *Run, alloc: std.mem.Allocator) []const u8 {
    run.stderr_mutex.lockUncancelable(util.io);
    defer run.stderr_mutex.unlock(util.io);
    if (run.stderr_len == 0) return "";
    return alloc.dupe(u8, run.stderr_buf[0..run.stderr_len]) catch "";
}

fn readLine(run: *Run, alloc: std.mem.Allocator, timeout_ms: i32) !std.json.Value {
    while (true) {
        if (std.mem.indexOfScalar(u8, run.pending.items, '\n')) |idx| {
            const line = try alloc.dupe(u8, run.pending.items[0..idx]);
            const rest_len = run.pending.items.len - (idx + 1);
            std.mem.copyForwards(u8, run.pending.items[0..rest_len], run.pending.items[idx + 1 ..]);
            run.pending.shrinkRetainingCapacity(rest_len);
            const trimmed = std.mem.trim(u8, line, " \t\r");
            if (trimmed.len == 0) continue;
            const parsed = std.json.parseFromSlice(std.json.Value, alloc, trimmed, .{}) catch continue;
            return parsed.value;
        }
        if (run.pending.items.len > max_line_bytes) return error.LineTooLong;

        const file = run.child.stdout orelse return error.EndOfStream;
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
        try run.pending.appendSlice(page_alloc, buf[0..n]);
    }
}

fn emitUpdate(win: mygo.Window, turn_id: []const u8, kind: []const u8, text: []const u8) void {
    win.emit("acp:update", types.AcpUpdate{
        .turnId = turn_id,
        .kind = kind,
        .text = text,
    }) catch {};
}

fn handlePiLine(
    run: *Run,
    win: mygo.Window,
    turn_id: []const u8,
    text_out: *std.Io.Writer.Allocating,
    obj: std.json.ObjectMap,
) void {
    const kind = asString(obj.get("type") orelse return) orelse return;
    if (eql(kind, "message_update")) {
        const event = asObject(obj.get("assistantMessageEvent") orelse return) orelse return;
        const event_type = asString(event.get("type") orelse return) orelse return;
        if (!eql(event_type, "text_delta")) return;
        const delta = asString(event.get("delta") orelse return) orelse return;
        if (delta.len == 0) return;
        text_out.writer.writeAll(delta) catch return;
        emitUpdate(win, turn_id, "text", delta);
        return;
    }
    if (eql(kind, "tool_execution_start")) {
        const name = asString(obj.get("toolName") orelse return) orelse "工具调用";
        emitUpdate(win, turn_id, "tool", name);
        return;
    }
    if (eql(kind, "message_end")) {
        const message = asObject(obj.get("message") orelse return) orelse return;
        const role = asString(message.get("role") orelse return) orelse return;
        if (!eql(role, "assistant")) return;
        const stop = asString(message.get("stopReason") orelse return) orelse "";
        if (eql(stop, "error")) {
            run.failed = true;
            run.error_message = asString(message.get("errorMessage") orelse return) orelse "Pi 返回了错误。";
        }
        return;
    }
    if (eql(kind, "agent_settled")) run.settled = true;
}

fn handleCodexLine(
    run: *Run,
    win: mygo.Window,
    turn_id: []const u8,
    text_out: *std.Io.Writer.Allocating,
    obj: std.json.ObjectMap,
) void {
    const kind = asString(obj.get("type") orelse return) orelse return;
    if (eql(kind, "thread.started")) {
        if (asString(obj.get("thread_id") orelse return)) |id| {
            run.thread_id = page_alloc.dupe(u8, id) catch "";
        }
        return;
    }
    if (eql(kind, "item.completed")) {
        const item = asObject(obj.get("item") orelse return) orelse return;
        const item_type = asString(item.get("type") orelse return) orelse return;
        if (eql(item_type, "agent_message")) {
            const text = asString(item.get("text") orelse return) orelse return;
            if (text.len == 0) return;
            text_out.writer.writeAll(text) catch return;
            emitUpdate(win, turn_id, "text", text);
        } else if (eql(item_type, "command_execution")) {
            emitUpdate(win, turn_id, "tool", "运行命令");
        }
        return;
    }
    if (eql(kind, "turn.completed")) {
        run.settled = true;
        return;
    }
    if (eql(kind, "turn.failed")) {
        run.failed = true;
        run.settled = true;
        if (asObject(obj.get("error") orelse return)) |error_obj| {
            run.error_message = asString(error_obj.get("message") orelse return) orelse "Codex 返回了错误。";
        } else {
            run.error_message = "Codex 返回了错误。";
        }
        return;
    }
    if (eql(kind, "error")) {
        run.failed = true;
        run.error_message = asString(obj.get("message") orelse return) orelse "Codex 返回了错误。";
    }
}

fn setRunActive(turn_id: []const u8, pid: std.posix.pid_t) void {
    run_mutex.lockUncancelable(util.io);
    defer run_mutex.unlock(util.io);
    run_active = true;
    run_cancelled = false;
    run_pid = pid;
    const t = turn_id[0..@min(turn_id.len, run_turn.len)];
    @memcpy(run_turn[0..t.len], t);
    run_turn_len = t.len;
}

fn clearRunActive() void {
    run_mutex.lockUncancelable(util.io);
    defer run_mutex.unlock(util.io);
    run_active = false;
    run_cancelled = false;
    run_pid = 0;
    run_turn_len = 0;
}

fn runCancelled(turn_id: []const u8) bool {
    run_mutex.lockUncancelable(util.io);
    defer run_mutex.unlock(util.io);
    if (!run_active or !run_cancelled) return false;
    return eql(run_turn[0..run_turn_len], turn_id);
}

pub fn cancel(turn_id: []const u8) void {
    run_mutex.lockUncancelable(util.io);
    defer run_mutex.unlock(util.io);
    if (!run_active) return;
    if (!eql(run_turn[0..run_turn_len], turn_id)) return;
    run_cancelled = true;
    if (run_pid > 0) {
        std.posix.kill(-run_pid, std.posix.SIG.TERM) catch {
            std.posix.kill(run_pid, std.posix.SIG.TERM) catch {};
        };
    }
}

fn killRun(run: *Run, sig: std.posix.SIG) void {
    const pid = run.child.id orelse return;
    std.posix.kill(-pid, sig) catch {
        std.posix.kill(pid, sig) catch {};
    };
}

fn stoppedReply(alloc: std.mem.Allocator, raw_text: []const u8) types.ChatReply {
    const text = std.mem.trim(u8, raw_text, " \t\r\n");
    if (text.len == 0) return .{ .ok = false, .message = "已停止。" };
    return .{
        .ok = true,
        .text = std.fmt.allocPrint(alloc, "{s}\n\n（已停止）", .{text}) catch text,
    };
}

fn waitForExit(run: *Run, timeout_ms: i64) void {
    const deadline = util.nowMillis() + timeout_ms;
    while (util.nowMillis() < deadline) {
        const file = run.child.stdout orelse break;
        var fds = [_]std.posix.pollfd{.{
            .fd = file.handle,
            .events = std.posix.POLL.IN,
            .revents = 0,
        }};
        const ready = std.posix.poll(&fds, 100) catch break;
        if (ready == 0) continue;
        var buf: [4096]u8 = undefined;
        const n = std.posix.read(file.handle, &buf) catch break;
        if (n == 0) break;
    }
    killRun(run, std.posix.SIG.KILL);
    _ = run.child.wait(util.io) catch {};
}

/// 把命令和参数交给登录 shell 执行：`exec "$0" "$@"`，参数不经过二次转义。
fn shellArgv(alloc: std.mem.Allocator, argv: []const []const u8) ![]const []const u8 {
    var out: std.ArrayList([]const u8) = .empty;
    errdefer out.deinit(alloc);
    try out.append(alloc, "/bin/zsh");
    try out.append(alloc, "-l");
    try out.append(alloc, "-c");

    const path = acp.loginShellPath();
    var script: std.Io.Writer.Allocating = .init(alloc);
    errdefer script.deinit();
    if (path.len > 0) {
        try script.writer.writeAll("export PATH='");
        for (path) |c| {
            if (c == '\'') {
                try script.writer.writeAll("'\\''");
            } else {
                try script.writer.writeByte(c);
            }
        }
        try script.writer.writeAll("'; ");
    }
    try script.writer.writeAll("exec \"$0\" \"$@\"");
    try out.append(alloc, script.written());
    try out.appendSlice(alloc, argv);
    return out.items;
}

fn spawnError(engine: Engine) types.ChatReply {
    return switch (engine) {
        .pi => .{ .ok = false, .message = "启动 Pi 失败。请确认 `pi` 在终端里能直接运行。" },
        .codex => .{ .ok = false, .message = "启动 Codex 失败。请确认 `codex` 在终端里能直接运行。" },
    };
}

fn failureMessage(engine: Engine, run: *Run, alloc: std.mem.Allocator) []const u8 {
    if (run.error_message.len > 0) return run.error_message;
    const tail = stderrTail(run, alloc);
    const trimmed = std.mem.trim(u8, tail, " \t\r\n");
    if (trimmed.len > 0) {
        return std.fmt.allocPrint(alloc, "{s} 返回了错误：\n{s}", .{
            if (engine == .pi) "Pi" else "Codex",
            trimmed,
        }) catch "Agent 返回了错误。";
    }
    return if (engine == .pi) "Pi 返回了错误。" else "Codex 返回了错误。";
}

pub fn prompt(
    ctx: mygo.CallContext,
    engine: Engine,
    command_raw: []const u8,
    cwd: []const u8,
    session_key: []const u8,
    turn_id: []const u8,
    system_prompt: []const u8,
    user_text: []const u8,
) types.ChatReply {
    const alloc = ctx.allocator;
    const command = std.mem.trim(u8, command_raw, " \t\r\n");
    if (command.len == 0) return .{ .ok = false, .message = "还没有配置外部 Agent 的命令。" };

    var session_id: []const u8 = "";
    if (session_key.len > 0) {
        if (storedSessionId(alloc, engine, session_key)) |id| session_id = id;
    }
    if (engine == .pi and session_id.len == 0) {
        session_id = newUuid(alloc) catch "";
        if (session_key.len > 0 and session_id.len > 0) {
            rememberSession(alloc, engine, session_key, session_id);
        }
    }

    var argv: std.ArrayList([]const u8) = .empty;
    defer argv.deinit(alloc);
    var parts = std.mem.tokenizeAny(u8, command, " \t");
    const exe = parts.next() orelse return .{ .ok = false, .message = "还没有配置外部 Agent 的命令。" };
    argv.append(alloc, exe) catch return .{ .ok = false, .message = "准备命令失败。" };
    while (parts.next()) |part| {
        argv.append(alloc, part) catch return .{ .ok = false, .message = "准备命令失败。" };
    }

    const prompt_text = if (engine == .codex and system_prompt.len > 0)
        std.fmt.allocPrint(alloc, "{s}\n\n{s}", .{ system_prompt, user_text }) catch user_text
    else
        user_text;

    switch (engine) {
        .pi => {
            argv.appendSlice(alloc, &.{ "-p", "--mode", "json", "--no-tools", "--no-themes" }) catch
                return .{ .ok = false, .message = "准备命令失败。" };
            if (session_id.len > 0) {
                argv.appendSlice(alloc, &.{ "--session-id", session_id }) catch
                    return .{ .ok = false, .message = "准备命令失败。" };
            }
            if (system_prompt.len > 0) {
                argv.appendSlice(alloc, &.{ "--append-system-prompt", system_prompt }) catch
                    return .{ .ok = false, .message = "准备命令失败。" };
            }
            argv.append(alloc, prompt_text) catch return .{ .ok = false, .message = "准备命令失败。" };
        },
        .codex => {
            argv.append(alloc, "exec") catch return .{ .ok = false, .message = "准备命令失败。" };
            if (session_id.len > 0) {
                argv.appendSlice(alloc, &.{ "resume", session_id }) catch
                    return .{ .ok = false, .message = "准备命令失败。" };
            }
            argv.appendSlice(alloc, &.{ "--json", "--skip-git-repo-check", "-c", "sandbox_mode=\"read-only\"" }) catch
                return .{ .ok = false, .message = "准备命令失败。" };
            argv.append(alloc, prompt_text) catch return .{ .ok = false, .message = "准备命令失败。" };
        },
    }

    const shell_argv = shellArgv(alloc, argv.items) catch return .{ .ok = false, .message = "准备命令失败。" };
    util.ensureDir(cwd) catch {};

    const child = std.process.spawn(util.io, .{
        .argv = shell_argv,
        .cwd = .{ .path = cwd },
        .stdin = .ignore,
        .stdout = .pipe,
        .stderr = .pipe,
        .pgid = 0,
    }) catch return spawnError(engine);

    var run = Run{ .child = child, .stderr_buf = undefined };
    defer {
        run.pending.deinit(page_alloc);
        if (run.thread_id.len > 0) page_alloc.free(run.thread_id);
    }
    const stderr_thread = std.Thread.spawn(.{}, drainStderr, .{&run}) catch null;

    setRunActive(turn_id, child.id orelse 0);
    defer clearRunActive();

    var text_out: std.Io.Writer.Allocating = .init(alloc);
    defer text_out.deinit();

    const overall_deadline = util.nowMillis() + prompt_timeout_ms;
    var stop_deadline: i64 = 0;
    var cancelled = false;
    var timed_out = false;

    while (!run.settled) {
        const now = util.nowMillis();
        if (stop_deadline == 0 and runCancelled(turn_id)) {
            stop_deadline = now + stop_timeout_ms;
            killRun(&run, std.posix.SIG.TERM);
        }
        if (stop_deadline > 0 and now >= stop_deadline) {
            cancelled = true;
            killRun(&run, std.posix.SIG.KILL);
            break;
        }
        if (now >= overall_deadline) {
            timed_out = true;
            killRun(&run, std.posix.SIG.KILL);
            break;
        }
        const wait_ms: i32 = @intCast(@min(@as(i64, read_slice_ms), overall_deadline - now));
        const msg = readLine(&run, alloc, wait_ms) catch |err| switch (err) {
            error.Timeout => continue,
            error.EndOfStream => break,
            else => {
                killRun(&run, std.posix.SIG.KILL);
                break;
            },
        };
        const obj = switch (msg) {
            .object => |o| o,
            else => continue,
        };
        switch (engine) {
            .pi => handlePiLine(&run, ctx.window, turn_id, &text_out, obj),
            .codex => handleCodexLine(&run, ctx.window, turn_id, &text_out, obj),
        }
    }

    waitForExit(&run, exit_grace_ms);
    if (stderr_thread) |thread| thread.join();

    if (engine == .codex and run.thread_id.len > 0 and session_key.len > 0) {
        rememberSession(alloc, engine, session_key, run.thread_id);
    }

    if (cancelled or runCancelled(turn_id)) return stoppedReply(alloc, text_out.written());
    if (timed_out) {
        return .{ .ok = false, .message = if (engine == .pi)
            "Pi 长时间没有响应，已经停止它。"
        else
            "Codex 长时间没有响应，已经停止它。" };
    }
    // Codex 的 resume 失败（线程已被删掉）时，清掉记录，下次开新线程，
    // 免得每轮提问都卡在同一个失效的会话上。
    if (engine == .codex and session_id.len > 0 and session_key.len > 0 and run.thread_id.len == 0) {
        forgetSession(alloc, engine, session_key);
        if (run.failed) {
            const message = failureMessage(engine, &run, alloc);
            return .{
                .ok = false,
                .message = std.fmt.allocPrint(alloc, "{s}\n（上次的 Codex 会话已经失效，下一次提问会开新会话。）", .{message}) catch message,
            };
        }
        return .{ .ok = false, .message = "上次的 Codex 会话已经找不到了，下一次提问会开新会话。" };
    }
    if (run.failed) return .{ .ok = false, .message = failureMessage(engine, &run, alloc) };

    const text = std.mem.trim(u8, text_out.written(), " \t\r\n");
    if (text.len == 0) {
        return .{ .ok = false, .message = if (engine == .pi)
            "Pi 没有返回文字，请再试一次。"
        else
            "Codex 没有返回文字，请再试一次。" };
    }
    return .{ .ok = true, .text = alloc.dupe(u8, text) catch text };
}

/// 设置页的「检测连接」：跑一次 --version，确认 CLI 能启动。
pub fn check(ctx: mygo.CallContext, engine: Engine, command_raw: []const u8, cwd: []const u8) types.AgentTest {
    const alloc = ctx.allocator;
    const command = std.mem.trim(u8, command_raw, " \t\r\n");
    if (command.len == 0) return .{ .ok = false, .message = "还没有配置外部 Agent 的命令。" };

    var argv: std.ArrayList([]const u8) = .empty;
    defer argv.deinit(alloc);
    var parts = std.mem.tokenizeAny(u8, command, " \t");
    const exe = parts.next() orelse return .{ .ok = false, .message = "还没有配置外部 Agent 的命令。" };
    argv.append(alloc, exe) catch return .{ .ok = false, .message = "准备命令失败。" };
    while (parts.next()) |part| {
        argv.append(alloc, part) catch return .{ .ok = false, .message = "准备命令失败。" };
    }
    argv.append(alloc, "--version") catch return .{ .ok = false, .message = "准备命令失败。" };

    const shell_argv = shellArgv(alloc, argv.items) catch return .{ .ok = false, .message = "准备命令失败。" };
    util.ensureDir(cwd) catch {};
    const child = std.process.spawn(util.io, .{
        .argv = shell_argv,
        .cwd = .{ .path = cwd },
        .stdin = .ignore,
        .stdout = .pipe,
        .stderr = .pipe,
        .pgid = 0,
    }) catch return .{ .ok = false, .message = "启动失败。请确认命令在终端里能直接运行。" };

    var run = Run{ .child = child, .stderr_buf = undefined };
    defer run.pending.deinit(page_alloc);
    const stderr_thread = std.Thread.spawn(.{}, drainStderr, .{&run}) catch null;

    var out: std.Io.Writer.Allocating = .init(alloc);
    defer out.deinit();

    // --version 的输出是纯文本，不是 JSON，直接按字节读。
    const deadline = util.nowMillis() + 10_000;
    if (run.child.stdout) |file| {
        var buf: [4096]u8 = undefined;
        while (util.nowMillis() < deadline) {
            var fds = [_]std.posix.pollfd{.{
                .fd = file.handle,
                .events = std.posix.POLL.IN,
                .revents = 0,
            }};
            const ready = std.posix.poll(&fds, 200) catch break;
            if (ready == 0) continue;
            const n = std.posix.read(file.handle, &buf) catch break;
            if (n == 0) break;
            out.writer.writeAll(buf[0..n]) catch break;
        }
    }
    waitForExit(&run, 1_000);
    if (stderr_thread) |thread| thread.join();

    const version = std.mem.trim(u8, out.written(), " \t\r\n");
    const name = if (engine == .pi) "Pi" else "Codex";
    if (version.len == 0) {
        return .{ .ok = true, .name = name, .message = "" };
    }
    const first_line = std.mem.trim(u8, version[0..@min(version.len, 200)], " \t\r\n");
    // 复制一份：out 的缓冲区在函数返回时会被释放，返回的字符串必须独立存活。
    return .{ .ok = true, .name = name, .message = alloc.dupe(u8, first_line) catch "" };
}
