import type { ReadingSettings } from "../../mygo";

export interface ReaderTheme {
  id: "paper" | "day" | "night";
  label: string;
  /** 书页底色，同时用于 iframe 和父容器的 .reader-page。 */
  bg: string;
  ink: string;
  muted: string;
  selection: string;
  link: string;
  /** 设置面板里的色块。 */
  swatch: string;
  swatchInk: string;
}

export const READER_THEMES: ReaderTheme[] = [
  {
    id: "paper",
    label: "浅色纸",
    bg: "#f7f1e3",
    ink: "#38332a",
    muted: "#7d7466",
    selection: "#e6d39c",
    link: "#7a5c2e",
    swatch: "#f7f1e3",
    swatchInk: "#38332a",
  },
  {
    id: "day",
    label: "白昼",
    bg: "#ffffff",
    ink: "#26241f",
    muted: "#6f6a60",
    selection: "#ffe9a3",
    link: "#2f6b52",
    swatch: "#ffffff",
    swatchInk: "#26241f",
  },
  {
    id: "night",
    label: "夜间",
    bg: "#17181a",
    ink: "#c9c6bf",
    muted: "#8f8a80",
    selection: "#4a4330",
    link: "#8fb7a0",
    swatch: "#17181a",
    swatchInk: "#d8d4cc",
  },
];

export const DEFAULT_READER_SETTINGS: ReadingSettings = {
  theme: "paper",
  fontSize: 17,
  lineHeight: 1.95,
  columnWidth: 780,
  vertical: false,
  convert: "none",
  zen: false,
  spread: false,
};

export const COLUMN_WIDTH_OPTIONS = [
  { value: 620, label: "窄" },
  { value: 780, label: "中" },
  { value: 940, label: "宽" },
] as const;

export const MIN_FONT_SIZE = 14;
export const MAX_FONT_SIZE = 24;
export const MIN_LINE_HEIGHT = 1.5;
export const MAX_LINE_HEIGHT = 2.4;

export function themeById(id: string): ReaderTheme {
  return READER_THEMES.find((theme) => theme.id === id) ?? READER_THEMES[0]!;
}

export function clampFontSize(size: number): number {
  return Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE, Math.round(size)));
}

export function clampLineHeight(height: number): number {
  const clamped = Math.min(MAX_LINE_HEIGHT, Math.max(MIN_LINE_HEIGHT, height));
  return Math.round(clamped * 100) / 100;
}

// 只是给设置面板一个轻提示，不自动改用户的排版选择。
const CLASSIC_KEYWORDS = [
  "论语",
  "孟子",
  "大学",
  "中庸",
  "诗经",
  "尚书",
  "礼记",
  "周易",
  "春秋",
  "左传",
  "公羊传",
  "谷梁传",
  "史记",
  "汉书",
  "后汉书",
  "三国志",
  "资治通鉴",
  "战国策",
  "国语",
  "楚辞",
  "文选",
  "古文观止",
  "唐诗",
  "宋词",
  "元曲",
  "全唐诗",
  "全宋词",
  "世说新语",
  "文心雕龙",
  "道德经",
  "老子",
  "庄子",
  "列子",
  "荀子",
  "韩非子",
  "墨子",
  "孙子兵法",
  "三十六计",
  "山海经",
  "水经注",
  "本草纲目",
  "黄帝内经",
  "红楼梦",
  "三国演义",
  "水浒传",
  "西游记",
  "儒林外史",
  "聊斋志异",
  "金瓶梅",
  "封神演义",
  "西厢记",
  "牡丹亭",
  "桃花扇",
  "长生殿",
  "镜花缘",
  "老残游记",
  "官场现形记",
];

export function looksLikeClassic(title: string): boolean {
  const compact = title.replace(/\s+/g, "");
  return CLASSIC_KEYWORDS.some((keyword) => compact.includes(keyword));
}
