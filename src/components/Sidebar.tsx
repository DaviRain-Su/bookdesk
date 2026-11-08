import { useMemo, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  FolderClosed,
  FolderInput,
  LayoutGrid,
  Plus,
} from "lucide-react";
import { coverSrc, type LibraryBook } from "./LibraryDrawer";
import type { BoardOption } from "./TopBar";

interface Props {
  books: LibraryBook[];
  boards: BoardOption[];
  activeBoardId: string;
  onRead: (book: LibraryBook) => void;
  onAddToBoard: (book: LibraryBook) => void;
  onSwitchBoard: (id: string) => void;
  onCreateBoard: () => void;
  onSetCollection: (bookId: string, collection: string) => void;
  onSetFolder: (boardId: string, folder: string) => void;
  onImport: () => void;
}

interface Group<T> {
  name: string;
  items: T[];
}

/** 按收藏/文件夹分组；未分组的排最后。 */
function groupItems<T>(items: T[], nameOf: (item: T) => string | null): Group<T>[] {
  const map = new Map<string, T[]>();
  for (const item of items) {
    const name = (nameOf(item) ?? "").trim();
    const list = map.get(name);
    if (list) list.push(item);
    else map.set(name, [item]);
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => {
      if (a === "") return 1;
      if (b === "") return -1;
      return a.localeCompare(b, "zh-Hans-CN");
    })
    .map(([name, list]) => ({ name, items: list }));
}

function MovePanel({
  current,
  groups,
  onMove,
  onClose,
}: {
  current: string;
  groups: string[];
  onMove: (name: string) => void;
  onClose: () => void;
}) {
  const [value, setValue] = useState("");
  const options = groups.filter((name) => name !== current);
  return (
    <div className="sidebar-move">
      <div className="sidebar-move-chips">
        <button
          className={current === "" ? "is-active" : ""}
          onClick={() => {
            onMove("");
            onClose();
          }}
        >
          未分组
        </button>
        {options.map((name) => (
          <button
            key={name}
            onClick={() => {
              onMove(name);
              onClose();
            }}
          >
            {name}
          </button>
        ))}
      </div>
      <form
        className="sidebar-move-new"
        onSubmit={(event) => {
          event.preventDefault();
          const name = value.trim();
          if (!name) return;
          onMove(name);
          onClose();
        }}
      >
        <input
          value={value}
          autoFocus
          spellCheck={false}
          placeholder="新建…"
          onChange={(event) => setValue(event.target.value)}
        />
        <button type="submit">确定</button>
      </form>
    </div>
  );
}

export default function Sidebar({
  books,
  boards,
  activeBoardId,
  onRead,
  onAddToBoard,
  onSwitchBoard,
  onCreateBoard,
  onSetCollection,
  onSetFolder,
  onImport,
}: Props) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [moveOpen, setMoveOpen] = useState<string | null>(null);

  const bookGroups = useMemo(() => groupItems(books, (b) => b.collection), [books]);
  const boardGroups = useMemo(() => groupItems(boards, (b) => b.folder), [boards]);
  const collections = useMemo(
    () => bookGroups.map((g) => g.name).filter((name) => name !== ""),
    [bookGroups],
  );
  const folders = useMemo(
    () => boardGroups.map((g) => g.name).filter((name) => name !== ""),
    [boardGroups],
  );

  const toggle = (key: string) => {
    setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  return (
    <aside className="sidebar" aria-label="书架与画布">
      <div className="sidebar-scroll">
        <section className="sidebar-section">
          <header className="sidebar-head">
            <span className="sidebar-head-title">书架</span>
            <span className="sidebar-count">{books.length}</span>
            <button className="sidebar-add" title="导入 EPUB" onClick={onImport}>
              <Plus size={14} />
            </button>
          </header>

          {bookGroups.length === 0 ? (
            <p className="sidebar-empty">还没有书，导入一本 EPUB 开始。</p>
          ) : (
            bookGroups.map((group) => {
              const key = `book:${group.name}`;
              const isCollapsed = collapsed[key] ?? false;
              return (
                <div className="sidebar-group" key={key}>
                  <button className="sidebar-group-row" onClick={() => toggle(key)}>
                    {isCollapsed ? <ChevronRight size={13} /> : <ChevronDown size={13} />}
                    <FolderClosed size={14} />
                    <span className="sidebar-group-name">{group.name || "未分组"}</span>
                    <span className="sidebar-group-count">{group.items.length}</span>
                  </button>

                  {isCollapsed
                    ? null
                    : group.items.map((book) => {
                        const src = coverSrc(book);
                        const moveKey = `book:${book.id}`;
                        return (
                          <div className="sidebar-item" key={book.id}>
                            <button
                              className="sidebar-book"
                              onClick={() => onRead(book)}
                              title={`全屏阅读《${book.title}》`}
                            >
                              <span className="sidebar-book-cover">
                                {src ? (
                                  <img src={src} alt="" draggable={false} />
                                ) : (
                                  <span>{book.title.slice(0, 1)}</span>
                                )}
                              </span>
                              <span className="sidebar-book-text">
                                <span className="sidebar-book-title">{book.title}</span>
                                <span className="sidebar-book-sub">
                                  {book.author || "未知作者"}
                                </span>
                              </span>
                            </button>
                            <span className="sidebar-actions">
                              <button
                                title="放到当前画布"
                                onClick={() => onAddToBoard(book)}
                              >
                                <Plus size={13} />
                              </button>
                              <button
                                title="移到收藏"
                                onClick={() =>
                                  setMoveOpen((prev) => (prev === moveKey ? null : moveKey))
                                }
                              >
                                <FolderInput size={13} />
                              </button>
                            </span>
                            {moveOpen === moveKey ? (
                              <MovePanel
                                current={book.collection ?? ""}
                                groups={collections}
                                onMove={(name) => onSetCollection(book.id, name)}
                                onClose={() => setMoveOpen(null)}
                              />
                            ) : null}
                          </div>
                        );
                      })}
                </div>
              );
            })
          )}
        </section>

        <section className="sidebar-section">
          <header className="sidebar-head">
            <span className="sidebar-head-title">画布</span>
            <span className="sidebar-count">{boards.length}</span>
            <button className="sidebar-add" title="新建画布" onClick={onCreateBoard}>
              <Plus size={14} />
            </button>
          </header>

          {boardGroups.length === 0 ? (
            <p className="sidebar-empty">还没有画布。</p>
          ) : (
            boardGroups.map((group) => {
              const key = `board:${group.name}`;
              const isCollapsed = collapsed[key] ?? false;
              return (
                <div className="sidebar-group" key={key}>
                  <button className="sidebar-group-row" onClick={() => toggle(key)}>
                    {isCollapsed ? <ChevronRight size={13} /> : <ChevronDown size={13} />}
                    <FolderClosed size={14} />
                    <span className="sidebar-group-name">{group.name || "未分组"}</span>
                    <span className="sidebar-group-count">{group.items.length}</span>
                  </button>

                  {isCollapsed
                    ? null
                    : group.items.map((board) => {
                        const moveKey = `board:${board.id}`;
                        return (
                          <div className="sidebar-item" key={board.id}>
                            <button
                              className={`sidebar-board ${
                                board.id === activeBoardId ? "is-active" : ""
                              }`}
                              onClick={() => onSwitchBoard(board.id)}
                              title={`切换到「${board.name}」`}
                            >
                              <LayoutGrid size={14} />
                              <span className="sidebar-board-name">{board.name}</span>
                              {board.id === activeBoardId ? (
                                <span className="sidebar-board-current">当前</span>
                              ) : null}
                            </button>
                            <span className="sidebar-actions">
                              <button
                                title="移到文件夹"
                                onClick={() =>
                                  setMoveOpen((prev) => (prev === moveKey ? null : moveKey))
                                }
                              >
                                <FolderInput size={13} />
                              </button>
                            </span>
                            {moveOpen === moveKey ? (
                              <MovePanel
                                current={board.folder ?? ""}
                                groups={folders}
                                onMove={(name) => onSetFolder(board.id, name)}
                                onClose={() => setMoveOpen(null)}
                              />
                            ) : null}
                          </div>
                        );
                      })}
                </div>
              );
            })
          )}
        </section>
      </div>
    </aside>
  );
}
