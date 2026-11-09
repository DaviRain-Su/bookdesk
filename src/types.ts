export type BookId = string;
export type WhiteboardId = string;
export type CardId = string;
export type HighlightId = string;

export interface Book {
  id: BookId;
  title: string;
  author?: string | null;
  language?: string | null;
  coverRelPath?: string | null;
  epubRelPath: string;
  importedAt: number;
}

export interface BookLocation {
  bookId: BookId;
  cfi: string | null;
  spineIndex: number;
  href: string | null;
  pageLabel: string | null;
  charOffset: number | null;
}

export interface Highlight {
  id: HighlightId;
  bookId: BookId;
  location: BookLocation;
  text: string;
  color: string;
  createdAt: number;
}

export type CardType = "text" | "image" | "character";

export interface Card {
  id: CardId;
  type: CardType;
  title?: string | null;
  body?: string | null;
  quote?: string | null;
  quoteLocation?: BookLocation | null;
  imageRelPath?: string | null;
  sourceBookId?: BookId | null;
  characterName?: string | null;
  createdAt: number;
  updatedAt: number;
}

export interface BoardNode {
  id: string;
  kind: "book" | "card";
  bookId?: BookId | null;
  cardId?: CardId | null;
  x: number;
  y: number;
  width?: number | null;
  height?: number | null;
}

export interface BoardEdge {
  id: string;
  source: string;
  target: string;
}

export interface Whiteboard {
  id: WhiteboardId;
  name: string;
  viewport: { x: number; y: number; zoom: number };
  nodes: BoardNode[];
  edges: BoardEdge[];
  updatedAt: number;
}

export interface SearchHit {
  bookId: BookId;
  title: string;
  location: BookLocation;
  snippet: string;
  highlights: [number, number][];
}

export interface ImportProgress {
  done: number;
  total: number;
  title: string;
}
