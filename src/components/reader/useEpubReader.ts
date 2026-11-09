import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import ePub, {
  type Book as EpubBook,
  type Contents,
  type NavItem,
  type Rendition,
} from "epubjs";
import { Library, Notes, type ReadingSettings } from "../../mygo";
import { registerReadingContext } from "../../lib/reading";
import { convertHan } from "../../lib/han";
import { REVEAL_EVENT, type RevealTarget } from "../../lib/reveal";
import { highlightFill } from "./highlightColors";
import { themeById, type ReaderTheme } from "./settings";

export interface TocEntry {
  label: string;
  href: string;
  depth: number;
}

export interface ReaderSelection {
  cfiRange: string;
  text: string;
  x: number;
  y: number;
  flip: boolean;
  existingId?: string;
}

export interface ReaderHover {
  word: string;
  sentence: string;
  x: number;
  y: number;
  flip: boolean;
}

export interface ReaderProgress {
  bookId: string;
  cfi: string;
  spineIndex: number;
  percent: number;
  chapter: string;
}

export type ReaderAction =
  | "highlight"
  | "recolor"
  | "card"
  | "remove"
  | "character"
  | "lookup"
  | "ask"
  | "explain"
  | "translate"
  | "share";

export interface UseEpubReaderOptions {
  bookId: string;
  title: string;
  initialCfi?: string | null;
  initialSpineIndex?: number;
  initialPercent?: number;
  initialReveal?: RevealTarget | null;
  /** 只有全屏阅读传；书卡不传，保持原来的紧凑样式。 */
  settings?: ReadingSettings;
  /** 全屏阅读里创建人物后可以直接开始对话；书卡保持原来的行为。 */
  mode?: "card" | "reader";
  onProgress?: (progress: ReaderProgress) => void;
  onReady?: () => void;
}

export interface EpubReader {
  status: "loading" | "ready" | "error";
  error: string;
  toc: TocEntry[];
  chapter: string;
  page: { index: number; total: number; percent: number };
  selection: ReaderSelection | null;
  hover: ReaderHover | null;
  turn: (dir: "prev" | "next") => void;
  jumpTo: (href: string) => void;
  reveal: (target: RevealTarget) => void;
  runAction: (action: ReaderAction, arg?: string) => void;
  dismissSelection: () => void;
  dismissHover: () => void;
}

const highlightStyles = (color: string) => ({
  fill: highlightFill(color),
  "fill-opacity": "0.6",
  "mix-blend-mode": "multiply",
});

const SEARCH_HIGHLIGHT_STYLES = {
  fill: "#8fc1e3",
  "fill-opacity": "0.55",
  "mix-blend-mode": "multiply",
};

const THEME_STYLE_ID = "epubjs-inserted-css-default";

/** 全屏阅读的主题样式；用 registerCss 整体替换，反复改字号也不会越堆越多。 */
function readerThemeCss(settings: ReadingSettings, theme: ReaderTheme): string {
  // 夜间模式把书自带的深色字也压成浅色，否则黑字会消失在黑底上。
  const forceInk =
    settings.theme === "night"
      ? `html body p, html body div, html body span, html body li,
         html body blockquote, html body td, html body th,
         html body h1, html body h2, html body h3, html body h4,
         html body h5, html body h6, html body em, html body strong,
         html body b, html body i, html body small, html body figcaption {
           color: ${theme.ink} !important;
         }`
      : "";
  return `
html, body {
  background: ${theme.bg} !important;
  color: ${theme.ink} !important;
}
body {
  font-family: "Songti SC", "STSong", "Noto Serif SC", "Source Han Serif SC", serif;
  font-size: ${settings.fontSize}px !important;
  line-height: ${settings.lineHeight} !important;
  margin: 0;
}
p {
  margin: 0;
  margin-block-end: 0.9em;
  text-indent: 2em;
}
h1, h2, h3 {
  font-weight: 600;
  line-height: 1.5;
}
::selection {
  background: ${theme.selection};
}
.rc-highlight {
  cursor: pointer;
}
a {
  color: ${theme.link} !important;
}
img {
  max-width: 100%;
}
${forceInk}
`;
}

function locateText(nodes: Text[], index: number): { node: Text; offset: number } | null {
  let acc = 0;
  for (const node of nodes) {
    const len = node.data.length;
    if (index <= acc + len) return { node, offset: index - acc };
    acc += len;
  }
  return null;
}

/** 在渲染后的章节里找到第 occurrence 次出现的 query，返回 DOM Range。 */
function findOccurrenceRange(doc: Document, query: string, occurrence: number): Range | null {
  const needle = query.trim().toLowerCase();
  if (!needle) return null;
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  let full = "";
  while (walker.nextNode()) {
    const node = walker.currentNode as Text;
    if (!node.data) continue;
    nodes.push(node);
    full += node.data;
  }
  const haystack = full.toLowerCase();
  const matches: number[] = [];
  let from = 0;
  while (matches.length <= occurrence) {
    const found = haystack.indexOf(needle, from);
    if (found < 0) break;
    matches.push(found);
    from = found + Math.max(1, needle.length);
  }
  const index = matches[Math.min(occurrence, matches.length - 1)];
  if (index === undefined) return null;
  const start = locateText(nodes, index);
  const end = locateText(nodes, index + needle.length);
  if (!start || !end) return null;
  const range = doc.createRange();
  range.setStart(start.node, start.offset);
  range.setEnd(end.node, end.offset);
  return range;
}

function flattenToc(items: NavItem[] | undefined, depth = 0): TocEntry[] {
  if (!items) return [];
  const out: TocEntry[] = [];
  for (const item of items) {
    const href = String(item.href ?? "");
    if (href) {
      out.push({ label: (item.label || "未命名章节").trim(), href, depth });
    }
    if (item.subitems?.length) {
      out.push(...flattenToc(item.subitems, depth + 1));
    }
  }
  return out;
}

function hrefKey(href: string): string {
  return href.split("#")[0] ?? href;
}

function bookSize(book: EpubBook): number {
  const spine = book.spine as unknown as { length?: number; spineItems?: unknown[] };
  return spine.length ?? spine.spineItems?.length ?? 0;
}

function highlightsChanged() {
  window.dispatchEvent(new CustomEvent("bookdesk:highlights-changed"));
}

/** 划选的是不是英文单词或短语：决定要不要显示「查词」。 */
export function isEnglishSelection(text: string): boolean {
  const t = text.replace(/\s+/g, " ").trim();
  if (!t || t.length > 48) return false;
  return /^[A-Za-z][A-Za-z'’\-]*(?: [A-Za-z][A-Za-z'’\-]*){0,4}$/.test(t);
}

/** 取选区所在段落，作为查词的语境。 */
export function sentenceAround(range: Range): string {
  let el: Element | null = range.startContainer.parentElement;
  while (el && !/^(P|LI|BLOCKQUOTE|DIV|TD|TH|H1|H2|H3|H4|H5|H6)$/.test(el.tagName)) {
    el = el.parentElement;
  }
  const text = (el?.textContent ?? "").replace(/\s+/g, " ").trim();
  return text.slice(0, 600);
}

/**
 * 书卡和全屏阅读共用的 epub.js 引擎。两边只负责自己的外壳，
 * 打开书、翻页、划线、悬停查词、目录和进度都走这一份代码。
 */
export function useEpubReader(
  hostRef: RefObject<HTMLDivElement | null>,
  options: UseEpubReaderOptions,
): EpubReader {
  const { bookId } = options;
  const titleRef = useRef(options.title);
  titleRef.current = options.title;
  const modeRef = useRef(options.mode ?? "card");
  modeRef.current = options.mode ?? "card";
  const onProgressRef = useRef(options.onProgress);
  onProgressRef.current = options.onProgress;
  const onReadyRef = useRef(options.onReady);
  onReadyRef.current = options.onReady;
  const initialCfi = useRef<string | null>(options.initialCfi ?? null);
  const initialSpineIndex = useRef<number>(options.initialSpineIndex ?? 0);
  const initialReveal = useRef<RevealTarget | null>(options.initialReveal ?? null);

  const renditionRef = useRef<Rendition | null>(null);
  const bookRef = useRef<EpubBook | null>(null);
  const tocRef = useRef<TocEntry[]>([]);
  const lastCfi = useRef<string | null>(null);
  const currentHref = useRef("");
  const highlightTexts = useRef<Map<string, string>>(new Map());
  const highlightCfis = useRef<Map<string, string>>(new Map());
  const highlightColors = useRef<Map<string, string>>(new Map());
  const lastSelectionCfi = useRef("");
  const pendingReveal = useRef<RevealTarget | null>(null);
  const pendingSyncCfi = useRef<string | null>(null);
  const revealRef = useRef<((target: RevealTarget) => void) | null>(null);
  const tempHighlightRef = useRef<string | null>(null);
  const clearSelectionRef = useRef<(() => void) | null>(null);
  // latestSettingsRef 跟着每次渲染走；settingsRef 记录已经应用到书页上的那一份，
  // 用来判断这次变化要不要重新分页。
  const latestSettingsRef = useRef<ReadingSettings | null>(options.settings ?? null);
  latestSettingsRef.current = options.settings ?? null;
  const settingsRef = useRef<ReadingSettings | null>(options.settings ?? null);
  const applySettingsRef = useRef<
    ((next: ReadingSettings, opts?: { relayout?: boolean; force?: boolean }) => void) | null
  >(null);

  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState("");
  const [toc, setToc] = useState<TocEntry[]>([]);
  const [chapter, setChapter] = useState("");
  const [selection, setSelection] = useState<ReaderSelection | null>(null);
  const [hover, setHover] = useState<ReaderHover | null>(null);
  const [page, setPage] = useState({
    index: initialSpineIndex.current,
    total: 0,
    percent: options.initialPercent ?? 0,
  });

  const pageRef = useRef(page);
  pageRef.current = page;
  const chapterRef = useRef("");
  const selectionRef = useRef<ReaderSelection | null>(null);
  selectionRef.current = selection;
  const hoverRef = useRef<ReaderHover | null>(null);
  hoverRef.current = hover;
  const sectionTitles = useRef<Map<string, string>>(new Map());
  const selectionRangeRef = useRef<{ contents: Contents; range: Range } | null>(null);
  const actionRef = useRef<Record<ReaderAction, (arg?: string) => void> | null>(null);

  // 外部（搜索命中、摘录）要求跳到书里的某个位置。
  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<RevealTarget>).detail;
      if (!detail || detail.bookId !== bookId) return;
      if (revealRef.current) void revealRef.current(detail);
      else pendingReveal.current = detail;
    };
    window.addEventListener(REVEAL_EVENT, handler);
    return () => window.removeEventListener(REVEAL_EVENT, handler);
  }, [bookId]);

  // 另一边（书卡或全屏阅读）新划的线，立刻同步过来。
  // 不同章节的划线先存进 epub.js 的注解表，翻到那一节时自动出现。
  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{
        bookId?: string;
        id?: string;
        cfi?: string;
        text?: string;
        color?: string;
      }>).detail;
      if (detail?.bookId !== bookId || !detail.id || !detail.cfi) return;
      if (highlightCfis.current.has(detail.id)) return;
      highlightTexts.current.set(detail.id, detail.text ?? "");
      highlightColors.current.set(detail.id, detail.color ?? "yellow");
      const rendition = renditionRef.current;
      if (!rendition) return;
      highlightCfis.current.set(detail.id, detail.cfi);
      try {
        rendition.annotations.highlight(
          detail.cfi,
          { id: detail.id },
          undefined,
          "rc-highlight",
          highlightStyles(detail.color ?? "yellow"),
        );
      } catch {
        /* 位置失效的旧划线忽略 */
      }
    };
    window.addEventListener("bookdesk:highlight-created", handler);
    return () => window.removeEventListener("bookdesk:highlight-created", handler);
  }, [bookId]);

  // 另一边改了划线颜色，书页上的高亮跟着换色。
  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{
        bookId?: string;
        id?: string;
        color?: string;
      }>).detail;
      if (detail?.bookId !== bookId || !detail.id || !detail.color) return;
      const cfi = highlightCfis.current.get(detail.id);
      if (!cfi) return;
      highlightColors.current.set(detail.id, detail.color);
      const rendition = renditionRef.current;
      if (!rendition) return;
      try {
        rendition.annotations.remove(cfi, "highlight");
      } catch {
        /* ignore */
      }
      try {
        rendition.annotations.highlight(
          cfi,
          { id: detail.id },
          undefined,
          "rc-highlight",
          highlightStyles(detail.color),
        );
      } catch {
        /* 位置失效的旧划线忽略 */
      }
    };
    window.addEventListener("bookdesk:highlight-color-changed", handler);
    return () => window.removeEventListener("bookdesk:highlight-color-changed", handler);
  }, [bookId]);

  // 从摘录面板删除的划线，也要把书页上的高亮一起撤掉。
  useEffect(() => {
    const handler = (event: Event) => {
      const highlightId = (event as CustomEvent<{ id?: string }>).detail?.id;
      if (typeof highlightId !== "string") return;
      const cfi = highlightCfis.current.get(highlightId);
      if (!cfi) return;
      try {
        renditionRef.current?.annotations.remove(cfi, "highlight");
      } catch {
        /* 书还没渲染完时忽略 */
      }
      highlightTexts.current.delete(highlightId);
      highlightCfis.current.delete(highlightId);
      highlightColors.current.delete(highlightId);
      setSelection((cur) => (cur?.existingId === highlightId ? null : cur));
    };
    window.addEventListener("bookdesk:highlight-deleted", handler);
    return () => window.removeEventListener("bookdesk:highlight-deleted", handler);
  }, []);

  // 全屏阅读回到书桌时，让书卡跟着跳到同一页。
  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{ bookId?: string; cfi?: string }>).detail;
      if (detail?.bookId !== bookId || !detail.cfi) return;
      if (renditionRef.current) void renditionRef.current.display(detail.cfi);
      else pendingSyncCfi.current = detail.cfi;
    };
    window.addEventListener("bookdesk:sync-position", handler);
    return () => window.removeEventListener("bookdesk:sync-position", handler);
  }, [bookId]);

  useEffect(() => {
    let cancelled = false;
    let unregisterContext: (() => void) | null = null;
    let hoverTimer = 0;
    let lastHoverAt = 0;
    let resizeFrame = 0;
    let resizeObserver: ResizeObserver | null = null;
    const host = hostRef.current;
    if (!host) return;

    setStatus("loading");
    setError("");
    setToc([]);
    setChapter("");
    setSelection(null);
    setHover(null);
    setPage({
      index: initialSpineIndex.current,
      total: 0,
      percent: options.initialPercent ?? 0,
    });
    tocRef.current = [];
    lastCfi.current = null;
    currentHref.current = "";
    chapterRef.current = "";
    lastSelectionCfi.current = "";
    highlightTexts.current = new Map();
    highlightCfis.current = new Map();
    sectionTitles.current = new Map();
    selectionRangeRef.current = null;
    tempHighlightRef.current = null;
    actionRef.current = null;
    revealRef.current = null;
    clearSelectionRef.current = null;

    const chapterFor = (href: string): string => {
      const key = hrefKey(href);
      for (const entry of tocRef.current) {
        if (hrefKey(entry.href) === key) {
          return entry.label;
        }
      }
      // 没有目录的书，退回章节自己的第一个标题。
      return sectionTitles.current.get(key) ?? "";
    };

    const placePopover = (
      rect: DOMRect,
      frame: HTMLElement | null,
    ): Omit<ReaderSelection, "cfiRange" | "text" | "existingId"> => {
      const hostRect = host.getBoundingClientRect();
      const frameRect = frame?.getBoundingClientRect();
      const offsetX = (frameRect?.left ?? 0) - hostRect.left;
      const offsetY = (frameRect?.top ?? 0) - hostRect.top;
      const left = offsetX + rect.left + rect.width / 2;
      const below = offsetY + rect.bottom;
      const flip = below > hostRect.height - 58;
      // 浮层最宽 460px，留出半宽再夹一次，免得靠边时被书卡裁掉。
      const half = 240;
      return {
        x: Math.min(Math.max(left, half), Math.max(half, hostRect.width - half)),
        y: flip ? offsetY + rect.top - 8 : below,
        flip,
      };
    };

    const applyHighlight = (cfiRange: string, highlightId: string, color: string) => {
      const rendition = renditionRef.current;
      if (!rendition) return;
      highlightCfis.current.set(highlightId, cfiRange);
      highlightColors.current.set(highlightId, color);
      // 点击由书页文档上的统一监听处理，这样划线的 SVG 层可以
      // pointer-events: none，划选也能从已经划过的文字上起手。
      rendition.annotations.highlight(
        cfiRange,
        { id: highlightId },
        undefined,
        "rc-highlight",
        highlightStyles(color),
      );
    };

    // 字号/行距变了以后文字会重新排版，高亮 SVG 还停在旧位置，重挂一遍。
    const refreshHighlights = () => {
      const rendition = renditionRef.current;
      if (!rendition) return;
      for (const cfi of Array.from(highlightCfis.current.values())) {
        try {
          rendition.annotations.remove(cfi, "highlight");
        } catch {
          /* ignore */
        }
      }
      for (const [id, cfi] of highlightCfis.current) {
        try {
          rendition.annotations.highlight(
            cfi,
            { id },
            undefined,
            "rc-highlight",
            highlightStyles(highlightColors.current.get(id) ?? "yellow"),
          );
        } catch {
          /* 位置失效的旧划线忽略 */
        }
      }
    };

    const saveHighlight = async (
      cfiRange: string,
      text: string,
      color = "yellow",
    ): Promise<string | null> => {
      try {
        const saved = await Notes.saveHighlight({
          id: null,
          bookId,
          location: {
            bookId,
            cfi: cfiRange,
            spineIndex: pageRef.current.index,
            href: currentHref.current || null,
            pageLabel: chapterRef.current || null,
            charOffset: null,
          },
          text,
          color,
          chapter: chapterRef.current || null,
        });
        highlightTexts.current.set(saved.id, text);
        applyHighlight(cfiRange, saved.id, saved.color || color);
        window.dispatchEvent(
          new CustomEvent("bookdesk:highlight-created", {
            detail: { bookId, id: saved.id, cfi: cfiRange, text, color: saved.color || color },
          }),
        );
        highlightsChanged();
        return saved.id;
      } catch {
        return null;
      }
    };

    const clearSelection = () => {
      const rendition = renditionRef.current;
      const list = rendition?.getContents() as unknown as Contents[] | undefined;
      list?.forEach((contents) => {
        contents.window.getSelection()?.removeAllRanges();
      });
    };
    clearSelectionRef.current = clearSelection;

    const originalText = new WeakMap<Text, string>();

    const applyWritingMode = (doc: Document, vertical: boolean) => {
      const value = vertical ? "vertical-rl" : "horizontal-tb";
      try {
        doc.documentElement?.style.setProperty("writing-mode", value, "important");
        doc.body?.style.setProperty("writing-mode", value, "important");
        doc.documentElement?.style.setProperty("text-orientation", "mixed", "important");
      } catch {
        /* 结构不标准的书忽略 */
      }
    };

    // epub.js 的 themes.inject 只认 rules / url，不认 registerCss 的
    // serialized 样式；重建视图后主题会丢。章节进 iframe 之前先把样式
    // 写进 head，字号和行距也能参与第一次分页。
    const injectThemeStyle = (doc: Document, settings: ReadingSettings) => {
      try {
        const head = doc.head ?? doc.documentElement;
        if (!head) return;
        let style = doc.querySelector(`#${THEME_STYLE_ID}`) as HTMLStyleElement | null;
        if (!style) {
          style = doc.createElement("style") as HTMLStyleElement;
          style.id = THEME_STYLE_ID;
          head.appendChild(style);
        }
        style.textContent = readerThemeCss(settings, themeById(settings.theme));
      } catch {
        /* 结构不标准的书忽略 */
      }
    };

    // 逐字转换，字符偏移不变，所以 CFI、划线和进度都不用改。
    const convertDoc = (doc: Document, mode: string) => {
      if (!doc.body) return;
      const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
      const nodes: Text[] = [];
      while (walker.nextNode()) nodes.push(walker.currentNode as Text);
      for (const node of nodes) {
        const parent = node.parentElement;
        if (!parent || /^(SCRIPT|STYLE|NOSCRIPT|TEXTAREA)$/.test(parent.tagName)) continue;
        let original = originalText.get(node);
        if (original === undefined) {
          original = node.data;
          originalText.set(node, original);
        }
        const next = convertHan(original, mode);
        if (next !== node.data) node.data = next;
      }
    };

    const applyConversionToContents = (mode: string) => {
      const list = renditionRef.current?.getContents() as unknown as Contents[] | undefined;
      list?.forEach((contents) => convertDoc(contents.document, mode));
    };

    // section.load 会缓存已经解析过的章节；不清掉的话，重新 display 同一章
    // 不会再触发 spine 的 content hook，横竖排就换不过来。
    const resetSectionCache = (cfi: string | null) => {
      const book = bookRef.current;
      if (!book || !cfi) return;
      try {
        const section = book.spine.get(cfi) as unknown as
          | { contents?: unknown; document?: unknown }
          | undefined;
        if (section) {
          section.contents = undefined;
          section.document = undefined;
        }
      } catch {
        /* CFI 失效时忽略 */
      }
    };

    const applySettings = (
      next: ReadingSettings,
      opts?: { relayout?: boolean; force?: boolean },
    ) => {
      const rendition = renditionRef.current;
      if (!rendition) return;
      const previous = settingsRef.current;
      settingsRef.current = next;
      rendition.themes.registerCss("default", readerThemeCss(next, themeById(next.theme)));

      const contents = rendition.getContents() as unknown as Contents[];
      contents.forEach((item) => applyWritingMode(item.document, next.vertical));
      applyConversionToContents(next.convert);

      if (!opts?.relayout) return;
      const verticalChanged = previous?.vertical !== next.vertical;
      const spreadChanged = previous?.spread !== next.spread;
      const layoutChanged =
        verticalChanged ||
        spreadChanged ||
        previous?.fontSize !== next.fontSize ||
        previous?.lineHeight !== next.lineHeight;
      if (!layoutChanged && !opts.force) return;

      if (verticalChanged || spreadChanged) {
        rendition.spread(next.spread && !next.vertical ? "auto" : "none", 700);
      }

      const cfi = lastCfi.current;
      if (verticalChanged) {
        // 竖排要换分页轴，重建当前视图，让新的 writing-mode 参与第一次分页。
        resetSectionCache(cfi);
        rendition.clear();
        const shown = cfi ? rendition.display(cfi) : rendition.display();
        void shown.then(() => {
          // 新视图的样式在 spine hook 里已经写好；这里再同步一次，
          // 防止某本书的结构让 hook 没跑到，顺便把高亮按新排版重挂。
          const current = settingsRef.current;
          if (current) {
            rendition.themes.registerCss(
              "default",
              readerThemeCss(current, themeById(current.theme)),
            );
          }
          refreshHighlights();
        });
        return;
      }
      const manager = (rendition as unknown as { manager?: { updateLayout?: () => void } })
        .manager;
      manager?.updateLayout?.();
      if (cfi) {
        void rendition.display(cfi).then(() => refreshHighlights());
      } else {
        refreshHighlights();
      }
    };
    applySettingsRef.current = applySettings;

    (async () => {
      try {
        // 走 books:// 自定义协议，由 Go 后端直接回文件；不再把整本书 base64 搬进前端。
        const url = await Library.epubURL(bookId);
        if (cancelled) return;
        const book = ePub(url, { openAs: "epub" });
        bookRef.current = book;
        await book.ready;
        if (cancelled) {
          book.destroy();
          return;
        }

        // 书页 iframe 需要 allow-scripts 才能收到鼠标事件（悬停、点击划线）。
        // 书自带的脚本用 CSP 全部禁掉，只保留样式、图片和文字。
        const spineHooks = (book.spine as unknown as {
          hooks?: { content?: { register: (fn: (doc: Document) => void) => void } };
        }).hooks;
        spineHooks?.content?.register((doc: Document) => {
          try {
            // 在 epub.js 第一次分页之前就写好横竖排和主题，让它按正确的
            // 轴、字号和行距排版。
            const current = latestSettingsRef.current;
            applyWritingMode(doc, current?.vertical ?? false);
            if (current) injectThemeStyle(doc, current);
            const head = doc.head ?? doc.documentElement;
            if (!head) return;
            const meta = doc.createElement("meta");
            meta.setAttribute("http-equiv", "Content-Security-Policy");
            meta.setAttribute("content", "script-src 'none'; object-src 'none'; frame-src 'none'");
            head.insertBefore(meta, head.firstChild);
            // meta refresh 能把 iframe 导航到别处，绕开上面的 CSP。
            for (const el of Array.from(doc.querySelectorAll('meta[http-equiv="refresh" i]'))) {
              el.remove();
            }
          } catch {
            /* 结构不标准的书忽略 */
          }
        });

        const rendition = book.renderTo(host, {
          width: "100%",
          height: "100%",
          flow: "paginated",
          spread: "none",
          allowScriptedContent: true,
        });
        renditionRef.current = rendition;

        // 栏宽、双页和禅意模式都会改 .reader-page 的宽度；epub.js 只监听窗口
        // resize，所以这里盯住容器，宽度一变就重新分页并留在原来的 CFI。
        resizeObserver = new ResizeObserver(() => {
          if (resizeFrame) return;
          resizeFrame = window.requestAnimationFrame(() => {
            resizeFrame = 0;
            try {
              const current = renditionRef.current as unknown as
                | { resize: () => void }
                | null;
              current?.resize();
            } catch {
              /* 书还没渲染完时忽略 */
            }
          });
        });
        resizeObserver.observe(host);

        // 取当前这一页能看到的文字。取不到就返回空，宁可少发给模型。
        const visiblePageText = (): string => {
          const current = renditionRef.current;
          if (!current) return "";
          const list = current.getContents() as unknown as Contents[];
          const contents = list[0];
          if (!contents) return "";
          try {
            const doc = contents.document;
            const win = contents.window;
            const start = doc.caretRangeFromPoint(2, 2);
            const end = doc.caretRangeFromPoint(
              Math.max(2, win.innerWidth - 2),
              Math.max(2, win.innerHeight - 2),
            );
            if (start && end) {
              const range = doc.createRange();
              range.setStart(start.startContainer, start.startOffset);
              range.setEnd(end.endContainer, end.endOffset);
              const text = range.toString().replace(/\s+/g, " ").trim();
              if (text) return text;
            }
          } catch {
            /* 沙箱 iframe 里个别 API 可能不可用，退回空字符串 */
          }
          return "";
        };

        unregisterContext = registerReadingContext(bookId, () => ({
          chapterTitle: chapterRef.current,
          spineIndex: pageRef.current.index,
          visibleText: visiblePageText(),
        }));

        const initialSettings = latestSettingsRef.current;
        if (initialSettings) {
          settingsRef.current = initialSettings;
          rendition.themes.registerCss(
            "default",
            readerThemeCss(initialSettings, themeById(initialSettings.theme)),
          );
          rendition.spread(
            initialSettings.spread && !initialSettings.vertical ? "auto" : "none",
            700,
          );
        } else {
          // 书卡不传设置，保持原来的紧凑样式。
          rendition.themes.default({
            "html, body": { background: "#fffefb" },
            body: {
              "font-family": '"Songti SC", "STSong", "Noto Serif SC", serif',
              "line-height": "1.95",
              color: "#33312b",
            },
            p: { margin: "0 0 0.9em", "text-indent": "2em" },
            "h1, h2, h3": { "font-weight": "600", "line-height": "1.5" },
            "::selection": { background: "#ffe9a3" },
            ".rc-highlight": { cursor: "pointer" },
          });
        }

        const total = bookSize(book);
        setPage((p) => ({ ...p, total }));

        rendition.on("relocated", (loc: unknown) => {
          const start = (loc as { start?: Record<string, unknown> })?.start;
          if (!start) return;
          const index = Number(start.index ?? 0);
          const within = Number(start.percentage ?? 0);
          const overall = Math.min(1, Math.max(0, (index + within) / Math.max(1, total)));
          currentHref.current = String(start.href ?? "");
          const label = chapterFor(currentHref.current);
          chapterRef.current = label;
          pageRef.current = { index, total, percent: overall };
          setPage({ index, total, percent: overall });
          setChapter(label);
          setSelection(null);
          setHover(null);
          if (hoverTimer) {
            window.clearTimeout(hoverTimer);
            hoverTimer = 0;
          }
          const cfi = String(start.cfi ?? "");
          if (cfi && cfi !== lastCfi.current) {
            lastCfi.current = cfi;
            onProgressRef.current?.({
              bookId,
              cfi,
              spineIndex: index,
              percent: overall,
              chapter: label,
            });
          }
        });

        rendition.on("keydown", (event: KeyboardEvent) => {
          if (event.key === "ArrowRight" || event.key === "PageDown") {
            void rendition.next();
          } else if (event.key === "ArrowLeft" || event.key === "PageUp") {
            void rendition.prev();
          }
        });

        const showSelection = (
          cfiRange: string,
          text: string,
          rect: DOMRect,
          frame: HTMLElement | null,
          contents: Contents,
          range: Range,
        ) => {
          if (!cfiRange || !text || cfiRange === lastSelectionCfi.current) return;
          lastSelectionCfi.current = cfiRange;
          selectionRangeRef.current = { contents, range };
          setSelection({ cfiRange, text, ...placePopover(rect, frame) });
        };

        // 书页 iframe 现在允许脚本（见上面的 CSP），事件能正常收到。
        // 悬停英文单词时先显示一个「查词」小浮层，点了才真正发请求。
        const clearHover = () => {
          if (hoverTimer) {
            window.clearTimeout(hoverTimer);
            hoverTimer = 0;
          }
          if (hoverRef.current) setHover(null);
        };

        const wordAtPoint = (doc: Document, x: number, y: number) => {
          let range: Range | null = null;
          try {
            range = doc.caretRangeFromPoint(x, y);
          } catch {
            return null;
          }
          if (!range) return null;
          const node = range.startContainer;
          if (node.nodeType !== Node.TEXT_NODE) return null;
          const text = (node as Text).data;
          const isWordChar = (ch: string | undefined) => !!ch && /[A-Za-z'’\-]/.test(ch);
          let start = range.startOffset;
          let end = range.startOffset;
          while (start > 0 && isWordChar(text[start - 1])) start -= 1;
          while (end < text.length && isWordChar(text[end])) end += 1;
          const word = text.slice(start, end).trim();
          if (!isEnglishSelection(word)) return null;
          const wordRange = doc.createRange();
          wordRange.setStart(node, start);
          wordRange.setEnd(node, end);
          return { word, range: wordRange };
        };

        const contentsByDoc = new WeakMap<Document, Contents>();
        const boundDocs = new WeakSet<Document>();

        const onHoverMove = (event: MouseEvent) => {
          if (selectionRef.current) return;
          const now = Date.now();
          if (now - lastHoverAt < 80) return;
          lastHoverAt = now;
          const doc = event.currentTarget as Document;
          const found = wordAtPoint(doc, event.clientX, event.clientY);
          if (!found) {
            clearHover();
            return;
          }
          if (hoverRef.current?.word === found.word) return;
          if (hoverTimer) window.clearTimeout(hoverTimer);
          const frame = doc.defaultView?.frameElement as HTMLElement | null;
          const rect = found.range.getBoundingClientRect();
          const sentence = sentenceAround(found.range);
          hoverTimer = window.setTimeout(() => {
            hoverTimer = 0;
            setHover({ word: found.word, sentence, ...placePopover(rect, frame) });
          }, 320);
        };

        const onHoverLeave = () => {
          clearHover();
        };

        const onContentClick = (event: MouseEvent) => {
          if (hoverRef.current) clearHover();
          const doc = event.currentTarget as Document;
          const contents = contentsByDoc.get(doc);
          if (!contents) return;
          const sel = contents.window.getSelection();
          if (sel && sel.rangeCount > 0 && !sel.isCollapsed) return;
          let caret: Range | null = null;
          try {
            caret = doc.caretRangeFromPoint(event.clientX, event.clientY);
          } catch {
            caret = null;
          }
          if (!caret) {
            if (selectionRef.current) {
              lastSelectionCfi.current = "";
              setSelection(null);
            }
            return;
          }
          // contents.range() 只按 CFI 的 range 段在当前文档里找位置，
          // 别章的划线会被映射到本章同样的段落上；先按章节过滤。
          const book = bookRef.current;
          const currentSection = book?.spine.get(currentHref.current) ?? null;
          for (const [highlightId, cfi] of highlightCfis.current) {
            if (book && currentSection) {
              const section = book.spine.get(cfi);
              if (section && section.index !== currentSection.index) continue;
            }
            let range: Range | null = null;
            try {
              range = contents.range(cfi);
            } catch {
              continue;
            }
            if (!range) continue;
            let inside = false;
            try {
              inside = range.isPointInRange(caret.startContainer, caret.startOffset);
            } catch {
              continue;
            }
            if (!inside) continue;
            lastSelectionCfi.current = "";
            const frame = doc.defaultView?.frameElement as HTMLElement | null;
            setSelection({
              cfiRange: cfi,
              text: highlightTexts.current.get(highlightId) ?? "",
              existingId: highlightId,
              ...placePopover(range.getBoundingClientRect(), frame),
            });
            return;
          }
          if (selectionRef.current) {
            lastSelectionCfi.current = "";
            setSelection(null);
          }
        };

        const bindContents = (contents: Contents | undefined, href: string) => {
          const doc = contents?.document;
          if (!contents || !doc || boundDocs.has(doc)) return;
          boundDocs.add(doc);
          contentsByDoc.set(doc, contents);
          convertDoc(doc, latestSettingsRef.current?.convert ?? "none");
          doc.addEventListener("mousemove", onHoverMove);
          doc.addEventListener("mouseleave", onHoverLeave);
          doc.addEventListener("click", onContentClick);
          // 没有目录的书，用章节自己的第一个标题当分区标题。
          const heading = doc.querySelector("h1, h2, h3");
          const label = (heading?.textContent ?? "").replace(/\s+/g, " ").trim().slice(0, 60);
          if (!href || !label) return;
          sectionTitles.current.set(hrefKey(href), label);
          if (!chapterRef.current && hrefKey(currentHref.current) === hrefKey(href)) {
            chapterRef.current = label;
            setChapter(label);
          }
        };

        rendition.on("rendered", (section: { href?: string }, view: { contents?: Contents }) => {
          bindContents(view?.contents, String(section?.href ?? ""));
        });

        rendition.on("selected", (cfiRange: string, contents: Contents) => {
          const sel = contents.window.getSelection();
          if (!sel || sel.rangeCount === 0) return;
          const text = sel.toString().trim();
          if (!text) return;
          const range = sel.getRangeAt(0);
          const frame = contents.document.defaultView?.frameElement as HTMLElement | null;
          showSelection(cfiRange, text, range.getBoundingClientRect(), frame, contents, range);
        });

        book.loaded.navigation
          .then((nav) => {
            if (cancelled) return;
            const entries = flattenToc(nav.toc as NavItem[]);
            tocRef.current = entries;
            setToc(entries);
          })
          .catch(() => {});

        try {
          if (initialCfi.current) {
            await rendition.display(initialCfi.current);
          } else if (initialSpineIndex.current > 0) {
            await rendition.display(initialSpineIndex.current);
          } else {
            await rendition.display();
          }
        } catch {
          await rendition.display();
        }
        if (pendingSyncCfi.current) {
          const cfi = pendingSyncCfi.current;
          pendingSyncCfi.current = null;
          try {
            await rendition.display(cfi);
          } catch {
            /* 位置失效时留在开头 */
          }
        }
        if (cancelled) return;

        try {
          const existing = await Notes.listHighlights(bookId);
          for (const item of existing) {
            const cfi = item.location.cfi;
            if (!cfi) continue;
            highlightTexts.current.set(item.id, item.text);
            applyHighlight(cfi, item.id, item.color || "yellow");
          }
        } catch {
          /* 划线读取失败不影响阅读 */
        }

        setStatus("ready");
        onReadyRef.current?.();
        if (settingsRef.current) {
          // 主题样式是在首次分页之后才注入 iframe 的，这里再排一次，
          // 保证字号、行距和横竖排从一开始就生效。
          applySettings(settingsRef.current, { relayout: true, force: true });
        }

        const reveal = async (target: RevealTarget) => {
          const current = renditionRef.current;
          if (!current) return;
          if (target.cfi) {
            try {
              await current.display(target.cfi);
              return;
            } catch {
              /* CFI 失效时退回按章节和文字找 */
            }
          }
          try {
            await current.display(target.spineIndex);
          } catch {
            if (!target.href) return;
            try {
              await current.display(target.href);
            } catch {
              return;
            }
          }
          await new Promise((resolve) => window.setTimeout(resolve, 160));
          const contents = (current.getContents() as unknown as Contents[])[0];
          if (!contents) return;
          const query = convertHan(target.query, latestSettingsRef.current?.convert ?? "none");
          const range = findOccurrenceRange(contents.document, query, target.occurrence);
          if (!range) return;
          let cfi = "";
          try {
            cfi = contents.cfiFromRange(range);
          } catch {
            return;
          }
          if (tempHighlightRef.current) {
            try {
              current.annotations.remove(tempHighlightRef.current, "highlight");
            } catch {
              /* ignore */
            }
            tempHighlightRef.current = null;
          }
          try {
            current.annotations.highlight(
              cfi,
              { id: "search-hit" },
              undefined,
              "rc-search-hit",
              SEARCH_HIGHLIGHT_STYLES,
            );
            tempHighlightRef.current = cfi;
          } catch {
            return;
          }
          const el = range.startContainer.parentElement;
          el?.scrollIntoView({ block: "center", behavior: "smooth" });
          window.setTimeout(() => {
            try {
              current.annotations.remove(cfi, "highlight");
            } catch {
              /* ignore */
            }
            if (tempHighlightRef.current === cfi) tempHighlightRef.current = null;
          }, 6000);
        };
        revealRef.current = reveal;
        const initial = initialReveal.current ?? pendingReveal.current;
        initialReveal.current = null;
        pendingReveal.current = null;
        if (initial) void reveal(initial);

        // 供划选浮层按钮调用
        const onHighlightRequest = (color?: string) => {
          const current = selectionRef.current;
          if (!current) return;
          void saveHighlight(current.cfiRange, current.text, color || "yellow").then(() => {
            clearSelection();
            setSelection(null);
          });
        };
        const onRecolorRequest = (color?: string) => {
          const current = selectionRef.current;
          if (!current?.existingId || !color) return;
          const highlightId = current.existingId;
          const cfi = highlightCfis.current.get(highlightId) ?? current.cfiRange;
          void Notes.setHighlightColor(highlightId, color)
            .then(() => {
              highlightColors.current.set(highlightId, color);
              const rendition = renditionRef.current;
              if (rendition) {
                try {
                  rendition.annotations.remove(cfi, "highlight");
                } catch {
                  /* ignore */
                }
                try {
                  rendition.annotations.highlight(
                    cfi,
                    { id: highlightId },
                    undefined,
                    "rc-highlight",
                    highlightStyles(color),
                  );
                } catch {
                  /* 位置失效的旧划线忽略 */
                }
              }
              window.dispatchEvent(
                new CustomEvent("bookdesk:highlight-color-changed", {
                  detail: { bookId, id: highlightId, color },
                }),
              );
              highlightsChanged();
            })
            .catch(() => {});
          setSelection(null);
        };
        const onCardRequest = () => {
          const current = selectionRef.current;
          if (!current) return;
          void (async () => {
            if (!current.existingId) {
              await saveHighlight(current.cfiRange, current.text);
            }
            window.dispatchEvent(
              new CustomEvent("bookdesk:add-quote-card", {
                detail: {
                  bookId,
                  bookTitle: titleRef.current || "未命名书籍",
                  quote: current.text,
                  chapter: chapterRef.current || null,
                  source: `${titleRef.current || "未命名书籍"}${chapterRef.current ? ` · ${chapterRef.current}` : ""}`,
                  location: {
                    bookId,
                    cfi: current.cfiRange,
                    spineIndex: pageRef.current.index,
                    href: currentHref.current || null,
                    pageLabel: chapterRef.current || null,
                    charOffset: null,
                  },
                },
              }),
            );
            clearSelection();
            setSelection(null);
          })();
        };
        const onDeleteRequest = () => {
          const current = selectionRef.current;
          if (!current?.existingId) return;
          const highlightId = current.existingId;
          void Notes.deleteHighlight(highlightId)
            .then(() => {
              rendition.annotations.remove(current.cfiRange, "highlight");
              highlightTexts.current.delete(highlightId);
              highlightCfis.current.delete(highlightId);
              highlightColors.current.delete(highlightId);
              window.dispatchEvent(
                new CustomEvent("bookdesk:highlight-deleted", {
                  detail: { id: highlightId },
                }),
              );
              highlightsChanged();
            })
            .catch(() => {});
          setSelection(null);
        };
        const onCharacterRequest = () => {
          const current = selectionRef.current;
          if (!current) return;
          const name = current.text.replace(/\s+/g, " ").trim().slice(0, 24);
          if (!name) return;
          window.dispatchEvent(
            new CustomEvent("bookdesk:add-character", {
              detail: {
                bookId,
                bookTitle: titleRef.current || "未命名书籍",
                name,
                // 全屏阅读里点「人物」是想马上对话；书卡保持原来的行为，
                // 先把人物卡放到画布上，由用户自己决定什么时候聊。
                openChat: modeRef.current === "reader",
              },
            }),
          );
          clearSelection();
          setSelection(null);
        };
        const onAskRequest = (mode: "ask" | "explain" | "translate") => {
          const current = selectionRef.current;
          if (!current) return;
          const text = current.text.trim();
          if (!text) return;
          window.dispatchEvent(
            new CustomEvent("bookdesk:ask-selection", {
              detail: {
                bookId,
                bookTitle: titleRef.current || "未命名书籍",
                mode,
                selection: text,
              },
            }),
          );
          setSelection(null);
        };
        const onShareRequest = () => {
          const current = selectionRef.current;
          if (!current) return;
          const text = current.text.trim();
          if (!text) return;
          window.dispatchEvent(
            new CustomEvent("bookdesk:share-card", {
              detail: {
                bookId,
                title: titleRef.current || "未命名书籍",
                quote: text,
                chapter: chapterRef.current || null,
                color: current.existingId
                  ? (highlightColors.current.get(current.existingId) ?? null)
                  : null,
              },
            }),
          );
          setSelection(null);
        };
        const onLookupRequest = () => {
          const current = selectionRef.current;
          if (!current) return;
          const word = current.text.replace(/\s+/g, " ").trim().slice(0, 60);
          if (!word) return;
          const last = selectionRangeRef.current;
          const sentence = last ? sentenceAround(last.range) : "";
          window.dispatchEvent(
            new CustomEvent("bookdesk:lookup-word", {
              detail: { word, sentence, bookTitle: titleRef.current || "未命名书籍" },
            }),
          );
          setSelection(null);
        };
        actionRef.current = {
          highlight: onHighlightRequest,
          recolor: onRecolorRequest,
          card: onCardRequest,
          remove: onDeleteRequest,
          character: onCharacterRequest,
          lookup: onLookupRequest,
          ask: () => onAskRequest("ask"),
          explain: () => onAskRequest("explain"),
          translate: () => onAskRequest("translate"),
          share: onShareRequest,
        };
      } catch (err) {
        if (cancelled) return;
        setStatus("error");
        setError(err instanceof Error ? err.message : String(err));
      }
    })();

    return () => {
      cancelled = true;
      actionRef.current = null;
      revealRef.current = null;
      pendingReveal.current = null;
      tempHighlightRef.current = null;
      selectionRangeRef.current = null;
      clearSelectionRef.current = null;
      applySettingsRef.current = null;
      unregisterContext?.();
      resizeObserver?.disconnect();
      resizeObserver = null;
      if (resizeFrame) window.cancelAnimationFrame(resizeFrame);
      if (hoverTimer) window.clearTimeout(hoverTimer);
      try {
        renditionRef.current?.destroy();
      } catch {
        /* ignore */
      }
      try {
        bookRef.current?.destroy();
      } catch {
        /* ignore */
      }
      renditionRef.current = null;
      bookRef.current = null;
    };
    // 只按书切换引擎；回调通过 ref 读取，避免每次渲染重开书。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bookId]);

  // 设置变化只改样式和分页，不重开书。
  const settingsKey = options.settings ? JSON.stringify(options.settings) : "";
  useEffect(() => {
    if (!options.settings) return;
    applySettingsRef.current?.(options.settings, { relayout: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsKey]);

  const turn = useCallback((dir: "prev" | "next") => {
    const rendition = renditionRef.current;
    if (!rendition) return;
    if (dir === "prev") void rendition.prev();
    else void rendition.next();
  }, []);

  const jumpTo = useCallback((href: string) => {
    void renditionRef.current?.display(href);
  }, []);

  const reveal = useCallback((target: RevealTarget) => {
    void revealRef.current?.(target);
  }, []);

  const runAction = useCallback((action: ReaderAction, arg?: string) => {
    actionRef.current?.[action](arg);
  }, []);

  const dismissSelection = useCallback(() => {
    clearSelectionRef.current?.();
    setSelection(null);
  }, []);

  const dismissHover = useCallback(() => {
    setHover(null);
  }, []);

  return {
    status,
    error,
    toc,
    chapter,
    page,
    selection,
    hover,
    turn,
    jumpTo,
    reveal,
    runAction,
    dismissSelection,
    dismissHover,
  };
}
