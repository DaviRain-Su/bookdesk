const std = @import("std");

pub var threaded: std.Io.Threaded = undefined;
pub var io: std.Io = undefined;

pub fn initIo() void {
    threaded = .init(std.heap.page_allocator, .{});
    io = threaded.io();
}

pub fn nowSeconds() i64 {
    return std.Io.Timestamp.toSeconds(std.Io.Timestamp.now(io, .real));
}

pub fn nowMillis() i64 {
    return std.Io.Timestamp.toMilliseconds(std.Io.Timestamp.now(io, .real));
}

pub fn join(alloc: std.mem.Allocator, parts: []const []const u8) ![]u8 {
    return std.mem.join(alloc, "/", parts);
}

pub fn readFile(alloc: std.mem.Allocator, path: []const u8, limit: usize) ![]u8 {
    return std.Io.Dir.cwd().readFileAlloc(io, path, alloc, .limited(limit));
}

pub fn writeFile(path: []const u8, data: []const u8) !void {
    if (std.fs.path.dirname(path)) |dir| {
        std.Io.Dir.cwd().createDirPath(io, dir) catch {};
    }
    try std.Io.Dir.cwd().writeFile(io, .{ .sub_path = path, .data = data });
}

pub fn ensureDir(path: []const u8) !void {
    try std.Io.Dir.cwd().createDirPath(io, path);
}

pub fn copyFile(src: []const u8, dest: []const u8) !void {
    try std.Io.Dir.cwd().copyFile(src, std.Io.Dir.cwd(), dest, io, .{
        .make_path = true,
        .replace = true,
    });
}

pub fn fileExists(path: []const u8) bool {
    _ = std.Io.Dir.cwd().statFile(io, path, .{}) catch return false;
    return true;
}

pub fn newId(alloc: std.mem.Allocator, prefix: []const u8) ![]u8 {
    var bytes: [8]u8 = undefined;
    io.random(&bytes);
    const digits = "0123456789abcdef";
    var hex: [16]u8 = undefined;
    for (bytes, 0..) |b, i| {
        hex[i * 2] = digits[b >> 4];
        hex[i * 2 + 1] = digits[b & 0xf];
    }
    return std.fmt.allocPrint(alloc, "{s}-{s}", .{ prefix, hex[0..] });
}

pub fn jsonString(alloc: std.mem.Allocator, value: anytype) ![]u8 {
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    try std.json.Stringify.value(value, .{}, &out.writer);
    return out.toOwnedSlice();
}

/// Extracts the last path component, e.g. "a/b/c.epub" -> "c.epub".
pub fn baseName(path: []const u8) []const u8 {
    return std.fs.path.basename(path);
}

pub fn extensionLower(buf: []u8, path: []const u8) []const u8 {
    const ext = std.fs.path.extension(path);
    const n = @min(ext.len, buf.len);
    for (ext[0..n], 0..) |c, i| buf[i] = std.ascii.toLower(c);
    return buf[0..n];
}

pub fn eqlIgnoreCase(a: []const u8, b: []const u8) bool {
    return std.ascii.eqlIgnoreCase(a, b);
}

pub fn containsIgnoreCase(haystack: []const u8, needle: []const u8) bool {
    if (needle.len == 0) return true;
    if (needle.len > haystack.len) return false;
    var i: usize = 0;
    while (i + needle.len <= haystack.len) : (i += 1) {
        if (std.ascii.eqlIgnoreCase(haystack[i .. i + needle.len], needle)) return true;
    }
    return false;
}
