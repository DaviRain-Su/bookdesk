export interface HighlightColorDef {
  id: string;
  label: string;
  /** marks-pane 的填充色，和书页纸色相乘后仍然清楚。 */
  fill: string;
}

const DEFAULT_COLOR: HighlightColorDef = { id: "yellow", label: "黄色", fill: "#ffe9a3" };

/** 五色划线：颜色只影响显示，存进已有的 highlights.color 字段。 */
export const HIGHLIGHT_COLORS: HighlightColorDef[] = [
  DEFAULT_COLOR,
  { id: "green", label: "绿色", fill: "#bfe3c0" },
  { id: "blue", label: "蓝色", fill: "#b9d8f2" },
  { id: "pink", label: "粉色", fill: "#f6c6d4" },
  { id: "purple", label: "紫色", fill: "#d9c9f0" },
];

export function highlightFill(color: string): string {
  return HIGHLIGHT_COLORS.find((item) => item.id === color)?.fill ?? DEFAULT_COLOR.fill;
}

export function highlightLabel(color: string): string {
  return HIGHLIGHT_COLORS.find((item) => item.id === color)?.label ?? DEFAULT_COLOR.label;
}
