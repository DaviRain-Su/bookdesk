import type { Edge, Node, Viewport } from "@xyflow/react";

export const DEFAULT_VIEWPORT: Viewport = { x: 0, y: 0, zoom: 1 };

export interface BoardMeta {
  id: string;
  name: string;
}

export function parseViewport(json: string | null | undefined): Viewport {
  if (!json) return DEFAULT_VIEWPORT;
  try {
    const raw = JSON.parse(json) as Partial<Viewport>;
    if (typeof raw.x !== "number" || typeof raw.y !== "number" || typeof raw.zoom !== "number") {
      return DEFAULT_VIEWPORT;
    }
    return { x: raw.x, y: raw.y, zoom: raw.zoom };
  } catch {
    return DEFAULT_VIEWPORT;
  }
}

export function parseNodes(json: string | null | undefined): Node[] {
  if (!json) return [];
  try {
    const raw: unknown = JSON.parse(json);
    if (!Array.isArray(raw)) return [];
    const out: Node[] = [];
    for (const item of raw) {
      if (!item || typeof item !== "object") continue;
      const n = item as Record<string, unknown>;
      if (typeof n.id !== "string") continue;
      const position = n.position as { x?: unknown; y?: unknown } | undefined;
      if (!position) continue;
      out.push({
        id: n.id,
        type: typeof n.type === "string" ? n.type : "text",
        position: { x: Number(position.x) || 0, y: Number(position.y) || 0 },
        data: (n.data as Record<string, unknown>) ?? {},
        style: (n.style as Node["style"]) ?? undefined,
      });
    }
    return out;
  } catch {
    return [];
  }
}

export function parseEdges(json: string | null | undefined): Edge[] {
  if (!json) return [];
  try {
    const raw: unknown = JSON.parse(json);
    if (!Array.isArray(raw)) return [];
    const out: Edge[] = [];
    for (const item of raw) {
      if (!item || typeof item !== "object") continue;
      const e = item as Record<string, unknown>;
      if (typeof e.id !== "string" || typeof e.source !== "string" || typeof e.target !== "string") {
        continue;
      }
      out.push({
        id: e.id,
        source: e.source,
        target: e.target,
        type: typeof e.type === "string" ? e.type : "default",
        style: (e.style as Edge["style"]) ?? undefined,
      });
    }
    return out;
  } catch {
    return [];
  }
}

function persistedData(data: Record<string, unknown>): Record<string, unknown> {
  if (!("reveal" in data)) return data;
  const clean = { ...data };
  delete clean.reveal;
  return clean;
}

export function serializeNodes(nodes: Node[]): string {
  return JSON.stringify(
    nodes.map((n) => ({
      id: n.id,
      type: n.type,
      position: { x: Math.round(n.position.x), y: Math.round(n.position.y) },
      data: persistedData(n.data),
      style: n.style,
    })),
  );
}

export function serializeEdges(edges: Edge[]): string {
  return JSON.stringify(
    edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      type: e.type,
      style: e.style,
    })),
  );
}
