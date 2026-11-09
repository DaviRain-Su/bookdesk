import { useContext, useState } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { MapPin, Quote, Share2, X } from "lucide-react";
import { ActiveBookContext } from "../ActiveBookContext";
import { chapterFromSource, SHARE_CARD_EVENT } from "../../lib/share";
import type { BookLocation } from "../../types";

export interface TextNodeData {
  cardId?: string;
  title?: string;
  body?: string;
  quote?: string;
  source?: string;
  sourceBookId?: string | null;
  quoteLocation?: BookLocation | null;
  tone?: "default" | "muted" | "other-book";
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

export default function TextNode({ id, data, selected }: NodeProps) {
  const d = data as TextNodeData;
  const activeBookId = useContext(ActiveBookContext);
  const otherBook =
    !!activeBookId && !!d.sourceBookId && d.sourceBookId !== activeBookId;
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");

  const startEdit = () => {
    setTitle(d.title ?? "");
    setBody(d.body ?? "");
    setEditing(true);
  };

  const commit = () => {
    setEditing(false);
    updateNode(id, { title: title.trim(), body: body.trim() });
  };

  return (
    <article
      className={`card text-card ${d.tone === "muted" ? "is-muted" : ""} ${otherBook ? "is-other-book" : ""} ${selected ? "is-selected" : ""}`}
      onDoubleClick={() => {
        if (!editing) startEdit();
      }}
    >
      <Handle type="target" position={Position.Left} className="node-handle" />
      <button
        className="node-delete"
        title="删除卡片"
        onClick={(event) => {
          event.stopPropagation();
          deleteNode(id);
        }}
      >
        <X size={13} />
      </button>

      {d.quote ? (
        <div className="quote-block">
          <Quote size={13} className="quote-icon" />
          <p>{d.quote}</p>
          <div className="quote-foot">
            {d.source ? <span className="quote-source">{d.source}</span> : <span />}
            <span className="quote-actions">
            {d.quote ? (
              <button
                className="quote-locate nodrag"
                title="做成图片分享"
                onClick={(event) => {
                  event.stopPropagation();
                  const chapter =
                    d.quoteLocation?.pageLabel || chapterFromSource(d.source) || null;
                  const sourceTitle = d.source?.split(" · ")[0]?.trim();
                  window.dispatchEvent(
                    new CustomEvent(SHARE_CARD_EVENT, {
                      detail: {
                        bookId: d.sourceBookId ?? "",
                        bookTitle: sourceTitle || undefined,
                        chapter,
                        quote: d.quote,
                        note: (d.body ?? "").trim(),
                        color: "yellow",
                      },
                    }),
                  );
                }}
              >
                <Share2 size={11} />
                分享
              </button>
            ) : null}
            {d.quoteLocation && d.sourceBookId ? (
              <button
                className="quote-locate nodrag"
                title="回到书里"
                onClick={(event) => {
                  event.stopPropagation();
                  window.dispatchEvent(
                    new CustomEvent("bookdesk:open-quote-location", {
                      detail: {
                        bookId: d.sourceBookId,
                        quote: d.quote ?? "",
                        location: d.quoteLocation,
                      },
                    }),
                  );
                }}
              >
                <MapPin size={11} />
                回到书里
              </button>
            ) : null}
            </span>
          </div>
        </div>
      ) : null}

      {editing ? (
        <div className="card-edit nodrag" onDoubleClick={(event) => event.stopPropagation()}>
          <input
            className="card-edit-title"
            value={title}
            placeholder="标题"
            autoFocus
            onChange={(event) => setTitle(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") commit();
              if (event.key === "Escape") setEditing(false);
            }}
          />
          <textarea
            className="card-edit-body"
            value={body}
            placeholder="写下你自己的话…"
            rows={4}
            onChange={(event) => setBody(event.target.value)}
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
        <>
          <h3 className="card-title">{d.title || "未命名卡片"}</h3>
          <div className="card-body">
            {d.body ? <p>{d.body}</p> : <p className="placeholder-text">写下你自己的话…</p>}
          </div>
        </>
      )}
      <Handle type="source" position={Position.Right} className="node-handle" />
    </article>
  );
}
