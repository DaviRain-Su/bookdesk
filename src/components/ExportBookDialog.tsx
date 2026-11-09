import { useEffect, useState } from "react";
import { FileOutput, X } from "lucide-react";
import { Share, type ShareBook } from "../mygo";
import { errorText, saveLocalFile } from "../lib/localFile";
import { selectAllQuoteIds } from "../lib/shareCard";

interface Props {
  bookId: string | null;
  onClose: () => void;
  onToast: (message: string, kind?: "info" | "error") => void;
}

export default function ExportBookDialog({ bookId, onClose, onToast }: Props) {
  const [book, setBook] = useState<ShareBook | null>(null);
  const [quoteIds, setQuoteIds] = useState<Set<string>>(new Set());
  const [noteIds, setNoteIds] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"html" | "md" | null>(null);

  useEffect(() => {
    if (!bookId) return;
    setBook(null);
    setQuoteIds(new Set());
    setNoteIds(new Set());
    setError("");
    setLoading(true);
    let cancelled = false;
    void Share.listBook(bookId)
      .then((result) => {
        if (!cancelled) setBook({ ...result, items: result.items ?? [] });
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
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [bookId, onClose]);

  if (!bookId) return null;

  const items = book?.items ?? [];
  const allQuotes = items.length > 0 && items.every((item) => quoteIds.has(item.id));

  const toggleQuote = (id: string) => {
    setQuoteIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
        setNoteIds((notes) => {
          if (!notes.has(id)) return notes;
          const copy = new Set(notes);
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
    if (!quoteIds.has(id)) return;
    setNoteIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectQuotes = () => {
    const choice = selectAllQuoteIds(items.map((item) => item.id));
    setQuoteIds(new Set(choice.quoteIds));
    // 全选只补上划线。已经勾过的笔记留着，没勾过的仍然不带。
    setNoteIds((prev) => {
      const next = new Set<string>();
      for (const id of prev) {
        if (choice.quoteIds.includes(id)) next.add(id);
      }
      return next;
    });
  };

  const selectNotesToo = () => {
    setQuoteIds(new Set(items.map((item) => item.id)));
    setNoteIds(new Set(items.filter((item) => item.note.trim()).map((item) => item.id)));
  };

  const exportAs = async (kind: "html" | "md") => {
    if (!book || quoteIds.size === 0) return;
    setBusy(kind);
    try {
      const rendered = await Share.renderExport(book.id, {
        quoteIds: [...quoteIds],
        noteIds: [...noteIds].filter((id) => quoteIds.has(id)),
      });
      const ext = kind === "html" ? "html" : "md";
      const name = `${rendered.suggestedBase || "公开页"}-公开页.${ext}`;
      const text = kind === "html" ? rendered.html : rendered.markdown;
      const type = kind === "html" ? "text/html;charset=utf-8" : "text/markdown;charset=utf-8";
      const blob = new Blob([text], { type });
      const outcome = await saveLocalFile(() => Share.saveText(name, text), blob, name);
      if (outcome === "cancelled") return;
      const notes = rendered.noteCount > 0 ? `，含 ${rendered.noteCount} 条笔记` : "，没有带笔记";
      onToast(
        outcome === "saved"
          ? `已保存 ${rendered.quoteCount} 条划线${notes}`
          : `已开始下载 ${rendered.quoteCount} 条划线${notes}`,
      );
    } catch (err) {
      onToast(`导出失败：${errorText(err)}`, "error");
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="share-layer" role="presentation">
      <button className="share-backdrop" aria-label="关闭" onClick={onClose} />
      <section className="share-dialog export-dialog" role="dialog" aria-modal="true" aria-label="导出公开页">
        <header className="share-head">
          <div>
            <h2>公开页</h2>
            <p>
              {book ? `《${book.title}》` : "这本书"}
              {book?.author ? ` · ${book.author}` : ""}
              。一个文件，没有外链，也不会自己去请求别的服务器。你可以之后放到任何地方。
            </p>
          </div>
          <button className="icon-button" title="关闭" onClick={onClose}>
            <X size={17} />
          </button>
        </header>
        <div className="export-toolbar">
          <button onClick={selectQuotes} disabled={items.length === 0 || allQuotes}>
            全选划线
          </button>
          <button onClick={selectNotesToo} disabled={items.length === 0}>
            划线和笔记都选上
          </button>
          <button
            onClick={() => {
              setQuoteIds(new Set());
              setNoteIds(new Set());
            }}
            disabled={quoteIds.size === 0 && noteIds.size === 0}
          >
            清空
          </button>
          <span className="export-count">
            已选 {quoteIds.size} 条划线
            {noteIds.size > 0 ? `，${noteIds.size} 条笔记` : ""}
          </span>
        </div>
        <div className="export-list">
          {loading ? <p className="share-quiet">正在读取划线…</p> : null}
          {error ? <div className="settings-notice is-error">{error}</div> : null}
          {!loading && !error && items.length === 0 ? (
            <p className="share-quiet">这本书还没有划线或摘录。先在书里划一段，再回到这里选。</p>
          ) : null}
          {items.map((item) => {
            const quoteOn = quoteIds.has(item.id);
            const noteOn = noteIds.has(item.id);
            return (
              <article key={item.id} className={`export-item ${quoteOn ? "is-on" : ""}`}>
                <label className="export-quote">
                  <input
                    type="checkbox"
                    checked={quoteOn}
                    onChange={() => toggleQuote(item.id)}
                  />
                  <span>
                    <span className="export-quote-text">{item.quote}</span>
                    {item.chapter ? <span className="export-chapter">{item.chapter}</span> : null}
                  </span>
                </label>
                {item.note.trim() ? (
                  <label className={`export-note ${quoteOn ? "" : "is-locked"}`}>
                    <input
                      type="checkbox"
                      checked={noteOn}
                      disabled={!quoteOn}
                      onChange={() => toggleNote(item.id)}
                    />
                    <span>
                      带上这条笔记
                      <small>{item.note}</small>
                    </span>
                  </label>
                ) : null}
              </article>
            );
          })}
        </div>
        <footer className="export-foot">
          <p>笔记默认不带。只有勾过的才会写进文件。</p>
          <div className="share-actions">
            <button
              className="primary-button"
              disabled={quoteIds.size === 0 || busy !== null}
              onClick={() => void exportAs("html")}
            >
              <FileOutput size={15} />
              {busy === "html" ? "正在导出…" : "导出 HTML"}
            </button>
            <button
              className="pill-button"
              disabled={quoteIds.size === 0 || busy !== null}
              onClick={() => void exportAs("md")}
            >
              {busy === "md" ? "正在导出…" : "导出 Markdown"}
            </button>
          </div>
        </footer>
      </section>
    </div>
  );
}
