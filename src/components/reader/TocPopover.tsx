import type { TocEntry } from "./useEpubReader";

interface Props {
  toc: TocEntry[];
  onJump: (href: string) => void;
}

/** 目录浮层：书卡和全屏阅读共用。 */
export default function TocPopover({ toc, onJump }: Props) {
  if (toc.length === 0) return null;
  return (
    <div className="toc-popover">
      <div className="toc-head">目录</div>
      <div className="toc-list">
        {toc.map((entry, i) => (
          <button
            key={`${entry.href}-${i}`}
            className="toc-item"
            style={{ paddingLeft: 12 + entry.depth * 14 }}
            onClick={() => onJump(entry.href)}
          >
            {entry.label}
          </button>
        ))}
      </div>
    </div>
  );
}
