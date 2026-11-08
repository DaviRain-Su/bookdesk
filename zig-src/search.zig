const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const db = @import("db.zig");
const types = @import("types.zig");

const max_hits = 100;
const max_hits_per_book = 20;
const max_hits_per_chapter = 6;
const context_bytes = 36;

fn findIgnoreCase(haystack: []const u8, needle: []const u8, from: usize) ?usize {
    if (needle.len == 0 or from >= haystack.len) return null;
    if (needle.len > haystack.len - from) return null;
    var i = from;
    while (i + needle.len <= haystack.len) : (i += 1) {
        if (std.ascii.eqlIgnoreCase(haystack[i .. i + needle.len], needle)) return i;
    }
    return null;
}

fn floorCharBoundary(text: []const u8, pos: usize) usize {
    var p = @min(pos, text.len);
    while (p > 0 and (text[p] & 0xC0) == 0x80) p -= 1;
    return p;
}

fn ceilCharBoundary(text: []const u8, pos: usize) usize {
    var p = @min(pos, text.len);
    while (p < text.len and (text[p] & 0xC0) == 0x80) p += 1;
    return p;
}

fn utf16Len(text: []const u8, byte_len: usize) i64 {
    var i: usize = 0;
    var units: i64 = 0;
    const end = @min(byte_len, text.len);
    while (i < end) {
        const n = std.unicode.utf8ByteSequenceLength(text[i]) catch 1;
        const cp = std.unicode.utf8Decode(text[i..@min(i + n, text.len)]) catch 0xFFFD;
        units += if (cp > 0xFFFF) 2 else 1;
        i += n;
    }
    return units;
}

fn run(alloc: std.mem.Allocator, q_raw: []const u8, book_id: ?[]const u8) ![]types.SearchHit {
    const q = std.mem.trim(u8, q_raw, " \t\r\n");
    if (q.len == 0) return &.{};
    const chapters = try db.searchChapters(alloc, q, book_id);
    const books = try db.listBooks(alloc);
    var titles = std.StringHashMap([]const u8).init(alloc);
    for (books) |b| try titles.put(b.id, b.title);

    var hits: std.ArrayList(types.SearchHit) = .empty;
    var per_book = std.StringHashMap(usize).init(alloc);
    for (chapters) |ch| {
        if (hits.items.len >= max_hits) break;
        const count = per_book.get(ch.book_id) orelse 0;
        if (count >= max_hits_per_book) continue;
        const title = titles.get(ch.book_id) orelse "";
        var pos: usize = 0;
        var chapter_hits: usize = 0;
        while (chapter_hits < max_hits_per_chapter and hits.items.len < max_hits) {
            const rel = findIgnoreCase(ch.text, q, pos) orelse break;
            const match_start = rel;
            const match_end = match_start + q.len;
            const raw_start = if (match_start > context_bytes) match_start - context_bytes else 0;
            const raw_end = @min(ch.text.len, match_end + context_bytes);
            const start = floorCharBoundary(ch.text, raw_start);
            const end = ceilCharBoundary(ch.text, raw_end);
            const prefix: []const u8 = if (start > 0) "…" else "";
            const suffix: []const u8 = if (end < ch.text.len) "…" else "";
            const snippet = try std.fmt.allocPrint(alloc, "{s}{s}{s}", .{
                prefix,
                ch.text[start..end],
                suffix,
            });
            const hl_start = utf16Len(snippet, prefix.len + (match_start - start));
            const hl_end = utf16Len(snippet, prefix.len + (match_end - start));
            var ranges = try alloc.alloc([2]i64, 1);
            ranges[0] = .{ hl_start, hl_end };
            try hits.append(alloc, .{
                .bookId = ch.book_id,
                .title = title,
                .location = .{
                    .bookId = ch.book_id,
                    .spineIndex = ch.spine_index,
                    .href = ch.href,
                    .charOffset = @intCast(match_start),
                },
                .snippet = snippet,
                .highlights = ranges,
            });
            chapter_hits += 1;
            pos = match_end;
        }
        try per_book.put(ch.book_id, count + chapter_hits);
    }
    return hits.items;
}

pub fn query(ctx: mygo.CallContext, q: []const u8) ![]types.SearchHit {
    return run(ctx.allocator, q, null);
}

pub fn queryBook(ctx: mygo.CallContext, book_id: []const u8, q: []const u8) ![]types.SearchHit {
    return run(ctx.allocator, q, book_id);
}
