const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const types = @import("types.zig");

var handle: ?mygo.SQLite = null;

pub fn ensureOpen() !void {
    if (handle != null) return;
    const alloc = std.heap.page_allocator;
    const user_data = try mygo.Path.get(alloc, "userData");
    const path = try util.join(alloc, &.{ user_data, "bookdesk.db" });
    handle = try mygo.SQLite.open(path, false);
    try migrate();
}

pub fn db() mygo.SQLite {
    return handle.?;
}

fn execRaw(sql: []const u8, args_json: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const res = try handle.?.execute(alloc, sql, args_json);
    alloc.free(res);
}

fn execJson(sql: []const u8, args_json: []const u8) !void {
    try ensureOpen();
    try execRaw(sql, args_json);
}

pub fn exec(sql: []const u8) !void {
    return execJson(sql, "[]");
}

pub fn queryJson(alloc: std.mem.Allocator, sql: []const u8, args_json: []const u8) !std.json.Parsed(std.json.Value) {
    try ensureOpen();
    const raw = try handle.?.query(alloc, sql, args_json);
    defer alloc.free(raw);
    return std.json.parseFromSlice(std.json.Value, alloc, raw, .{}) catch error.BadDatabaseResponse;
}

pub fn transactionJson(stmts_json: []const u8) !void {
    try ensureOpen();
    const alloc = std.heap.page_allocator;
    const res = try handle.?.transaction(alloc, stmts_json);
    alloc.free(res);
}

fn migrate() !void {
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS books (
        \\  id TEXT PRIMARY KEY,
        \\  title TEXT NOT NULL,
        \\  author TEXT,
        \\  language TEXT,
        \\  cover_rel_path TEXT,
        \\  epub_rel_path TEXT NOT NULL,
        \\  imported_at INTEGER NOT NULL
        \\)
    , "[]");
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS book_text (
        \\  book_id TEXT NOT NULL,
        \\  spine_index INTEGER NOT NULL,
        \\  href TEXT,
        \\  text TEXT NOT NULL,
        \\  PRIMARY KEY (book_id, spine_index)
        \\)
    , "[]");
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS highlights (
        \\  id TEXT PRIMARY KEY,
        \\  book_id TEXT NOT NULL,
        \\  cfi TEXT,
        \\  spine_index INTEGER NOT NULL DEFAULT 0,
        \\  href TEXT,
        \\  char_offset INTEGER,
        \\  text TEXT NOT NULL,
        \\  color TEXT NOT NULL DEFAULT 'yellow',
        \\  created_at INTEGER NOT NULL
        \\)
    , "[]");
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS cards (
        \\  id TEXT PRIMARY KEY,
        \\  type TEXT NOT NULL,
        \\  title TEXT,
        \\  body TEXT,
        \\  quote TEXT,
        \\  quote_book_id TEXT,
        \\  quote_cfi TEXT,
        \\  quote_spine_index INTEGER,
        \\  quote_href TEXT,
        \\  quote_char_offset INTEGER,
        \\  image_rel_path TEXT,
        \\  source_book_id TEXT,
        \\  character_name TEXT,
        \\  created_at INTEGER NOT NULL,
        \\  updated_at INTEGER NOT NULL
        \\)
    , "[]");
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS whiteboards (
        \\  id TEXT PRIMARY KEY,
        \\  name TEXT NOT NULL,
        \\  viewport_json TEXT NOT NULL DEFAULT '{}',
        \\  nodes_json TEXT NOT NULL DEFAULT '[]',
        \\  edges_json TEXT NOT NULL DEFAULT '[]',
        \\  updated_at INTEGER NOT NULL
        \\)
    , "[]");
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS characters (
        \\  id TEXT PRIMARY KEY,
        \\  book_id TEXT NOT NULL,
        \\  name TEXT NOT NULL,
        \\  description TEXT,
        \\  image_rel_path TEXT,
        \\  created_at INTEGER NOT NULL
        \\)
    , "[]");
    try execRaw(
        \\CREATE TABLE IF NOT EXISTS settings (
        \\  key TEXT PRIMARY KEY,
        \\  value TEXT NOT NULL
        \\)
    , "[]");
    try execRaw("CREATE INDEX IF NOT EXISTS idx_highlights_book ON highlights(book_id)", "[]");
    try execRaw("CREATE INDEX IF NOT EXISTS idx_cards_source ON cards(source_book_id)", "[]");
    try execRaw("CREATE INDEX IF NOT EXISTS idx_characters_book ON characters(book_id)", "[]");
    // 旧版本把默认画布叫「我的白板」，跟着新说法改一次。
    try execRaw("UPDATE whiteboards SET name = '我的画布' WHERE name = '我的白板'", "[]");
}

// ---------- value helpers ----------

pub fn asString(v: std.json.Value) ?[]const u8 {
    return switch (v) {
        .string => |s| s,
        else => null,
    };
}

pub fn asInt(v: std.json.Value) ?i64 {
    return switch (v) {
        .integer => |n| n,
        .float => |f| @intFromFloat(f),
        else => null,
    };
}

fn optString(v: std.json.Value) ?[]const u8 {
    return switch (v) {
        .string => |s| s,
        .null => null,
        else => null,
    };
}

fn optInt(v: std.json.Value) ?i64 {
    return switch (v) {
        .integer => |n| n,
        .null => null,
        else => null,
    };
}

fn rowsOf(parsed: *const std.json.Parsed(std.json.Value)) ![]std.json.Value {
    const root = parsed.value;
    const obj = switch (root) {
        .object => |o| o,
        else => return error.BadDatabaseResponse,
    };
    const rows = obj.get("rows") orelse return error.BadDatabaseResponse;
    return switch (rows) {
        .array => |a| a.items,
        else => return error.BadDatabaseResponse,
    };
}

fn rowItems(row: std.json.Value) ![]std.json.Value {
    return switch (row) {
        .array => |a| a.items,
        else => return error.BadDatabaseResponse,
    };
}

// ---------- books ----------

pub fn listBooks(alloc: std.mem.Allocator) ![]types.Book {
    const parsed = try queryJson(
        alloc,
        "SELECT id, title, author, language, cover_rel_path, epub_rel_path, imported_at FROM books ORDER BY imported_at DESC",
        "[]",
    );
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(types.Book, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        out[i] = .{
            .id = asString(it[0]) orelse "",
            .title = asString(it[1]) orelse "",
            .author = optString(it[2]),
            .language = optString(it[3]),
            .coverRelPath = optString(it[4]),
            .epubRelPath = asString(it[5]) orelse "",
            .importedAt = asInt(it[6]) orelse 0,
        };
    }
    return out;
}

pub fn getBook(alloc: std.mem.Allocator, id: []const u8) !?types.Book {
    const args = try util.jsonString(alloc, .{id});
    defer alloc.free(args);
    const parsed = try queryJson(
        alloc,
        "SELECT id, title, author, language, cover_rel_path, epub_rel_path, imported_at FROM books WHERE id = ?",
        args,
    );
    const rows = try rowsOf(&parsed);
    if (rows.len == 0) return null;
    const it = try rowItems(rows[0]);
    return .{
        .id = asString(it[0]) orelse "",
        .title = asString(it[1]) orelse "",
        .author = optString(it[2]),
        .language = optString(it[3]),
        .coverRelPath = optString(it[4]),
        .epubRelPath = asString(it[5]) orelse "",
        .importedAt = asInt(it[6]) orelse 0,
    };
}

pub fn insertBook(book: types.Book) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{
        book.id,
        book.title,
        book.author,
        book.language,
        book.coverRelPath,
        book.epubRelPath,
        book.importedAt,
    });
    defer alloc.free(args);
    try execJson(
        "INSERT INTO books (id, title, author, language, cover_rel_path, epub_rel_path, imported_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
        args,
    );
}

// ---------- chapters ----------

pub const ChapterRow = struct {
    spineIndex: i64,
    href: []const u8,
    text: []const u8,
};

pub fn insertChapters(book_id: []const u8, chapters: []const ChapterRow) !void {
    if (chapters.len == 0) return;
    const alloc = std.heap.page_allocator;
    var out: std.Io.Writer.Allocating = .init(alloc);
    defer out.deinit();
    try out.writer.writeAll("[");
    for (chapters, 0..) |ch, i| {
        if (i > 0) try out.writer.writeAll(",");
        try out.writer.writeAll("{\"sql\":\"INSERT INTO book_text (book_id, spine_index, href, text) VALUES (?, ?, ?, ?)\",\"args\":");
        try std.json.Stringify.value(.{ book_id, ch.spineIndex, ch.href, ch.text }, .{}, &out.writer);
        try out.writer.writeAll("}");
    }
    try out.writer.writeAll("]");
    try transactionJson(out.written());
}

pub fn chapterText(alloc: std.mem.Allocator, book_id: []const u8, spine_index: i64) !?types.ChapterText {
    const args = try util.jsonString(alloc, .{ book_id, spine_index });
    defer alloc.free(args);
    const parsed = try queryJson(
        alloc,
        "SELECT spine_index, href, text FROM book_text WHERE book_id = ? AND spine_index = ?",
        args,
    );
    const rows = try rowsOf(&parsed);
    if (rows.len == 0) return null;
    const it = try rowItems(rows[0]);
    return .{
        .spineIndex = asInt(it[0]) orelse 0,
        .href = optString(it[1]),
        .text = asString(it[2]) orelse "",
    };
}

// ---------- highlights ----------

pub fn insertHighlight(h: types.Highlight) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{
        h.id,
        h.bookId,
        h.location.cfi,
        h.location.spineIndex,
        h.location.href,
        h.location.charOffset,
        h.text,
        h.color,
        h.createdAt,
    });
    defer alloc.free(args);
    try execJson(
        "INSERT INTO highlights (id, book_id, cfi, spine_index, href, char_offset, text, color, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
        args,
    );
}

pub fn listHighlights(alloc: std.mem.Allocator, book_id: ?[]const u8) ![]types.Highlight {
    const parsed = if (book_id) |bid| blk: {
        const args = try util.jsonString(alloc, .{bid});
        defer alloc.free(args);
        break :blk try queryJson(
            alloc,
            "SELECT id, book_id, cfi, spine_index, href, char_offset, text, color, created_at FROM highlights WHERE book_id = ? ORDER BY created_at ASC",
            args,
        );
    } else try queryJson(
        alloc,
        "SELECT id, book_id, cfi, spine_index, href, char_offset, text, color, created_at FROM highlights ORDER BY created_at ASC",
        "[]",
    );
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(types.Highlight, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        out[i] = .{
            .id = asString(it[0]) orelse "",
            .bookId = asString(it[1]) orelse "",
            .location = .{
                .bookId = asString(it[1]) orelse "",
                .cfi = optString(it[2]),
                .spineIndex = asInt(it[3]) orelse 0,
                .href = optString(it[4]),
                .charOffset = optInt(it[5]),
            },
            .text = asString(it[6]) orelse "",
            .color = asString(it[7]) orelse "yellow",
            .createdAt = asInt(it[8]) orelse 0,
        };
    }
    return out;
}

pub fn findHighlightByCfi(alloc: std.mem.Allocator, book_id: []const u8, cfi: []const u8) !?types.Highlight {
    const args = try util.jsonString(alloc, .{ book_id, cfi });
    defer alloc.free(args);
    const parsed = try queryJson(
        alloc,
        "SELECT id, book_id, cfi, spine_index, href, char_offset, text, color, created_at FROM highlights WHERE book_id = ? AND cfi = ? LIMIT 1",
        args,
    );
    const rows = try rowsOf(&parsed);
    if (rows.len == 0) return null;
    const it = try rowItems(rows[0]);
    return .{
        .id = asString(it[0]) orelse "",
        .bookId = asString(it[1]) orelse "",
        .location = .{
            .bookId = asString(it[1]) orelse "",
            .cfi = optString(it[2]),
            .spineIndex = asInt(it[3]) orelse 0,
            .href = optString(it[4]),
            .charOffset = optInt(it[5]),
        },
        .text = asString(it[6]) orelse "",
        .color = asString(it[7]) orelse "yellow",
        .createdAt = asInt(it[8]) orelse 0,
    };
}

pub fn deleteHighlight(id: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{id});
    defer alloc.free(args);
    try execJson("DELETE FROM highlights WHERE id = ?", args);
}

// ---------- cards ----------

pub fn upsertCard(card: types.Card) !void {
    const alloc = std.heap.page_allocator;
    const loc = card.quoteLocation;
    const args = try util.jsonString(alloc, .{
        card.id,
        card.type,
        card.title,
        card.body,
        card.quote,
        if (loc) |l| l.bookId else null,
        if (loc) |l| l.cfi else null,
        if (loc) |l| l.spineIndex else null,
        if (loc) |l| l.href else null,
        if (loc) |l| l.charOffset else null,
        card.imageRelPath,
        card.sourceBookId,
        card.characterName,
        card.createdAt,
        card.updatedAt,
    });
    defer alloc.free(args);
    try execJson(
        \\INSERT INTO cards (id, type, title, body, quote, quote_book_id, quote_cfi, quote_spine_index, quote_href, quote_char_offset, image_rel_path, source_book_id, character_name, created_at, updated_at)
        \\VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        \\ON CONFLICT(id) DO UPDATE SET type = excluded.type, title = excluded.title, body = excluded.body, quote = excluded.quote, quote_book_id = excluded.quote_book_id, quote_cfi = excluded.quote_cfi, quote_spine_index = excluded.quote_spine_index, quote_href = excluded.quote_href, quote_char_offset = excluded.quote_char_offset, image_rel_path = excluded.image_rel_path, source_book_id = excluded.source_book_id, character_name = excluded.character_name, updated_at = excluded.updated_at
    ,
        args,
    );
}

pub fn listCards(alloc: std.mem.Allocator) ![]types.Card {
    const parsed = try queryJson(
        alloc,
        "SELECT id, type, title, body, quote, quote_book_id, quote_cfi, quote_spine_index, quote_href, quote_char_offset, image_rel_path, source_book_id, character_name, created_at, updated_at FROM cards ORDER BY created_at ASC",
        "[]",
    );
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(types.Card, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        const quote_book = optString(it[5]);
        out[i] = .{
            .id = asString(it[0]) orelse "",
            .type = asString(it[1]) orelse "text",
            .title = optString(it[2]),
            .body = optString(it[3]),
            .quote = optString(it[4]),
            .quoteLocation = if (quote_book) |bid| .{
                .bookId = bid,
                .cfi = optString(it[6]),
                .spineIndex = optInt(it[7]) orelse 0,
                .href = optString(it[8]),
                .charOffset = optInt(it[9]),
            } else null,
            .imageRelPath = optString(it[10]),
            .sourceBookId = optString(it[11]),
            .characterName = optString(it[12]),
            .createdAt = asInt(it[13]) orelse 0,
            .updatedAt = asInt(it[14]) orelse 0,
        };
    }
    return out;
}

pub fn deleteCard(id: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{id});
    defer alloc.free(args);
    try execJson("DELETE FROM cards WHERE id = ?", args);
}

// ---------- characters ----------

pub fn upsertCharacter(c: types.Character) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{
        c.id,
        c.bookId,
        c.name,
        c.description,
        c.imageRelPath,
        c.createdAt,
    });
    defer alloc.free(args);
    try execJson(
        \\INSERT INTO characters (id, book_id, name, description, image_rel_path, created_at)
        \\VALUES (?, ?, ?, ?, ?, ?)
        \\ON CONFLICT(id) DO UPDATE SET book_id = excluded.book_id, name = excluded.name, description = excluded.description, image_rel_path = excluded.image_rel_path
    ,
        args,
    );
}

pub fn listCharacters(alloc: std.mem.Allocator, book_id: ?[]const u8) ![]types.Character {
    const parsed = if (book_id) |bid| blk: {
        const args = try util.jsonString(alloc, .{bid});
        defer alloc.free(args);
        break :blk try queryJson(
            alloc,
            "SELECT id, book_id, name, description, image_rel_path, created_at FROM characters WHERE book_id = ? ORDER BY created_at ASC",
            args,
        );
    } else try queryJson(
        alloc,
        "SELECT id, book_id, name, description, image_rel_path, created_at FROM characters ORDER BY created_at ASC",
        "[]",
    );
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(types.Character, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        out[i] = .{
            .id = asString(it[0]) orelse "",
            .bookId = asString(it[1]) orelse "",
            .name = asString(it[2]) orelse "",
            .description = optString(it[3]),
            .imageRelPath = optString(it[4]),
            .createdAt = asInt(it[5]) orelse 0,
        };
    }
    return out;
}

pub fn deleteCharacter(id: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{id});
    defer alloc.free(args);
    try execJson("DELETE FROM characters WHERE id = ?", args);
}

// ---------- settings ----------

pub fn getSetting(alloc: std.mem.Allocator, key: []const u8) !?[]const u8 {
    const args = try util.jsonString(alloc, .{key});
    defer alloc.free(args);
    const parsed = try queryJson(alloc, "SELECT value FROM settings WHERE key = ?", args);
    const rows = try rowsOf(&parsed);
    if (rows.len == 0) return null;
    const it = try rowItems(rows[0]);
    return asString(it[0]);
}

pub fn setSetting(key: []const u8, value: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{ key, value });
    defer alloc.free(args);
    try execJson(
        \\INSERT INTO settings (key, value) VALUES (?, ?)
        \\ON CONFLICT(key) DO UPDATE SET value = excluded.value
    ,
        args,
    );
}

pub fn deleteSetting(key: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{key});
    defer alloc.free(args);
    try execJson("DELETE FROM settings WHERE key = ?", args);
}

/// 按 LIKE 模式删除设置；pattern 里的 % _ \ 需要调用方自己转义。
pub fn deleteSettingsLike(pattern: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{pattern});
    defer alloc.free(args);
    try execJson(
        \\DELETE FROM settings WHERE key LIKE ? ESCAPE '\'
    ,
        args,
    );
}

// ---------- chapters for AI context ----------

pub const ChapterRangeRow = struct {
    spine_index: i64,
    href: ?[]const u8,
    text: []const u8,
};

fn chapterRows(alloc: std.mem.Allocator, sql: []const u8, args_json: []const u8) ![]ChapterRangeRow {
    const parsed = try queryJson(alloc, sql, args_json);
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(ChapterRangeRow, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        out[i] = .{
            .spine_index = asInt(it[0]) orelse 0,
            .href = optString(it[1]),
            .text = asString(it[2]) orelse "",
        };
    }
    return out;
}

/// 当前阅读位置之前的章节（不含当前章节）。
pub fn chaptersBefore(alloc: std.mem.Allocator, book_id: []const u8, spine_index: i64) ![]ChapterRangeRow {
    const args = try util.jsonString(alloc, .{ book_id, spine_index });
    defer alloc.free(args);
    return chapterRows(
        alloc,
        "SELECT spine_index, href, text FROM book_text WHERE book_id = ? AND spine_index < ? ORDER BY spine_index ASC",
        args,
    );
}

pub fn allChapters(alloc: std.mem.Allocator, book_id: []const u8) ![]ChapterRangeRow {
    const args = try util.jsonString(alloc, .{book_id});
    defer alloc.free(args);
    return chapterRows(
        alloc,
        "SELECT spine_index, href, text FROM book_text WHERE book_id = ? ORDER BY spine_index ASC",
        args,
    );
}

// ---------- boards ----------

pub fn listBoards(alloc: std.mem.Allocator) ![]types.BoardSummary {
    const parsed = try queryJson(
        alloc,
        "SELECT id, name, updated_at FROM whiteboards ORDER BY updated_at DESC",
        "[]",
    );
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(types.BoardSummary, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        out[i] = .{
            .id = asString(it[0]) orelse "",
            .name = asString(it[1]) orelse "",
            .updatedAt = asInt(it[2]) orelse 0,
        };
    }
    return out;
}

pub fn getBoard(alloc: std.mem.Allocator, id: []const u8) !?types.Board {
    const args = try util.jsonString(alloc, .{id});
    defer alloc.free(args);
    const parsed = try queryJson(
        alloc,
        "SELECT id, name, viewport_json, nodes_json, edges_json, updated_at FROM whiteboards WHERE id = ?",
        args,
    );
    const rows = try rowsOf(&parsed);
    if (rows.len == 0) return null;
    const it = try rowItems(rows[0]);
    return .{
        .id = asString(it[0]) orelse "",
        .name = asString(it[1]) orelse "",
        .viewportJson = asString(it[2]) orelse "{}",
        .nodesJson = asString(it[3]) orelse "[]",
        .edgesJson = asString(it[4]) orelse "[]",
        .updatedAt = asInt(it[5]) orelse 0,
    };
}

pub fn saveBoard(board: types.Board) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{
        board.id,
        board.name,
        board.viewportJson,
        board.nodesJson,
        board.edgesJson,
        board.updatedAt,
    });
    defer alloc.free(args);
    try execJson(
        \\INSERT INTO whiteboards (id, name, viewport_json, nodes_json, edges_json, updated_at)
        \\VALUES (?, ?, ?, ?, ?, ?)
        \\ON CONFLICT(id) DO UPDATE SET name = excluded.name, viewport_json = excluded.viewport_json, nodes_json = excluded.nodes_json, edges_json = excluded.edges_json, updated_at = excluded.updated_at
    ,
        args,
    );
}

pub fn deleteBoard(id: []const u8) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{id});
    defer alloc.free(args);
    try execJson("DELETE FROM whiteboards WHERE id = ?", args);
}

pub fn renameBoard(id: []const u8, name: []const u8, updated_at: i64) !void {
    const alloc = std.heap.page_allocator;
    const args = try util.jsonString(alloc, .{ name, updated_at, id });
    defer alloc.free(args);
    try execJson("UPDATE whiteboards SET name = ?, updated_at = ? WHERE id = ?", args);
}

// ---------- search ----------

pub const ChapterSearchRow = struct {
    book_id: []const u8,
    spine_index: i64,
    href: ?[]const u8,
    text: []const u8,
};

pub fn searchChapters(alloc: std.mem.Allocator, q: []const u8, book_id: ?[]const u8) ![]ChapterSearchRow {
    var parsed: std.json.Parsed(std.json.Value) = undefined;
    if (book_id) |bid| {
        const args = try util.jsonString(alloc, .{ q, bid });
        defer alloc.free(args);
        parsed = try queryJson(
            alloc,
            "SELECT book_id, spine_index, href, text FROM book_text WHERE instr(lower(text), lower(?)) > 0 AND book_id = ? ORDER BY book_id, spine_index LIMIT 400",
            args,
        );
    } else {
        const args = try util.jsonString(alloc, .{q});
        defer alloc.free(args);
        parsed = try queryJson(
            alloc,
            "SELECT book_id, spine_index, href, text FROM book_text WHERE instr(lower(text), lower(?)) > 0 ORDER BY book_id, spine_index LIMIT 400",
            args,
        );
    }
    const rows = try rowsOf(&parsed);
    var out = try alloc.alloc(ChapterSearchRow, rows.len);
    for (rows, 0..) |row, i| {
        const it = try rowItems(row);
        out[i] = .{
            .book_id = asString(it[0]) orelse "",
            .spine_index = asInt(it[1]) orelse 0,
            .href = optString(it[2]),
            .text = asString(it[3]) orelse "",
        };
    }
    return out;
}
