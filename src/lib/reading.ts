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

// 同一本书可能同时开着书卡和全屏阅读；后注册的（全屏）优先，
// 它关掉后书卡的登记还在，AI 上下文不会断。
const getters = new Map<string, Getter[]>();

export function registerReadingContext(bookId: string, getter: Getter): () => void {
  const list = getters.get(bookId) ?? [];
  list.push(getter);
  getters.set(bookId, list);
  return () => {
    const current = getters.get(bookId);
    if (!current) return;
    const index = current.lastIndexOf(getter);
    if (index >= 0) current.splice(index, 1);
    if (current.length === 0) getters.delete(bookId);
  };
}

export function readReadingContext(bookId: string): ReadingSnapshot | null {
  const list = getters.get(bookId);
  const getter = list?.[list.length - 1];
  if (!getter) return null;
  try {
    return getter();
  } catch {
    return null;
  }
}
