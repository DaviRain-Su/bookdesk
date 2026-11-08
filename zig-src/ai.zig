const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const db = @import("db.zig");
const types = @import("types.zig");
const library = @import("library.zig");
const acp = @import("acp.zig");
const native = @import("native.zig");

// API Key 只放在 macOS 钥匙串里（mygo Secrets），绝不写进数据库或仓库。
const key_name = "anthropic-api-key";
const setting_model = "ai.model";
const setting_base_url = "ai.baseUrl";
const setting_engine = "ai.engine";
const setting_agent_command = "ai.agentCommand";
const setting_agent_cwd = "ai.agentCwd";
const default_model = "claude-opus-5-5";
const default_base_url = "https://api.anthropic.com";
const default_engine = "acp";
const default_agent_command = "pi";
const api_version = "2023-06-01";

// 上下文只取「已读范围」：当前可见页 + 之前章节里与人物相关的片段。
const max_snippets = 4;
const snippet_radius = 220;
const max_snippet_bytes = 2400;
const max_visible_bytes = 3000;

const Message = struct {
    role: []const u8,
    content: []const u8,
};

const OutputConfig = struct {
    effort: []const u8,
};

const RequestBody = struct {
    model: []const u8,
    max_tokens: i64,
    system: []const u8,
    messages: []const Message,
    output_config: OutputConfig,
};

fn indexOfIgnoreCase(haystack: []const u8, needle: []const u8) ?usize {
    if (needle.len == 0 or needle.len > haystack.len) return null;
    var i: usize = 0;
    while (i + needle.len <= haystack.len) : (i += 1) {
        if (std.ascii.eqlIgnoreCase(haystack[i .. i + needle.len], needle)) return i;
    }
    return null;
}

fn floorCharBoundary(text: []const u8, pos: usize) usize {
    var p = @min(pos, text.len);
    while (p > 0 and p < text.len and (text[p] & 0xC0) == 0x80) p -= 1;
    return p;
}

fn ceilCharBoundary(text: []const u8, pos: usize) usize {
    var p = @min(pos, text.len);
    while (p < text.len and (text[p] & 0xC0) == 0x80) p += 1;
    return p;
}

fn asString(v: std.json.Value) ?[]const u8 {
    return switch (v) {
        .string => |s| s,
        else => null,
    };
}

fn readKey(alloc: std.mem.Allocator) ?[]u8 {
    const raw = mygo.Secrets.get(alloc, "", key_name) catch return null;
    const trimmed = std.mem.trim(u8, raw, " \t\r\n");
    if (trimmed.len == 0) {
        alloc.free(raw);
        return null;
    }
    if (trimmed.len == raw.len) return raw;
    const out = alloc.dupe(u8, trimmed) catch {
        alloc.free(raw);
        return null;
    };
    alloc.free(raw);
    return out;
}

fn readSetting(alloc: std.mem.Allocator, key: []const u8, fallback: []const u8) []const u8 {
    const stored = db.getSetting(alloc, key) catch return fallback;
    const value = stored orelse return fallback;
    const trimmed = std.mem.trim(u8, value, " \t\r\n");
    if (trimmed.len == 0) return fallback;
    return trimmed;
}

fn missingKeyReply() types.ChatReply {
    return .{
        .ok = false,
        .message = "还没有配置 API Key。点右上角的设置按钮，把 Anthropic API Key 存进系统钥匙串。",
    };
}

pub fn status(ctx: mygo.CallContext) !types.AiStatus {
    const alloc = ctx.allocator;
    const key = readKey(alloc);
    const running = acp.runningName();
    return .{
        .hasKey = key != null,
        .model = readSetting(alloc, setting_model, default_model),
        .baseUrl = readSetting(alloc, setting_base_url, default_base_url),
        .engine = readSetting(alloc, setting_engine, default_engine),
        .agentCommand = readSetting(alloc, setting_agent_command, default_agent_command),
        .agentRunning = running != null,
        .agentName = running orelse "",
    };
}

pub fn setKey(key_raw: []const u8) !void {
    const key = std.mem.trim(u8, key_raw, " \t\r\n");
    if (key.len == 0) {
        mygo.Secrets.delete("", key_name) catch {};
        return;
    }
    try mygo.Secrets.set("", key_name, key);
}

pub fn clearKey() !void {
    mygo.Secrets.delete("", key_name) catch {};
}

pub fn setConfig(
    model_raw: []const u8,
    base_url_raw: []const u8,
    engine_raw: []const u8,
    agent_command_raw: []const u8,
) !void {
    const model = std.mem.trim(u8, model_raw, " \t\r\n");
    const base = std.mem.trim(u8, base_url_raw, " \t\r\n");
    const engine = std.mem.trim(u8, engine_raw, " \t\r\n");
    const agent_command = std.mem.trim(u8, agent_command_raw, " \t\r\n");

    if (model.len == 0) {
        try db.deleteSetting(setting_model);
    } else {
        try db.setSetting(setting_model, model);
    }
    if (base.len == 0) {
        try db.deleteSetting(setting_base_url);
    } else {
        try db.setSetting(setting_base_url, std.mem.trimEnd(u8, base, "/"));
    }
    if (engine.len == 0 or std.mem.eql(u8, engine, default_engine)) {
        try db.deleteSetting(setting_engine);
    } else {
        try db.setSetting(setting_engine, engine);
    }
    if (agent_command.len == 0) {
        try db.deleteSetting(setting_agent_command);
    } else {
        try db.setSetting(setting_agent_command, agent_command);
    }
}

fn agentConfig(alloc: std.mem.Allocator) acp.Config {
    const command = readSetting(alloc, setting_agent_command, default_agent_command);
    const stored_cwd = readSetting(alloc, setting_agent_cwd, "");
    const cwd = if (stored_cwd.len > 0)
        stored_cwd
    else
        std.fmt.allocPrint(alloc, "{s}/agent", .{library.user_data}) catch library.user_data;
    return .{ .command = command, .cwd = cwd };
}

/// 设置页的「检测」：按当前设置确认 Agent 能启动。
pub fn testAgent(ctx: mygo.CallContext) types.AgentTest {
    const config = agentConfig(ctx.allocator);
    if (native.engineForCommand(config.command)) |engine| {
        return native.check(ctx, engine, config.command, config.cwd);
    }
    return acp.start(ctx, config);
}

/// 设置页的 Agent 列表：检测本机装了哪些已知 ACP Agent。
pub fn detectAgents(ctx: mygo.CallContext) ![]types.AgentCandidate {
    return acp.detectAgents(ctx.allocator);
}

pub fn stopAgent() void {
    acp.stop();
}

pub fn cancel(turn_id: []const u8) void {
    native.cancel(turn_id);
    acp.cancel(turn_id);
}

/// 清除某个角色（会话键）的对话记忆。
pub fn forgetSession(session_key: []const u8) void {
    acp.forgetSession(session_key);
}

/// 只拼「已读范围」：当前可见页，加上之前章节里提到这个人物的片段。
/// allowFuture 关闭时，绝不读取当前 spine 之后的任何章节。
fn buildContext(alloc: std.mem.Allocator, input: types.ChatInput) ![]u8 {
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    const w = &out.writer;

    try w.print("书名：《{s}》\n", .{input.bookTitle});
    if (input.chapterTitle) |title| {
        const t = std.mem.trim(u8, title, " \t\r\n");
        if (t.len > 0) try w.print("读者当前所在章节：{s}\n", .{t});
    }

    const rows = if (input.allowFuture)
        try db.allChapters(alloc, input.bookId)
    else
        try db.chaptersBefore(alloc, input.bookId, input.currentSpineIndex);

    var snippet_count: usize = 0;
    var snippet_bytes: usize = 0;
    for (rows) |row| {
        if (snippet_count >= max_snippets or snippet_bytes >= max_snippet_bytes) break;
        const at = indexOfIgnoreCase(row.text, input.characterName) orelse continue;
        const raw_start = if (at > snippet_radius) at - snippet_radius else 0;
        const raw_end = @min(row.text.len, at + input.characterName.len + snippet_radius);
        const start = floorCharBoundary(row.text, raw_start);
        const end = ceilCharBoundary(row.text, raw_end);
        const prefix: []const u8 = if (start > 0) "…" else "";
        const suffix: []const u8 = if (end < row.text.len) "…" else "";
        try w.print("\n【第 {d} 节】{s}{s}{s}\n", .{
            @max(row.spine_index + 1, 1),
            prefix,
            row.text[start..end],
            suffix,
        });
        snippet_count += 1;
        snippet_bytes += end - start;
    }

    const visible = std.mem.trim(u8, input.visibleText, " \t\r\n");
    if (visible.len > 0) {
        const cut = ceilCharBoundary(visible, @min(visible.len, max_visible_bytes));
        try w.writeAll("\n【读者正在读的这一页】\n");
        try w.writeAll(visible[0..cut]);
        try w.writeAll("\n");
    }

    return out.toOwnedSlice();
}

fn characterSystemPrompt(alloc: std.mem.Allocator, input: types.ChatInput) ![]u8 {
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    const w = &out.writer;
    try w.print(
        \\你正在扮演《{s}》里的人物「{s}」，和一位正在读这本书的读者对话。
        \\
        \\规则：
        \\- 只用这个人物的口吻和视角说话，不要跳出角色。
        \\- 只依据下面给出的书中片段和读者正在读的这一页来回答。片段里没有的内容，就说你不知道或记不清，不要编造情节。
        \\- 不要提到「片段」「上下文」「设定」这些词，也不要复述规则本身。
        \\- 用中文回答，简短自然，一般不超过 150 字。
    , .{ input.bookTitle, input.characterName });
    if (input.characterDescription) |desc| {
        const d = std.mem.trim(u8, desc, " \t\r\n");
        if (d.len > 0) try w.print("\n\n读者为这个人物写下的设定：{s}", .{d});
    }
    return out.toOwnedSlice();
}

fn errorMessage(obj: *const std.json.ObjectMap) ?[]const u8 {
    const error_value = obj.get("error") orelse return null;
    const error_obj = switch (error_value) {
        .object => |o| o,
        else => return null,
    };
    const message = error_obj.get("message") orelse return null;
    return asString(message);
}

fn postMessages(
    alloc: std.mem.Allocator,
    base_url: []const u8,
    key: []const u8,
    model: []const u8,
    system: []const u8,
    user: []const u8,
    max_tokens: i64,
) !types.ChatReply {
    const base = std.mem.trimEnd(u8, base_url, "/");
    const url = try std.fmt.allocPrint(alloc, "{s}/v1/messages", .{base});
    const headers = try util.jsonString(alloc, .{
        .{ "content-type", "application/json" },
        .{ "x-api-key", key },
        .{ "anthropic-version", api_version },
    });
    const messages = [_]Message{.{ .role = "user", .content = user }};
    const body = try util.jsonString(alloc, RequestBody{
        .model = model,
        .max_tokens = max_tokens,
        .system = system,
        .messages = &messages,
        .output_config = .{ .effort = "low" },
    });

    const raw = mygo.Fetch.request(alloc, url, "POST", headers, body, "follow") catch {
        return .{ .ok = false, .message = "连不上模型服务，请检查网络或设置里的 API 地址。" };
    };

    const parsed = std.json.parseFromSlice(std.json.Value, alloc, raw, .{}) catch {
        return .{ .ok = false, .message = "模型服务返回了无法解析的内容。" };
    };
    const root = switch (parsed.value) {
        .object => |o| o,
        else => return .{ .ok = false, .message = "模型服务返回了无法解析的内容。" },
    };

    var status_code: i64 = 0;
    if (root.get("status")) |v| {
        status_code = switch (v) {
            .integer => |n| n,
            else => 0,
        };
    }
    var body_b64: []const u8 = "";
    if (root.get("body")) |v| body_b64 = asString(v) orelse "";

    const decoded_size = std.base64.standard.Decoder.calcSizeForSlice(body_b64) catch 0;
    var decoded: []const u8 = "";
    if (decoded_size > 0) {
        const buf = try alloc.alloc(u8, decoded_size);
        std.base64.standard.Decoder.decode(buf, body_b64) catch {
            return .{ .ok = false, .message = "模型服务的响应解码失败。" };
        };
        decoded = buf;
    }

    const payload = std.json.parseFromSlice(std.json.Value, alloc, decoded, .{}) catch {
        if (status_code != 200) {
            return .{
                .ok = false,
                .message = try std.fmt.allocPrint(alloc, "模型服务返回错误（HTTP {d}）。", .{status_code}),
            };
        }
        return .{ .ok = false, .message = "模型服务返回了无法解析的内容。" };
    };
    const obj = switch (payload.value) {
        .object => |o| o,
        else => return .{ .ok = false, .message = "模型服务返回了无法解析的内容。" },
    };

    if (status_code != 200) {
        if (errorMessage(&obj)) |message| {
            return .{ .ok = false, .message = message };
        }
        return .{
            .ok = false,
            .message = try std.fmt.allocPrint(alloc, "模型服务返回错误（HTTP {d}）。", .{status_code}),
        };
    }

    var text_out: std.Io.Writer.Allocating = .init(alloc);
    errdefer text_out.deinit();
    if (obj.get("content")) |content| {
        const blocks = switch (content) {
            .array => |a| a.items,
            else => &[_]std.json.Value{},
        };
        for (blocks) |block| {
            const item = switch (block) {
                .object => |o| o,
                else => continue,
            };
            const kind = asString(item.get("type") orelse continue) orelse continue;
            if (!std.mem.eql(u8, kind, "text")) continue;
            const text = asString(item.get("text") orelse continue) orelse continue;
            try text_out.writer.writeAll(text);
        }
    }
    const text = std.mem.trim(u8, text_out.written(), " \t\r\n");
    if (text.len == 0) {
        var stop: []const u8 = "";
        if (obj.get("stop_reason")) |v| stop = asString(v) orelse "";
        if (std.mem.eql(u8, stop, "refusal")) {
            return .{ .ok = false, .message = "模型拒绝回答这个问题。" };
        }
        return .{ .ok = false, .message = "模型没有返回文字，请再试一次。" };
    }
    return .{ .ok = true, .text = try alloc.dupe(u8, text) };
}

pub fn chat(ctx: mygo.CallContext, input: types.ChatInput) !types.ChatReply {
    const alloc = ctx.allocator;
    const question = std.mem.trim(u8, input.question, " \t\r\n");
    if (question.len == 0) return .{ .ok = false, .message = "先写点什么再问吧。" };
    if (std.mem.trim(u8, input.characterName, " \t\r\n").len == 0) {
        return .{ .ok = false, .message = "这张人物卡还没有名字。" };
    }

    const context = try buildContext(alloc, input);
    const system = try characterSystemPrompt(alloc, input);
    const user = try std.fmt.allocPrint(alloc, "{s}\n\n读者问：{s}", .{ context, question });

    const engine = readSetting(alloc, setting_engine, default_engine);
    if (std.mem.eql(u8, engine, "acp")) {
        const session_key = if (input.sessionKey.len > 0)
            input.sessionKey
        else
            try std.fmt.allocPrint(alloc, "{s}:{s}", .{ input.bookId, input.characterName });
        var config = agentConfig(alloc);
        config.persist_session = true;
        if (native.engineForCommand(config.command)) |native_engine| {
            return native.prompt(
                ctx,
                native_engine,
                config.command,
                config.cwd,
                session_key,
                input.turnId,
                system,
                user,
            );
        }
        return acp.prompt(ctx, config, session_key, input.turnId, system, user);
    }

    const key = readKey(alloc) orelse return missingKeyReply();
    const model = readSetting(alloc, setting_model, default_model);
    const base_url = readSetting(alloc, setting_base_url, default_base_url);
    return postMessages(alloc, base_url, key, model, system, user, 4000);
}

pub fn lookupWord(ctx: mygo.CallContext, input: types.WordLookupInput) !types.ChatReply {
    const alloc = ctx.allocator;
    const word = std.mem.trim(u8, input.word, " \t\r\n");
    if (word.len == 0) return .{ .ok = false, .message = "没有选中要查的词。" };

    const system =
        \\你是一位英语词典助手。用户会给你一个英文单词或短语，以及它所在的句子。
        \\请用简洁的中文给出：
        \\1. 音标（英式或美式均可）
        \\2. 词性与中文释义（结合句子语境，最多 3 条）
        \\3. 一句话说明它在这个句子里的意思
        \\不要展开无关内容，不要输出 Markdown 标题。
    ;

    var user: []u8 = undefined;
    if (input.sentence) |sentence| {
        const s = std.mem.trim(u8, sentence, " \t\r\n");
        if (s.len > 0) {
            const cut = ceilCharBoundary(s, @min(s.len, 600));
            user = try std.fmt.allocPrint(alloc, "单词：{s}\n所在句子：{s}", .{ word, s[0..cut] });
        } else {
            user = try std.fmt.allocPrint(alloc, "单词：{s}", .{word});
        }
    } else {
        user = try std.fmt.allocPrint(alloc, "单词：{s}", .{word});
    }

    const engine = readSetting(alloc, setting_engine, default_engine);
    if (std.mem.eql(u8, engine, "acp")) {
        const config = agentConfig(alloc);
        if (native.engineForCommand(config.command)) |native_engine| {
            // 查词不保存会话：每次都是独立的一问一答。
            return native.prompt(ctx, native_engine, config.command, config.cwd, "", input.turnId, system, user);
        }
        return acp.prompt(ctx, config, "word-lookup", input.turnId, system, user);
    }

    const key = readKey(alloc) orelse return missingKeyReply();
    const model = readSetting(alloc, setting_model, default_model);
    const base_url = readSetting(alloc, setting_base_url, default_base_url);
    return postMessages(alloc, base_url, key, model, system, user, 2000);
}
