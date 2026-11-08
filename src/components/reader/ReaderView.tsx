import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
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
} from "lucide-react";
import FindPopover from "./FindPopover";
import TocPopover from "./TocPopover";
import {
  isEnglishSelection,
  useEpubReader,
  type ReaderProgress,
} from "./useEpubReader";

export interface ReaderBook {
  id: string;
  title: string;
  author: string | null;
}

interface Props {
  book: ReaderBook;
  initialCfi: string | null;
  initialSpineIndex: number;
  initialPercent: number;
  onClose: () => void;
  onProgress: (progress: ReaderProgress) => void;
}

/** 全屏阅读：一本书占满主区域，和书卡共用同一个 epub.js 引擎。 */
export default function ReaderView({
  book,
  initialCfi,
  initialSpineIndex,
  initialPercent,
  onClose,
  onProgress,
}: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [tocOpen, setTocOpen] = useState(false);
  const [findOpen, setFindOpen] = useState(false);

  const reader = useEpubReader(hostRef, {
    bookId: book.id,
    title: book.title,
    initialCfi,
    initialSpineIndex,
    initialPercent,
    onProgress,
  });

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (findOpen) {
        setFindOpen(false);
        return;
      }
      if (tocOpen) {
        setTocOpen(false);
        return;
      }
      if (reader.selection) {
        reader.dismissSelection();
        return;
      }
      if (reader.hover) {
        reader.dismissHover();
        return;
      }
      onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    findOpen,
    tocOpen,
    reader.selection,
    reader.hover,
    reader.dismissSelection,
    reader.dismissHover,
    onClose,
  ]);

  return (
    <section className="reader-overlay" aria-label="全屏阅读">
      <header className="reader-top">
        <button className="reader-back" onClick={onClose} title="回到书桌（Esc）">
          <ArrowLeft size={16} />
          <span>书桌</span>
        </button>
        <div className="reader-heading">
          <span className="reader-title">{book.title}</span>
          <span className="reader-sub">
            {book.author || "未知作者"}
            {reader.chapter ? ` · ${reader.chapter}` : ""}
          </span>
        </div>
        <div className="reader-tools">
          <button
            className={`reader-tool ${findOpen ? "is-active" : ""}`}
            title="书内搜索"
            onClick={() => {
              setTocOpen(false);
              setFindOpen((v) => !v);
            }}
            disabled={reader.status !== "ready"}
          >
            <SearchIcon size={16} />
          </button>
          <button
            className={`reader-tool ${tocOpen ? "is-active" : ""}`}
            title="目录"
            onClick={() => {
              setFindOpen(false);
              setTocOpen((v) => !v);
            }}
            disabled={reader.status !== "ready"}
          >
            <List size={16} />
          </button>
        </div>
      </header>

      <main className="reader-main">
        <div className="reader-page">
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
            bookId={book.id}
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
            <div
              className={`selection-popover ${reader.selection.flip ? "is-flipped" : ""}`}
              style={{ left: reader.selection.x, top: reader.selection.y }}
              onMouseDown={(event) => event.preventDefault()}
            >
              {reader.selection.existingId ? (
                <>
                  <button onClick={() => reader.runAction("card")}>
                    <Quote size={13} />
                    生成卡片
                  </button>
                  <button className="is-danger" onClick={() => reader.runAction("remove")}>
                    <Trash2 size={13} />
                    删除划线
                  </button>
                </>
              ) : (
                <>
                  <button onClick={() => reader.runAction("highlight")}>
                    <Highlighter size={13} />
                    划线
                  </button>
                  <button onClick={() => reader.runAction("card")}>
                    <Quote size={13} />
                    生成卡片
                  </button>
                  <button onClick={() => reader.runAction("character")}>
                    <UserRound size={13} />
                    人物
                  </button>
                  {isEnglishSelection(reader.selection.text) ? (
                    <button onClick={() => reader.runAction("lookup")}>
                      <Languages size={13} />
                      查词
                    </button>
                  ) : null}
                </>
              )}
            </div>
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
                        bookTitle: book.title,
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
      </main>

      <footer className="reader-foot">
        <button
          className="page-button"
          title="上一页"
          onClick={() => reader.turn("prev")}
          disabled={reader.status !== "ready"}
        >
          <ChevronLeft size={16} />
        </button>
        <div className="reader-progress">
          <span>
            {reader.page.total > 0
              ? `${reader.page.index + 1} / ${reader.page.total} 章`
              : "—"}
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
          title="下一页"
          onClick={() => reader.turn("next")}
          disabled={reader.status !== "ready"}
        >
          <ChevronRight size={16} />
        </button>
      </footer>
    </section>
  );
}
