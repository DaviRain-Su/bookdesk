import { useState } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { MessageCircle, X } from "lucide-react";
import { Library } from "../../mygo";

export interface CharacterNodeData {
  characterId?: string | null;
  bookId?: string;
  name?: string;
  description?: string | null;
  imageRelPath?: string | null;
  [key: string]: unknown;
}

function updateNode(nodeId: string, patch: Record<string, unknown>) {
  window.dispatchEvent(
    new CustomEvent("bookdesk:update-node", { detail: { nodeId, patch } }),
  );
}

function deleteNode(nodeId: string) {
  window.dispatchEvent(new CustomEvent("bookdesk:delete-node", { detail: { nodeId } }));
}

function openChat(data: CharacterNodeData) {
  window.dispatchEvent(
    new CustomEvent("bookdesk:open-chat", {
      detail: {
        characterId: typeof data.characterId === "string" ? data.characterId : null,
        bookId: data.bookId ?? "",
        name: data.name ?? "",
        description: typeof data.description === "string" ? data.description : "",
      },
    }),
  );
}

export default function CharacterNode({ id, data, selected }: NodeProps) {
  const d = data as CharacterNodeData;
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const startEdit = () => {
    setName(d.name ?? "");
    setDescription(typeof d.description === "string" ? d.description : "");
    setEditing(true);
  };

  const commit = () => {
    setEditing(false);
    const nextName = name.trim();
    updateNode(id, {
      name: nextName || d.name || "未命名人物",
      description: description.trim(),
    });
  };

  const imageName = typeof d.imageRelPath === "string" ? d.imageRelPath.split("/").pop() : "";
  const initial = (d.name ?? "").trim().slice(0, 1) || "人";

  return (
    <article
      className={`card character-card ${selected ? "is-selected" : ""}`}
      onDoubleClick={() => {
        if (!editing) startEdit();
      }}
    >
      <Handle type="target" position={Position.Left} className="node-handle" />
      <button
        className="node-delete"
        title="删除人物卡"
        onClick={(event) => {
          event.stopPropagation();
          deleteNode(id);
        }}
      >
        <X size={13} />
      </button>

      <div className="character-head">
        <button
          className="character-avatar-button nodrag"
          title="选择头像图片"
          onClick={(event) => {
            event.stopPropagation();
            void Library.pickImage()
              .then((relPath) => {
                if (relPath) updateNode(id, { imageRelPath: relPath });
              })
              .catch(() => {});
          }}
        >
          {imageName ? (
            <img
              className="character-avatar"
              src={`books://localhost/image/${imageName}`}
              alt={d.name ?? "人物"}
              draggable={false}
            />
          ) : (
            <span className="character-avatar is-letter">{initial}</span>
          )}
        </button>
        {editing ? (
          <input
            className="character-name-input nodrag"
            value={name}
            placeholder="人物名字"
            autoFocus
            onChange={(event) => setName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") commit();
              if (event.key === "Escape") setEditing(false);
            }}
          />
        ) : (
          <div className="character-heading">
            <span className="character-name">{d.name || "未命名人物"}</span>
            <span className="character-kind">人物</span>
          </div>
        )}
      </div>

      {editing ? (
        <div className="nodrag" onDoubleClick={(event) => event.stopPropagation()}>
          <textarea
            className="character-desc-input"
            value={description}
            placeholder="这个人是谁？写几句你自己的理解…"
            rows={3}
            onChange={(event) => setDescription(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") setEditing(false);
              if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) commit();
            }}
          />
          <div className="card-edit-actions">
            <button className="text-button" onClick={() => setEditing(false)}>
              取消
            </button>
            <button className="small-primary" onClick={commit}>
              保存
            </button>
          </div>
        </div>
      ) : (
        <p className={`character-desc ${d.description ? "" : "is-placeholder"}`}>
          {d.description || "双击写下这个人物是谁。"}
        </p>
      )}

      <div className="character-actions">
        <button
          className="small-primary character-chat-button"
          onClick={(event) => {
            event.stopPropagation();
            openChat(d);
          }}
        >
          <MessageCircle size={14} />
          和 {d.name || "这个人物"} 对话
        </button>
      </div>
      <Handle type="source" position={Position.Right} className="node-handle" />
    </article>
  );
}
