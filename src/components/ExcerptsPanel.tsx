import { useEffect, useState } from "react";
import { FileOutput, Highlighter, Share2, Trash2, X } from "lucide-react";
import { Notes } from "../mygo";
import type { Highlight } from "../types";
import type { LibraryBook } from "./LibraryDrawer";

interface Props {
  open: boolean;
  books: LibraryBook[];
  onClose: () => void;
  onOpenHighlight: (highlight: Highlight) => void;
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

export default function ExcerptsPanel({ open, books, onClose, onOpenHighlight }: Props) {
  const [items, setItems] = useState<Highlight[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    (async () => {
      try {
        const list = await Notes.listHighlights(null);
        if (!cancelled) setItems(list);
      } catch (err) {
        if (!cancelled) setError(`读取摘录失败：${errorText(err)}`);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [open]);

  const remove = async (item: Highlight) => {
    try {
      await Notes.deleteHighlight(item.id);
      setItems((list) => list.filter((x) => x.id !== item.id));
      window.dispatchEvent(
        new CustomEvent("bookdesk:highlight-deleted", { detail: { id: item.id } }),
      );
      window.dispatchEvent(new Event("bookdesk:highlights-changed"));
    } catch (err) {
      setError(`删除摘录失败：${errorText(err)}`);
    }
  };

  const groups: { bookId: string; title: string; items: Highlight[] }[] = [];
  for (const item of items) {
    let group = groups.find((g) => g.bookId === item.bookId);
    if (!group) {
      group = {
        bookId: item.bookId,
        title: books.find((b) => b.id === item.bookId)?.title ?? "未知书籍",
        items: [],
      };
      groups.push(group);
    }
    group.items.push(item);
  }

  return (
    <>
      <div
        className={`drawer-backdrop ${open ? "is-open" : ""}`}
        onClick={onClose}
        aria-hidden
      />
      <aside
        className={`drawer excerpts-drawer ${open ? "is-open" : ""}`}
        aria-hidden={!open}
      >
        <header className="drawer-head">
          <div>
            <h2>摘录</h2>
            <span className="drawer-sub">
              {items.length} 条划线
              {groups.length > 0 ? `，来自 ${groups.length} 本书` : ""}
            </span>
          </div>
          <div className="drawer-actions">
            <button className="icon-button" onClick={onClose} title="关闭">
              <X size={17} />
            </button>
          </div>
        </header>

        <div className="drawer-body">
          {loading ? <p className="settings-hint">正在读取摘录…</p> : null}
          {error ? <div className="settings-notice is-error">{error}</div> : null}
          {!loading && !error && items.length === 0 ? (
            <div className="drawer-empty">
              <p>还没有摘录。</p>
              <p className="drawer-empty-hint">
                在书里划选文字，点「划线」，就会出现在这里。点击一条摘录可以跳回书里的位置。
              </p>
            </div>
          ) : null}
          {groups.map((group) => (
            <section key={group.bookId} className="excerpt-group">
              <div className="excerpt-group-head">
                <h3 className="excerpt-book">{group.title}</h3>
                <button
                  className="excerpt-export"
                  title="导出这本书的公开页"
                  onClick={() =>
                    window.dispatchEvent(
                      new CustomEvent("bookdesk:export-book", { detail: { bookId: group.bookId } }),
                    )
                  }
                >
                  <FileOutput size={13} />
                  公开页
                </button>
              </div>
              {group.items.map((item) => (
                <div key={item.id} className="excerpt-item">
                  <button
                    className="excerpt-row"
                    onClick={() => onOpenHighlight(item)}
                    title="跳到书里的位置"
                  >
                    <span className="excerpt-text">{item.text}</span>
                    <span className="excerpt-meta">
                      {item.chapter ? <span>{item.chapter}</span> : null}
                      {item.location.pageLabel ? <span>{item.location.pageLabel}</span> : null}
                      <Highlighter size={12} />
                    </span>
                  </button>
                  <button
                    className="excerpt-share"
                    title="做成分享卡片"
                    onClick={() => {
                      const book = books.find((entry) => entry.id === item.bookId);
                      window.dispatchEvent(
                        new CustomEvent("bookdesk:share-card", {
                          detail: {
                            bookId: item.bookId,
                            quote: item.text,
                            title: book?.title ?? group.title,
                            author: book?.author ?? null,
                            chapter: item.chapter ?? null,
                            color: item.color,
                          },
                        }),
                      );
                    }}
                  >
                    <Share2 size={13} />
                  </button>
                  <button
                    className="excerpt-delete"
                    title="删除这条摘录"
                    onClick={() => void remove(item)}
                  >
                    <Trash2 size={13} />
                  </button>
                </div>
              ))}
            </section>
          ))}
        </div>
      </aside>
    </>
  );
}
