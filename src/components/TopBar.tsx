import { useEffect, useRef, useState } from "react";
import {
  BookPlus,
  Check,
  ChevronDown,
  Highlighter,
  Library,
  Maximize2,
  NotebookPen,
  PanelLeft,
  Pencil,
  Plus,
  Search,
  Settings,
  Trash2,
} from "lucide-react";

export interface BoardOption {
  id: string;
  name: string;
  folder: string | null;
}

interface Props {
  boards: BoardOption[];
  activeBoardId: string;
  boardName: string;
  highlightCount: number;
  searchOpen: boolean;
  settingsOpen: boolean;
  sidebarOpen: boolean;
  onToggleSidebar: () => void;
  onAddBook: () => void;
  onOpenLibrary: () => void;
  onOpenExcerpts: () => void;
  onImportNotes: () => void;
  onOpenSearch: () => void;
  onOpenSettings: () => void;
  onOpenReader: () => void;
  canRead: boolean;
  onOpenBoards: () => void;
  onSwitchBoard: (id: string) => void;
  onCreateBoard: () => void;
  onRenameBoard: (id: string, name: string) => void;
  onDeleteBoard: (id: string) => void;
}

export default function TopBar({
  boards,
  activeBoardId,
  boardName,
  highlightCount,
  searchOpen,
  settingsOpen,
  sidebarOpen,
  onToggleSidebar,
  onAddBook,
  onOpenLibrary,
  onOpenExcerpts,
  onImportNotes,
  onOpenSearch,
  onOpenSettings,
  onOpenReader,
  canRead,
  onOpenBoards,
  onSwitchBoard,
  onCreateBoard,
  onRenameBoard,
  onDeleteBoard,
}: Props) {
  const [open, setOpen] = useState(false);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      if (!wrapRef.current?.contains(event.target as globalThis.Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  useEffect(() => {
    if (!confirmDeleteId) return;
    const timer = window.setTimeout(() => setConfirmDeleteId(null), 4000);
    return () => window.clearTimeout(timer);
  }, [confirmDeleteId]);

  const toggle = () => {
    if (!open) onOpenBoards();
    setOpen((v) => !v);
    setRenamingId(null);
    setConfirmDeleteId(null);
  };

  const startRename = (board: BoardOption) => {
    setRenamingId(board.id);
    setRenameValue(board.name);
    setConfirmDeleteId(null);
  };

  const commitRename = () => {
    if (!renamingId) return;
    const name = renameValue.trim();
    const current = boards.find((b) => b.id === renamingId);
    if (name && current && name !== current.name) onRenameBoard(renamingId, name);
    setRenamingId(null);
  };

  return (
    <header className="topbar">
      <div className="topbar-left">
        <button
          className={`icon-button sidebar-toggle ${sidebarOpen ? "is-active" : ""}`}
          onClick={onToggleSidebar}
          title={sidebarOpen ? "收起侧栏" : "展开侧栏"}
        >
          <PanelLeft size={17} />
        </button>
        <div className="app-mark" aria-hidden>
          <span />
          <span />
          <span />
        </div>
        <div className="board-switcher" ref={wrapRef}>
          <button
            className={`board-title ${open ? "is-open" : ""}`}
            onClick={toggle}
            title="切换画布"
          >
            <span className="board-name">{boardName}</span>
            <span className="board-kind">画布</span>
            <ChevronDown size={13} className="board-chevron" />
          </button>
          {open ? (
            <div className="board-menu">
              <div className="board-menu-head">画布</div>
              {boards.map((b) => (
                <div
                  key={b.id}
                  className={`board-menu-row ${b.id === activeBoardId ? "is-active" : ""}`}
                >
                  {renamingId === b.id ? (
                    <input
                      className="board-menu-input"
                      value={renameValue}
                      autoFocus
                      spellCheck={false}
                      onChange={(event) => setRenameValue(event.target.value)}
                      onBlur={commitRename}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          commitRename();
                        } else if (event.key === "Escape") {
                          event.preventDefault();
                          setRenamingId(null);
                        }
                      }}
                    />
                  ) : (
                    <button
                      className="board-menu-name"
                      onClick={() => {
                        onSwitchBoard(b.id);
                        setOpen(false);
                      }}
                      title={b.name}
                    >
                      <span>{b.name}</span>
                      {b.id === activeBoardId ? <Check size={13} /> : null}
                    </button>
                  )}
                  {confirmDeleteId === b.id ? (
                    <button
                      className="board-menu-confirm"
                      onClick={() => {
                        onDeleteBoard(b.id);
                        setConfirmDeleteId(null);
                      }}
                    >
                      确认删除
                    </button>
                  ) : (
                    <>
                      <button
                        className="board-menu-action"
                        title="重命名"
                        onClick={() => startRename(b)}
                      >
                        <Pencil size={12} />
                      </button>
                      <button
                        className="board-menu-action"
                        title={boards.length <= 1 ? "至少保留一块画布" : "删除画布"}
                        disabled={boards.length <= 1}
                        onClick={() => {
                          setConfirmDeleteId(b.id);
                          setRenamingId(null);
                        }}
                      >
                        <Trash2 size={12} />
                      </button>
                    </>
                  )}
                </div>
              ))}
              <button
                className="board-menu-new"
                onClick={() => {
                  onCreateBoard();
                  setOpen(false);
                }}
              >
                <Plus size={14} />
                新建画布
              </button>
            </div>
          ) : null}
        </div>
      </div>

      <div className="topbar-right">
        <button
          className="pill-button reader-entry"
          title={canRead ? "全屏阅读当前书（⌘⇧R）" : "先添加一本书"}
          onClick={onOpenReader}
          disabled={!canRead}
        >
          <Maximize2 size={15} />
          全屏阅读
        </button>
        <button
          className={`pill-button ${searchOpen ? "is-active" : ""}`}
          title="跨书搜索"
          onClick={onOpenSearch}
        >
          <Search size={15} />
          跨书搜索
        </button>
        <button className="stat stat-button" title="查看所有摘录" onClick={onOpenExcerpts}>
          <Highlighter size={15} />
          <span>摘录 {highlightCount}</span>
        </button>
        <button className="icon-button" onClick={onImportNotes} title="导入笔记">
          <NotebookPen size={17} />
        </button>
        <button className="icon-button" onClick={onOpenLibrary} title="书架">
          <Library size={17} />
        </button>
        <button
          className={`icon-button ${settingsOpen ? "is-active" : ""}`}
          onClick={onOpenSettings}
          title="设置"
        >
          <Settings size={17} />
        </button>
        <button className="primary-button" onClick={onAddBook}>
          <BookPlus size={16} />
          添加书籍
        </button>
      </div>
    </header>
  );
}
