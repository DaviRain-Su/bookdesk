const std = @import("std");
const mygo = @import("mygo");
const util = @import("util.zig");
const db = @import("db.zig");
const types = @import("types.zig");
const epub = @import("epub.zig");

pub var user_data: []const u8 = "";
pub var books_dir: []const u8 = "";
pub var images_dir: []const u8 = "";

pub fn initPaths(alloc: std.mem.Allocator) !void {
    user_data = try mygo.Path.get(alloc, "userData");
    books_dir = try util.join(alloc, &.{ user_data, "books" });
    images_dir = try util.join(alloc, &.{ user_data, "images" });
    try util.ensureDir(books_dir);
    try util.ensureDir(images_dir);
}

fn emitProgress(ctx: mygo.CallContext, done: i64, total: i64, title: []const u8) void {
    ctx.window.emit("library:import-progress", types.ImportProgress{
        .done = done,
        .total = total,
        .title = title,
    }) catch {};
}

pub fn pickAndImport(ctx: mygo.CallContext) !types.ImportResult {
    const filters = [_]mygo.FileFilter{
        .{ .name = "EPUB 电子书", .extensions = "epub" },
    };
    const raw = try mygo.Dialog.open(
        ctx.allocator,
        ctx.window,
        "选择 EPUB 电子书",
        "",
        &filters,
        mygo.DialogFlag.multiple,
    );
    if (raw.len == 0) {
        return .{ .books = &.{}, .errors = &.{} };
    }
    var paths: std.ArrayList([]const u8) = .empty;
    var it = std.mem.tokenizeScalar(u8, raw, 0);
    while (it.next()) |p| try paths.append(ctx.allocator, p);
    return importPaths(ctx, paths.items);
}

pub fn importPaths(ctx: mygo.CallContext, paths: []const []const u8) !types.ImportResult {
    var books: std.ArrayList(types.Book) = .empty;
    var errors: std.ArrayList(types.ImportError) = .empty;
    const total: i64 = @intCast(paths.len);
    for (paths, 0..) |path, i| {
        emitProgress(ctx, @intCast(i), total, util.baseName(path));
        const book = importOne(ctx.allocator, path) catch |err| {
            try errors.append(ctx.allocator, .{
                .path = path,
                .message = importErrorMessage(err),
            });
            continue;
        };
        try books.append(ctx.allocator, book);
        emitProgress(ctx, @intCast(i + 1), total, book.title);
    }
    return .{ .books = books.items, .errors = errors.items };
}

fn importErrorMessage(err: anyerror) []const u8 {
    return switch (err) {
        epub.Error.Encrypted => "不支持加密的电子书（DRM）",
        epub.Error.NotEpub => "不是有效的 EPUB 文件",
        epub.Error.MissingContainer, epub.Error.MissingOpf => "EPUB 结构不完整，找不到书籍内容",
        epub.Error.BadPackage => "EPUB 内容无法解析",
        error.FileNotFound => "文件不存在",
        error.AccessDenied => "没有权限读取该文件",
        else => "导入失败",
    };
}

fn importOne(alloc: std.mem.Allocator, path: []const u8) !types.Book {
    var ext_buf: [16]u8 = undefined;
    const ext = util.extensionLower(&ext_buf, path);
    if (!std.mem.eql(u8, ext, ".epub")) return epub.Error.NotEpub;

    const id = try util.newId(alloc, "book");
    const book_dir = try util.join(alloc, &.{ books_dir, id });
    try util.ensureDir(book_dir);
    errdefer std.Io.Dir.cwd().deleteTree(util.io, book_dir) catch {};

    const dest_epub = try util.join(alloc, &.{ book_dir, "book.epub" });
    try util.copyFile(path, dest_epub);

    const parsed = try epub.parse(alloc, dest_epub);

    var cover_rel: ?[]const u8 = null;
    if (parsed.cover_bytes) |bytes| {
        const ext_name = parsed.cover_ext orelse "jpg";
        const cover_name = try std.fmt.allocPrint(alloc, "cover.{s}", .{ext_name});
        const cover_abs = try util.join(alloc, &.{ book_dir, cover_name });
        try util.writeFile(cover_abs, bytes);
        cover_rel = try std.fmt.allocPrint(alloc, "books/{s}/{s}", .{ id, cover_name });
    }

    const book = types.Book{
        .id = id,
        .title = parsed.title,
        .author = parsed.author,
        .language = parsed.language,
        .coverRelPath = cover_rel,
        .epubRelPath = try std.fmt.allocPrint(alloc, "books/{s}/book.epub", .{id}),
        .importedAt = util.nowSeconds(),
    };
    try db.insertBook(book);

    var chapters = try alloc.alloc(db.ChapterRow, parsed.chapters.len);
    for (parsed.chapters, 0..) |ch, i| {
        chapters[i] = .{
            .spineIndex = @intCast(i),
            .href = ch.href,
            .text = ch.text,
        };
    }
    try db.insertChapters(book.id, chapters);
    return book;
}

pub fn list(ctx: mygo.CallContext) ![]types.Book {
    return db.listBooks(ctx.allocator);
}

pub fn chapterText(ctx: mygo.CallContext, book_id: []const u8, spine_index: i64) !?types.ChapterText {
    return db.chapterText(ctx.allocator, book_id, spine_index);
}

pub fn epubURL(ctx: mygo.CallContext, book_id: []const u8) ![]u8 {
    return std.fmt.allocPrint(ctx.allocator, "books://localhost/epub/{s}", .{book_id});
}

pub fn epubBase64(ctx: mygo.CallContext, book_id: []const u8) ![]u8 {
    const book = (try db.getBook(ctx.allocator, book_id)) orelse return error.BookNotFound;
    const abs = try util.join(ctx.allocator, &.{ user_data, book.epubRelPath });
    const data = try util.readFile(ctx.allocator, abs, 256 * 1024 * 1024);
    const enc = std.base64.standard.Encoder;
    const out = try ctx.allocator.alloc(u8, enc.calcSize(data.len));
    return @constCast(enc.encode(out, data));
}

pub fn coverURL(ctx: mygo.CallContext, book_id: []const u8) ![]u8 {
    const book = (try db.getBook(ctx.allocator, book_id)) orelse return try ctx.allocator.dupe(u8, "");
    if (book.coverRelPath == null) return try ctx.allocator.dupe(u8, "");
    return std.fmt.allocPrint(ctx.allocator, "books://localhost/cover/{s}", .{book_id});
}

pub fn pickImage(ctx: mygo.CallContext) ![]u8 {
    const filters = [_]mygo.FileFilter{
        .{ .name = "图片", .extensions = "png,jpg,jpeg,gif,webp" },
    };
    const raw = try mygo.Dialog.open(
        ctx.allocator,
        ctx.window,
        "选择图片",
        "",
        &filters,
        0,
    );
    if (raw.len == 0) return try ctx.allocator.dupe(u8, "");
    const path = std.mem.sliceTo(raw, 0);
    var ext_buf: [16]u8 = undefined;
    var ext = util.extensionLower(&ext_buf, path);
    if (ext.len == 0) ext = ".png";
    const id = try util.newId(ctx.allocator, "image");
    const name = try std.fmt.allocPrint(ctx.allocator, "{s}{s}", .{ id, ext });
    const dest = try util.join(ctx.allocator, &.{ images_dir, name });
    try util.copyFile(path, dest);
    return std.fmt.allocPrint(ctx.allocator, "images/{s}", .{name});
}

// ---------- protocol ----------

fn urlPath(url: []const u8) []const u8 {
    const scheme_end = std.mem.find(u8, url, "://") orelse return "";
    const after = url[scheme_end + 3 ..];
    const slash = std.mem.findScalar(u8, after, '/') orelse return "/";
    var path = after[slash..];
    if (std.mem.findScalar(u8, path, '?')) |q| path = path[0..q];
    return path;
}

fn mimeFor(rel_path: []const u8) []const u8 {
    var buf: [16]u8 = undefined;
    const ext = util.extensionLower(&buf, rel_path);
    if (std.mem.eql(u8, ext, ".epub")) return "application/epub+zip";
    if (std.mem.eql(u8, ext, ".png")) return "image/png";
    if (std.mem.eql(u8, ext, ".gif")) return "image/gif";
    if (std.mem.eql(u8, ext, ".webp")) return "image/webp";
    if (std.mem.eql(u8, ext, ".svg")) return "image/svg+xml";
    return "image/jpeg";
}

fn respondFile(req_id: usize, rel_path: []const u8) void {
    const alloc = std.heap.page_allocator;
    const abs = util.join(alloc, &.{ user_data, rel_path }) catch {
        mygo.Protocol.respond(req_id, 500, "{\"Content-Type\":\"text/plain\"}", "out of memory");
        return;
    };
    defer alloc.free(abs);
    const data = util.readFile(alloc, abs, 256 * 1024 * 1024) catch {
        mygo.Protocol.respond(req_id, 404, "{\"Content-Type\":\"text/plain\"}", "not found");
        return;
    };
    defer alloc.free(data);
    const headers = std.fmt.allocPrint(alloc, "{{\"Content-Type\":\"{s}\"}}", .{mimeFor(rel_path)}) catch {
        mygo.Protocol.respond(req_id, 500, "{\"Content-Type\":\"text/plain\"}", "out of memory");
        return;
    };
    defer alloc.free(headers);
    mygo.Protocol.respond(req_id, 200, headers, data);
}

pub fn handleProtocol(_: usize, req_id: usize, req: mygo.ProtocolRequest) void {
    const alloc = std.heap.page_allocator;
    const path = urlPath(req.url);
    if (std.mem.startsWith(u8, path, "/epub/")) {
        const id = path["/epub/".len..];
        const book = (db.getBook(alloc, id) catch null) orelse {
            mygo.Protocol.respond(req_id, 404, "{\"Content-Type\":\"text/plain\"}", "book not found");
            return;
        };
        respondFile(req_id, book.epubRelPath);
        return;
    }
    if (std.mem.startsWith(u8, path, "/cover/")) {
        const id = path["/cover/".len..];
        const book = (db.getBook(alloc, id) catch null) orelse {
            mygo.Protocol.respond(req_id, 404, "{\"Content-Type\":\"text/plain\"}", "book not found");
            return;
        };
        const rel = book.coverRelPath orelse {
            mygo.Protocol.respond(req_id, 404, "{\"Content-Type\":\"text/plain\"}", "no cover");
            return;
        };
        respondFile(req_id, rel);
        return;
    }
    if (std.mem.startsWith(u8, path, "/image/")) {
        const name = path["/image/".len..];
        if (std.mem.findScalar(u8, name, '/') != null) {
            mygo.Protocol.respond(req_id, 400, "{\"Content-Type\":\"text/plain\"}", "bad path");
            return;
        }
        const rel = std.fmt.allocPrint(alloc, "images/{s}", .{name}) catch {
            mygo.Protocol.respond(req_id, 500, "{\"Content-Type\":\"text/plain\"}", "out of memory");
            return;
        };
        defer alloc.free(rel);
        respondFile(req_id, rel);
        return;
    }
    mygo.Protocol.respond(req_id, 404, "{\"Content-Type\":\"text/plain\"}", "not found");
}
