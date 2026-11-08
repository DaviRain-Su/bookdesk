export interface RevealTarget {
  bookId: string;
  href: string | null;
  spineIndex: number;
  query: string;
  occurrence: number;
}

export const REVEAL_EVENT = "bookdesk:reveal-location";
