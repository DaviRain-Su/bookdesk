const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const db = @import("db.zig");
const types = @import("types.zig");

pub fn load(ctx: mygo.CallContext, id: []const u8) !?types.Board {
    return db.getBoard(ctx.allocator, id);
}

pub fn save(board: types.Board) !void {
    var b = board;
    b.updatedAt = util.nowSeconds();
    try db.saveBoard(b);
}

pub fn list(ctx: mygo.CallContext) ![]types.BoardSummary {
    return db.listBoards(ctx.allocator);
}

pub fn create(ctx: mygo.CallContext, name: []const u8) !types.Board {
    const id = try util.newId(ctx.allocator, "board");
    const trimmed = std.mem.trim(u8, name, " \t\r\n");
    const b = types.Board{
        .id = id,
        .name = if (trimmed.len > 0) trimmed else "新画布",
        .viewportJson = "{\"x\":0,\"y\":0,\"zoom\":1}",
        .nodesJson = "[]",
        .edgesJson = "[]",
        .updatedAt = util.nowSeconds(),
    };
    try db.saveBoard(b);
    return b;
}

pub fn rename(ctx: mygo.CallContext, id: []const u8, name: []const u8) !types.BoardSummary {
    _ = ctx;
    const trimmed = std.mem.trim(u8, name, " \t\r\n");
    if (trimmed.len == 0) return error.EmptyName;
    const updated_at = util.nowSeconds();
    try db.renameBoard(id, trimmed, updated_at);
    return .{ .id = id, .name = trimmed, .updatedAt = updated_at };
}

pub fn delete(ctx: mygo.CallContext, id: []const u8) !void {
    _ = ctx;
    try db.deleteBoard(id);
}
