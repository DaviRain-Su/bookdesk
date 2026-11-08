const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const db = @import("db.zig");
const types = @import("types.zig");

pub fn saveHighlight(ctx: mygo.CallContext, input: types.SaveHighlightInput) !types.Highlight {
    if (input.id == null) {
        if (input.location.cfi) |cfi| {
            if (try db.findHighlightByCfi(ctx.allocator, input.bookId, cfi)) |existing| {
                return existing;
            }
        }
    }
    const id = input.id orelse try util.newId(ctx.allocator, "hl");
    const h = types.Highlight{
        .id = id,
        .bookId = input.bookId,
        .location = input.location,
        .text = input.text,
        .color = input.color orelse "yellow",
        .createdAt = util.nowSeconds(),
    };
    try db.insertHighlight(h);
    return h;
}

pub fn listHighlights(ctx: mygo.CallContext, book_id: ?[]const u8) ![]types.Highlight {
    return db.listHighlights(ctx.allocator, book_id);
}

pub fn deleteHighlight(id: []const u8) !void {
    try db.deleteHighlight(id);
}

pub fn saveCard(ctx: mygo.CallContext, input: types.SaveCardInput) !types.Card {
    const id = input.id orelse try util.newId(ctx.allocator, "card");
    const now = util.nowSeconds();
    const card = types.Card{
        .id = id,
        .type = input.type,
        .title = input.title,
        .body = input.body,
        .quote = input.quote,
        .quoteLocation = input.quoteLocation,
        .imageRelPath = input.imageRelPath,
        .sourceBookId = input.sourceBookId,
        .characterName = input.characterName,
        .createdAt = now,
        .updatedAt = now,
    };
    try db.upsertCard(card);
    return card;
}

pub fn listCards(ctx: mygo.CallContext) ![]types.Card {
    return db.listCards(ctx.allocator);
}

pub fn deleteCard(id: []const u8) !void {
    try db.deleteCard(id);
}
