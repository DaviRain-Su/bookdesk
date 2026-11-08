import { useEffect, useState } from "react";
import { Languages, Loader2, X } from "lucide-react";
import { Ai } from "../mygo";

export interface LookupRequest {
  word: string;
  sentence: string;
  bookTitle: string;
}

interface Props {
  request: LookupRequest | null;
  onClose: () => void;
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

export default function LookupPanel({ request, onClose }: Props) {
  const [loading, setLoading] = useState(false);
  const [text, setText] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!request) return;
    let cancelled = false;
    setLoading(true);
    setText("");
    setError("");
    void Ai.lookupWord({
      word: request.word,
      sentence: request.sentence || null,
      bookTitle: request.bookTitle || null,
      turnId: crypto.randomUUID(),
    })
      .then((reply) => {
        if (cancelled) return;
        if (reply.ok) setText(reply.text);
        else setError(reply.message);
      })
      .catch((err) => {
        if (!cancelled) setError(`查词失败：${errorText(err)}`);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [request]);

  if (!request) return null;

  return (
    <div className="lookup-panel">
      <header className="lookup-head">
        <div className="lookup-title">
          <Languages size={14} />
          <span>{request.word}</span>
        </div>
        <button className="ghost-icon" onClick={onClose} title="关闭">
          <X size={14} />
        </button>
      </header>
      <div className="lookup-body">
        {loading ? (
          <div className="lookup-loading">
            <Loader2 size={15} className="spin" />
            <span>正在查…</span>
          </div>
        ) : error ? (
          <p className="lookup-error">{error}</p>
        ) : (
          <p className="lookup-text">{text}</p>
        )}
      </div>
      {request.sentence ? <p className="lookup-sentence">{request.sentence}</p> : null}
    </div>
  );
}
