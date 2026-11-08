import {
  Hand,
  Image as ImageIcon,
  Minus,
  MousePointer2,
  Plus,
  Spline,
  Type,
} from "lucide-react";

export type Tool = "select" | "pan" | "connect" | "text" | "image" | "divider";

interface Props {
  tool: Tool;
  onSelect: (tool: Tool) => void;
  onAddTextCard: () => void;
  onAddImageCard: () => void;
}

const tools: { id: Tool; label: string; icon: typeof Hand }[] = [
  { id: "select", label: "选择 / 拖动", icon: MousePointer2 },
  { id: "pan", label: "平移画布", icon: Hand },
  { id: "connect", label: "连线", icon: Spline },
  { id: "text", label: "文本卡片", icon: Type },
  { id: "image", label: "图片卡片", icon: ImageIcon },
  { id: "divider", label: "分隔线", icon: Minus },
];

export default function Toolbar({ tool, onSelect, onAddTextCard, onAddImageCard }: Props) {
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
      <button className="tool" title="更多">
        <Plus size={18} strokeWidth={1.8} />
      </button>
    </nav>
  );
}
