import { useEffect, useMemo, useRef, useState } from "react";
import { Search as SearchIcon, X } from "lucide-react";
import { Search } from "../../mygo";
import type { RevealTarget } from "../../lib/reveal";
import type { SearchHit } from "../../lib/search";
import Snippet from "../Snippet";

interface Props {
  bookId: string;
  open: boolean;
  onClose: () => void;
  onReveal: (target: RevealTarget) => void;
}

/** 书内搜索浮层：书卡和全屏阅读共用。 */
export default function FindPopover({ bookId, open, onClose, onReveal }: Props) {
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const requestRef = useRef(0);

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => inputRef.current?.focus(), 60);
    return () => window.clearTimeout(timer);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const q = query.trim();
    if (!q) {
      requestRef.current += 1;
      setHits([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    const request = (requestRef.current += 1);
    const timer = window.setTimeout(() => {
      void Search.queryBook(bookId, q)
        .then((list) => {
          if (requestRef.current !== request) return;
          setHits(list);
        })
        .catch(() => {
          if (requestRef.current !== request) return;
          setHits([]);
        })
        .finally(() => {
          if (requestRef.current === request) setLoading(false);
        });
    }, 240);
    return () => window.clearTimeout(timer);
  }, [bookId, open, query]);

  const results = useMemo(() => {
    const counters = new Map<number, number>();
    return hits.map((hit) => {
      const key = hit.location.spineIndex;
      const occurrence = counters.get(key) ?? 0;
      counters.set(key, occurrence + 1);
      return { hit, occurrence };
    });
  }, [hits]);

  if (!open) return null;

  return (
    <div className="toc-popover find-popover">
      <div className="find-head">
        <SearchIcon size={14} />
        <input
          ref={inputRef}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="在这本书里搜索"
          spellCheck={false}
        />
        <button className="ghost-icon" onClick={onClose} title="关闭">
          <X size={14} />
        </button>
      </div>
      <div className="toc-list find-list">
        {!query.trim() ? (
          <div className="find-empty">输入关键词，在当前书里查找。</div>
        ) : loading && results.length === 0 ? (
          <div className="find-empty">正在搜索…</div>
        ) : results.length === 0 ? (
          <div className="find-empty">没有找到「{query.trim()}」。</div>
        ) : (
          results.map(({ hit, occurrence }, i) => (
            <button
              key={`${hit.location.spineIndex}-${i}`}
              className="find-hit"
              onClick={() => {
                onClose();
                onReveal({
                  bookId,
                  href: hit.location.href,
                  spineIndex: hit.location.spineIndex,
                  query: query.trim(),
                  occurrence,
                });
              }}
            >
              <span className="find-hit-meta">
                {hit.location.pageLabel || `第 ${hit.location.spineIndex + 1} 节`}
              </span>
              <span className="find-hit-text">
                <Snippet text={hit.snippet} ranges={hit.highlights} />
              </span>
            </button>
          ))
        )}
      </div>
    </div>
  );
}
