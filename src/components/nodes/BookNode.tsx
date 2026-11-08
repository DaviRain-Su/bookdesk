import { useEffect, useMemo, useRef, useState } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import ePub, {
  type Book as EpubBook,
  type Contents,
  type NavItem,
  type Rendition,
} from "epubjs";
import {
  ChevronLeft,
  ChevronRight,
  Highlighter,
  Languages,
  List,
  Loader2,
  Quote,
  Search as SearchIcon,
  TriangleAlert,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import { Library, Notes, Search } from "../../mygo";
import { registerReadingContext } from "../../lib/reading";
import { REVEAL_EVENT, type RevealTarget } from "../../lib/reveal";
import type { SearchHit } from "../../lib/search";
import Snippet from "../Snippet";

export interface BookNodeData {
  bookId: string;
  title?: string;
  author?: string | null;
  cfi?: string | null;
  spineIndex?: number;
  percent?: number;
  reveal?: RevealTarget | null;
  [key: string]: unknown;
}

interface TocEntry {
  label: string;
  href: string;
  depth: number;
}

interface SelectionState {
  cfiRange: string;
  text: string;
  x: number;
  y: number;
  flip: boolean;
  existingId?: string;
}

interface HoverState {
  word: string;
  sentence: string;
  x: number;
  y: number;
  flip: boolean;
}

const HIGHLIGHT_STYLES = {
  fill: "#ffe9a3",
  "fill-opacity": "0.6",
  "mix-blend-mode": "multiply",
};

const SEARCH_HIGHLIGHT_STYLES = {
  fill: "#8fc1e3",
  "fill-opacity": "0.55",
  "mix-blend-mode": "multiply",
};

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
function isEnglishSelection(text: string): boolean {
  const t = text.replace(/\s+/g, " ").trim();
  if (!t || t.length > 48) return false;
  return /^[A-Za-z][A-Za-z'’\-]*(?: [A-Za-z][A-Za-z'’\-]*){0,4}$/.test(t);
}

/** 取选区所在段落，作为查词的语境。 */
function sentenceAround(range: Range): string {
  let el: Element | null = range.startContainer.parentElement;
  while (el && !/^(P|LI|BLOCKQUOTE|DIV|TD|TH|H1|H2|H3|H4|H5|H6)$/.test(el.tagName)) {
    el = el.parentElement;
  }
  const text = (el?.textContent ?? "").replace(/\s+/g, " ").trim();
  return text.slice(0, 600);
}

export default function BookNode({ id, data, selected }: NodeProps) {
  const d = data as BookNodeData;
  const hostRef = useRef<HTMLDivElement | null>(null);
  const renditionRef = useRef<Rendition | null>(null);
  const bookRef = useRef<EpubBook | null>(null);
  const tocRef = useRef<TocEntry[]>([]);
  const initialCfi = useRef<string | null>(typeof d.cfi === "string" ? d.cfi : null);
  const lastCfi = useRef<string | null>(null);
  const currentHref = useRef("");
  const highlightTexts = useRef<Map<string, string>>(new Map());
  const highlightCfis = useRef<Map<string, string>>(new Map());
  const lastSelectionCfi = useRef("");
  const pendingReveal = useRef<RevealTarget | null>(null);
  const revealRef = useRef<((target: RevealTarget) => void) | null>(null);
  const tempHighlightRef = useRef<string | null>(null);
  const findInputRef = useRef<HTMLInputElement | null>(null);
  const findRequestRef = useRef(0);

  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState("");
  const [toc, setToc] = useState<TocEntry[]>([]);
  const [tocOpen, setTocOpen] = useState(false);
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findHits, setFindHits] = useState<SearchHit[]>([]);
  const [findLoading, setFindLoading] = useState(false);
  const [chapter, setChapter] = useState("");
  const [selection, setSelection] = useState<SelectionState | null>(null);
  const [hover, setHover] = useState<HoverState | null>(null);
  const [page, setPage] = useState({
    index: typeof d.spineIndex === "number" ? d.spineIndex : 0,
    total: 0,
    percent: typeof d.percent === "number" ? d.percent : 0,
  });

  const pageRef = useRef(page);
  pageRef.current = page;
  const chapterRef = useRef("");
  const selectionRef = useRef<SelectionState | null>(null);
  selectionRef.current = selection;
  const hoverRef = useRef<HoverState | null>(null);
  hoverRef.current = hover;
  const sectionTitles = useRef<Map<string, string>>(new Map());
  const selectionRangeRef = useRef<{ contents: Contents; range: Range } | null>(null);
  const actionRef = useRef<{
    highlight: () => void;
    card: () => void;
    remove: () => void;
    character: () => void;
    lookup: () => void;
  } | null>(null);

  const findResults = useMemo(() => {
    const counters = new Map<number, number>();
    return findHits.map((hit) => {
      const key = hit.location.spineIndex;
      const occurrence = counters.get(key) ?? 0;
      counters.set(key, occurrence + 1);
      return { hit, occurrence };
    });
  }, [findHits]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<RevealTarget>).detail;
      if (!detail || detail.bookId !== d.bookId) return;
      if (revealRef.current) void revealRef.current(detail);
      else pendingReveal.current = detail;
    };
    window.addEventListener(REVEAL_EVENT, handler);
    return () => window.removeEventListener(REVEAL_EVENT, handler);
  }, [d.bookId]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{ nodeId?: string }>).detail;
      if (detail?.nodeId !== id) return;
      setTocOpen(false);
      setFindOpen(true);
      window.setTimeout(() => findInputRef.current?.focus(), 60);
    };
    window.addEventListener("bookdesk:find-in-book", handler);
    return () => window.removeEventListener("bookdesk:find-in-book", handler);
  }, [id]);

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
      setSelection((cur) => (cur?.existingId === highlightId ? null : cur));
    };
    window.addEventListener("bookdesk:highlight-deleted", handler);
    return () => window.removeEventListener("bookdesk:highlight-deleted", handler);
  }, []);

  useEffect(() => {
    if (!findOpen) return;
    const q = findQuery.trim();
    if (!q) {
      findRequestRef.current += 1;
      setFindHits([]);
      setFindLoading(false);
      return;
    }
    setFindLoading(true);
    const request = (findRequestRef.current += 1);
    const timer = window.setTimeout(() => {
      void Search.queryBook(d.bookId, q)
        .then((list) => {
          if (findRequestRef.current !== request) return;
          setFindHits(list);
        })
        .catch(() => {
          if (findRequestRef.current !== request) return;
          setFindHits([]);
        })
        .finally(() => {
          if (findRequestRef.current === request) setFindLoading(false);
        });
    }, 240);
    return () => window.clearTimeout(timer);
  }, [d.bookId, findOpen, findQuery]);

  useEffect(() => {
    let cancelled = false;
    let unregisterContext: (() => void) | null = null;
    let hoverTimer = 0;
    let lastHoverAt = 0;
    const host = hostRef.current;
    if (!host) return;

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

    const placePopover = (rect: DOMRect, frame: HTMLElement | null): Omit<SelectionState, "cfiRange" | "text" | "existingId"> => {
      const hostRect = host.getBoundingClientRect();
      const frameRect = frame?.getBoundingClientRect();
      const offsetX = (frameRect?.left ?? 0) - hostRect.left;
      const offsetY = (frameRect?.top ?? 0) - hostRect.top;
      const left = offsetX + rect.left + rect.width / 2;
      const below = offsetY + rect.bottom;
      const flip = below > hostRect.height - 58;
      return {
        x: Math.min(Math.max(left, 140), Math.max(140, hostRect.width - 140)),
        y: flip ? offsetY + rect.top - 8 : below,
        flip,
      };
    };

    const applyHighlight = (cfiRange: string, highlightId: string) => {
      const rendition = renditionRef.current;
      if (!rendition) return;
      highlightCfis.current.set(highlightId, cfiRange);
      // 点击由书页文档上的统一监听处理，这样划线的 SVG 层可以
      // pointer-events: none，划选也能从已经划过的文字上起手。
      rendition.annotations.highlight(
        cfiRange,
        { id: highlightId },
        undefined,
        "rc-highlight",
        HIGHLIGHT_STYLES,
      );
    };

    const saveHighlight = async (cfiRange: string, text: string): Promise<string | null> => {
      try {
        const saved = await Notes.saveHighlight({
          id: null,
          bookId: d.bookId,
          location: {
            bookId: d.bookId,
            cfi: cfiRange,
            spineIndex: pageRef.current.index,
            href: currentHref.current || null,
            pageLabel: chapterRef.current || null,
            charOffset: null,
          },
          text,
          color: "yellow",
        });
        highlightTexts.current.set(saved.id, text);
        applyHighlight(cfiRange, saved.id);
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

    (async () => {
      try {
        // 走 books:// 自定义协议，由 Go 后端直接回文件；不再把整本书 base64 搬进前端。
        const url = await Library.epubURL(d.bookId);
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

        unregisterContext = registerReadingContext(d.bookId, () => ({
          chapterTitle: chapterRef.current,
          spineIndex: pageRef.current.index,
          visibleText: visiblePageText(),
        }));

        rendition.themes.default({
          "html, body": { background: "#fffefb" },
          body: {
            "font-family": '"Songti SC", "STSong", "Noto Serif SC", serif',
            "line-height": "1.95",
            color: "#33312b",
            padding: "0 6px",
          },
          p: { margin: "0 0 0.9em", "text-indent": "2em" },
          "h1, h2, h3": { "font-weight": "600", "line-height": "1.5" },
          "::selection": { background: "#ffe9a3" },
          ".rc-highlight": { cursor: "pointer" },
        });

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
            window.dispatchEvent(
              new CustomEvent("bookdesk:book-progress", {
                detail: { nodeId: id, cfi, spineIndex: index, percent: overall },
              }),
            );
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
          for (const [highlightId, cfi] of highlightCfis.current) {
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
          } else {
            await rendition.display();
          }
        } catch {
          await rendition.display();
        }
        if (cancelled) return;

        try {
          const existing = await Notes.listHighlights(d.bookId);
          for (const item of existing) {
            const cfi = item.location.cfi;
            if (!cfi) continue;
            highlightTexts.current.set(item.id, item.text);
            applyHighlight(cfi, item.id);
          }
        } catch {
          /* 划线读取失败不影响阅读 */
        }

        setStatus("ready");

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
          const range = findOccurrenceRange(contents.document, target.query, target.occurrence);
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
        const initialReveal = d.reveal ?? pendingReveal.current;
        pendingReveal.current = null;
        if (initialReveal) void reveal(initialReveal);

        // 供划选浮层按钮调用
        const onHighlightRequest = () => {
          const current = selectionRef.current;
          if (!current) return;
          void saveHighlight(current.cfiRange, current.text).then(() => {
            clearSelection();
            setSelection(null);
          });
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
                  bookId: d.bookId,
                  bookTitle: d.title ?? "未命名书籍",
                  quote: current.text,
                  source: `${d.title ?? "未命名书籍"}${chapterRef.current ? ` · ${chapterRef.current}` : ""}`,
                  location: {
                    bookId: d.bookId,
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
              detail: { bookId: d.bookId, bookTitle: d.title ?? "未命名书籍", name },
            }),
          );
          clearSelection();
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
              detail: { word, sentence, bookTitle: d.title ?? "未命名书籍" },
            }),
          );
          setSelection(null);
        };
        actionRef.current = {
          highlight: onHighlightRequest,
          card: onCardRequest,
          remove: onDeleteRequest,
          character: onCharacterRequest,
          lookup: onLookupRequest,
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
      unregisterContext?.();
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
  }, [d.bookId, id]);

  const turn = (dir: "prev" | "next") => {
    const rendition = renditionRef.current;
    if (!rendition) return;
    if (dir === "prev") void rendition.prev();
    else void rendition.next();
  };

  const jumpTo = (href: string) => {
    setTocOpen(false);
    void renditionRef.current?.display(href);
  };

  return (
    <article className={`card book-card ${selected ? "is-selected" : ""}`}>
      <Handle type="target" position={Position.Left} className="node-handle" />
      <header className="book-header">
        <div className="book-heading">
          <span className="book-title">{d.title || "未命名书籍"}</span>
          <span className="book-subtitle">
            {d.author ? d.author : "未知作者"}
            {chapter ? ` · ${chapter}` : ""}
          </span>
        </div>
        <div className="book-actions">
          <button
            className={`ghost-icon ${findOpen ? "is-active" : ""}`}
            title="书内搜索（⌘F）"
            onClick={() => {
              setTocOpen(false);
              setFindOpen((v) => !v);
              window.setTimeout(() => findInputRef.current?.focus(), 60);
            }}
            disabled={status !== "ready"}
          >
            <SearchIcon size={15} />
          </button>
          <button
            className={`ghost-icon ${tocOpen ? "is-active" : ""}`}
            title="目录"
            onClick={() => {
              setFindOpen(false);
              setTocOpen((v) => !v);
            }}
            disabled={status !== "ready"}
          >
            <List size={15} />
          </button>
          <button
            className="ghost-icon"
            title="从画布移除"
            onClick={() =>
              window.dispatchEvent(
                new CustomEvent("bookdesk:delete-node", { detail: { nodeId: id } }),
              )
            }
          >
            <Trash2 size={15} />
          </button>
        </div>
      </header>

      <div className="book-content book-reader">
        <div ref={hostRef} className="epub-viewport" />
        {status === "loading" ? (
          <div className="reader-state">
            <Loader2 size={18} className="spin" />
            <span>正在打开书籍…</span>
          </div>
        ) : null}
        {status === "error" ? (
          <div className="reader-state is-error">
            <TriangleAlert size={18} />
            <span>无法打开这本书</span>
            <small>{error}</small>
          </div>
        ) : null}
        {findOpen ? (
          <div className="toc-popover find-popover">
            <div className="find-head">
              <SearchIcon size={14} />
              <input
                ref={findInputRef}
                value={findQuery}
                onChange={(event) => setFindQuery(event.target.value)}
                placeholder="在这本书里搜索"
                spellCheck={false}
              />
              <button className="ghost-icon" onClick={() => setFindOpen(false)} title="关闭">
                <X size={14} />
              </button>
            </div>
            <div className="toc-list find-list">
              {!findQuery.trim() ? (
                <div className="find-empty">输入关键词，在当前书里查找。</div>
              ) : findLoading && findResults.length === 0 ? (
                <div className="find-empty">正在搜索…</div>
              ) : findResults.length === 0 ? (
                <div className="find-empty">没有找到「{findQuery.trim()}」。</div>
              ) : (
                findResults.map(({ hit, occurrence }, i) => (
                  <button
                    key={`${hit.location.spineIndex}-${i}`}
                    className="find-hit"
                    onClick={() => {
                      setFindOpen(false);
                      void revealRef.current?.({
                        bookId: d.bookId,
                        href: hit.location.href,
                        spineIndex: hit.location.spineIndex,
                        query: findQuery.trim(),
                        occurrence,
                      });
                    }}
                  >
                    <span className="find-hit-meta">
                      {hit.location.pageLabel || `第 ${hit.location.spineIndex + 1} 节`}
                    </span>
                    <span className="find-hit-text">
                      <Snippet text={hit.snippet} ranges={hit.highlights} />
                    </span>
                  </button>
                ))
              )}
            </div>
          </div>
        ) : null}
        {tocOpen && toc.length > 0 ? (
          <div className="toc-popover">
            <div className="toc-head">目录</div>
            <div className="toc-list">
              {toc.map((entry, i) => (
                <button
                  key={`${entry.href}-${i}`}
                  className="toc-item"
                  style={{ paddingLeft: 12 + entry.depth * 14 }}
                  onClick={() => jumpTo(entry.href)}
                >
                  {entry.label}
                </button>
              ))}
            </div>
          </div>
        ) : null}
        {selection ? (
          <div
            className={`selection-popover ${selection.flip ? "is-flipped" : ""}`}
            style={{ left: selection.x, top: selection.y }}
            onMouseDown={(event) => event.preventDefault()}
          >
            {selection.existingId ? (
              <>
                <button onClick={() => actionRef.current?.card()}>
                  <Quote size={13} />
                  生成卡片
                </button>
                <button className="is-danger" onClick={() => actionRef.current?.remove()}>
                  <Trash2 size={13} />
                  删除划线
                </button>
              </>
            ) : (
              <>
                <button onClick={() => actionRef.current?.highlight()}>
                  <Highlighter size={13} />
                  划线
                </button>
                <button onClick={() => actionRef.current?.card()}>
                  <Quote size={13} />
                  生成卡片
                </button>
                <button onClick={() => actionRef.current?.character()}>
                  <UserRound size={13} />
                  人物
                </button>
                {isEnglishSelection(selection.text) ? (
                  <button onClick={() => actionRef.current?.lookup()}>
                    <Languages size={13} />
                    查词
                  </button>
                ) : null}
              </>
            )}
          </div>
        ) : null}
        {hover ? (
          <div
            className={`hover-popover ${hover.flip ? "is-flipped" : ""}`}
            style={{ left: hover.x, top: hover.y }}
            onMouseDown={(event) => event.preventDefault()}
          >
            <span className="hover-word">{hover.word}</span>
            <button
              onClick={() => {
                const current = hover;
                setHover(null);
                window.dispatchEvent(
                  new CustomEvent("bookdesk:lookup-word", {
                    detail: {
                      word: current.word,
                      sentence: current.sentence,
                      bookTitle: d.title ?? "未命名书籍",
                    },
                  }),
                );
              }}
            >
              <Languages size={13} />
              查词
            </button>
          </div>
        ) : null}
      </div>

      <footer className="book-footer">
        <button className="page-button" title="上一页" onClick={() => turn("prev")} disabled={status !== "ready"}>
          <ChevronLeft size={16} />
        </button>
        <div className="book-progress">
          <span>{page.total > 0 ? `${page.index + 1} / ${page.total} 章` : "—"}</span>
          <div className="progress-track">
            <div className="progress-fill" style={{ width: `${Math.round(page.percent * 100)}%` }} />
          </div>
          <span>{Math.round(page.percent * 100)}%</span>
        </div>
        <button className="page-button" title="下一页" onClick={() => turn("next")} disabled={status !== "ready"}>
          <ChevronRight size={16} />
        </button>
      </footer>
      <Handle type="source" position={Position.Right} className="node-handle" />
    </article>
  );
}
