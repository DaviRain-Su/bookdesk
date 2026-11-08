export interface RevealTarget {
  bookId: string;
  href: string | null;
  spineIndex: number;
  query: string;
  occurrence: number;
  /// 有 cfi 时直接按位置跳转（摘录用），不再按 query 找第几次出现。
  cfi?: string | null;
}

export const REVEAL_EVENT = "bookdesk:reveal-location";
