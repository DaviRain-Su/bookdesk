const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const db = @import("db.zig");
const types = @import("types.zig");

pub fn save(ctx: mygo.CallContext, input: types.SaveCharacterInput) !types.Character {
    const name = std.mem.trim(u8, input.name, " \t\r\n");
    if (name.len == 0) return error.NameRequired;
    const id = input.id orelse try util.newId(ctx.allocator, "char");
    const character = types.Character{
        .id = id,
        .bookId = input.bookId,
        .name = name,
        .description = input.description,
        .imageRelPath = input.imageRelPath,
        .createdAt = util.nowSeconds(),
    };
    try db.upsertCharacter(character);
    return character;
}

pub fn list(ctx: mygo.CallContext, book_id: ?[]const u8) ![]types.Character {
    return db.listCharacters(ctx.allocator, book_id);
}

pub fn delete(id: []const u8) !void {
    try db.deleteCharacter(id);
}
