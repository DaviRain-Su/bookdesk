import { useCallback, useEffect, useRef, useState } from "react";
import { event as appEvent, onFileDrop } from "mygo-runtime";
import type { Edge, Node, Viewport } from "@xyflow/react";
import TopBar, { type BoardOption } from "./components/TopBar";
import Toolbar, { type Tool } from "./components/Toolbar";
import CanvasBoard, { markBoardDeleted } from "./components/CanvasBoard";
import LibraryDrawer, { type LibraryBook } from "./components/LibraryDrawer";
import ExcerptsPanel from "./components/ExcerptsPanel";
import SearchPanel from "./components/SearchPanel";
import ChatPanel, { type ChatTarget } from "./components/ChatPanel";
import SettingsPanel from "./components/SettingsPanel";
import LookupPanel, { type LookupRequest } from "./components/LookupPanel";
import type { SearchHit } from "./lib/search";
import type { Highlight } from "./types";
import { Board, Library, Notes, events } from "./mygo";
import { parseEdges, parseNodes, parseViewport } from "./lib/board";
import { REVEAL_EVENT, type RevealTarget } from "./lib/reveal";

type Toast = { id: string; message: string; kind: "info" | "error" };

interface BoardState {
  nodes: Node[];
  edges: Edge[];
  viewport: Viewport;
  stored: boolean;
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

function fileName(path: string): string {
  const parts = path.split("/");
  return parts[parts.length - 1] || path;
}

export default function App() {
  const [tool, setTool] = useState<Tool>("select");
  const [books, setBooks] = useState<LibraryBook[]>([]);
  const [board, setBoard] = useState<{ id: string; name: string } | null>(null);
  const [boards, setBoards] = useState<BoardOption[]>([]);
  const [boardState, setBoardState] = useState<BoardState | null>(null);
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [excerptsOpen, setExcerptsOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [chatTarget, setChatTarget] = useState<ChatTarget | null>(null);
  const [lookup, setLookup] = useState<LookupRequest | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [highlightCount, setHighlightCount] = useState(0);
  const [boardBookIds, setBoardBookIds] = useState<string[]>([]);
  const [importing, setImporting] = useState<{ done: number; total: number; title: string } | null>(
    null,
  );
  const toastTimers = useRef<number[]>([]);

  const toast = useCallback((message: string, kind: Toast["kind"] = "info") => {
    const id = crypto.randomUUID();
    setToasts((list) => [...list, { id, message, kind }]);
    const timer = window.setTimeout(
      () => setToasts((list) => list.filter((t) => t.id !== id)),
      kind === "error" ? 6500 : 3200,
    );
    toastTimers.current.push(timer);
  }, []);

  useEffect(() => {
    return () => {
      for (const timer of toastTimers.current) window.clearTimeout(timer);
    };
  }, []);

  const refreshBooks = useCallback(async () => {
    try {
      const list = await Library.list();
      setBooks(
        list.map((b) => ({
          id: b.id,
          title: b.title,
          author: b.author,
          coverRelPath: b.coverRelPath,
        })),
      );
    } catch (err) {
      toast(`读取书架失败：${errorText(err)}`, "error");
    }
  }, [toast]);

  const refreshHighlights = useCallback(async () => {
    try {
      const list = await Notes.listHighlights(null);
      // 摘录是整座书桌的，不跟着当前画布变；否则别的画布上的划线会「看不到」。
      setHighlightCount(list.length);
    } catch {
      /* 统计失败不影响阅读 */
    }
  }, []);

  useEffect(() => {
    void refreshBooks();
  }, [refreshBooks]);

  useEffect(() => {
    void refreshHighlights();
  }, [refreshHighlights]);

  useEffect(() => {
    const onChanged = () => void refreshHighlights();
    const onBooks = (event: Event) => {
      const ids = (event as CustomEvent).detail?.bookIds;
      if (!Array.isArray(ids)) return;
      setBoardBookIds((prev) =>
        prev.length === ids.length && prev.every((v, i) => v === ids[i]) ? prev : ids,
      );
    };
    window.addEventListener("bookdesk:highlights-changed", onChanged);
    window.addEventListener("bookdesk:board-books", onBooks);
    return () => {
      window.removeEventListener("bookdesk:highlights-changed", onChanged);
      window.removeEventListener("bookdesk:board-books", onBooks);
    };
  }, [refreshHighlights]);

  const refreshBoards = useCallback(async () => {
    try {
      const list = await Board.list();
      setBoards(list.map((b) => ({ id: b.id, name: b.name })));
    } catch {
      /* 列表刷新失败不影响当前画布 */
    }
  }, []);

  const openBoard = useCallback(async (id: string) => {
    const full = await Board.load(id);
    if (!full) return false;
    const nodes = parseNodes(full.nodesJson);
    const edges = parseEdges(full.edgesJson);
    setBoard({ id: full.id, name: full.name });
    setBoardState({
      nodes,
      edges,
      viewport: parseViewport(full.viewportJson),
      stored: nodes.length > 0 || edges.length > 0,
    });
    return true;
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const summaries = await Board.list();
        const target = summaries[0] ?? (await Board.create("我的画布"));
        if (cancelled) return;
        setBoards(
          summaries.length > 0
            ? summaries.map((b) => ({ id: b.id, name: b.name }))
            : [{ id: target.id, name: target.name }],
        );
        const ok = await openBoard(target.id);
        if (!ok && !cancelled) throw new Error("画布不存在");
      } catch (err) {
        if (cancelled) return;
        toast(`无法打开画布：${errorText(err)}`, "error");
        setBoard({ id: "local", name: "我的画布" });
        setBoardState({ nodes: [], edges: [], viewport: { x: 0, y: 0, zoom: 1 }, stored: false });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [openBoard, toast]);

  const switchBoard = useCallback(
    async (id: string) => {
      if (board?.id === id) return;
      try {
        const ok = await openBoard(id);
        if (!ok) throw new Error("画布不存在");
        setChatTarget(null);
        void refreshBoards();
      } catch (err) {
        toast(`切换画布失败：${errorText(err)}`, "error");
      }
    },
    [board?.id, openBoard, refreshBoards, toast],
  );

  const createBoard = useCallback(async () => {
    try {
      const created = await Board.create(`画布 ${boards.length + 1}`);
      await refreshBoards();
      await openBoard(created.id);
      setChatTarget(null);
    } catch (err) {
      toast(`新建画布失败：${errorText(err)}`, "error");
    }
  }, [boards.length, openBoard, refreshBoards, toast]);

  const renameBoard = useCallback(
    async (id: string, name: string) => {
      try {
        const updated = await Board.rename(id, name);
        setBoards((list) => list.map((b) => (b.id === id ? { ...b, name: updated.name } : b)));
        setBoard((cur) => (cur && cur.id === id ? { ...cur, name: updated.name } : cur));
      } catch (err) {
        toast(`重命名失败：${errorText(err)}`, "error");
      }
    },
    [toast],
  );

  const deleteBoard = useCallback(
    async (id: string) => {
      try {
        // 先标记，避免被删画布卸载时的最后一次保存又把它写回数据库。
        markBoardDeleted(id);
        await Board.delete(id);
        const rest = boards.filter((b) => b.id !== id);
        setBoards(rest);
        if (board?.id === id) {
          const next = rest[0];
          if (next) {
            await openBoard(next.id);
          } else {
            const created = await Board.create("我的画布");
            setBoards([{ id: created.id, name: created.name }]);
            await openBoard(created.id);
          }
          setChatTarget(null);
        }
      } catch (err) {
        toast(`删除画布失败：${errorText(err)}`, "error");
      }
    },
    [board?.id, boards, openBoard, toast],
  );

  useEffect(() => {
    const stop = events["library:import-progress"].on((payload) => {
      setImporting(payload);
      if (payload.total > 0 && payload.done >= payload.total) {
        window.setTimeout(() => setImporting(null), 900);
      }
    });
    return stop;
  }, []);

  const addBookToBoard = useCallback((book: LibraryBook) => {
    window.dispatchEvent(
      new CustomEvent("bookdesk:add-book", {
        detail: { bookId: book.id, title: book.title, author: book.author },
      }),
    );
  }, []);

  const openSearchHit = useCallback(
    (hit: SearchHit, occurrence: number, query: string) => {
      setSearchOpen(false);
      const reveal: RevealTarget = {
        bookId: hit.bookId,
        href: hit.location.href,
        spineIndex: hit.location.spineIndex,
        query,
        occurrence,
      };
      if (boardBookIds.includes(hit.bookId)) {
        window.dispatchEvent(new CustomEvent(REVEAL_EVENT, { detail: reveal }));
        return;
      }
      const book = books.find((b) => b.id === hit.bookId);
      window.dispatchEvent(
        new CustomEvent("bookdesk:add-book", {
          detail: {
            bookId: hit.bookId,
            title: hit.title,
            author: book?.author ?? null,
            reveal,
          },
        }),
      );
    },
    [boardBookIds, books],
  );

  const openHighlight = useCallback(
    (highlight: Highlight) => {
      setExcerptsOpen(false);
      const reveal: RevealTarget = {
        bookId: highlight.bookId,
        href: highlight.location.href ?? null,
        spineIndex: highlight.location.spineIndex,
        query: "",
        occurrence: 0,
        cfi: highlight.location.cfi ?? null,
      };
      if (boardBookIds.includes(highlight.bookId)) {
        window.dispatchEvent(new CustomEvent(REVEAL_EVENT, { detail: reveal }));
        return;
      }
      const book = books.find((b) => b.id === highlight.bookId);
      window.dispatchEvent(
        new CustomEvent("bookdesk:add-book", {
          detail: {
            bookId: highlight.bookId,
            title: book?.title ?? "这本书",
            author: book?.author ?? null,
            reveal,
          },
        }),
      );
    },
    [boardBookIds, books],
  );

  const runImport = useCallback(
    async (paths?: string[]) => {
      try {
        const result = paths
          ? await Library.importPaths(paths)
          : await Library.pickAndImport();
        await refreshBooks();
        for (const err of result.errors) {
          toast(`${fileName(err.path)}：${err.message}`, "error");
        }
        const first = result.books[0];
        if (first) {
          toast(
            result.books.length === 1
              ? `已导入《${first.title}》`
              : `已导入 ${result.books.length} 本书`,
          );
          addBookToBoard({
            id: first.id,
            title: first.title,
            author: first.author,
            coverRelPath: first.coverRelPath,
          });
        }
      } catch (err) {
        toast(`导入失败：${errorText(err)}`, "error");
      } finally {
        setImporting(null);
      }
    },
    [addBookToBoard, refreshBooks, toast],
  );

  useEffect(() => {
    let stop: (() => void) | undefined;
    try {
      stop = onFileDrop(({ paths }) => {
        const epubs = paths.filter((p) => p.toLowerCase().endsWith(".epub"));
        if (epubs.length === 0) {
          toast("只支持导入 .epub 文件", "error");
          return;
        }
        void runImport(epubs);
      });
    } catch {
      /* 在普通浏览器里预览时没有文件拖放 */
    }
    return () => stop?.();
  }, [runImport, toast]);

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || !event.shiftKey) return;
      if (event.key.toLowerCase() !== "f") return;
      event.preventDefault();
      setLibraryOpen(false);
      setSearchOpen((v) => !v);
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  useEffect(() => {
    const stop = appEvent("app:add-book").on(() => {
      void runImport();
    });
    return stop;
  }, [runImport]);

  useEffect(() => {
    const onChat = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | { characterId?: string | null; bookId?: string; name?: string; description?: string }
        | undefined;
      if (!detail?.bookId) return;
      const book = books.find((b) => b.id === detail.bookId);
      setLibraryOpen(false);
      setSearchOpen(false);
      setSettingsOpen(false);
      setChatTarget({
        characterId: typeof detail.characterId === "string" ? detail.characterId : null,
        bookId: detail.bookId,
        bookTitle: book?.title ?? "这本书",
        name: detail.name || "这个人物",
        description: detail.description ?? "",
      });
    };
    const onLookup = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | { word?: string; sentence?: string; bookTitle?: string }
        | undefined;
      if (!detail?.word) return;
      setLookup({
        word: detail.word,
        sentence: detail.sentence ?? "",
        bookTitle: detail.bookTitle ?? "",
      });
    };
    window.addEventListener("bookdesk:open-chat", onChat);
    window.addEventListener("bookdesk:lookup-word", onLookup);
    return () => {
      window.removeEventListener("bookdesk:open-chat", onChat);
      window.removeEventListener("bookdesk:lookup-word", onLookup);
    };
  }, [books]);

  const addTextCard = useCallback(() => {
    window.dispatchEvent(new CustomEvent("bookdesk:add-text-card"));
  }, []);

  const addImageCard = useCallback(async () => {
    try {
      const relPath = await Library.pickImage();
      if (!relPath) return;
      window.dispatchEvent(
        new CustomEvent("bookdesk:add-image-card", { detail: { relPath } }),
      );
    } catch (err) {
      toast(`选择图片失败：${errorText(err)}`, "error");
    }
  }, [toast]);

  return (
    <div className="app">
      <TopBar
        boards={boards}
        activeBoardId={board?.id ?? ""}
        boardName={board?.name ?? "我的画布"}
        highlightCount={highlightCount}
        searchOpen={searchOpen}
        settingsOpen={settingsOpen}
        onAddBook={() => void runImport()}
        onOpenBoards={() => void refreshBoards()}
        onSwitchBoard={(id) => void switchBoard(id)}
        onCreateBoard={() => void createBoard()}
        onRenameBoard={(id, name) => void renameBoard(id, name)}
        onDeleteBoard={(id) => void deleteBoard(id)}
        onOpenLibrary={() => {
          setSearchOpen(false);
          setSettingsOpen(false);
          setChatTarget(null);
          setLibraryOpen((v) => !v);
        }}
        onOpenExcerpts={() => {
          setLibraryOpen(false);
          setSearchOpen(false);
          setSettingsOpen(false);
          setChatTarget(null);
          setExcerptsOpen((v) => !v);
        }}
        onOpenSearch={() => {
          setLibraryOpen(false);
          setSettingsOpen(false);
          setChatTarget(null);
          setSearchOpen((v) => !v);
        }}
        onOpenSettings={() => {
          setLibraryOpen(false);
          setSearchOpen(false);
          setChatTarget(null);
          setSettingsOpen((v) => !v);
        }}
      />
      <div className="app-body">
        <Toolbar
          tool={tool}
          onSelect={setTool}
          onAddTextCard={addTextCard}
          onAddImageCard={() => void addImageCard()}
        />
        {board && boardState ? (
          <CanvasBoard
            key={board.id}
            tool={tool}
            board={board}
            initialNodes={boardState.nodes}
            initialEdges={boardState.edges}
            initialViewport={boardState.viewport}
            hasStoredViewport={boardState.stored}
            hasBooks={books.length > 0}
            onAddBook={() => void runImport()}
            onOpenLibrary={() => {
              setSearchOpen(false);
              setLibraryOpen(true);
            }}
          />
        ) : (
          <div className="canvas-wrap canvas-loading">正在打开画布…</div>
        )}
        <LibraryDrawer
          open={libraryOpen}
          books={books}
          onClose={() => setLibraryOpen(false)}
          onAdd={(book) => {
            addBookToBoard(book);
            setLibraryOpen(false);
          }}
          onImport={() => void runImport()}
        />
        <ExcerptsPanel
          open={excerptsOpen}
          books={books}
          onClose={() => setExcerptsOpen(false)}
          onOpenHighlight={openHighlight}
        />
        <SearchPanel
          open={searchOpen}
          books={books}
          onClose={() => setSearchOpen(false)}
          onOpenHit={openSearchHit}
        />
        <ChatPanel target={chatTarget} onClose={() => setChatTarget(null)} />
        <SettingsPanel open={settingsOpen} onClose={() => setSettingsOpen(false)} />
        <LookupPanel request={lookup} onClose={() => setLookup(null)} />
      </div>

      {importing ? (
        <div className="import-progress">
          <span className="import-progress-label">
            正在导入 {importing.title}
            {importing.total > 1 ? `（${importing.done}/${importing.total}）` : ""}
          </span>
          <div className="progress-track">
            <div
              className="progress-fill"
              style={{
                width: `${importing.total > 0 ? Math.round((importing.done / importing.total) * 100) : 0}%`,
              }}
            />
          </div>
        </div>
      ) : null}

      <div className="toasts">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.kind === "error" ? "is-error" : ""}`}>
            {t.message}
          </div>
        ))}
      </div>
    </div>
  );
}
