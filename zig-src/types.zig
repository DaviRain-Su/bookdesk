const std = @import("std");

pub const Book = struct {
    id: []const u8,
    title: []const u8,
    author: ?[]const u8 = null,
    language: ?[]const u8 = null,
    coverRelPath: ?[]const u8 = null,
    epubRelPath: []const u8,
    importedAt: i64,
};

pub const ImportError = struct {
    path: []const u8,
    message: []const u8,
};

pub const ImportResult = struct {
    books: []Book,
    errors: []ImportError,
};

pub const ImportProgress = struct {
    done: i64,
    total: i64,
    title: []const u8,
};

pub const BookLocation = struct {
    bookId: []const u8,
    cfi: ?[]const u8 = null,
    spineIndex: i64 = 0,
    href: ?[]const u8 = null,
    pageLabel: ?[]const u8 = null,
    charOffset: ?i64 = null,
};

pub const Highlight = struct {
    id: []const u8,
    bookId: []const u8,
    location: BookLocation,
    text: []const u8,
    color: []const u8 = "yellow",
    createdAt: i64,
};

pub const Card = struct {
    id: []const u8,
    type: []const u8,
    title: ?[]const u8 = null,
    body: ?[]const u8 = null,
    quote: ?[]const u8 = null,
    quoteLocation: ?BookLocation = null,
    imageRelPath: ?[]const u8 = null,
    sourceBookId: ?[]const u8 = null,
    characterName: ?[]const u8 = null,
    createdAt: i64,
    updatedAt: i64,
};

pub const Board = struct {
    id: []const u8,
    name: []const u8,
    viewportJson: []const u8,
    nodesJson: []const u8,
    edgesJson: []const u8,
    updatedAt: i64,
};

pub const BoardSummary = struct {
    id: []const u8,
    name: []const u8,
    updatedAt: i64,
};

pub const SearchHit = struct {
    bookId: []const u8,
    title: []const u8,
    location: BookLocation,
    snippet: []const u8,
    highlights: [][2]i64,
};

pub const ChapterText = struct {
    spineIndex: i64,
    href: ?[]const u8 = null,
    text: []const u8,
};

pub const SaveCardInput = struct {
    id: ?[]const u8 = null,
    type: []const u8,
    title: ?[]const u8 = null,
    body: ?[]const u8 = null,
    quote: ?[]const u8 = null,
    quoteLocation: ?BookLocation = null,
    imageRelPath: ?[]const u8 = null,
    sourceBookId: ?[]const u8 = null,
    characterName: ?[]const u8 = null,
};

pub const SaveHighlightInput = struct {
    id: ?[]const u8 = null,
    bookId: []const u8,
    location: BookLocation,
    text: []const u8,
    color: ?[]const u8 = null,
};

// ---------- 人物与 AI ----------

pub const Character = struct {
    id: []const u8,
    bookId: []const u8,
    name: []const u8,
    description: ?[]const u8 = null,
    imageRelPath: ?[]const u8 = null,
    createdAt: i64,
};

pub const SaveCharacterInput = struct {
    id: ?[]const u8 = null,
    bookId: []const u8,
    name: []const u8,
    description: ?[]const u8 = null,
    imageRelPath: ?[]const u8 = null,
};

pub const AiStatus = struct {
    hasKey: bool,
    model: []const u8,
    baseUrl: []const u8,
    /// "acp"（外部 Agent）或 "builtin"（内置 Anthropic API）。
    engine: []const u8,
    agentCommand: []const u8,
    agentRunning: bool,
    agentName: []const u8,
};

/// 外部 Agent 的流式输出，按 turnId 对应到前端那一次提问。
pub const AcpUpdate = struct {
    turnId: []const u8,
    /// "text" | "thought" | "tool"
    kind: []const u8,
    text: []const u8,
};

pub const AgentTest = struct {
    ok: bool,
    name: []const u8 = "",
    message: []const u8 = "",
};

/// 设置页里可以点选的已知 ACP Agent（按本机是否安装检测）。
pub const AgentCandidate = struct {
    id: []const u8,
    name: []const u8,
    /// 选中后保存的命令；未安装且无法用 npx 拉取时为空。
    command: []const u8,
    /// "installed"（PATH 里有可执行文件）| "download"（可用 npx 拉取）| "missing"
    state: []const u8,
    /// 找到的可执行文件名，或安装提示。
    detail: []const u8,
};

pub const ChatInput = struct {
    bookId: []const u8,
    bookTitle: []const u8,
    characterName: []const u8,
    characterDescription: ?[]const u8 = null,
    question: []const u8,
    /// 当前可见页的文字，由前端从阅读器里取。
    visibleText: []const u8 = "",
    chapterTitle: ?[]const u8 = null,
    /// 当前阅读位置所在的 spine 序号；关闭「允许后续章节」时只检索它之前的章节。
    currentSpineIndex: i64 = 0,
    allowFuture: bool = false,
    /// 前端为这一次提问生成的 id，流式事件按它归属。
    turnId: []const u8 = "",
    /// ACP 会话的键：同一个人物复用同一个会话，Agent 才能记住上下文。
    sessionKey: []const u8 = "",
};

pub const ChatReply = struct {
    ok: bool,
    text: []const u8 = "",
    /// ok 为 false 时给用户看的中文说明。
    message: []const u8 = "",
};

pub const WordLookupInput = struct {
    word: []const u8,
    sentence: ?[]const u8 = null,
    bookTitle: ?[]const u8 = null,
    turnId: []const u8 = "",
};
