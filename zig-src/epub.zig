const std = @import("std");
const util = @import("util.zig");

pub const Chapter = struct {
    href: []const u8,
    text: []const u8,
};

pub const Parsed = struct {
    title: []const u8,
    author: ?[]const u8 = null,
    language: ?[]const u8 = null,
    cover_bytes: ?[]const u8 = null,
    cover_ext: ?[]const u8 = null,
    chapters: []Chapter = &.{},
};

pub const Error = error{
    NotEpub,
    Encrypted,
    MissingContainer,
    MissingOpf,
    BadPackage,
};

const max_entry_bytes = 64 * 1024 * 1024;

const Tag = struct {
    name: []const u8,
    attrs: []const u8,
    closing: bool,
    self_closing: bool,
    start: usize,
    content_start: usize,
};

fn nextTag(xml: []const u8, from: usize) ?Tag {
    var i = from;
    while (i < xml.len) {
        const lt = std.mem.findScalar(u8, xml[i..], '<') orelse return null;
        const pos = i + lt;
        if (pos + 1 >= xml.len) return null;
        if (std.mem.startsWith(u8, xml[pos..], "<!--")) {
            const end = std.mem.findPos(u8, xml, pos + 4, "-->") orelse return null;
            i = end + 3;
            continue;
        }
        if (std.mem.startsWith(u8, xml[pos..], "<?")) {
            const end = std.mem.findPos(u8, xml, pos + 2, "?>") orelse return null;
            i = end + 2;
            continue;
        }
        if (std.mem.startsWith(u8, xml[pos..], "<![CDATA[")) {
            const end = std.mem.findPos(u8, xml, pos + 9, "]]>") orelse return null;
            i = end + 3;
            continue;
        }
        const gt = std.mem.findScalar(u8, xml[pos + 1 ..], '>') orelse return null;
        const gt_pos = pos + 1 + gt;
        var inner = xml[pos + 1 .. gt_pos];
        const closing = inner.len > 0 and inner[0] == '/';
        if (closing) inner = inner[1..];
        var self_closing = false;
        if (inner.len > 0 and inner[inner.len - 1] == '/') {
            self_closing = true;
            inner = inner[0 .. inner.len - 1];
        }
        var name_end: usize = 0;
        while (name_end < inner.len and !std.ascii.isWhitespace(inner[name_end])) : (name_end += 1) {}
        return .{
            .name = inner[0..name_end],
            .attrs = inner[name_end..],
            .closing = closing,
            .self_closing = self_closing,
            .start = pos,
            .content_start = gt_pos + 1,
        };
    }
    return null;
}

fn localName(name: []const u8) []const u8 {
    if (std.mem.findScalarLast(u8, name, ':')) |i| return name[i + 1 ..];
    return name;
}

fn getAttr(attrs: []const u8, name: []const u8) ?[]const u8 {
    var i: usize = 0;
    while (i < attrs.len) {
        while (i < attrs.len and std.ascii.isWhitespace(attrs[i])) : (i += 1) {}
        if (i >= attrs.len) break;
        const key_start = i;
        while (i < attrs.len and attrs[i] != '=' and !std.ascii.isWhitespace(attrs[i])) : (i += 1) {}
        const key = attrs[key_start..i];
        while (i < attrs.len and std.ascii.isWhitespace(attrs[i])) : (i += 1) {}
        if (i >= attrs.len or attrs[i] != '=') continue;
        i += 1;
        while (i < attrs.len and std.ascii.isWhitespace(attrs[i])) : (i += 1) {}
        if (i >= attrs.len) break;
        const quote = attrs[i];
        if (quote != '"' and quote != '\'') continue;
        i += 1;
        const val_start = i;
        while (i < attrs.len and attrs[i] != quote) : (i += 1) {}
        const val = attrs[val_start..i];
        if (i < attrs.len) i += 1;
        if (std.mem.eql(u8, key, name)) return val;
    }
    return null;
}

fn decodeEntity(ent: []const u8) ?u21 {
    if (ent.len == 0) return null;
    if (ent[0] == '#') {
        var base: u8 = 10;
        var digits = ent[1..];
        if (digits.len > 0 and (digits[0] == 'x' or digits[0] == 'X')) {
            base = 16;
            digits = digits[1..];
        }
        if (digits.len == 0) return null;
        const value = std.fmt.parseUnsigned(u32, digits, base) catch return null;
        if (value > 0x10FFFF) return null;
        return @intCast(value);
    }
    const named = .{
        .{ "amp", @as(u21, '&') },
        .{ "lt", @as(u21, '<') },
        .{ "gt", @as(u21, '>') },
        .{ "quot", @as(u21, '"') },
        .{ "apos", @as(u21, '\'') },
        .{ "nbsp", @as(u21, 0xA0) },
        .{ "mdash", @as(u21, 0x2014) },
        .{ "ndash", @as(u21, 0x2013) },
        .{ "hellip", @as(u21, 0x2026) },
        .{ "ldquo", @as(u21, 0x201C) },
        .{ "rdquo", @as(u21, 0x201D) },
        .{ "lsquo", @as(u21, 0x2018) },
        .{ "rsquo", @as(u21, 0x2019) },
        .{ "middot", @as(u21, 0xB7) },
    };
    inline for (named) |pair| {
        if (std.mem.eql(u8, ent, pair[0])) return pair[1];
    }
    return null;
}

fn appendDecoded(w: *std.Io.Writer, s: []const u8) !void {
    var i: usize = 0;
    while (i < s.len) {
        if (s[i] == '&') {
            if (std.mem.findScalar(u8, s[i..], ';')) |rel| {
                const semi = i + rel;
                if (decodeEntity(s[i + 1 .. semi])) |cp| {
                    var buf: [4]u8 = undefined;
                    const n = std.unicode.utf8Encode(cp, &buf) catch 0;
                    if (n > 0) {
                        try w.writeAll(buf[0..n]);
                        i = semi + 1;
                        continue;
                    }
                }
            }
        }
        try w.writeByte(s[i]);
        i += 1;
    }
}

fn elementText(alloc: std.mem.Allocator, xml: []const u8, wanted: []const u8) !?[]u8 {
    var i: usize = 0;
    while (nextTag(xml, i)) |tag| {
        i = tag.content_start;
        if (tag.closing or tag.self_closing) continue;
        if (!std.mem.eql(u8, localName(tag.name), wanted)) continue;
        // Find the matching close tag for this element.
        var j = tag.content_start;
        var depth: usize = 1;
        var text_end = xml.len;
        while (nextTag(xml, j)) |t| {
            if (std.mem.eql(u8, localName(t.name), wanted)) {
                if (t.closing) {
                    depth -= 1;
                    if (depth == 0) {
                        text_end = t.start;
                        break;
                    }
                } else if (!t.self_closing) {
                    depth += 1;
                }
            }
            j = t.content_start;
        }
        var out: std.Io.Writer.Allocating = .init(alloc);
        errdefer out.deinit();
        var k = tag.content_start;
        while (k < text_end) {
            if (xml[k] == '<') {
                const t = nextTag(xml, k) orelse break;
                k = t.content_start;
            } else {
                const lt = std.mem.findScalar(u8, xml[k..], '<') orelse text_end;
                const end = @min(k + lt, text_end);
                try appendDecoded(&out.writer, xml[k..end]);
                k = end;
            }
        }
        const trimmed = std.mem.trim(u8, out.written(), " \t\r\n");
        return try alloc.dupe(u8, trimmed);
    }
    return null;
}

fn isBlockTag(name: []const u8) bool {
    const blocks = .{
        "p",     "div",   "br",    "h1",    "h2",    "h3",     "h4",
        "h5",    "h6",    "li",    "tr",    "blockquote", "section", "article",
        "header", "footer", "pre",  "figure", "figcaption", "dd", "dt",
    };
    inline for (blocks) |b| {
        if (std.ascii.eqlIgnoreCase(name, b)) return true;
    }
    return false;
}

fn htmlToText(alloc: std.mem.Allocator, html: []const u8) ![]u8 {
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    var i: usize = 0;
    var skip: ?[]const u8 = null;
    while (i < html.len) {
        if (html[i] == '<') {
            const tag = nextTag(html, i) orelse break;
            const ln = localName(tag.name);
            if (skip) |name| {
                if (tag.closing and std.ascii.eqlIgnoreCase(ln, name)) skip = null;
            } else if (!tag.closing and !tag.self_closing and
                (std.ascii.eqlIgnoreCase(ln, "head") or
                    std.ascii.eqlIgnoreCase(ln, "script") or
                    std.ascii.eqlIgnoreCase(ln, "style")))
            {
                skip = ln;
            } else if (!tag.closing and isBlockTag(ln)) {
                try out.writer.writeByte('\n');
            }
            i = tag.content_start;
        } else {
            const rest = html[i..];
            const lt = std.mem.findScalar(u8, rest, '<') orelse rest.len;
            const end = i + lt;
            if (skip == null) try appendDecoded(&out.writer, rest[0..lt]);
            i = end;
        }
    }
    return normalizeWhitespace(alloc, out.written());
}

fn normalizeWhitespace(alloc: std.mem.Allocator, raw: []const u8) ![]u8 {
    const out = try alloc.alloc(u8, raw.len);
    var n: usize = 0;
    var last_space = false;
    var newlines: usize = 0;
    for (raw) |c| {
        if (c == '\r') continue;
        if (c == '\n') {
            if (n == 0) continue;
            while (n > 0 and out[n - 1] == ' ') n -= 1;
            newlines += 1;
            if (newlines <= 2) {
                out[n] = '\n';
                n += 1;
            }
            last_space = false;
        } else if (c == ' ' or c == '\t') {
            if (!last_space and n > 0 and out[n - 1] != '\n') {
                out[n] = ' ';
                n += 1;
            }
            last_space = true;
        } else {
            out[n] = c;
            n += 1;
            last_space = false;
            newlines = 0;
        }
    }
    while (n > 0 and (out[n - 1] == '\n' or out[n - 1] == ' ')) n -= 1;
    return out[0..n];
}

fn percentDecode(alloc: std.mem.Allocator, s: []const u8) ![]u8 {
    const out = try alloc.alloc(u8, s.len);
    var n: usize = 0;
    var i: usize = 0;
    while (i < s.len) {
        if (s[i] == '%' and i + 2 < s.len) {
            const hi = std.fmt.charToDigit(s[i + 1], 16) catch {
                out[n] = s[i];
                n += 1;
                i += 1;
                continue;
            };
            const lo = std.fmt.charToDigit(s[i + 2], 16) catch {
                out[n] = s[i];
                n += 1;
                i += 1;
                continue;
            };
            out[n] = hi * 16 + lo;
            n += 1;
            i += 3;
        } else {
            out[n] = s[i];
            n += 1;
            i += 1;
        }
    }
    return out[0..n];
}

fn normalizePath(alloc: std.mem.Allocator, path: []const u8) ![]u8 {
    var stack: [128][]const u8 = undefined;
    var n: usize = 0;
    var it = std.mem.splitScalar(u8, path, '/');
    while (it.next()) |seg| {
        if (seg.len == 0 or std.mem.eql(u8, seg, ".")) continue;
        if (std.mem.eql(u8, seg, "..")) {
            if (n > 0) n -= 1;
            continue;
        }
        if (n < stack.len) {
            stack[n] = seg;
            n += 1;
        }
    }
    return std.mem.join(alloc, "/", stack[0..n]);
}

fn resolveHref(alloc: std.mem.Allocator, opf_path: []const u8, href: []const u8) ![]u8 {
    var clean = href;
    if (std.mem.findScalar(u8, clean, '#')) |i| clean = clean[0..i];
    if (std.mem.findScalar(u8, clean, '?')) |i| clean = clean[0..i];
    const decoded = try percentDecode(alloc, clean);
    if (decoded.len > 0 and decoded[0] == '/') {
        return normalizePath(alloc, decoded[1..]);
    }
    const dir = std.fs.path.dirname(opf_path) orelse "";
    const joined = try std.mem.join(alloc, "/", &.{ dir, decoded });
    return normalizePath(alloc, joined);
}

const ManifestItem = struct {
    id: []const u8,
    href: []const u8,
    media_type: []const u8,
    properties: []const u8,
};

fn coverExtension(media_type: []const u8, href: []const u8) []const u8 {
    if (std.mem.eql(u8, media_type, "image/png")) return "png";
    if (std.mem.eql(u8, media_type, "image/gif")) return "gif";
    if (std.mem.eql(u8, media_type, "image/webp")) return "webp";
    if (std.mem.eql(u8, media_type, "image/svg+xml")) return "svg";
    const ext = std.fs.path.extension(href);
    if (ext.len > 1) {
        const e = ext[1..];
        if (std.ascii.eqlIgnoreCase(e, "png")) return "png";
        if (std.ascii.eqlIgnoreCase(e, "gif")) return "gif";
        if (std.ascii.eqlIgnoreCase(e, "webp")) return "webp";
        if (std.ascii.eqlIgnoreCase(e, "svg")) return "svg";
    }
    return "jpg";
}

fn extractEntry(alloc: std.mem.Allocator, reader: *std.Io.File.Reader, entry: std.zip.Iterator.Entry) ![]u8 {
    if (entry.uncompressed_size > max_entry_bytes) return error.EntryTooLarge;
    var out: std.Io.Writer.Allocating = .init(alloc);
    errdefer out.deinit();
    try entry.extractTo(reader, &out.writer);
    return out.toOwnedSlice();
}

pub fn parse(alloc: std.mem.Allocator, path: []const u8) !Parsed {
    const file = try std.Io.Dir.cwd().openFile(util.io, path, .{});
    defer file.close(util.io);
    var read_buf: [64 * 1024]u8 = undefined;
    var reader = file.reader(util.io, &read_buf);
    var iter = std.zip.Iterator.init(&reader) catch return Error.NotEpub;

    var entries = std.StringHashMap(std.zip.Iterator.Entry).init(alloc);
    var name_buf: [4096]u8 = undefined;
    var has_encryption = false;
    while (iter.next() catch return Error.NotEpub) |entry| {
        const name = entry.getFilename(&reader, &name_buf, .{}) catch continue;
        if (std.mem.eql(u8, name, "META-INF/encryption.xml")) has_encryption = true;
        const key = try alloc.dupe(u8, name);
        try entries.put(key, entry);
    }
    if (has_encryption) {
        if (entries.get("META-INF/encryption.xml")) |enc_entry| {
            const enc = extractEntry(alloc, &reader, enc_entry) catch null;
            if (enc) |data| {
                if (std.mem.find(u8, data, "EncryptedData") != null) return Error.Encrypted;
            }
        }
    }

    const container_entry = entries.get("META-INF/container.xml") orelse return Error.MissingContainer;
    const container = try extractEntry(alloc, &reader, container_entry);
    const opf_path = blk: {
        var i: usize = 0;
        while (nextTag(container, i)) |tag| {
            i = tag.content_start;
            if (!tag.closing and std.mem.eql(u8, localName(tag.name), "rootfile")) {
                if (getAttr(tag.attrs, "full-path")) |p| break :blk try percentDecode(alloc, p);
            }
        }
        return Error.MissingContainer;
    };

    const opf_entry = entries.get(opf_path) orelse return Error.MissingOpf;
    const opf = try extractEntry(alloc, &reader, opf_entry);

    const title = (try elementText(alloc, opf, "title")) orelse blk: {
        break :blk try alloc.dupe(u8, "未命名书籍");
    };
    const author = try elementText(alloc, opf, "creator");
    const language = try elementText(alloc, opf, "language");

    var manifest: std.ArrayList(ManifestItem) = .empty;
    var spine_ids: std.ArrayList([]const u8) = .empty;
    var cover_id: ?[]const u8 = null;
    var cover_href_direct: ?[]const u8 = null;

    var i: usize = 0;
    while (nextTag(opf, i)) |tag| {
        i = tag.content_start;
        if (tag.closing) continue;
        const ln = localName(tag.name);
        if (std.mem.eql(u8, ln, "item")) {
            const id = getAttr(tag.attrs, "id") orelse continue;
            const href = getAttr(tag.attrs, "href") orelse continue;
            const media_type = getAttr(tag.attrs, "media-type") orelse "";
            const properties = getAttr(tag.attrs, "properties") orelse "";
            const resolved = try resolveHref(alloc, opf_path, href);
            try manifest.append(alloc, .{
                .id = try alloc.dupe(u8, id),
                .href = resolved,
                .media_type = try alloc.dupe(u8, media_type),
                .properties = try alloc.dupe(u8, properties),
            });
        } else if (std.mem.eql(u8, ln, "itemref")) {
            const idref = getAttr(tag.attrs, "idref") orelse continue;
            const linear = getAttr(tag.attrs, "linear") orelse "yes";
            if (std.ascii.eqlIgnoreCase(linear, "no")) continue;
            try spine_ids.append(alloc, try alloc.dupe(u8, idref));
        } else if (std.mem.eql(u8, ln, "meta")) {
            const name = getAttr(tag.attrs, "name") orelse "";
            if (std.ascii.eqlIgnoreCase(name, "cover")) {
                cover_id = getAttr(tag.attrs, "content");
            }
        } else if (std.mem.eql(u8, ln, "reference")) {
            const kind = getAttr(tag.attrs, "type") orelse "";
            if (std.ascii.eqlIgnoreCase(kind, "cover")) {
                cover_href_direct = getAttr(tag.attrs, "href");
            }
        }
    }

    var cover_item: ?ManifestItem = null;
    for (manifest.items) |item| {
        if (std.mem.find(u8, item.properties, "cover-image") != null) {
            cover_item = item;
            break;
        }
    }
    if (cover_item == null) {
        if (cover_id) |cid| {
            for (manifest.items) |item| {
                if (std.mem.eql(u8, item.id, cid)) {
                    cover_item = item;
                    break;
                }
            }
        }
    }
    if (cover_item == null) {
        for (manifest.items) |item| {
            if (std.mem.startsWith(u8, item.media_type, "image/") and
                (std.ascii.eqlIgnoreCase(item.id, "cover") or
                    std.mem.find(u8, item.href, "cover") != null))
            {
                cover_item = item;
                break;
            }
        }
    }
    if (cover_item == null and cover_href_direct != null) {
        const resolved = try resolveHref(alloc, opf_path, cover_href_direct.?);
        for (manifest.items) |item| {
            if (std.mem.eql(u8, item.href, resolved)) {
                cover_item = item;
                break;
            }
        }
    }

    var cover_bytes: ?[]const u8 = null;
    var cover_ext: ?[]const u8 = null;
    if (cover_item) |item| {
        if (entries.get(item.href)) |entry| {
            cover_bytes = extractEntry(alloc, &reader, entry) catch null;
            if (cover_bytes != null) cover_ext = coverExtension(item.media_type, item.href);
        }
    }

    var chapters: std.ArrayList(Chapter) = .empty;
    for (spine_ids.items) |idref| {
        var href: ?[]const u8 = null;
        var media_type: []const u8 = "";
        for (manifest.items) |item| {
            if (std.mem.eql(u8, item.id, idref)) {
                href = item.href;
                media_type = item.media_type;
                break;
            }
        }
        const h = href orelse continue;
        if (media_type.len > 0 and
            std.mem.find(u8, media_type, "html") == null and
            std.mem.find(u8, media_type, "xml") == null)
        {
            continue;
        }
        const entry = entries.get(h) orelse continue;
        const data = extractEntry(alloc, &reader, entry) catch continue;
        const text = htmlToText(alloc, data) catch continue;
        try chapters.append(alloc, .{ .href = h, .text = text });
    }

    return .{
        .title = title,
        .author = author,
        .language = language,
        .cover_bytes = cover_bytes,
        .cover_ext = cover_ext,
        .chapters = chapters.items,
    };
}
