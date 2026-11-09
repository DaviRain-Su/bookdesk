export const SHARE_CARD_EVENT = "bookdesk:share-card";
export const EXPORT_BOOK_EVENT = "bookdesk:export-book";

/** 从书桌或全屏阅读发出的一条分享。note 为 undefined 时，对话框自己去摘录卡里找。 */
export interface ShareCardRequest {
  bookId: string;
  bookTitle?: string;
  author?: string | null;
  chapter?: string | null;
  quote: string;
  note?: string | null;
  color?: string;
}

export function normalizeQuote(text: string): string {
  return text.split(/\s+/).filter(Boolean).join(" ");
}

export function chapterFromSource(source: string | undefined): string | null {
  if (!source) return null;
  const parts = source
    .split(" · ")
    .map((part) => part.trim())
    .filter(Boolean);
  if (parts.length < 2) return null;
  return parts.slice(1).join(" · ");
}
