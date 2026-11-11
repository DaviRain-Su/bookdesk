import { useEffect, useRef, useState } from "react";
import { Languages, Lightbulb, Send, Sparkles, Square, X } from "lucide-react";
import { Ai, events } from "../mygo";
import { readReadingContext } from "../lib/reading";

export type AskMode = "ask" | "explain" | "translate";

export interface AskRequest {
  bookId: string;
  bookTitle: string;
  mode: AskMode;
  selection: string;
}

interface AskMessage {
  id: string;
  role: "user" | "assistant" | "error";
  text: string;
}

interface Props {
  request: AskRequest | null;
  onClose: () => void;
}

const MODE_LABEL: Record<AskMode, string> = {
  ask: "问 AI",
  explain: "解释",
  translate: "翻译",
};

const MODE_ICON = {
  ask: Sparkles,
  explain: Lightbulb,
  translate: Languages,
};

const MODE_QUESTION: Record<AskMode, string> = {
  ask: "",
  explain: "解释这段文字",
  translate: "翻译这段文字",
};

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

/** 同一段文字复用同一个会话，追问时 Agent 还记得上一轮。 */
function sessionKeyFor(bookId: string, selection: string): string {
  let hash = 5381;
  for (let i = 0; i < selection.length; i += 1) {
    hash = ((hash << 5) + hash + selection.charCodeAt(i)) | 0;
  }
  return `ask:${bookId}:${(hash >>> 0).toString(36)}`;
}

export default function AskPanel({ request, onClose }: Props) {
  const historyRef = useRef<Map<string, AskMessage[]>>(new Map());
  const listRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const streamRef = useRef<string | null>(null);
  const streamKeyRef = useRef("");
  const autoSentRef = useRef("");

  const requestKey = request
    ? `${request.bookId}:${request.mode}:${request.selection}`
    : "";
  const requestKeyRef = useRef(requestKey);
  requestKeyRef.current = requestKey;
  const [messages, setMessages] = useState<AskMessage[]>([]);
  const [input, setInput] = useState("");
  const [allowFuture, setAllowFuture] = useState(false);
  const [sending, setSending] = useState(false);
  const [streamText, setStreamText] = useState("");
  const [toolStatus, setToolStatus] = useState("");

  useEffect(() => {
    streamRef.current = null;
    streamKeyRef.current = "";
    setSending(false);
    setStreamText("");
    setToolStatus("");
    if (!request) {
      // 关掉面板后，同一段文字再点一次「解释」应该重新发。
      autoSentRef.current = "";
      return;
    }
    setMessages(historyRef.current.get(requestKey) ?? []);
    setInput("");
    setAllowFuture(false);
    let focusTimer: number | undefined;
    if (request.mode === "ask") {
      autoSentRef.current = "";
      focusTimer = window.setTimeout(() => inputRef.current?.focus(), 220);
    }
    return () => {
      window.clearTimeout(focusTimer);
      const turnId = streamRef.current;
      streamRef.current = null;
      streamKeyRef.current = "";
      if (turnId) void Ai.cancel(turnId).catch(() => {});
    };
    // 相同的书、动作和选区不重置正在进行的请求。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestKey]);

  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [messages, sending, streamText]);

  useEffect(() => {
    const stop = events.acpUpdate.on((payload) => {
      if (!streamRef.current || streamRef.current !== payload.turnId) return;
      if (streamKeyRef.current !== requestKeyRef.current) return;
      if (payload.kind === "text") {
        setStreamText((current) => current + payload.text);
        setToolStatus("");
      } else if (payload.kind === "tool") {
        setToolStatus(payload.text);
      }
    });
    return stop;
  }, []);

  const appendTo = (key: string, message: AskMessage) => {
    const next = [...(historyRef.current.get(key) ?? []), message];
    historyRef.current.set(key, next);
    if (requestKeyRef.current === key) {
      setMessages((current) => (requestKeyRef.current === key ? next : current));
    }
  };

  const send = async (questionOverride?: string) => {
    if (!request || streamRef.current) return;
    const question = (questionOverride ?? input).trim();
    if (request.mode === "ask" && !question) return;
    const sendKey = requestKey;
    const turnId = crypto.randomUUID();
    streamRef.current = turnId;
    streamKeyRef.current = sendKey;
    setStreamText("");
    setToolStatus("");
    if (question) {
      appendTo(sendKey, { id: crypto.randomUUID(), role: "user", text: question });
    }
    setInput("");
    setSending(true);
    const context = readReadingContext(request.bookId);
    try {
      const reply = await Ai.ask({
        bookId: request.bookId,
        bookTitle: request.bookTitle,
        mode: request.mode,
        selection: request.selection,
        question,
        visibleText: context?.visibleText ?? "",
        chapterTitle: context?.chapterTitle ?? null,
        currentSpineIndex: context?.spineIndex ?? 0,
        allowFuture,
        turnId,
        sessionKey: sessionKeyFor(request.bookId, request.selection),
      });
      if (streamRef.current !== turnId || requestKeyRef.current !== sendKey) return;
      appendTo(sendKey, {
        id: crypto.randomUUID(),
        role: reply.ok ? "assistant" : "error",
        text: reply.ok ? reply.text : reply.message,
      });
    } catch (err) {
      if (streamRef.current !== turnId || requestKeyRef.current !== sendKey) return;
      appendTo(sendKey, {
        id: crypto.randomUUID(),
        role: "error",
        text: `请求失败：${errorText(err)}`,
      });
    } finally {
      if (streamRef.current === turnId) {
        streamRef.current = null;
        streamKeyRef.current = "";
        setStreamText("");
        setToolStatus("");
        setSending(false);
      }
    }
  };

  // 解释和翻译点开就发；问 AI 等用户自己写问题。
  useEffect(() => {
    if (!request || request.mode === "ask") return;
    if (autoSentRef.current === requestKey) return;
    autoSentRef.current = requestKey;
    void send(MODE_QUESTION[request.mode]);
    // send 只依赖当前 request 和输入，这里按 requestKey 触发一次即可。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestKey]);

  const stop = () => {
    if (streamRef.current) void Ai.cancel(streamRef.current).catch(() => {});
  };

  const mode = request?.mode ?? "ask";
  const Icon = MODE_ICON[mode];
  const streaming = sending && streamText.length > 0;

  return (
    <>
      <div
        className={`drawer-backdrop ${request ? "is-open" : ""}`}
        onClick={onClose}
        aria-hidden
      />
      <aside
        className={`drawer ask-drawer ${request ? "is-open" : ""}`}
        aria-hidden={!request}
        aria-label="问 AI"
      >
        <header className="drawer-head">
          <div>
            <h2>
              <Icon size={16} />
              {MODE_LABEL[mode]}
            </h2>
            <span className="drawer-sub">{request ? `《${request.bookTitle}》` : ""}</span>
          </div>
          <div className="drawer-actions">
            <button className="icon-button" onClick={onClose} title="关闭">
              <X size={17} />
            </button>
          </div>
        </header>

        {request ? (
          <>
            <div className="ask-selection">
              <span className="ask-selection-label">选中的文字</span>
              <blockquote>{request.selection}</blockquote>
            </div>

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
                  : "默认关闭：只发送当前这一页、选中的文字和之前章节里的相关片段。"}
              </p>
            </div>

            <div className="chat-messages" ref={listRef}>
              {messages.length === 0 && !sending ? (
                <div className="chat-empty">
                  <Sparkles size={18} />
                  <p>就这段文字问点什么。</p>
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
                placeholder={mode === "ask" ? "问点什么，⌘↩ 发送" : "继续追问，⌘↩ 发送"}
                rows={2}
                disabled={sending}
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
                  disabled={mode === "ask" && !input.trim()}
                  title="发送"
                >
                  <Send size={14} />
                </button>
              )}
            </div>
          </>
        ) : null}
      </aside>
    </>
  );
}
