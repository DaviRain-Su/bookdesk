const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const types = @import("types.zig");
const library = @import("library.zig");
const notes = @import("notes.zig");
const board = @import("board.zig");
const search = @import("search.zig");
const characters = @import("characters.zig");
const ai = @import("ai.zig");

const AppState = struct {
    window: ?mygo.Window = null,
};

var app_state: AppState = .{};

const menu_items = [_]mygo.MenuItem{
    .{ .label = "书桌", .role = "appMenu" },
    .{
        .label = "文件",
        .role = "fileMenu",
        .submenu = &.{
            .{ .id = "file.add-book", .label = "添加书籍…", .accelerator = "Cmd+O" },
            .{ .kind = .separator },
            .{ .label = "关闭窗口", .role = "close" },
            .{ .label = "退出书桌", .role = "quit" },
        },
    },
    .{ .label = "编辑", .role = "editMenu" },
    .{ .label = "显示", .role = "viewMenu" },
    .{ .label = "窗口", .role = "windowMenu" },
};

fn onMenu(_: *AppState, id: []const u8) void {
    if (std.mem.eql(u8, id, "file.add-book")) {
        if (app_state.window) |win| {
            win.emit("app:add-book", .{}) catch {};
        }
    }
}

/// 旧版本叫「阅读白板」，数据目录和数据库文件名都跟着旧名字。
/// 第一次用新名字启动时把旧目录搬到新位置，用户的书和笔记不会丢。
fn migrateLegacyData(alloc: std.mem.Allocator) void {
    const app_data = mygo.Path.get(alloc, "appData") catch return;
    const new_dir = std.fmt.allocPrint(alloc, "{s}/书桌", .{app_data}) catch return;
    const old_dir = std.fmt.allocPrint(alloc, "{s}/阅读白板", .{app_data}) catch return;
    if (!util.fileExists(new_dir) and util.fileExists(old_dir)) {
        std.Io.Dir.renameAbsolute(old_dir, new_dir, util.io) catch {};
    }

    const old_db = std.fmt.allocPrint(alloc, "{s}/reading-canvas.db", .{new_dir}) catch return;
    const new_db = std.fmt.allocPrint(alloc, "{s}/bookdesk.db", .{new_dir}) catch return;
    if (!util.fileExists(new_db) and util.fileExists(old_db)) {
        std.Io.Dir.renameAbsolute(old_db, new_db, util.io) catch {};
    }
}

fn init() !void {
    util.initIo();
    mygo.setName("书桌");
    try mygo.Desktop.setAppMenu(*AppState, &app_state, &menu_items, onMenu);

    migrateLegacyData(std.heap.page_allocator);
    try library.initPaths(std.heap.page_allocator);

    mygo.declareEvent("library:import-progress", types.ImportProgress);
    mygo.declareEvent("acp:update", types.AcpUpdate);

    try mygo.bind("Library.pickAndImport", library.pickAndImport);
    try mygo.bind("Library.importPaths", library.importPaths);
    try mygo.bind("Library.list", library.list);
    try mygo.bind("Library.chapterText", library.chapterText);
    try mygo.bind("Library.epubURL", library.epubURL);
    try mygo.bind("Library.epubBase64", library.epubBase64);
    try mygo.bind("Library.coverURL", library.coverURL);
    try mygo.bind("Library.pickImage", library.pickImage);

    try mygo.bind("Notes.saveHighlight", notes.saveHighlight);
    try mygo.bind("Notes.listHighlights", notes.listHighlights);
    try mygo.bind("Notes.deleteHighlight", notes.deleteHighlight);
    try mygo.bind("Notes.saveCard", notes.saveCard);
    try mygo.bind("Notes.listCards", notes.listCards);
    try mygo.bind("Notes.deleteCard", notes.deleteCard);

    try mygo.bind("Board.load", board.load);
    try mygo.bind("Board.save", board.save);
    try mygo.bind("Board.list", board.list);
    try mygo.bind("Board.create", board.create);
    try mygo.bind("Board.rename", board.rename);
    try mygo.bind("Board.delete", board.delete);

    try mygo.bind("Search.query", search.query);
    try mygo.bind("Search.queryBook", search.queryBook);

    try mygo.bind("Characters.save", characters.save);
    try mygo.bind("Characters.list", characters.list);
    try mygo.bind("Characters.delete", characters.delete);

    try mygo.bind("Ai.status", ai.status);
    try mygo.bind("Ai.setKey", ai.setKey);
    try mygo.bind("Ai.clearKey", ai.clearKey);
    try mygo.bind("Ai.setConfig", ai.setConfig);
    try mygo.bind("Ai.chat", ai.chat);
    try mygo.bind("Ai.lookupWord", ai.lookupWord);
    try mygo.bind("Ai.testAgent", ai.testAgent);
    try mygo.bind("Ai.detectAgents", ai.detectAgents);
    try mygo.bind("Ai.stopAgent", ai.stopAgent);
    try mygo.bind("Ai.cancel", ai.cancel);
    try mygo.bind("Ai.forgetSession", ai.forgetSession);

    try mygo.Protocol.handle(*AppState, &app_state, "books", onProtocol);
}

fn onProtocol(_: *AppState, req_id: usize, req: mygo.ProtocolRequest) void {
    library.handleProtocol(0, req_id, req);
}

fn onReady() void {
    const win = mygo.newWindow(.{
        .title = "书桌",
        .url = "/",
        .width = 1440,
        .height = 900,
        .min_width = 960,
        .min_height = 640,
        .background_color = "#f5f4f0",
    }) catch |err| {
        std.log.err("failed to create window: {s}", .{@errorName(err)});
        mygo.quit();
        return;
    };
    app_state.window = win;
}

comptime {
    mygo.exportApp(.{ .init = init, .ready = onReady });
}
