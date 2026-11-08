import { useEffect, useRef, useState } from "react";
import { Eraser, MessageCircle, Send, Square, X } from "lucide-react";
import { Ai, events } from "../mygo";
import { readReadingContext } from "../lib/reading";

export interface ChatTarget {
  characterId: string | null;
  bookId: string;
  bookTitle: string;
  name: string;
  description: string;
}

interface ChatMessage {
  id: string;
  role: "user" | "assistant" | "error";
  text: string;
}

interface Props {
  target: ChatTarget | null;
  onClose: () => void;
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

export default function ChatPanel({ target, onClose }: Props) {
  const historyRef = useRef<Map<string, ChatMessage[]>>(new Map());
  const listRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const streamRef = useRef<{ turnId: string; key: string } | null>(null);

  const key = target ? (target.characterId ?? `${target.bookId}:${target.name}`) : "";
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [allowFuture, setAllowFuture] = useState(false);
  const [sending, setSending] = useState(false);
  const [streamText, setStreamText] = useState("");
  const [streamKey, setStreamKey] = useState("");
  const [toolStatus, setToolStatus] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);

  useEffect(() => {
    if (!target) return;
    setMessages(historyRef.current.get(key) ?? []);
    setInput("");
    setAllowFuture(false);
    setSending(false);
    setStreamText("");
    setToolStatus("");
    setConfirmClear(false);
    window.setTimeout(() => inputRef.current?.focus(), 220);
  }, [key, target]);

  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [messages, sending, streamText]);

  useEffect(() => {
    const stop = events.acpUpdate.on((payload) => {
      const stream = streamRef.current;
      if (!stream || stream.turnId !== payload.turnId) return;
      if (payload.kind === "text") {
        setStreamText((current) => current + payload.text);
        setToolStatus("");
      } else if (payload.kind === "tool") {
        setToolStatus(payload.text);
      }
    });
    return stop;
  }, []);

  const appendTo = (targetKey: string, message: ChatMessage) => {
    const next = [...(historyRef.current.get(targetKey) ?? []), message];
    historyRef.current.set(targetKey, next);
    // 回复到达时用户可能已经切到别的人物，只更新对应的那段对话。
    setMessages((current) => (key === targetKey ? next : current));
  };

  const pushMessage = (message: ChatMessage) => appendTo(key, message);

  const send = async () => {
    const question = input.trim();
    if (!question || sending || !target) return;
    const sendKey = key;
    const turnId = crypto.randomUUID();
    streamRef.current = { turnId, key: sendKey };
    setStreamKey(sendKey);
    setStreamText("");
    setToolStatus("");
    pushMessage({ id: crypto.randomUUID(), role: "user", text: question });
    setInput("");
    setSending(true);
    const context = readReadingContext(target.bookId);
    try {
      const reply = await Ai.chat({
        bookId: target.bookId,
        bookTitle: target.bookTitle,
        characterName: target.name,
        characterDescription: target.description || null,
        question,
        visibleText: context?.visibleText ?? "",
        chapterTitle: context?.chapterTitle ?? null,
        currentSpineIndex: context?.spineIndex ?? 0,
        allowFuture,
        turnId,
        sessionKey: sendKey,
      });
      appendTo(sendKey, {
        id: crypto.randomUUID(),
        role: reply.ok ? "assistant" : "error",
        text: reply.ok ? reply.text : reply.message,
      });
    } catch (err) {
      appendTo(sendKey, {
        id: crypto.randomUUID(),
        role: "error",
        text: `请求失败：${errorText(err)}`,
      });
    } finally {
      streamRef.current = null;
      setStreamText("");
      setToolStatus("");
      setSending(false);
    }
  };

  const stop = () => {
    const stream = streamRef.current;
    if (stream) void Ai.cancel(stream.turnId).catch(() => {});
  };

  const clearMemory = async () => {
    if (!target || sending) return;
    if (!confirmClear) {
      setConfirmClear(true);
      window.setTimeout(() => setConfirmClear(false), 4000);
      return;
    }
    setConfirmClear(false);
    await Ai.forgetSession(key).catch(() => {});
    historyRef.current.set(key, []);
    setMessages([]);
    setStreamText("");
    setToolStatus("");
  };

  const streaming = sending && streamKey === key && streamText.length > 0;

  return (
    <>
      <div
        className={`drawer-backdrop ${target ? "is-open" : ""}`}
        onClick={onClose}
        aria-hidden
      />
      <aside
        className={`drawer chat-drawer ${target ? "is-open" : ""}`}
        aria-hidden={!target}
      >
        <header className="drawer-head">
          <div>
            <h2>{target ? `和 ${target.name} 对话` : "对话"}</h2>
            <span className="drawer-sub">
              {target ? `《${target.bookTitle}》` : "从人物卡打开"}
            </span>
          </div>
          <div className="drawer-actions">
            {confirmClear ? (
              <button
                className="chat-clear-confirm"
                onClick={() => void clearMemory()}
                disabled={sending}
              >
                确认清除
              </button>
            ) : (
              <button
                className="icon-button"
                onClick={() => void clearMemory()}
                disabled={!target || sending}
                title="清除记忆"
              >
                <Eraser size={16} />
              </button>
            )}
            <button className="icon-button" onClick={onClose} title="关闭">
              <X size={17} />
            </button>
          </div>
        </header>

        <div className="chat-scope">
          <label className="chat-scope-toggle">
            <input
              type="checkbox"
              checked={allowFuture}
              onChange={(event) => setAllowFuture(event.target.checked)}
              disabled={sending}
            />
            <span>允许后续章节</span>
          </label>
          <p className="chat-scope-hint">
            {allowFuture
              ? "已允许：模型可以参考这本书里你还没读到的内容。"
              : "默认关闭：只发送你当前这一页和之前章节里的相关片段。"}
          </p>
        </div>

        <div className="chat-messages" ref={listRef}>
          {messages.length === 0 ? (
            <div className="chat-empty">
              <MessageCircle size={18} />
              <p>问问 {target?.name ?? "这个人物"} 点什么。</p>
              <span>回答只依据你已经读到的内容。</span>
            </div>
          ) : (
            messages.map((message) => (
              <div key={message.id} className={`chat-message is-${message.role}`}>
                {message.text}
              </div>
            ))
          )}
          {streaming ? (
            <div className="chat-message is-assistant is-streaming">{streamText}</div>
          ) : null}
          {sending && !streaming ? (
            <div className="chat-message is-pending">
              <span className="chat-dots">
                <i />
                <i />
                <i />
              </span>
              {toolStatus ? <span className="chat-tool-status">{toolStatus}</span> : null}
            </div>
          ) : null}
        </div>

        <div className="chat-composer">
          <textarea
            ref={inputRef}
            value={input}
            placeholder="写你的问题，⌘↩ 发送"
            rows={2}
            disabled={!target || sending}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                event.preventDefault();
                void send();
              }
            }}
          />
          {sending ? (
            <button className="small-primary chat-send" onClick={stop} title="停止">
              <Square size={13} />
            </button>
          ) : (
            <button
              className="small-primary chat-send"
              onClick={() => void send()}
              disabled={!input.trim() || !target}
              title="发送"
            >
              <Send size={14} />
            </button>
          )}
        </div>
      </aside>
    </>
  );
}
