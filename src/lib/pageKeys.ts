export type PageTurn = "prev" | "next";

/** 用节点能力判断，兼容 EPUB iframe 里的另一套 DOM 构造器。 */
function elementFor(target: EventTarget | null): Element | null {
  return target && "closest" in target ? target as Element : null;
}

export function pageTurnForKey(event: KeyboardEvent): PageTurn | null {
  if (event.defaultPrevented || event.isComposing || event.metaKey || event.ctrlKey || event.altKey) {
    return null;
  }
  const target = elementFor(event.target);
  if (target?.closest(
    'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="slider"], [role="spinbutton"], [role="combobox"]',
  )) return null;

  if (event.key === " " || event.key === "Spacebar") {
    // 保留按钮和链接自己的空格键操作。
    if (target?.closest('button, a[href], summary, [role="button"]')) return null;
    return event.shiftKey ? "prev" : "next";
  }
  if (event.shiftKey) return null;
  if (event.key === "ArrowLeft" || event.key === "PageUp") return "prev";
  if (event.key === "ArrowRight" || event.key === "PageDown") return "next";
  return null;
}

export function bindPageKeys(
  host: HTMLElement,
  mode: "card" | "reader",
  isReady: () => boolean,
  turn: (direction: PageTurn) => void,
) {
  const doc = host.ownerDocument;
  const win = doc.defaultView;
  const handle = (event: KeyboardEvent, fromContents: boolean) => {
    const direction = pageTurnForKey(event);
    if (!direction || !isReady()) return;
    // 弹出的目录、设置、搜索或对话面板优先接收键盘。
    if (doc.querySelector('.drawer.is-open, [role="dialog"], .toc-popover, .board-menu')) return;
    const fullscreen = doc.querySelector(".reader-overlay");
    if (mode === "card") {
      if (fullscreen) return;
      if (!fromContents) {
        const selected = doc.querySelectorAll(".book-card.is-selected");
        if (selected.length !== 1 || !selected[0]?.contains(host)) return;
        const target = elementFor(event.target);
        // 焦点在侧栏或其他卡片时，保留那里的操作。
        if (target && target !== doc.body && target !== doc.documentElement &&
          !selected[0].contains(target) &&
          target !== selected[0].closest(".react-flow__node") &&
          !target.matches(".react-flow__pane")) return;
      }
    }
    event.preventDefault();
    event.stopPropagation();
    turn(direction);
  };
  const onWindowKey = (event: KeyboardEvent) => handle(event, false);
  // 在 React Flow 的节点移动处理之前接住翻页键，避免把书卡挪走。
  win?.addEventListener("keydown", onWindowKey, true);
  return {
    onContentsKey: (event: KeyboardEvent) => handle(event, true),
    destroy: () => win?.removeEventListener("keydown", onWindowKey, true),
  };
}
