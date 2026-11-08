import { useEffect, useMemo, useRef, useState } from "react";
import { Search as SearchIcon, X } from "lucide-react";
import { Search } from "../mygo";
import { coverSrc, type LibraryBook } from "./LibraryDrawer";
import Snippet from "./Snippet";
import type { SearchHit } from "../lib/search";

interface Props {
  open: boolean;
  books: LibraryBook[];
  onClose: () => void;
  onOpenHit: (hit: SearchHit, occurrence: number, query: string) => void;
}

export default function SearchPanel({ open, books, onClose, onOpenHit }: Props) {
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);
  const requestRef = useRef(0);

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => inputRef.current?.focus(), 200);
    return () => window.clearTimeout(timer);
  }, [open]);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      requestRef.current += 1;
      setHits([]);
      setLoading(false);
      setError("");
      return;
    }
    setLoading(true);
    const id = (requestRef.current += 1);
    const timer = window.setTimeout(() => {
      void Search.query(q)
        .then((list) => {
          if (requestRef.current !== id) return;
          setHits(list);
          setError("");
        })
        .catch((err: unknown) => {
          if (requestRef.current !== id) return;
          setHits([]);
          setError(err instanceof Error ? err.message : String(err));
        })
        .finally(() => {
          if (requestRef.current === id) setLoading(false);
        });
    }, 240);
    return () => window.clearTimeout(timer);
  }, [query]);

  const groups = useMemo(() => {
    const map = new Map<
      string,
      { bookId: string; title: string; hits: { hit: SearchHit; occurrence: number }[] }
    >();
    const counters = new Map<string, number>();
    for (const hit of hits) {
      const key = `${hit.bookId}:${hit.location.spineIndex}`;
      const occurrence = counters.get(key) ?? 0;
      counters.set(key, occurrence + 1);
      let group = map.get(hit.bookId);
      if (!group) {
        group = { bookId: hit.bookId, title: hit.title, hits: [] };
        map.set(hit.bookId, group);
      }
      group.hits.push({ hit, occurrence });
    }
    return [...map.values()];
  }, [hits]);

  const trimmed = query.trim();

  return (
    <>
      <div className={`drawer-backdrop ${open ? "is-open" : ""}`} onClick={onClose} aria-hidden />
      <aside className={`drawer search-drawer ${open ? "is-open" : ""}`} aria-hidden={!open}>
        <header className="drawer-head">
          <div>
            <h2>跨书搜索</h2>
            <span className="drawer-sub">{books.length} 本书 · 只在本地搜索</span>
          </div>
          <button className="icon-button" onClick={onClose} title="关闭">
            <X size={17} />
          </button>
        </header>

        <div className="search-box">
          <SearchIcon size={15} />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索所有书里的词句"
            spellCheck={false}
          />
          {query ? (
            <button className="search-clear" onClick={() => setQuery("")} title="清空">
              ×
            </button>
          ) : null}
        </div>

        <div className="drawer-body search-body">
          {!trimmed ? (
            <div className="drawer-empty">
              <p>输入关键词，搜索书架里的所有书。</p>
              <p className="drawer-empty-hint">结果按书分组，点击可跳到原文。</p>
            </div>
          ) : loading && hits.length === 0 ? (
            <div className="drawer-empty">
              <p>正在搜索…</p>
            </div>
          ) : error ? (
            <div className="drawer-empty">
              <p>搜索失败：{error}</p>
            </div>
          ) : hits.length === 0 ? (
            <div className="drawer-empty">
              <p>没有找到「{trimmed}」。</p>
            </div>
          ) : (
            groups.map((group) => {
              const book = books.find((b) => b.id === group.bookId);
              const src = book ? coverSrc(book) : null;
              return (
                <section key={group.bookId} className="search-group">
                  <header className="search-group-head">
                    <div className="search-cover">
                      {src ? (
                        <img src={src} alt="" draggable={false} />
                      ) : (
                        <span>{group.title.slice(0, 1)}</span>
                      )}
                    </div>
                    <span className="search-group-title">{group.title}</span>
                    <span className="search-group-count">{group.hits.length} 处</span>
                  </header>
                  {group.hits.map(({ hit, occurrence }, i) => (
                    <button
                      key={`${hit.bookId}-${hit.location.spineIndex}-${i}`}
                      className="search-hit"
                      onClick={() => onOpenHit(hit, occurrence, trimmed)}
                    >
                      <span className="search-hit-meta">第 {hit.location.spineIndex + 1} 节</span>
                      <span className="search-hit-text">
                        <Snippet text={hit.snippet} ranges={hit.highlights} />
                      </span>
                    </button>
                  ))}
                </section>
              );
            })
          )}
        </div>

        <footer className="drawer-foot">只搜索你导入的书，不会联网，也不会上传任何内容。</footer>
      </aside>
    </>
  );
}
