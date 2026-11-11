import { useEffect, useState } from "react";
import { Copy, ImageDown, X } from "lucide-react";
import { Notes, Share, type ShareCardInput } from "../mygo";
import { normalizeQuote, type ShareCardRequest } from "../lib/share";

interface Props {
  request: ShareCardRequest | null;
  onClose: () => void;
  onDone: (message: string) => void;
  onError: (message: string) => void;
}

const TEMPLATES = [
  { id: "light", label: "浅色" },
  { id: "dark", label: "夜间" },
  { id: "vertical", label: "竖排" },
] as const;

const RATIOS = [
  { id: "square", label: "方形 1:1" },
  { id: "portrait", label: "竖图 3:4" },
] as const;

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

function fileName(path: string): string {
  const parts = path.split(/[/\\]/);
  return parts[parts.length - 1] || path;
}

export default function ShareCardDialog({ request, onClose, onDone, onError }: Props) {
  const [template, setTemplate] = useState<(typeof TEMPLATES)[number]["id"]>("light");
  const [ratio, setRatio] = useState<(typeof RATIOS)[number]["id"]>("square");
  const [note, setNote] = useState("");
  const [includeNote, setIncludeNote] = useState(false);
  const [preview, setPreview] = useState("");
  const [rendering, setRendering] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!request) return;
    setTemplate("light");
    setRatio("square");
    setIncludeNote(false);
    setPreview("");
    setError("");
    if (request.note != null) {
      setNote(request.note.trim());
      return;
    }
    let cancelled = false;
    setNote("");
    const bookId = request.bookId;
    const quote = normalizeQuote(request.quote);
    void Notes.listCards()
      .then((cards) => {
        if (cancelled) return;
        const notes: string[] = [];
        for (const card of cards) {
          const sameBook =
            card.sourceBookId === bookId || card.quoteLocation?.bookId === bookId;
          if (!sameBook || normalizeQuote(card.quote ?? "") !== quote) continue;
          const body = (card.body ?? "").trim();
          if (body && !notes.includes(body)) notes.push(body);
        }
        setNote(notes.join("\n\n"));
      })
      .catch(() => {
        if (!cancelled) setNote("");
      });
    return () => {
      cancelled = true;
    };
  }, [request]);

  useEffect(() => {
    if (!request) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      onClose();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [request, onClose]);

  useEffect(() => {
    if (!request) return;
    let cancelled = false;
    setRendering(true);
    setError("");
    const timer = window.setTimeout(() => {
      const input: ShareCardInput = {
        quote: request.quote,
        note: includeNote ? note : "",
        includeNote: includeNote && note.trim() !== "",
        title: request.bookTitle || "未命名书籍",
        author: request.author || "",
        chapter: request.chapter || "",
        color: request.color || "yellow",
        template,
        ratio,
      };
      void Share.renderCard(input)
        .then((image) => {
          if (cancelled) return;
          setPreview(`data:image/png;base64,${image.pngBase64}`);
        })
        .catch((err) => {
          if (!cancelled) setError(errorText(err));
        })
        .finally(() => {
          if (!cancelled) setRendering(false);
        });
    }, 80);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [request, template, ratio, includeNote, note]);

  if (!request) return null;

  const input = (): ShareCardInput => ({
    quote: request.quote,
    note: includeNote ? note : "",
    includeNote: includeNote && note.trim() !== "",
    title: request.bookTitle || "未命名书籍",
    author: request.author || "",
    chapter: request.chapter || "",
    color: request.color || "yellow",
    template,
    ratio,
  });

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const result = await Share.saveCardPNG(input());
      if (!result.cancelled && result.path) {
        onDone(`已保存到「${fileName(result.path)}」`);
        onClose();
      }
    } catch (err) {
      const message = `保存失败：${errorText(err)}`;
      setError(message);
      onError(message);
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    setBusy(true);
    setError("");
    try {
      await Share.copyCardPNG(input());
      onDone("已复制图片");
    } catch (err) {
      const message = `复制失败：${errorText(err)}`;
      setError(message);
      onError(message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="share-layer">
      <div className="share-backdrop" onClick={onClose} />
      <section className="share-panel" role="dialog" aria-label="分享划线">
        <header className="share-head">
          <div>
            <h2>分享这张卡片</h2>
            <p>图片只在你保存或复制时离开书桌，不会上传。</p>
          </div>
          <button className="icon-button" onClick={onClose} title="关闭">
            <X size={17} />
          </button>
        </header>
        <div className="share-body">
          <div className="share-preview">
            {preview ? <img src={preview} alt="分享卡预览" /> : null}
            {rendering ? <span className="share-preview-status">正在排版…</span> : null}
          </div>
          <div className="share-controls">
            <div className="share-field">
              <span>模板</span>
              <div className="share-choices">
                {TEMPLATES.map((item) => (
                  <button
                    key={item.id}
                    className={`share-choice ${template === item.id ? "is-active" : ""}`}
                    onClick={() => setTemplate(item.id)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
            <div className="share-field">
              <span>比例</span>
              <div className="share-choices">
                {RATIOS.map((item) => (
                  <button
                    key={item.id}
                    className={`share-choice ${ratio === item.id ? "is-active" : ""}`}
                    onClick={() => setRatio(item.id)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
            <label className={`share-note ${note.trim() ? "" : "is-disabled"}`}>
              <input
                type="checkbox"
                checked={includeNote && note.trim() !== ""}
                disabled={!note.trim() || busy}
                onChange={(event) => setIncludeNote(event.target.checked)}
              />
              <span>
                带上我的笔记
                <small>
                  {note.trim()
                    ? "默认不放进图片。勾选后才会出现。"
                    : "这条划线还没有笔记。"}
                </small>
              </span>
            </label>
            {note.trim() && includeNote ? <p className="share-note-preview">{note}</p> : null}
            {error ? <p className="share-error">{error}</p> : null}
            <div className="share-actions">
              <button className="pill-button" disabled={busy || rendering} onClick={() => void copy()}>
                <Copy size={15} />
                复制图片
              </button>
              <button className="primary-button" disabled={busy || rendering} onClick={() => void save()}>
                <ImageDown size={15} />
                保存 PNG
              </button>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
