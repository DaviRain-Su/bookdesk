import { useCallback, useEffect, useMemo, useRef } from "react";
import {
  Background,
  BackgroundVariant,
  ReactFlow,
  ReactFlowProvider,
  SelectionMode,
  addEdge,
  useEdgesState,
  useNodesState,
  useReactFlow,
  useViewport,
  type Connection,
  type Edge,
  type Node,
  type Viewport,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { BookPlus, Library, Maximize2, Minus, Plus } from "lucide-react";
import BookNode from "./nodes/BookNode";
import TextNode from "./nodes/TextNode";
import ImageNode from "./nodes/ImageNode";
import CharacterNode from "./nodes/CharacterNode";
import { ActiveBookContext } from "./ActiveBookContext";
import type { Tool } from "./Toolbar";
import { Board, Characters, Notes } from "../mygo";
import { serializeEdges, serializeNodes, type BoardMeta } from "../lib/board";
import { REVEAL_EVENT, type RevealTarget } from "../lib/reveal";

const nodeTypes = {
  book: BookNode,
  text: TextNode,
  image: ImageNode,
  character: CharacterNode,
};

// 画布被删除后，卸载时的最后一次保存不能把它写回数据库。
const deletedBoards = new Set<string>();

export function markBoardDeleted(id: string) {
  deletedBoards.add(id);
}

function ZoomBar() {
  const { zoom } = useViewport();
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  return (
    <div className="zoom-bar">
      <button className="zoom-button" onClick={() => zoomOut({ duration: 160 })} title="缩小">
        <Minus size={15} />
      </button>
      <span className="zoom-value">{Math.round(zoom * 100)}%</span>
      <button className="zoom-button" onClick={() => zoomIn({ duration: 160 })} title="放大">
        <Plus size={15} />
      </button>
      <div className="zoom-sep" />
      <button
        className="zoom-button"
        onClick={() => fitView({ duration: 260, padding: 0.2 })}
        title="适应窗口"
      >
        <Maximize2 size={14} />
      </button>
    </div>
  );
}

interface Props {
  tool: Tool;
  board: BoardMeta;
  initialNodes: Node[];
  initialEdges: Edge[];
  initialViewport: Viewport;
  hasStoredViewport: boolean;
  hasBooks: boolean;
  onAddBook: () => void;
  onOpenLibrary: () => void;
}

interface QuoteLocation {
  bookId: string;
  cfi: string | null;
  spineIndex: number;
  href: string | null;
  pageLabel: string | null;
  charOffset: number | null;
}

function nodeSize(node: Node): { width: number; height: number } {
  const width = Number(
    (node.style as { width?: number } | undefined)?.width ?? (node.type === "book" ? 640 : 320),
  );
  const height =
    node.type === "book" ? 640 : node.type === "text" ? 240 : node.type === "character" ? 210 : 260;
  return { width, height };
}

/** 新卡片避开已有卡片，向下寻找空位。 */
function findFreePosition(
  nodes: Node[],
  base: { x: number; y: number },
  size: { width: number; height: number },
): { x: number; y: number } {
  let position = { ...base };
  for (let attempt = 0; attempt < 24; attempt += 1) {
    const collides = nodes.some((node) => {
      const s = nodeSize(node);
      return (
        node.position.x < position.x + size.width &&
        position.x < node.position.x + s.width &&
        node.position.y < position.y + size.height &&
        position.y < node.position.y + s.height
      );
    });
    if (!collides) break;
    position = { x: position.x, y: position.y + 40 };
  }
  return position;
}

function BoardCanvas({
  tool,
  board,
  initialNodes,
  initialEdges,
  initialViewport,
  hasStoredViewport,
  hasBooks,
  onAddBook,
  onOpenLibrary,
}: Props) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initialEdges);
  const { screenToFlowPosition, setCenter, fitView } = useReactFlow();

  useEffect(() => {
    const handler = () => {
      void fitView({ duration: 260, padding: 0.2 });
    };
    window.addEventListener("bookdesk:fit-view", handler);
    return () => window.removeEventListener("bookdesk:fit-view", handler);
  }, [fitView]);

  const nodesRef = useRef(nodes);
  nodesRef.current = nodes;
  const edgesRef = useRef(edges);
  edgesRef.current = edges;
  const viewportRef = useRef<Viewport>(initialViewport);
  const saveTimer = useRef<number | null>(null);
  const hydrated = useRef(false);
  const lastBookIds = useRef("");

  const flush = useCallback(() => {
    if (saveTimer.current) {
      window.clearTimeout(saveTimer.current);
      saveTimer.current = null;
    }
    if (deletedBoards.has(board.id)) return;
    void Board.save({
      id: board.id,
      name: board.name,
      viewportJson: JSON.stringify(viewportRef.current),
      nodesJson: serializeNodes(nodesRef.current),
      edgesJson: serializeEdges(edgesRef.current),
      updatedAt: Math.floor(Date.now() / 1000),
    }).catch(() => {});
  }, [board.id, board.name]);

  const scheduleSave = useCallback(() => {
    if (saveTimer.current) window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(flush, 500);
  }, [flush]);

  useEffect(() => {
    if (!hydrated.current) {
      hydrated.current = true;
      return;
    }
    scheduleSave();
  }, [nodes, edges, scheduleSave]);

  useEffect(() => {
    const onBlur = () => flush();
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("blur", onBlur);
      flush();
    };
  }, [flush]);

  useEffect(() => {
    const ids = nodes
      .filter((n) => n.type === "book")
      .map((n) => (n.data as { bookId?: string }).bookId)
      .filter((v): v is string => typeof v === "string");
    const key = ids.join("|");
    if (key === lastBookIds.current) return;
    lastBookIds.current = key;
    window.dispatchEvent(
      new CustomEvent("bookdesk:board-books", { detail: { bookIds: ids } }),
    );
  }, [nodes]);

  const persistNode = useCallback(
    (node: Node) => {
      if (node.type === "character") {
        const data = node.data as Record<string, unknown>;
        const characterId = typeof data.characterId === "string" ? data.characterId : null;
        const name = typeof data.name === "string" ? data.name.trim() : "";
        if (!characterId || !name) return;
        void Characters.save({
          id: characterId,
          bookId: String(data.bookId ?? ""),
          name,
          description: typeof data.description === "string" ? data.description : null,
          imageRelPath: typeof data.imageRelPath === "string" ? data.imageRelPath : null,
        }).catch(() => {});
        return;
      }
      if (node.type !== "text" && node.type !== "image") return;
      const data = node.data as Record<string, unknown>;
      const quoteLocation = (data.quoteLocation as QuoteLocation | null | undefined) ?? null;
      void Notes.saveCard({
        id: typeof data.cardId === "string" ? data.cardId : null,
        type: node.type === "image" ? "image" : "text",
        title: typeof data.title === "string" ? data.title : null,
        body: typeof data.body === "string" ? data.body : null,
        quote: typeof data.quote === "string" ? data.quote : null,
        quoteLocation,
        imageRelPath: typeof data.imageRelPath === "string" ? data.imageRelPath : null,
        sourceBookId: typeof data.sourceBookId === "string" ? data.sourceBookId : null,
        characterName: null,
      })
        .then((card) => {
          if (typeof data.cardId === "string") return;
          setNodes((nds) =>
            nds.map((n) => (n.id === node.id ? { ...n, data: { ...n.data, cardId: card.id } } : n)),
          );
        })
        .catch(() => {});
    },
    [setNodes],
  );

  const onConnect = useCallback(
    (connection: Connection) =>
      setEdges((eds) =>
        addEdge(
          {
            ...connection,
            id: `e-${crypto.randomUUID()}`,
            style: { stroke: "#b8b3a6", strokeWidth: 1.5 },
          },
          eds,
        ),
      ),
    [setEdges],
  );

  const addTextCard = useCallback(() => {
    const center = screenToFlowPosition({
      x: window.innerWidth / 2,
      y: window.innerHeight / 2,
    });
    const node: Node = {
      id: `text-${crypto.randomUUID()}`,
      type: "text",
      position: findFreePosition(
        nodesRef.current,
        { x: center.x - 150, y: center.y - 90 },
        { width: 300, height: 240 },
      ),
      data: { title: "", body: "" },
      style: { width: 300 },
    };
    setNodes((nds) => [...nds, node]);
    persistNode(node);
  }, [persistNode, screenToFlowPosition, setNodes]);

  useEffect(() => {
    const handler = () => addTextCard();
    window.addEventListener("bookdesk:add-text-card", handler);
    return () => window.removeEventListener("bookdesk:add-text-card", handler);
  }, [addTextCard]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | {
            bookId?: string;
            title?: string;
            author?: string | null;
            reveal?: RevealTarget;
          }
        | undefined;
      if (!detail?.bookId) return;
      const existing = nodesRef.current.find(
        (n) => n.type === "book" && (n.data as { bookId?: string }).bookId === detail.bookId,
      );
      if (existing) {
        setNodes((nds) => nds.map((n) => ({ ...n, selected: n.id === existing.id })));
        void setCenter(existing.position.x + 320, existing.position.y + 300, {
          zoom: 1,
          duration: 420,
        });
        if (detail.reveal) {
          window.dispatchEvent(new CustomEvent(REVEAL_EVENT, { detail: detail.reveal }));
        }
        return;
      }
      const center = screenToFlowPosition({
        x: window.innerWidth / 2,
        y: window.innerHeight / 2,
      });
      const bookNodes = nodesRef.current.filter((n) => n.type === "book");
      let base = { x: center.x - 320, y: center.y - 300 };
      if (bookNodes.length > 0) {
        const rightEdge = Math.max(
          ...nodesRef.current.map((n) => n.position.x + nodeSize(n).width),
        );
        base = {
          x: rightEdge + 80,
          y: Math.min(...bookNodes.map((n) => n.position.y)),
        };
      }
      const position = findFreePosition(nodesRef.current, base, { width: 640, height: 640 });
      const node: Node = {
        id: `book-${crypto.randomUUID()}`,
        type: "book",
        position,
        data: {
          bookId: detail.bookId,
          title: detail.title ?? "未命名书籍",
          author: detail.author ?? null,
          ...(detail.reveal ? { reveal: detail.reveal } : {}),
        },
        style: { width: 640 },
      };
      setNodes((nds) => [...nds.map((n) => ({ ...n, selected: false })), { ...node, selected: true }]);
      void setCenter(position.x + 320, position.y + 300, { zoom: 1, duration: 420 });
    };
    window.addEventListener("bookdesk:add-book", handler);
    return () => window.removeEventListener("bookdesk:add-book", handler);
  }, [screenToFlowPosition, setCenter, setNodes]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<RevealTarget>).detail;
      if (!detail?.bookId) return;
      const node = nodesRef.current.find(
        (n) => n.type === "book" && (n.data as { bookId?: string }).bookId === detail.bookId,
      );
      if (!node) return;
      setNodes((nds) => nds.map((n) => ({ ...n, selected: n.id === node.id })));
      void setCenter(node.position.x + 320, node.position.y + 300, {
        zoom: 1,
        duration: 420,
      });
    };
    window.addEventListener(REVEAL_EVENT, handler);
    return () => window.removeEventListener(REVEAL_EVENT, handler);
  }, [setCenter, setNodes]);

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.shiftKey) return;
      if (event.key.toLowerCase() !== "f") return;
      event.preventDefault();
      const books = nodesRef.current.filter((n) => n.type === "book");
      const target = books.find((n) => n.selected) ?? books[0];
      if (!target) return;
      window.dispatchEvent(
        new CustomEvent("bookdesk:find-in-book", { detail: { nodeId: target.id } }),
      );
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | {
            bookId?: string;
            quote?: string;
            source?: string;
            location?: QuoteLocation | null;
          }
        | undefined;
      if (!detail?.quote) return;
      const bookNode = nodesRef.current.find(
        (n) => n.type === "book" && (n.data as { bookId?: string }).bookId === detail.bookId,
      );
      let base: { x: number; y: number };
      if (bookNode) {
        base = { x: bookNode.position.x + 700, y: bookNode.position.y + 60 };
      } else {
        const center = screenToFlowPosition({
          x: window.innerWidth / 2,
          y: window.innerHeight / 2,
        });
        base = { x: center.x - 160, y: center.y - 120 };
      }
      const position = findFreePosition(nodesRef.current, base, { width: 320, height: 240 });
      const node: Node = {
        id: `text-${crypto.randomUUID()}`,
        type: "text",
        position,
        data: {
          title: "",
          body: "",
          quote: detail.quote,
          source: detail.source ?? "",
          sourceBookId: detail.bookId ?? null,
          quoteLocation: detail.location ?? null,
        },
        style: { width: 320 },
      };
      setNodes((nds) => [...nds, node]);
      persistNode(node);
    };
    window.addEventListener("bookdesk:add-quote-card", handler);
    return () => window.removeEventListener("bookdesk:add-quote-card", handler);
  }, [persistNode, screenToFlowPosition, setNodes]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | { bookId?: string; bookTitle?: string; name?: string }
        | undefined;
      if (!detail?.bookId) return;
      const bookId = detail.bookId;
      const name = (detail.name ?? "").trim().slice(0, 24) || "未命名人物";
      void Characters.save({
        id: null,
        bookId,
        name,
        description: null,
        imageRelPath: null,
      })
        .then((character) => {
          const bookNode = nodesRef.current.find(
            (n) => n.type === "book" && (n.data as { bookId?: string }).bookId === bookId,
          );
          let base: { x: number; y: number };
          if (bookNode) {
            base = { x: bookNode.position.x + 700, y: bookNode.position.y + 60 };
          } else {
            const center = screenToFlowPosition({
              x: window.innerWidth / 2,
              y: window.innerHeight / 2,
            });
            base = { x: center.x - 130, y: center.y - 100 };
          }
          const position = findFreePosition(nodesRef.current, base, { width: 260, height: 210 });
          const node: Node = {
            id: `character-${crypto.randomUUID()}`,
            type: "character",
            position,
            data: {
              characterId: character.id,
              bookId,
              name: character.name,
              description: "",
              imageRelPath: null,
            },
            style: { width: 260 },
          };
          setNodes((nds) => [...nds, node]);
          if (bookNode) {
            setEdges((eds) =>
              addEdge(
                {
                  id: `e-${crypto.randomUUID()}`,
                  source: bookNode.id,
                  target: node.id,
                  style: { stroke: "#b8b3a6", strokeWidth: 1.5 },
                },
                eds,
              ),
            );
          }
        })
        .catch(() => {});
    };
    window.addEventListener("bookdesk:add-character", handler);
    return () => window.removeEventListener("bookdesk:add-character", handler);
  }, [screenToFlowPosition, setEdges, setNodes]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as { relPath?: string } | undefined;
      if (!detail?.relPath) return;
      const name = detail.relPath.split("/").pop() ?? "";
      if (!name) return;
      const center = screenToFlowPosition({
        x: window.innerWidth / 2,
        y: window.innerHeight / 2,
      });
      const node: Node = {
        id: `image-${crypto.randomUUID()}`,
        type: "image",
        position: findFreePosition(
          nodesRef.current,
          { x: center.x - 160, y: center.y - 120 },
          { width: 320, height: 260 },
        ),
        data: {
          src: `books://localhost/image/${name}`,
          imageRelPath: detail.relPath,
          caption: "",
        },
        style: { width: 320 },
      };
      setNodes((nds) => [...nds, node]);
      persistNode(node);
    };
    window.addEventListener("bookdesk:add-image-card", handler);
    return () => window.removeEventListener("bookdesk:add-image-card", handler);
  }, [persistNode, screenToFlowPosition, setNodes]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | { nodeId?: string; patch?: Record<string, unknown> }
        | undefined;
      if (!detail?.nodeId || !detail.patch) return;
      const node = nodesRef.current.find((n) => n.id === detail.nodeId);
      if (!node) return;
      const next: Node = { ...node, data: { ...node.data, ...detail.patch } };
      setNodes((nds) => nds.map((n) => (n.id === detail.nodeId ? next : n)));
      persistNode(next);
    };
    window.addEventListener("bookdesk:update-node", handler);
    return () => window.removeEventListener("bookdesk:update-node", handler);
  }, [persistNode, setNodes]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as { nodeId?: string } | undefined;
      if (!detail?.nodeId) return;
      const node = nodesRef.current.find((n) => n.id === detail.nodeId);
      if (!node) return;
      setNodes((nds) => nds.filter((n) => n.id !== detail.nodeId));
      setEdges((eds) => eds.filter((e) => e.source !== detail.nodeId && e.target !== detail.nodeId));
      const cardId = (node.data as { cardId?: string }).cardId;
      if (typeof cardId === "string") void Notes.deleteCard(cardId).catch(() => {});
      if (node.type === "character") {
        const characterId = (node.data as { characterId?: string }).characterId;
        if (typeof characterId === "string") {
          void Characters.delete(characterId).catch(() => {});
        }
      }
    };
    window.addEventListener("bookdesk:delete-node", handler);
    return () => window.removeEventListener("bookdesk:delete-node", handler);
  }, [setEdges, setNodes]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail as
        | {
            nodeId?: string;
            bookId?: string;
            cfi?: string;
            spineIndex?: number;
            percent?: number;
          }
        | undefined;
      if (!detail?.cfi) return;
      if (!detail.nodeId && !detail.bookId) return;
      // 书卡自己翻页时带 nodeId；全屏阅读翻页只带 bookId，
      // 这里把同一本书的书卡一起更新，回到书桌时位置就是新的。
      setNodes((nds) =>
        nds.map((n) => {
          const data = n.data as Record<string, unknown>;
          const match = detail.nodeId
            ? n.id === detail.nodeId
            : n.type === "book" && data.bookId === detail.bookId;
          if (!match || data.cfi === detail.cfi) return n;
          return {
            ...n,
            data: {
              ...data,
              cfi: detail.cfi,
              spineIndex: detail.spineIndex,
              percent: detail.percent,
            },
          };
        }),
      );
    };
    window.addEventListener("bookdesk:book-progress", handler);
    return () => window.removeEventListener("bookdesk:book-progress", handler);
  }, [setNodes]);

  const selectionMode = useMemo(() => SelectionMode.Partial, []);

  // 选中的书就是「当前书」；摘录卡据此决定要不要标成「他书」的蓝色。
  const activeBookId = useMemo(() => {
    const book = nodes.find((n) => n.type === "book" && n.selected);
    return (book?.data as { bookId?: string } | undefined)?.bookId ?? null;
  }, [nodes]);

  // 菜单「显示 → 全屏阅读」优先打开当前选中的书。
  useEffect(() => {
    window.dispatchEvent(
      new CustomEvent("bookdesk:active-book", { detail: { bookId: activeBookId } }),
    );
  }, [activeBookId]);

  return (
    <div className="canvas-wrap">
      <ActiveBookContext.Provider value={activeBookId}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        nodeTypes={nodeTypes}
        defaultViewport={initialViewport}
        fitView={!hasStoredViewport}
        fitViewOptions={{ padding: 0.25 }}
        minZoom={0.2}
        maxZoom={2.4}
        panOnDrag={tool === "pan" ? true : [1, 2]}
        selectionOnDrag={tool === "select"}
        selectionMode={selectionMode}
        nodesConnectable={tool === "connect" || tool === "select"}
        proOptions={{ hideAttribution: true }}
        defaultEdgeOptions={{ type: "default" }}
        deleteKeyCode={["Backspace", "Delete"]}
        onMoveEnd={(_, viewport) => {
          viewportRef.current = viewport;
          scheduleSave();
        }}
      >
        <Background variant={BackgroundVariant.Dots} gap={22} size={1.4} color="#d8d4c9" />
        <ZoomBar />
      </ReactFlow>
      </ActiveBookContext.Provider>

      {nodes.length === 0 ? (
        <div className="canvas-empty">
          <div className="canvas-empty-card">
            <h2>{hasBooks ? "把书放到画布上" : "还没有书"}</h2>
            <p>
              {hasBooks
                ? "从书架选一本书，或导入新的 EPUB，在画布上阅读、划线、写卡片。"
                : "添加你拥有的 EPUB：购买的、有权使用的、公版的，或自己制作的学习材料。"}
            </p>
            <div className="canvas-empty-actions">
              {hasBooks ? (
                <button className="primary-button" onClick={onOpenLibrary}>
                  <Library size={16} />
                  从书架添加
                </button>
              ) : null}
              <button className={hasBooks ? "pill-button" : "primary-button"} onClick={onAddBook}>
                <BookPlus size={16} />
                添加书籍
              </button>
            </div>
            <span className="canvas-empty-hint">也可以把 .epub 文件直接拖进窗口</span>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function CanvasBoard(props: Props) {
  return (
    <ReactFlowProvider>
      <BoardCanvas {...props} />
    </ReactFlowProvider>
  );
}
