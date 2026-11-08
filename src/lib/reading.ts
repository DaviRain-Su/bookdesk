/**
 * 阅读位置登记处：每本打开的书把自己的「当前可见页」注册进来，
 * 对话面板在发送问题时按需读取。这样上下文永远是发送那一刻的，
 * 不会因为翻页而变旧。
 */
export interface ReadingSnapshot {
  chapterTitle: string;
  spineIndex: number;
  /** 当前这一页能看到的文字；取不到时为空字符串。 */
  visibleText: string;
}

type Getter = () => ReadingSnapshot;

const getters = new Map<string, Getter>();

export function registerReadingContext(bookId: string, getter: Getter): () => void {
  getters.set(bookId, getter);
  return () => {
    if (getters.get(bookId) === getter) getters.delete(bookId);
  };
}

export function readReadingContext(bookId: string): ReadingSnapshot | null {
  const getter = getters.get(bookId);
  if (!getter) return null;
  try {
    return getter();
  } catch {
    return null;
  }
}
