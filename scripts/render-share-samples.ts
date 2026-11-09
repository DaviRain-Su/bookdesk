// 用和桌面端同一套排版画出样张。只读本地字体，不访问网络。
// 用法：node --experimental-strip-types scripts/render-share-samples.ts [输出目录]
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { layoutShareCard, layoutToSVG, type ShareCardInput } from "../src/lib/shareCard.ts";

const CJK_SHORT = "庭有枇杷树，吾妻死之年所手植也，今已亭亭如盖矣。";
const CJK_LONG =
  "夜读宜静。灯只照到这一页，窗外的雨声比书页翻动还轻。有些句子不该被截成广告，它要留出空白，让人自己停下来。书桌不替你发表，它只是把你已经划下的字，排成一张可以带走的卡片。长的段落会换行，放不下的地方用省略号收住，而不是把字挤出卡片边缘。";
const EN_SHORT = "The quiet hours are the ones that stay.";
const EN_LONG =
  "Reading is a private room with the door left ajar. A long sentence should wrap on spaces rather than slicing ordinary words in half, and when the thought is longer than the card there is an ellipsis instead of a spill into the margin. Nothing here is posted anywhere. The picture stays on this machine until you save it.";

const samples: { name: string; input: ShareCardInput }[] = [
  {
    name: "cjk-short-light-square",
    input: {
      quote: CJK_SHORT,
      title: "项脊轩志",
      author: "归有光",
      chapter: "正文",
      template: "light",
      ratio: "square",
      color: "yellow",
    },
  },
  {
    name: "cjk-long-light-portrait",
    input: {
      quote: CJK_LONG.repeat(4),
      title: "夜读",
      author: "书桌",
      chapter: "灯下",
      template: "light",
      ratio: "portrait",
      color: "green",
    },
  },
  {
    name: "en-short-dark-square",
    input: {
      quote: EN_SHORT,
      title: "A Desk",
      author: "bookdesk",
      template: "dark",
      ratio: "square",
      color: "blue",
    },
  },
  {
    name: "en-long-dark-portrait",
    input: {
      quote: [EN_LONG, EN_LONG, EN_LONG].join(" "),
      title: "Margins",
      author: "bookdesk",
      chapter: "Notes",
      template: "dark",
      ratio: "portrait",
    },
  },
  {
    name: "cjk-short-vertical-square",
    input: {
      quote: CJK_SHORT,
      title: "项脊轩志",
      author: "归有光",
      chapter: "正文",
      template: "vertical",
      ratio: "square",
      color: "pink",
    },
  },
  {
    name: "cjk-note-vertical-portrait",
    input: {
      quote: "有些话只想留在自己的书页边上。",
      note: "这句话的笔记是我勾过才放上来的。没勾的时候，卡片上不该有它。",
      title: "灯下",
      author: "书桌",
      chapter: "旁注",
      template: "vertical",
      ratio: "portrait",
      color: "purple",
    },
  },
];

const outDir = process.argv[2] || "/tmp/share-cards";
mkdirSync(outDir, { recursive: true });

for (const sample of samples) {
  const layout = layoutShareCard(sample.input);
  const svg = layoutToSVG(layout);
  const html = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<style>
  html, body { margin: 0; padding: 0; width: ${layout.width}px; height: ${layout.height}px; overflow: hidden; background: transparent; }
  svg { display: block; }
</style>
</head>
<body>
${svg}
</body>
</html>
`;
  writeFileSync(join(outDir, `${sample.name}.svg`), svg);
  writeFileSync(join(outDir, `${sample.name}.html`), html);
  console.log(
    `${sample.name} ${layout.width}x${layout.height} quoteLines=${layout.quoteLines.length} truncated=${layout.quoteTruncated} note=${layout.noteLines.length > 0}`,
  );
}
