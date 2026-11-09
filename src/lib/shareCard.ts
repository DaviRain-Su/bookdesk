// 分享卡片的排版。宽度用「一个汉字 = 1」的单位估算，不依赖画布测量，
// 这样书桌和全屏阅读、测试、导出的 SVG 用的是同一套换行。
// 字体都是本机已有的，不会去下载。

export type CardTemplate = "light" | "dark" | "vertical";
export type CardRatio = "square" | "portrait";

export const FOOTER_TEXT = "read with bookdesk / 书桌";

export const CARD_SERIF =
  '"Songti SC", "STSong", "Noto Serif CJK SC", "Source Han Serif SC", "Songti TC", "WenQuanYi Micro Hei", "Droid Sans Fallback", serif';
export const CARD_SANS =
  '"PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "Source Han Sans SC", "WenQuanYi Micro Hei", "Droid Sans Fallback", sans-serif';

export const CARD_TEMPLATES: { id: CardTemplate; label: string }[] = [
  { id: "light", label: "浅色" },
  { id: "dark", label: "夜间" },
  { id: "vertical", label: "竖排" },
];

export const CARD_RATIOS: { id: CardRatio; label: string; hint: string }[] = [
  { id: "square", label: "方形", hint: "1:1 · X / 朋友圈" },
  { id: "portrait", label: "竖图", hint: "3:4 · 小红书" },
];

const CARD_SIZE: Record<CardRatio, { width: number; height: number }> = {
  square: { width: 1080, height: 1080 },
  portrait: { width: 1080, height: 1440 },
};

const THEME = {
  light: {
    bg: "#f4efe4",
    ink: "#2b261f",
    muted: "#7b7366",
    note: "#514b42",
    accent: "#2f6b52",
    rule: "#e3d9c8",
    mark: "#e4d7b8",
    footer: "#8d867a",
    border: "#e3d9c8",
  },
  dark: {
    bg: "#171614",
    ink: "#f4efe6",
    muted: "#b3aa9c",
    note: "#ddd4c6",
    accent: "#e4c27a",
    rule: "#3a342c",
    mark: "#3c362c",
    footer: "#8a8378",
    border: "#3a342c",
  },
  vertical: {
    bg: "#f3ead7",
    ink: "#2a241c",
    muted: "#6f624f",
    note: "#4e4538",
    accent: "#8d3b32",
    rule: "#e4d5bb",
    mark: "#e4d0c4",
    footer: "#8a7b68",
    border: "#d9c7a6",
  },
} as const;

type Theme = {
  bg: string;
  ink: string;
  muted: string;
  note: string;
  accent: string;
  rule: string;
  mark: string;
  footer: string;
  border: string;
};

const HIGHLIGHT_ACCENT: Record<string, string> = {
  yellow: "#c8962e",
  green: "#2f6b52",
  blue: "#3d6f99",
  pink: "#b85c78",
  purple: "#6d5496",
};

const CLOSING = new Set(Array.from("，。、；：！？）》」』】〉〕〗〙〛,.!?;:)]}\"”’"));
const OPENING = new Set(Array.from("（《「『【〈〔〖〘〚([“‘"));

export interface ShareCardInput {
  quote: string;
  /** 只有用户勾选「带上笔记」时才传入。不传或空白都不会画到卡片上。 */
  note?: string | null;
  title: string;
  author?: string | null;
  chapter?: string | null;
  template: CardTemplate;
  ratio: CardRatio;
  /** 划线颜色 id。只影响色条，不影响正文。 */
  color?: string | null;
}

export interface PaintRect {
  kind: "rect";
  x: number;
  y: number;
  w: number;
  h: number;
  fill?: string;
  stroke?: string;
  strokeWidth?: number;
}

export interface PaintText {
  kind: "text";
  text: string;
  x: number;
  y: number;
  size: number;
  font: "serif" | "sans";
  fill: string;
  align: "left" | "center" | "right";
  /** 像素。正文行的估算宽度不应超过它（标点悬挂最多约一个字）。 */
  maxWidth: number;
  role: "quote" | "note" | "title" | "author" | "chapter" | "footer" | "mark";
}

export interface PaintVColumn {
  kind: "vcolumn";
  text: string;
  x: number;
  y: number;
  size: number;
  font: "serif" | "sans";
  fill: string;
  /** 纵向步进 = size * step */
  step: number;
  role: "quote" | "note";
  maxChars: number;
}

export type Paint = PaintRect | PaintText | PaintVColumn;

export interface CardLayout {
  width: number;
  height: number;
  template: CardTemplate;
  ratio: CardRatio;
  quoteLines: string[];
  quoteTruncated: boolean;
  noteLines: string[];
  noteTruncated: boolean;
  footer: string;
  /** 横排是字宽单位上限；竖排是每列字数上限。 */
  quoteMaxUnits: number;
  noteMaxUnits: number;
  metaTop: number;
  paints: Paint[];
}

export interface WrapResult {
  lines: string[];
  truncated: boolean;
}

export function charWidth(ch: string): number {
  const cp = ch.codePointAt(0) ?? 0;
  if (cp === 0 || cp < 0x20 || cp === 0x7f) return 0;
  if (cp >= 0x300 && cp <= 0x36f) return 0;
  if (ch === " ") return 0.33;
  if (cp < 0x7f) {
    if ("ijlI.,:;'`!|".includes(ch)) return 0.32;
    if ("ft".includes(ch)) return 0.38;
    if ("mwMW@#%&".includes(ch)) return 0.95;
    return 0.56;
  }
  if (cp >= 0xff61 && cp <= 0xffdc) return 0.55;
  return 1;
}

export function charUnits(text: string): number {
  let n = 0;
  for (const ch of text) n += charWidth(ch);
  return n;
}

export function normalizeQuote(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

function isWordChar(ch: string): boolean {
  return /[A-Za-z0-9]/.test(ch) || ch === "'" || ch === "’" || ch === "-" || ch === "–";
}

function tokensOf(text: string): string[] {
  const norm = text.replace(/\s+/g, " ").trim();
  const out: string[] = [];
  let word = "";
  const flush = () => {
    if (!word) return;
    out.push(word);
    word = "";
  };
  for (const ch of norm) {
    if (isWordChar(ch)) {
      word += ch;
      continue;
    }
    flush();
    out.push(ch === " " ? " " : ch);
  }
  flush();
  return out;
}

function explode(parts: string[], maxWidth: number): string[] {
  const out: string[] = [];
  for (const part of parts) {
    if (part === " " || charUnits(part) <= maxWidth) {
      out.push(part);
      continue;
    }
    let buf = "";
    let w = 0;
    for (const ch of part) {
      const cw = charWidth(ch);
      if (buf && w + cw > maxWidth) {
        out.push(buf);
        buf = "";
        w = 0;
      }
      buf += ch;
      w += cw;
    }
    if (buf) out.push(buf);
  }
  return out;
}

function ellipsize(line: string, maxWidth: number): string {
  const ell = "…";
  const chars = Array.from(line.replace(/…+$/u, ""));
  while (chars.length > 0 && charUnits(chars.join("") + ell) > maxWidth + 0.01) {
    chars.pop();
  }
  const next = chars.join("") + ell;
  if (charUnits(next) > maxWidth + 0.01) return ell;
  return next;
}

/**
 * 按字宽换行。中文可在字间断开；英文优先在词间断开。
 * 行尾不留开引号，行首不放句读（能挂到上一行就挂，最多超出约一个字）。
 * 超出 maxLines 时最后一行以 … 收束。
 */
export function wrapText(text: string, maxWidth: number, maxLines: number): WrapResult {
  const glyphs = explode(tokensOf(text), Math.max(maxWidth, 0.3));
  if (maxLines <= 0) return { lines: [], truncated: glyphs.length > 0 };
  if (glyphs.length === 0 || maxWidth <= 0) {
    return { lines: [], truncated: glyphs.length > 0 };
  }

  const lines: string[] = [];
  let cur = "";
  let curW = 0;
  let i = 0;
  let truncated = false;
  const fits = (extra: string) => curW + charUnits(extra) <= maxWidth + 0.01;

  while (i < glyphs.length) {
    if (lines.length >= maxLines && cur === "") {
      truncated = true;
      break;
    }
    const g = glyphs[i] ?? "";
    if (g === " " && cur === "") {
      i += 1;
      continue;
    }
    const w = charUnits(g);
    const hang = g.length === 1 && CLOSING.has(g) && cur !== "" && curW + w <= maxWidth + 1.05;
    const next = glyphs[i + 1];
    if (
      g.length === 1 &&
      OPENING.has(g) &&
      cur !== "" &&
      next &&
      next !== " " &&
      fits(g) &&
      curW + w + charUnits(next) > maxWidth + 0.01
    ) {
      lines.push(cur.replace(/ +$/u, ""));
      cur = "";
      curW = 0;
      if (lines.length >= maxLines) {
        truncated = true;
        break;
      }
      continue;
    }
    if (cur !== "" && !fits(g) && !hang) {
      lines.push(cur.replace(/ +$/u, ""));
      cur = "";
      curW = 0;
      if (lines.length >= maxLines) {
        truncated = true;
        break;
      }
      continue;
    }
    if (cur === "" && lines.length > 0 && g.length === 1 && CLOSING.has(g)) {
      const prev = lines[lines.length - 1] ?? "";
      if (charUnits(prev + g) <= maxWidth + 1.05) {
        lines[lines.length - 1] = prev + g;
        i += 1;
        continue;
      }
    }
    if (cur === "" && w > maxWidth) {
      cur = g;
      curW = w;
      i += 1;
      lines.push(cur);
      cur = "";
      curW = 0;
      if (i < glyphs.length && lines.length >= maxLines) {
        truncated = true;
        break;
      }
      continue;
    }
    cur += g;
    curW += w;
    i += 1;
  }
  if (cur && lines.length < maxLines) lines.push(cur.replace(/ +$/u, ""));
  else if (cur) truncated = true;

  const cleaned = lines.map((line) => line.replace(/ +$/u, "")).filter((line) => line.length > 0);
  if (truncated && cleaned.length > 0) {
    const last = cleaned.length - 1;
    cleaned[last] = ellipsize(cleaned[last] ?? "", maxWidth);
  }
  return { lines: cleaned, truncated };
}

function isCJK(ch: string): boolean {
  const cp = ch.codePointAt(0) ?? 0;
  return cp > 0x7f && charWidth(ch) >= 0.9;
}

/** 竖排用的字序列。汉字两侧的空格去掉，英文单词之间保留一个空格。 */
export function verticalGlyphs(text: string): string[] {
  const norm = text.replace(/\s+/g, " ").trim();
  const raw: string[] = [];
  for (const ch of norm) raw.push(ch);
  const out: string[] = [];
  for (let i = 0; i < raw.length; i += 1) {
    const ch = raw[i] ?? "";
    if (ch !== " ") {
      out.push(ch);
      continue;
    }
    const prev = out[out.length - 1];
    const next = raw[i + 1];
    if (!prev || !next) continue;
    if (isCJK(prev) || isCJK(next)) continue;
    out.push(" ");
  }
  return out;
}

export function columnize(text: string, maxChars: number, maxCols: number): WrapResult {
  const glyphs = verticalGlyphs(text);
  if (maxCols <= 0 || maxChars <= 0) {
    return { lines: [], truncated: glyphs.length > 0 };
  }
  if (glyphs.length === 0) return { lines: [], truncated: false };
  const lines: string[] = [];
  let idx = 0;
  let truncated = false;
  while (idx < glyphs.length) {
    if (lines.length >= maxCols) {
      truncated = true;
      break;
    }
    const rest = glyphs.length - idx;
    const lastCol = lines.length === maxCols - 1;
    if (lastCol && rest > maxChars) {
      const kept = maxChars === 1 ? [] : glyphs.slice(idx, idx + maxChars - 1);
      lines.push(kept.join("") + "…");
      truncated = true;
      break;
    }
    lines.push(glyphs.slice(idx, idx + maxChars).join(""));
    idx += maxChars;
  }
  return { lines, truncated };
}

export interface NoteCard {
  quote?: string | null;
  body?: string | null;
  sourceBookId?: string | null;
  quoteLocation?: { bookId?: string | null } | null;
  updatedAt?: number;
}

/** 同一本书、同一段划线的卡片正文。没有就返回 null。多张时用最近改过的。 */
export function findCardNote(cards: NoteCard[], bookId: string, quote: string): string | null {
  const key = normalizeQuote(quote);
  if (!bookId || !key) return null;
  let best: { note: string; updated: number } | null = null;
  for (const card of cards) {
    const cardQuote = normalizeQuote(card.quote ?? "");
    if (!cardQuote || cardQuote !== key) continue;
    const owner = card.sourceBookId || card.quoteLocation?.bookId || "";
    if (owner !== bookId) continue;
    const note = (card.body ?? "").trim();
    if (!note) continue;
    const updated = card.updatedAt ?? 0;
    if (!best || updated >= best.updated) best = { note, updated };
  }
  return best?.note ?? null;
}

/** 卡片上的来源是「书名 · 章节」。章节可能自己也含间隔号。 */
/** 全选只勾划线。笔记要另外勾，默认不带出去。 */
export function selectAllQuoteIds(ids: string[]): { quoteIds: string[]; noteIds: string[] } {
  return { quoteIds: [...ids], noteIds: [] };
}

export function chapterFromSource(
  source: string | null | undefined,
  title?: string | null,
): string | null {
  const parts = (source ?? "")
    .split(" · ")
    .map((part) => part.trim())
    .filter(Boolean);
  if (parts.length < 2) return null;
  const book = (title ?? "").trim();
  if (book && parts[0] !== book) return parts.slice(1).join(" · ") || null;
  return parts.slice(1).join(" · ") || null;
}

function themeFor(template: CardTemplate): Theme {
  if (template === "dark") return THEME.dark;
  if (template === "vertical") return THEME.vertical;
  return THEME.light;
}

function accentFor(template: CardTemplate, color?: string | null): string {
  const theme = themeFor(template);
  if (color && HIGHLIGHT_ACCENT[color]) return HIGHLIGHT_ACCENT[color];
  return theme.accent;
}

interface MetaGeom {
  footerY: number;
  authorY: number;
  titleFirstY: number;
  titleLastY: number;
  titleStep: number;
  titleSize: number;
  metaTop: number;
}

function metaGeom(height: number, titleLines: number): MetaGeom {
  const footerY = height - 52;
  const authorY = footerY - 40;
  const titleStep = 36;
  const titleSize = 28;
  const count = Math.max(1, titleLines);
  const titleLastY = authorY - 36;
  const titleFirstY = titleLastY - (count - 1) * titleStep;
  const metaTop = titleFirstY - titleSize - 20;
  return { footerY, authorY, titleFirstY, titleLastY, titleStep, titleSize, metaTop };
}

function authorLine(author?: string | null, chapter?: string | null): string {
  return [author?.trim() || "", chapter?.trim() || ""].filter(Boolean).join("  ·  ");
}

export function layoutShareCard(input: ShareCardInput): CardLayout {
  const size = CARD_SIZE[input.ratio];
  const theme = themeFor(input.template);
  const accent = accentFor(input.template, input.color);
  const title = input.title.trim() || "未命名书籍";
  const byline = authorLine(input.author, input.chapter);
  const noteText = (input.note ?? "").trim();
  const hasNote = noteText.length > 0;
  const padX = 84;
  const titleBudget = (size.width - padX * 2) / 28;
  const titleWrap = wrapText(title, titleBudget, 2);
  const bylineWrap = byline
    ? wrapText(byline, (size.width - padX * 2) / 20, 1)
    : { lines: [], truncated: false };
  const meta = metaGeom(size.height, titleWrap.lines.length || 1);
  const paints: Paint[] = [];

  paints.push({ kind: "rect", x: 0, y: 0, w: size.width, h: size.height, fill: theme.bg });

  let quoteLines: string[] = [];
  let quoteTruncated = false;
  let noteLines: string[] = [];
  let noteTruncated = false;
  let quoteMaxUnits = 0;
  let noteMaxUnits = 0;

  if (input.template === "vertical") {
    const laid = layoutVertical({
      input,
      theme,
      accent,
      size,
      padX,
      meta,
      hasNote,
      noteText,
      paints,
    });
    quoteLines = laid.quoteLines;
    quoteTruncated = laid.quoteTruncated;
    noteLines = laid.noteLines;
    noteTruncated = laid.noteTruncated;
    quoteMaxUnits = laid.quoteMaxUnits;
    noteMaxUnits = laid.noteMaxUnits;
  } else {
    paints.push({ kind: "rect", x: 0, y: 0, w: 12, h: size.height, fill: accent });
    const laid = layoutHorizontal({
      input,
      theme,
      size,
      padX,
      meta,
      hasNote,
      noteText,
      paints,
    });
    quoteLines = laid.quoteLines;
    quoteTruncated = laid.quoteTruncated;
    noteLines = laid.noteLines;
    noteTruncated = laid.noteTruncated;
    quoteMaxUnits = laid.quoteMaxUnits;
    noteMaxUnits = laid.noteMaxUnits;
  }

  const titleSize = meta.titleSize;
  titleWrap.lines.forEach((line, index) => {
    paints.push({
      kind: "text",
      text: line,
      x: padX,
      y: meta.titleFirstY + index * meta.titleStep,
      size: titleSize,
      font: "sans",
      fill: theme.ink,
      align: "left",
      maxWidth: size.width - padX * 2,
      role: "title",
    });
  });
  if (bylineWrap.lines[0]) {
    paints.push({
      kind: "text",
      text: bylineWrap.lines[0],
      x: padX,
      y: meta.authorY,
      size: 20,
      font: "sans",
      fill: theme.muted,
      align: "left",
      maxWidth: size.width - padX * 2,
      role: input.chapter?.trim() ? "chapter" : "author",
    });
  }
  paints.push({
    kind: "text",
    text: FOOTER_TEXT,
    x: size.width / 2,
    y: meta.footerY,
    size: 16,
    font: "sans",
    fill: theme.footer,
    align: "center",
    maxWidth: size.width - padX * 2,
    role: "footer",
  });

  return {
    width: size.width,
    height: size.height,
    template: input.template,
    ratio: input.ratio,
    quoteLines,
    quoteTruncated,
    noteLines,
    noteTruncated,
    footer: FOOTER_TEXT,
    quoteMaxUnits,
    noteMaxUnits,
    metaTop: meta.metaTop,
    paints,
  };
}

interface LaidText {
  quoteLines: string[];
  quoteTruncated: boolean;
  noteLines: string[];
  noteTruncated: boolean;
  quoteMaxUnits: number;
  noteMaxUnits: number;
}

function layoutHorizontal(args: {
  input: ShareCardInput;
  theme: Theme;
  size: { width: number; height: number };
  padX: number;
  meta: MetaGeom;
  hasNote: boolean;
  noteText: string;
  paints: Paint[];
}): LaidText {
  const { theme, size, padX, meta, hasNote, noteText, paints } = args;
  const quoteSize = args.input.ratio === "portrait" ? 48 : 44;
  const quoteStep = Math.round(quoteSize * 1.55);
  const noteSize = 24;
  const noteStep = 36;
  const noteMaxLines = hasNote ? (args.input.ratio === "portrait" ? 4 : 3) : 0;
  const noteBlock = hasNote ? 24 + noteMaxLines * noteStep : 0;
  const quoteTop = 108;
  const quoteMaxUnits = (size.width - padX * 2) / quoteSize;
  const noteMaxUnits = (size.width - padX * 2) / noteSize;
  const quoteMaxLines = Math.max(
    2,
    Math.floor((meta.metaTop - noteBlock - quoteTop) / quoteStep),
  );
  const quoteSource = args.input.quote.trim() || "（没有文字）";
  const quote = wrapText(quoteSource, quoteMaxUnits, quoteMaxLines);
  let baseline = quoteTop + quoteSize;
  paints.push({
    kind: "text",
    text: "“",
    x: padX,
    y: quoteTop + 18,
    size: 86,
    font: "serif",
    fill: theme.mark,
    align: "left",
    maxWidth: 120,
    role: "mark",
  });
  for (const line of quote.lines) {
    paints.push({
      kind: "text",
      text: line,
      x: padX,
      y: baseline,
      size: quoteSize,
      font: "serif",
      fill: theme.ink,
      align: "left",
      maxWidth: size.width - padX * 2,
      role: "quote",
    });
    baseline += quoteStep;
  }
  let noteLines: string[] = [];
  let noteTruncated = false;
  if (hasNote) {
    const note = wrapText(noteText, noteMaxUnits, noteMaxLines);
    noteLines = note.lines;
    noteTruncated = note.truncated;
    const ruleY = Math.min(baseline + 8, meta.metaTop - noteBlock + 8);
    paints.push({
      kind: "rect",
      x: padX,
      y: ruleY,
      w: 48,
      h: 2,
      fill: theme.rule,
    });
    let noteY = ruleY + 22 + noteSize;
    for (const line of note.lines) {
      if (noteY > meta.metaTop - 4) break;
      paints.push({
        kind: "text",
        text: line,
        x: padX,
        y: noteY,
        size: noteSize,
        font: "serif",
        fill: theme.note,
        align: "left",
        maxWidth: size.width - padX * 2,
        role: "note",
      });
      noteY += noteStep;
    }
  }
  return {
    quoteLines: quote.lines,
    quoteTruncated: quote.truncated,
    noteLines,
    noteTruncated,
    quoteMaxUnits,
    noteMaxUnits: hasNote ? noteMaxUnits : 0,
  };
}

function layoutVertical(args: {
  input: ShareCardInput;
  theme: Theme;
  accent: string;
  size: { width: number; height: number };
  padX: number;
  meta: MetaGeom;
  hasNote: boolean;
  noteText: string;
  paints: Paint[];
}): LaidText {
  const { theme, accent, size, padX, meta, hasNote, noteText, paints } = args;
  const inset = 36;
  paints.push({
    kind: "rect",
    x: inset,
    y: inset,
    w: size.width - inset * 2,
    h: size.height - inset * 2,
    stroke: theme.border,
    strokeWidth: 1,
  });
  paints.push({
    kind: "rect",
    x: inset + 6,
    y: inset + 6,
    w: size.width - (inset + 6) * 2,
    h: size.height - (inset + 6) * 2,
    stroke: theme.border,
    strokeWidth: 1,
  });
  paints.push({ kind: "rect", x: 0, y: 0, w: size.width, h: 8, fill: accent });

  const quoteSize = 40;
  const step = 1.16;
  const padTop = 72;
  const avail = Math.max(quoteSize * 4, meta.metaTop - padTop - 8);
  const maxChars = Math.max(4, Math.floor(avail / (quoteSize * step)));
  const pitch = Math.round(quoteSize * 1.35);
  const maxCols = Math.max(4, Math.floor((size.width - padX * 2) / pitch));
  const noteCols = hasNote ? Math.min(5, Math.max(2, Math.floor(maxCols * 0.28))) : 0;
  const gap = hasNote ? 1 : 0;
  const quoteCols = Math.max(2, maxCols - noteCols - gap);
  const quoteSource = args.input.quote.trim() || "（没有文字）";
  const quote = columnize(quoteSource, maxChars, quoteCols);
  const note = hasNote
    ? columnize(noteText, maxChars, noteCols)
    : { lines: [], truncated: false };
  const innerRight = size.width - padX - quoteSize;
  const place = (lines: string[], startIndex: number, role: "quote" | "note", fill: string) => {
    lines.forEach((line, index) => {
      const x = innerRight - (startIndex + index) * pitch;
      if (x < padX - 1) return;
      paints.push({
        kind: "vcolumn",
        text: line,
        x,
        y: padTop,
        size: quoteSize,
        font: "serif",
        fill,
        step,
        role,
        maxChars,
      });
    });
  };
  place(quote.lines, 0, "quote", theme.ink);
  if (hasNote) place(note.lines, quote.lines.length + gap, "note", theme.note);

  return {
    quoteLines: quote.lines,
    quoteTruncated: quote.truncated,
    noteLines: note.lines,
    noteTruncated: note.truncated,
    quoteMaxUnits: maxChars,
    noteMaxUnits: hasNote ? maxChars : 0,
  };
}

export function layoutProblems(layout: CardLayout): string[] {
  const problems: string[] = [];
  const footers = layout.paints.filter(
    (paint): paint is PaintText => paint.kind === "text" && paint.role === "footer",
  );
  if (footers.length !== 1 || footers[0]?.text !== FOOTER_TEXT) {
    problems.push("footer");
  }
  const notePainted = layout.paints.some((paint) => paint.kind !== "rect" && paint.role === "note");
  if (layout.noteLines.length === 0 && notePainted) problems.push("note painted without lines");
  if (layout.noteLines.length > 0 && !notePainted) problems.push("note lines not painted");

  for (const paint of layout.paints) {
    if (paint.kind === "rect") {
      if (
        paint.x < -0.5 ||
        paint.y < -0.5 ||
        paint.x + paint.w > layout.width + 0.5 ||
        paint.y + paint.h > layout.height + 0.5
      ) {
        problems.push("rect outside");
      }
      continue;
    }
    if (paint.kind === "text") {
      const width = charUnits(paint.text) * paint.size;
      let left = paint.x;
      if (paint.align === "center") left = paint.x - width / 2;
      if (paint.align === "right") left = paint.x - width;
      const right = left + width;
      const hang = paint.role === "quote" || paint.role === "note" ? paint.size : 0;
      if (left < -1 || right > layout.width + hang + 1) {
        problems.push(`text outside ${paint.role}`);
      }
      if (paint.y < paint.size - 8 || paint.y > layout.height - 8) {
        problems.push(`text baseline ${paint.role}`);
      }
      if (
        (paint.role === "quote" || paint.role === "note" || paint.role === "title") &&
        charUnits(paint.text) * paint.size > paint.maxWidth + paint.size * 1.08
      ) {
        problems.push(`line too wide ${paint.role}`);
      }
      if (
        (paint.role === "quote" || paint.role === "note") &&
        layout.template !== "vertical" &&
        paint.y > layout.metaTop
      ) {
        problems.push(`text overlaps meta ${paint.role}`);
      }
      continue;
    }
    const chars = Array.from(paint.text);
    if (chars.length > paint.maxChars) problems.push("column too many chars");
    const bottom = paint.y + chars.length * paint.size * paint.step;
    if (
      paint.x < -1 ||
      paint.x + paint.size > layout.width + 1 ||
      paint.y < -1 ||
      bottom > layout.metaTop + 1
    ) {
      problems.push(`column outside ${paint.role}`);
    }
  }
  return problems;
}

function xmlEscape(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/** 把同一套排版画成不含外链的 SVG，用来在没有桌面端的环境里出样张。 */
export function layoutToSVG(layout: CardLayout): string {
  const font = (kind: "serif" | "sans") => (kind === "serif" ? CARD_SERIF : CARD_SANS);
  const parts: string[] = [];
  parts.push(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${layout.width}" height="${layout.height}" viewBox="0 0 ${layout.width} ${layout.height}">`,
  );
  for (const paint of layout.paints) {
    if (paint.kind === "rect") {
      const fill = paint.fill ? ` fill="${paint.fill}"` : ` fill="none"`;
      const stroke = paint.stroke
        ? ` stroke="${paint.stroke}" stroke-width="${paint.strokeWidth ?? 1}"`
        : "";
      parts.push(
        `<rect x="${paint.x}" y="${paint.y}" width="${paint.w}" height="${paint.h}"${fill}${stroke}/>`,
      );
      continue;
    }
    if (paint.kind === "text") {
      const anchor =
        paint.align === "center" ? "middle" : paint.align === "right" ? "end" : "start";
      parts.push(
        `<text x="${paint.x}" y="${paint.y}" fill="${paint.fill}" font-size="${paint.size}" font-family="${xmlEscape(font(paint.font))}" text-anchor="${anchor}">${xmlEscape(paint.text)}</text>`,
      );
      continue;
    }
    const chars = Array.from(paint.text);
    chars.forEach((ch, index) => {
      const punct = "，。、；：！？".includes(ch);
      const x = paint.x + (punct ? paint.size * 0.42 : 0);
      const y = paint.y + (index + 1) * paint.size * paint.step - (punct ? paint.size * 0.15 : 0);
      parts.push(
        `<text x="${x}" y="${y}" fill="${paint.fill}" font-size="${paint.size}" font-family="${xmlEscape(font(paint.font))}">${xmlEscape(ch)}</text>`,
      );
    });
  }
  parts.push("</svg>");
  return parts.join("");
}
