import { useEffect, useState } from "react";
import { FileText, X } from "lucide-react";
import { Share, type BookExportChoice, type BookShareItem } from "../mygo";

interface Props {
  bookId: string | null;
  onClose: () => void;
  onDone: (message: string) => void;
  onError: (message: string) => void;
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

function fileName(path: string): string {
  const parts = path.split(/[/\\]/);
  return parts[parts.length - 1] || path;
}

export default function BookExportDialog({ bookId, onClose, onDone, onError }: Props) {
  const [title, setTitle] = useState("");
  const [author, setAuthor] = useState("");
  const [items, setItems] = useState<BookShareItem[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [notes, setNotes] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!bookId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    setSelected(new Set());
    setNotes(new Set());
    setItems([]);
    void Share.listBook(bookId)
      .then((list) => {
        if (cancelled) return;
        setTitle(list.title || "未命名书籍");
        setAuthor(list.author || "");
        setItems(list.items ?? []);
      })
      .catch((err) => {
        if (!cancelled) setError(`读取划线失败：${errorText(err)}`);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [bookId]);

  useEffect(() => {
    if (!bookId) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      onClose();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [bookId, onClose]);

  if (!bookId) return null;

  const toggle = (id: string) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
        setNotes((noteSet) => {
          if (!noteSet.has(id)) return noteSet;
          const copy = new Set(noteSet);
          copy.delete(id);
          return copy;
        });
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const toggleNote = (id: string) => {
    setNotes((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAll = () => {
    setSelected(new Set(items.map((item) => item.id)));
    setNotes(new Set());
  };

  const selectNone = () => {
    setSelected(new Set());
    setNotes(new Set());
  };

  const choices = (): BookExportChoice[] =>
    items
      .filter((item) => selected.has(item.id))
      .map((item) => ({ id: item.id, includeNote: notes.has(item.id) }));

  const save = async (kind: "html" | "markdown") => {
    setBusy(true);
    setError("");
    try {
      const input = { bookId, items: choices() };
      const result =
        kind === "html" ? await Share.saveHTML(input) : await Share.saveMarkdown(input);
      if (!result.cancelled && result.path) {
        onDone(`已保存到「${fileName(result.path)}」`);
        onClose();
      }
    } catch (err) {
      const message = `导出失败：${errorText(err)}`;
      setError(message);
      onError(message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="share-layer">
      <div className="share-backdrop" onClick={onClose} />
      <section className="share-panel export-panel" role="dialog" aria-label="导出公开页">
        <header className="share-head">
          <div>
            <h2>导出《{title || "这本书"}》</h2>
            <p>
              {author ? `${author}。` : ""}
              默认一条都不选。笔记要单独勾选才会写进文件。页面不连外网，你可以自己托管。
            </p>
          </div>
          <button className="icon-button" onClick={onClose} title="关闭">
            <X size={17} />
          </button>
        </header>
        <div className="export-toolbar">
          <button className="text-button" onClick={selectAll} disabled={items.length === 0 || busy}>
            全选划线
          </button>
          <button className="text-button" onClick={selectNone} disabled={selected.size === 0 || busy}>
            清除选择
          </button>
          <span className="export-count">
            已选 {selected.size} / {items.length}
            {notes.size > 0 ? `，含 ${notes.size} 条笔记` : ""}
          </span>
        </div>
        <div className="export-list">
          {loading ? <p className="settings-hint">正在读取划线…</p> : null}
          {!loading && items.length === 0 && !error ? (
            <p className="settings-hint">这本书还没有划线。仍可以只导出书名和作者。</p>
          ) : null}
          {items.map((item) => {
            const on = selected.has(item.id);
            const hasNote = item.note.trim() !== "";
            return (
              <div key={item.id} className={`export-item ${on ? "is-on" : ""}`}>
                <label className="export-quote">
                  <input
                    type="checkbox"
                    checked={on}
                    disabled={busy}
                    onChange={() => toggle(item.id)}
                  />
                  <span>
                    {item.quote}
                    {item.chapter ? <small>{item.chapter}</small> : null}
                  </span>
                </label>
                {hasNote ? (
                  <label className={`export-note ${on ? "" : "is-disabled"}`}>
                    <input
                      type="checkbox"
                      checked={on && notes.has(item.id)}
                      disabled={!on || busy}
                      onChange={() => toggleNote(item.id)}
                    />
                    <span>包含这条笔记</span>
                  </label>
                ) : null}
              </div>
            );
          })}
        </div>
        {error ? <p className="share-error">{error}</p> : null}
        {selected.size === 0 && !loading ? (
          <p className="export-empty-hint">没有勾选划线。导出的文件里只有书名和作者。</p>
        ) : null}
        <div className="share-actions">
          <button className="pill-button" disabled={busy || loading} onClick={() => void save("markdown")}>
            <FileText size={15} />
            导出 Markdown
          </button>
          <button className="primary-button" disabled={busy || loading} onClick={() => void save("html")}>
            <FileText size={15} />
            导出 HTML
          </button>
        </div>
      </section>
    </div>
  );
}
