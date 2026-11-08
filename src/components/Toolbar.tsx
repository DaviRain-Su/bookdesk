import { useEffect, useRef, useState } from "react";
import {
  BookPlus,
  Hand,
  Image as ImageIcon,
  Library,
  Maximize2,
  Minus,
  MousePointer2,
  Plus,
  Spline,
  SquarePlus,
  Type,
} from "lucide-react";

export type Tool = "select" | "pan" | "connect" | "text" | "image" | "divider";

interface Props {
  tool: Tool;
  onSelect: (tool: Tool) => void;
  onAddTextCard: () => void;
  onAddImageCard: () => void;
  onAddBook: () => void;
  onOpenLibrary: () => void;
  onFitView: () => void;
  onCreateBoard: () => void;
}

const tools: { id: Tool; label: string; icon: typeof Hand }[] = [
  { id: "select", label: "选择 / 拖动", icon: MousePointer2 },
  { id: "pan", label: "平移画布", icon: Hand },
  { id: "connect", label: "连线", icon: Spline },
  { id: "text", label: "文本卡片", icon: Type },
  { id: "image", label: "图片卡片", icon: ImageIcon },
  { id: "divider", label: "分隔线", icon: Minus },
];

export default function Toolbar({
  tool,
  onSelect,
  onAddTextCard,
  onAddImageCard,
  onAddBook,
  onOpenLibrary,
  onFitView,
  onCreateBoard,
}: Props) {
  const [moreOpen, setMoreOpen] = useState(false);
  const moreRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!moreOpen) return;
    const onDown = (event: MouseEvent) => {
      if (!moreRef.current?.contains(event.target as Node)) setMoreOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMoreOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [moreOpen]);

  const run = (action: () => void) => {
    setMoreOpen(false);
    action();
  };

  return (
    <nav className="toolbar" aria-label="画布工具">
      {tools.map(({ id, label, icon: Icon }) => (
        <button
          key={id}
          className={`tool ${tool === id ? "is-active" : ""}`}
          title={label}
          onClick={() => {
            onSelect(id);
            if (id === "text") onAddTextCard();
            if (id === "image") onAddImageCard();
          }}
        >
          <Icon size={18} strokeWidth={1.8} />
        </button>
      ))}
      <div className="tool-sep" />
      <div className="tool-more" ref={moreRef}>
        <button
          className={`tool ${moreOpen ? "is-active" : ""}`}
          title="更多"
          aria-expanded={moreOpen}
          onClick={() => setMoreOpen((v) => !v)}
        >
          <Plus size={18} strokeWidth={1.8} />
        </button>
        {moreOpen ? (
          <div className="tool-more-menu" role="menu">
            <button role="menuitem" onClick={() => run(onAddBook)}>
              <BookPlus size={15} />
              添加书籍…
            </button>
            <button role="menuitem" onClick={() => run(onOpenLibrary)}>
              <Library size={15} />
              打开书架
            </button>
            <button role="menuitem" onClick={() => run(onFitView)}>
              <Maximize2 size={15} />
              适应窗口
            </button>
            <button role="menuitem" onClick={() => run(onCreateBoard)}>
              <SquarePlus size={15} />
              新建画布
            </button>
          </div>
        ) : null}
      </div>
    </nav>
  );
}
