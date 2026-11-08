import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Image as ImageIcon, X } from "lucide-react";

export interface ImageNodeData {
  cardId?: string;
  src?: string;
  caption?: string;
  imageRelPath?: string;
  [key: string]: unknown;
}

export default function ImageNode({ id, data, selected }: NodeProps) {
  const d = data as ImageNodeData;
  return (
    <figure className={`card image-card ${selected ? "is-selected" : ""}`}>
      <Handle type="target" position={Position.Left} className="node-handle" />
      <button
        className="node-delete"
        title="删除图片"
        onClick={(event) => {
          event.stopPropagation();
          window.dispatchEvent(
            new CustomEvent("bookdesk:delete-node", { detail: { nodeId: id } }),
          );
        }}
      >
        <X size={13} />
      </button>
      {d.src ? (
        <img src={d.src} alt={d.caption ?? ""} draggable={false} />
      ) : (
        <div className="image-placeholder">
          <ImageIcon size={22} strokeWidth={1.5} />
          <span>图片卡片</span>
        </div>
      )}
      {d.caption ? <figcaption>{d.caption}</figcaption> : null}
      <Handle type="source" position={Position.Right} className="node-handle" />
    </figure>
  );
}
