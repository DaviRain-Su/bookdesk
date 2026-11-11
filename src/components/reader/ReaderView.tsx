import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  FileOutput,
  Languages,
  LibraryBig,
  List,
  Loader2,
  MessageCircle,
  NotebookText,
  Search as SearchIcon,
  TriangleAlert,
  Type,
} from "lucide-react";
import { Characters, type Character, type ReadingSettings } from "../../mygo";
import { convertHan } from "../../lib/han";
import { EXPORT_BOOK_EVENT } from "../../lib/share";
import type { RevealTarget } from "../../lib/reveal";
import FindPopover from "./FindPopover";
import ReaderSettingsPanel from "./ReaderSettingsPanel";
import SelectionMenu from "./SelectionMenu";
import TocPopover from "./TocPopover";
import { useEpubReader, type ReaderProgress } from "./useEpubReader";

export interface ReaderBook {
  id: string;
  title: string;
  author: string | null;
}

interface Props {
  book: ReaderBook;
  settings: ReadingSettings;
  initialCfi: string | null;
  initialSpineIndex: number;
  initialPercent: number;
  initialReveal: RevealTarget | null;
  onSettingsChange: (patch: Partial<ReadingSettings>) => void;
  onClose: () => void;
  onProgress: (progress: ReaderProgress) => void;
  onOpenExcerpts: () => void;
  onOpenSearch: () => void;
}

/** 全屏阅读：一本书占满主区域，和书卡共用同一个 epub.js 引擎。 */
export default function ReaderView({
  book,
  settings,
  initialCfi,
  initialSpineIndex,
  initialPercent,
  initialReveal,
  onSettingsChange,
  onClose,
  onProgress,
  onOpenExcerpts,
  onOpenSearch,
}: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [tocOpen, setTocOpen] = useState(false);
  const [findOpen, setFindOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [charactersOpen, setCharactersOpen] = useState(false);
  const [characters, setCharacters] = useState<Character[] | null>(null);

  const reader = useEpubReader(hostRef, {
    bookId: book.id,
    title: book.title,
    initialCfi,
    initialSpineIndex,
    initialPercent,
    initialReveal,
    settings,
    mode: "reader",
    onProgress,
  });

  const openCharacters = () => {
    setSettingsOpen(false);
    setFindOpen(false);
    setTocOpen(false);
    if (charactersOpen) {
      setCharactersOpen(false);
      return;
    }
    setCharactersOpen(true);
    setCharacters(null);
    void Characters.list(book.id)
      .then((list) => setCharacters(list))
      .catch(() => setCharacters([]));
  };

  const startChat = (character: Character) => {
    setCharactersOpen(false);
    window.dispatchEvent(
      new CustomEvent("bookdesk:open-chat", {
        detail: {
          characterId: character.id,
          bookId: character.bookId,
          bookTitle: book.title,
          name: character.name,
          description: character.description ?? "",
        },
      }),
    );
  };

  const spread = settings.spread && !settings.vertical;
  const pageWidth = spread ? settings.columnWidth * 2 + 48 : settings.columnWidth;
  const displayChapter = reader.chapter
    ? convertHan(reader.chapter, settings.convert)
    : "";
  const displayToc = useMemo(
    () =>
      reader.toc.map((entry) => ({
        ...entry,
        label: convertHan(entry.label, settings.convert),
      })),
    [reader.toc, settings.convert],
  );

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (settingsOpen) {
        setSettingsOpen(false);
        return;
      }
      if (findOpen) {
        setFindOpen(false);
        return;
      }
      if (tocOpen) {
        setTocOpen(false);
        return;
      }
      if (charactersOpen) {
        setCharactersOpen(false);
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
      if (settings.zen) {
        onSettingsChange({ zen: false });
        return;
      }
      onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    settingsOpen,
    findOpen,
    tocOpen,
    charactersOpen,
    reader.selection,
    reader.hover,
    reader.dismissSelection,
    reader.dismissHover,
    settings.zen,
    onSettingsChange,
    onClose,
  ]);

  return (
    <section
      className={`reader-overlay theme-${settings.theme}${settings.zen ? " is-zen" : ""}${
        spread ? " is-spread" : ""
      }`}
      aria-label="全屏阅读"
    >
      {settings.zen ? null : (
        <header className="reader-top">
          <button className="reader-back" onClick={onClose} title="回到书桌（Esc）">
            <ArrowLeft size={16} />
            <span>书桌</span>
          </button>
          <div className="reader-heading">
            <span className="reader-title">{book.title}</span>
            <span className="reader-sub">
              {book.author || "未知作者"}
              {displayChapter ? ` · ${displayChapter}` : ""}
            </span>
          </div>
          <div className="reader-tools">
            <div className="reader-tool-wrap">
              <button
                className={`reader-tool ${charactersOpen ? "is-active" : ""}`}
                title="伴读：和书里的人物对话"
                onClick={openCharacters}
              >
                <MessageCircle size={16} />
              </button>
              {charactersOpen ? (
                <div className="reader-char-popover" role="dialog" aria-label="伴读">
                  <div className="reader-char-head">伴读</div>
                  {characters === null ? (
                    <div className="reader-char-empty">正在读取人物…</div>
                  ) : characters.length === 0 ? (
                    <div className="reader-char-empty">
                      还没有人物。在书里划选一个人名，点「人物」就会建一张人物卡。
                    </div>
                  ) : (
                    characters.map((character) => (
                      <button
                        key={character.id}
                        className="reader-char-row"
                        onClick={() => startChat(character)}
                      >
                        <span className="reader-char-name">{character.name}</span>
                        {character.description ? (
                          <span className="reader-char-desc">{character.description}</span>
                        ) : null}
                      </button>
                    ))
                  )}
                </div>
              ) : null}
            </div>
            <button
              className="reader-tool"
              title="导出这本书的公开页"
              onClick={() => {
                setSettingsOpen(false);
                setFindOpen(false);
                setTocOpen(false);
                setCharactersOpen(false);
                window.dispatchEvent(
                  new CustomEvent(EXPORT_BOOK_EVENT, { detail: { bookId: book.id } }),
                );
              }}
            >
              <FileOutput size={16} />
            </button>
            <button
              className="reader-tool"
              title="摘录"
              onClick={() => {
                setSettingsOpen(false);
                setFindOpen(false);
                setTocOpen(false);
                setCharactersOpen(false);
                onOpenExcerpts();
              }}
            >
              <NotebookText size={16} />
            </button>
            <button
              className="reader-tool"
              title="跨书搜索"
              onClick={() => {
                setSettingsOpen(false);
                setFindOpen(false);
                setTocOpen(false);
                setCharactersOpen(false);
                onOpenSearch();
              }}
            >
              <LibraryBig size={16} />
            </button>
            <button
              className={`reader-tool ${settingsOpen ? "is-active" : ""}`}
              title="阅读设置"
              onClick={() => {
                setFindOpen(false);
                setTocOpen(false);
                setCharactersOpen(false);
                setSettingsOpen((v) => !v);
              }}
            >
              <Type size={16} />
            </button>
            <button
              className={`reader-tool ${findOpen ? "is-active" : ""}`}
              title="书内搜索"
              onClick={() => {
                setSettingsOpen(false);
                setTocOpen(false);
                setCharactersOpen(false);
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
                setSettingsOpen(false);
                setFindOpen(false);
                setCharactersOpen(false);
                setTocOpen((v) => !v);
              }}
              disabled={reader.status !== "ready"}
            >
              <List size={16} />
            </button>
          </div>
        </header>
      )}

      <main className="reader-main">
        <div className="reader-page" style={{ width: `min(${pageWidth}px, 100%)` }}>
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
              toc={displayToc}
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

      {settings.zen ? null : (
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
      )}

      {settings.zen ? (
        <div className="reader-zen-bar">
          <button
            title="阅读设置"
            onClick={() => setSettingsOpen((v) => !v)}
            className={settingsOpen ? "is-active" : ""}
          >
            <Type size={14} />
          </button>
          <button onClick={() => onSettingsChange({ zen: false })}>退出禅意</button>
        </div>
      ) : null}

      {settingsOpen ? (
        <ReaderSettingsPanel
          settings={settings}
          bookTitle={book.title}
          onChange={onSettingsChange}
          onClose={() => setSettingsOpen(false)}
        />
      ) : null}
    </section>
  );
}
