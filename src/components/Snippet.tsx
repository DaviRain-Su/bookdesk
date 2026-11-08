import type { ReactNode } from "react";

export default function Snippet({ text, ranges }: { text: string; ranges: number[][] }) {
  const parts: ReactNode[] = [];
  let cursor = 0;
  ranges.forEach((range, i) => {
    const start = Math.max(0, Math.min(text.length, range[0] ?? 0));
    const end = Math.max(start, Math.min(text.length, range[1] ?? start));
    if (start < cursor) return;
    if (start > cursor) parts.push(text.slice(cursor, start));
    parts.push(<mark key={i}>{text.slice(start, end)}</mark>);
    cursor = end;
  });
  if (cursor < text.length) parts.push(text.slice(cursor));
  return <>{parts}</>;
}
