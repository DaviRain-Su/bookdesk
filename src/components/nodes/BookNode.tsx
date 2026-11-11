import { useEffect, useRef, useState } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import {
  ChevronLeft,
  ChevronRight,
  Languages,
  List,
  Loader2,
  Maximize2,
  Search as SearchIcon,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import type { RevealTarget } from "../../lib/reveal";
import FindPopover from "../reader/FindPopover";
import SelectionMenu from "../reader/SelectionMenu";
import TocPopover from "../reader/TocPopover";
import { useEpubReader, type ReaderProgress } from "../reader/useEpubReader";

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

export default function BookNode({ id, data, selected }: NodeProps) {
  const d = data as BookNodeData;
  const hostRef = useRef<HTMLDivElement | null>(null);
  const progressRef = useRef<ReaderProgress | null>(null);

  const [tocOpen, setTocOpen] = useState(false);
  const [findOpen, setFindOpen] = useState(false);

  const reader = useEpubReader(hostRef, {
    bookId: d.bookId,
    title: d.title ?? "未命名书籍",
    initialCfi: typeof d.cfi === "string" ? d.cfi : null,
    initialSpineIndex: typeof d.spineIndex === "number" ? d.spineIndex : 0,
    initialPercent: typeof d.percent === "number" ? d.percent : 0,
    initialReveal: d.reveal ?? null,
    onProgress: (progress) => {
      progressRef.current = progress;
      window.dispatchEvent(
        new CustomEvent("bookdesk:book-progress", {
          detail: { nodeId: id, ...progress },
        }),
      );
    },
  });

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{ nodeId?: string }>).detail;
      if (detail?.nodeId !== id) return;
      setTocOpen(false);
      setFindOpen(true);
    };
    window.addEventListener("bookdesk:find-in-book", handler);
    return () => window.removeEventListener("bookdesk:find-in-book", handler);
  }, [id]);

  const openFullscreen = () => {
    window.dispatchEvent(
      new CustomEvent("bookdesk:open-reader", {
        detail: {
          bookId: d.bookId,
          cfi:
            progressRef.current?.cfi ??
            (typeof d.cfi === "string" ? d.cfi : null),
        },
      }),
    );
  };

  return (
    <article className={`card book-card ${selected ? "is-selected" : ""}`}>
      <Handle type="target" position={Position.Left} className="node-handle" />
      <header className="book-header">
        <div
          className="book-heading"
          onDoubleClick={(event) => {
            event.stopPropagation();
            openFullscreen();
          }}
          title="双击全屏阅读"
        >
          <span className="book-title">{d.title || "未命名书籍"}</span>
          <span className="book-subtitle">
            {d.author ? d.author : "未知作者"}
            {reader.chapter ? ` · ${reader.chapter}` : ""}
          </span>
        </div>
        <div className="book-actions">
          <button
            className="book-fullscreen"
            title="全屏阅读（⌘⇧R）"
            onClick={openFullscreen}
            disabled={reader.status !== "ready"}
          >
            <Maximize2 size={14} />
            <span>全屏阅读</span>
          </button>
          <button
            className={`ghost-icon ${findOpen ? "is-active" : ""}`}
            title="书内搜索（⌘F）"
            onClick={() => {
              setTocOpen(false);
              setFindOpen((v) => !v);
            }}
            disabled={reader.status !== "ready"}
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
            disabled={reader.status !== "ready"}
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
        {reader.status === "loading" ? (
          <div className="reader-state">
            <Loader2 size={18} className="spin" />
            <span>正在打开书籍…</span>
          </div>
        ) : null}
        {reader.status === "error" ? (
          <div className="reader-state is-error">
            <TriangleAlert size={18} />
            <span>无法打开这本书</span>
            <small>{reader.error}</small>
          </div>
        ) : null}
        <FindPopover
          bookId={d.bookId}
          open={findOpen}
          onClose={() => setFindOpen(false)}
          onReveal={reader.reveal}
        />
        {tocOpen ? (
          <TocPopover
            toc={reader.toc}
            onJump={(href) => {
              setTocOpen(false);
              reader.jumpTo(href);
            }}
          />
        ) : null}
        {reader.selection ? (
          <SelectionMenu selection={reader.selection} onAction={reader.runAction} />
        ) : null}
        {reader.hover ? (
          <div
            className={`hover-popover ${reader.hover.flip ? "is-flipped" : ""}`}
            style={{ left: reader.hover.x, top: reader.hover.y }}
            onMouseDown={(event) => event.preventDefault()}
          >
            <span className="hover-word">{reader.hover.word}</span>
            <button
              onClick={() => {
                const current = reader.hover;
                reader.dismissHover();
                if (!current) return;
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
        <button
          className="page-button"
          title="上一页（← / Page Up）"
          aria-keyshortcuts="ArrowLeft PageUp"
          onClick={() => reader.turn("prev")}
          disabled={reader.status !== "ready"}
        >
          <ChevronLeft size={16} />
        </button>
        <div className="book-progress">
          <span>
            {reader.page.total > 0 ? `${reader.page.index + 1} / ${reader.page.total} 章` : "—"}
          </span>
          <div className="progress-track">
            <div
              className="progress-fill"
              style={{ width: `${Math.round(reader.page.percent * 100)}%` }}
            />
          </div>
          <span>{Math.round(reader.page.percent * 100)}%</span>
        </div>
        <button
          className="page-button"
          title="下一页（→ / Page Down / 空格）"
          aria-keyshortcuts="ArrowRight PageDown Space"
          onClick={() => reader.turn("next")}
          disabled={reader.status !== "ready"}
        >
          <ChevronRight size={16} />
        </button>
      </footer>
      <Handle type="source" position={Position.Right} className="node-handle" />
    </article>
  );
}
