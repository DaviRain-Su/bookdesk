import { useEffect, useRef, useState } from "react";
import { Check, Copy, Download, X } from "lucide-react";
import { Notes, Share } from "../mygo";
import { errorText, saveLocalFile } from "../lib/localFile";
import {
  CARD_RATIOS,
  CARD_TEMPLATES,
  findCardNote,
  layoutShareCard,
  type CardRatio,
  type CardTemplate,
} from "../lib/shareCard";
import { blobToBase64, canvasForLayout, canvasToBlob, drawShareCard } from "../lib/shareCardCanvas";

export interface ShareRequest {
  bookId?: string | null;
  quote: string;
  title?: string | null;
  author?: string | null;
  chapter?: string | null;
  /** 卡片上已经写好的笔记。noteProvided 为真时不再去别处找。 */
  note?: string | null;
  noteProvided?: boolean;
  color?: string | null;
}

interface BookRef {
  id: string;
  title: string;
  author: string | null;
}

interface Props {
  request: ShareRequest | null;
  books: BookRef[];
  onClose: () => void;
  onToast: (message: string, kind?: "info" | "error") => void;
}

function fileStem(title: string): string {
  const cleaned = title.replace(/[\\/:*?"<>|]/g, "").trim().slice(0, 40);
  return cleaned || "划线";
}

export default function ShareCardDialog({ request, books, onClose, onToast }: Props) {
  const [template, setTemplate] = useState<CardTemplate>("light");
  const [ratio, setRatio] = useState<CardRatio>("square");
  const [includeNote, setIncludeNote] = useState(false);
  const [availableNote, setAvailableNote] = useState("");
  const [busy, setBusy] = useState<"save" | "copy" | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    if (!request) return;
    setIncludeNote(false);
    setTemplate("light");
    setRatio("square");
    setBusy(null);
    if (request.noteProvided) {
      setAvailableNote((request.note ?? "").trim());
      return;
    }
    setAvailableNote("");
    if (!request.bookId) return;
    let cancelled = false;
    void Notes.listCards()
      .then((cards) => {
        if (cancelled) return;
        setAvailableNote(findCardNote(cards, request.bookId ?? "", request.quote) ?? "");
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [request]);

  useEffect(() => {
    if (!request) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [request, onClose]);

  const book = books.find((item) => item.id === request?.bookId);
  const title = (request?.title ?? "").trim() || book?.title || "未命名书籍";
  const author =
    request?.author !== undefined && request.author !== null
      ? request.author
      : (book?.author ?? null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !request) return;
    const layout = layoutShareCard({
      quote: request.quote,
      note: includeNote ? availableNote : null,
      title,
      author,
      chapter: request.chapter,
      template,
      ratio,
      color: request.color,
    });
    const scale = 2;
    canvas.width = layout.width * scale;
    canvas.height = layout.height * scale;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    drawShareCard(ctx, layout, scale);
  }, [request, includeNote, availableNote, title, author, template, ratio]);

  if (!request) return null;

  const paint = () => {
    const layout = layoutShareCard({
      quote: request.quote,
      note: includeNote ? availableNote : null,
      title,
      author,
      chapter: request.chapter,
      template,
      ratio,
      color: request.color,
    });
    return canvasForLayout(layout, 2);
  };

  const save = async () => {
    setBusy("save");
    try {
      const blob = await canvasToBlob(paint());
      const name = `${fileStem(title)}-划线.png`;
      const outcome = await saveLocalFile(() => blobToBase64(blob).then((data) => Share.savePNG(name, data)), blob, name);
      if (outcome === "cancelled") return;
      onToast(outcome === "saved" ? "卡片已保存到本机" : "已开始下载卡片，文件只在本机");
    } catch (err) {
      onToast(`保存卡片失败：${errorText(err)}`, "error");
    } finally {
      setBusy(null);
    }
  };

  const copy = async () => {
    setBusy("copy");
    try {
      const blob = await canvasToBlob(paint());
      const data = await blobToBase64(blob);
      try {
        await Share.copyPNG(data);
      } catch {
        if (!navigator.clipboard || typeof ClipboardItem === "undefined") {
          throw new Error("这个环境不能把图片放进剪贴板，可以改用保存 PNG");
        }
        await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
      }
      onToast("图片已复制，还没有发出去");
    } catch (err) {
      onToast(errorText(err), "error");
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="share-layer" role="presentation">
      <button className="share-backdrop" aria-label="关闭" onClick={onClose} />
      <section className="share-dialog" role="dialog" aria-modal="true" aria-label="分享卡片">
        <header className="share-head">
          <div>
            <h2>分享卡片</h2>
            <p>图片在本机画好。保存或复制之前，不会离开这台电脑。</p>
          </div>
          <button className="icon-button" title="关闭" onClick={onClose}>
            <X size={17} />
          </button>
        </header>
        <div className="share-body">
          <div className={`share-preview is-${template}`}>
            <canvas ref={canvasRef} />
          </div>
          <div className="share-controls">
            <div className="share-field">
              <span>模板</span>
              <div className="share-choices">
                {CARD_TEMPLATES.map((item) => (
                  <button
                    key={item.id}
                    className={template === item.id ? "is-active" : ""}
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
                {CARD_RATIOS.map((item) => (
                  <button
                    key={item.id}
                    className={ratio === item.id ? "is-active" : ""}
                    onClick={() => setRatio(item.id)}
                    title={item.hint}
                  >
                    {item.label}
                    <small>{item.hint}</small>
                  </button>
                ))}
              </div>
            </div>
            {availableNote ? (
              <label className="share-note-toggle">
                <input
                  type="checkbox"
                  checked={includeNote}
                  onChange={(event) => setIncludeNote(event.target.checked)}
                />
                <span>
                  带上我的笔记
                  <small>默认不带。勾上之后才会出现在图片里。</small>
                </span>
              </label>
            ) : (
              <p className="share-quiet">这条没有笔记。</p>
            )}
            {availableNote ? <p className="share-note-preview">{availableNote}</p> : null}
            <div className="share-actions">
              <button className="primary-button" onClick={() => void save()} disabled={busy !== null}>
                {busy === "save" ? <Check size={15} /> : <Download size={15} />}
                保存 PNG
              </button>
              <button className="pill-button" onClick={() => void copy()} disabled={busy !== null}>
                <Copy size={15} />
                复制图片
              </button>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
