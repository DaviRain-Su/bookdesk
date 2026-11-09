import assert from "node:assert/strict";
import { test } from "node:test";
import {
  FOOTER_TEXT,
  chapterFromSource,
  charUnits,
  columnize,
  findCardNote,
  layoutProblems,
  selectAllQuoteIds,
  layoutShareCard,
  layoutToSVG,
  normalizeQuote,
  verticalGlyphs,
  wrapText,
  type ShareCardInput,
} from "./shareCard.ts";

const CJK_SHORT = "庭有枇杷树，吾妻死之年所手植也，今已亭亭如盖矣。";
const CJK_LONG = Array.from({ length: 28 }, () => "夜读宜静，灯火只照到这一页。").join("");
const EN_SHORT = "The quiet hours are the ones that stay.";
const EN_LONG =
  "Reading is a private room with the door left ajar, and a long sentence should wrap on spaces rather than slicing ordinary words in half whenever the line runs out of room before the thought does.";
const PRIVATE_NOTE = "这是不会公开的私记XYZ";

function base(over: Partial<ShareCardInput> = {}): ShareCardInput {
  return {
    quote: CJK_SHORT,
    title: "项脊轩志",
    author: "归有光",
    chapter: "正文",
    template: "light",
    ratio: "square",
    ...over,
  };
}

test("english wraps on word boundaries", () => {
  const wrapped = wrapText("alpha beta gamma delta", 6, 4);
  assert.equal(wrapped.truncated, false);
  for (const line of wrapped.lines) {
    assert.ok(charUnits(line) <= 6.05, line);
    assert.equal(line.includes("alphabeta"), false);
  }
  assert.ok(wrapped.lines.length >= 2);
  assert.equal(wrapped.lines.join(" ").replace(/\s+/g, " "), "alpha beta gamma delta");
});

test("a long english word is broken only when it cannot fit", () => {
  const wrapped = wrapText("supercalifragilistic", 4, 6);
  assert.ok(wrapped.lines.length >= 2);
  for (const line of wrapped.lines) assert.ok(charUnits(line) <= 4.05, line);
  assert.equal(wrapped.lines.join(""), "supercalifragilistic");
});

test("cjk closing punctuation does not start a line", () => {
  const wrapped = wrapText("一二三，四五", 3, 4);
  assert.equal(wrapped.truncated, false);
  assert.equal(wrapped.lines[0]?.startsWith("，"), false);
  assert.ok(wrapped.lines[0]?.endsWith("，"), wrapped.lines.join("|"));
  for (const line of wrapped.lines.slice(1)) {
    assert.equal(line.startsWith("，"), false);
  }
});

test("opening punctuation is not stranded at the end of a line", () => {
  const wrapped = wrapText("一二三「四五」", 3, 4);
  assert.equal(wrapped.lines[0]?.endsWith("「"), false, wrapped.lines.join("|"));
  const joined = wrapped.lines.join("");
  assert.ok(joined.includes("「"));
  assert.equal(joined.includes("「四五"), true);
});

test("long text truncates with an ellipsis inside the measure", () => {
  const wrapped = wrapText(CJK_LONG, 12, 3);
  assert.equal(wrapped.truncated, true);
  assert.equal(wrapped.lines.length, 3);
  assert.ok(wrapped.lines[2]?.endsWith("…"));
  for (const line of wrapped.lines) assert.ok(charUnits(line) <= 12.05, `${line} ${charUnits(line)}`);
});

test("short text is not truncated", () => {
  const wrapped = wrapText(EN_SHORT, 40, 4);
  assert.equal(wrapped.truncated, false);
  assert.equal(wrapped.lines.join(" "), EN_SHORT);
});

test("vertical columns keep cjk and drop spaces beside han", () => {
  assert.deepEqual(verticalGlyphs("甲 乙丙"), ["甲", "乙", "丙"]);
  const cols = columnize(CJK_LONG, 8, 2);
  assert.equal(cols.truncated, true);
  assert.equal(cols.lines.length, 2);
  assert.ok(cols.lines[1]?.endsWith("…"));
  for (const col of cols.lines) assert.ok(Array.from(col).length <= 8);
});

test("portrait holds more quote than square, and a note takes room", () => {
  const square = layoutShareCard(base({ quote: CJK_LONG, template: "light", ratio: "square" }));
  const portrait = layoutShareCard(base({ quote: CJK_LONG, template: "light", ratio: "portrait" }));
  const withNote = layoutShareCard(
    base({ quote: CJK_LONG, template: "light", ratio: "square", note: PRIVATE_NOTE.repeat(8) }),
  );
  assert.ok(portrait.quoteLines.length > square.quoteLines.length);
  assert.ok(withNote.quoteLines.length < square.quoteLines.length);
  assert.equal(square.quoteTruncated, true);
  assert.equal(withNote.noteLines.length > 0, true);
});

test("vertical portrait fits more characters per column", () => {
  const square = layoutShareCard(base({ template: "vertical", ratio: "square", quote: CJK_LONG }));
  const portrait = layoutShareCard(
    base({ template: "vertical", ratio: "portrait", quote: CJK_LONG }),
  );
  assert.ok(portrait.quoteMaxUnits > square.quoteMaxUnits);
  assert.equal(square.quoteTruncated, true);
  assert.ok(square.paints.some((paint) => paint.kind === "vcolumn" && paint.role === "quote"));
});

test("note is omitted unless provided, and the footer stays put", () => {
  const plain = layoutShareCard(base({ note: "   " }));
  const noted = layoutShareCard(base({ note: PRIVATE_NOTE }));
  assert.equal(plain.noteLines.length, 0);
  assert.equal(
    plain.paints.some((paint) => paint.kind !== "rect" && paint.role === "note"),
    false,
  );
  assert.ok(noted.noteLines.join("").includes("私记XYZ"));
  assert.equal(plain.footer, FOOTER_TEXT);
  assert.equal(noted.footer, FOOTER_TEXT);
  assert.equal(layoutToSVG(plain).includes("私记XYZ"), false);
  assert.equal(layoutToSVG(noted).includes("私记XYZ"), true);
});

test("cards stay inside the frame for short and long quotes", () => {
  const cases: ShareCardInput[] = [
    base(),
    base({ template: "dark", ratio: "portrait", quote: EN_LONG, note: "A margin note." }),
    base({ template: "vertical", ratio: "square", quote: CJK_SHORT }),
    base({
      template: "vertical",
      ratio: "portrait",
      quote: CJK_LONG,
      note: "读到这里停了一下。灯还亮着。",
    }),
    base({ template: "light", ratio: "square", quote: EN_SHORT, author: "", chapter: "" }),
    base({ template: "dark", ratio: "square", quote: "", title: "" }),
  ];
  for (const input of cases) {
    const layout = layoutShareCard(input);
    const problems = layoutProblems(layout);
    assert.deepEqual(problems, [], `${input.template}/${input.ratio} ${problems.join(",")}`);
    if (input.ratio === "square") {
      assert.equal(layout.width, 1080);
      assert.equal(layout.height, 1080);
    } else {
      assert.equal(layout.width, 1080);
      assert.equal(layout.height, 1440);
    }
  }
});

test("svg sample has the quote and no external resource", () => {
  const layout = layoutShareCard(
    base({ quote: `见 <img src="https://example.com/a.png"> 与 ${CJK_SHORT}` }),
  );
  const svg = layoutToSVG(layout);
  assert.equal(svg.includes("<img"), false);
  assert.equal(svg.includes("<script"), false);
  assert.equal(svg.includes("@import"), false);
  assert.equal(svg.includes("url("), false);
  assert.equal(/<[^>]*\s(?:src|href)=/i.test(svg), false);
  assert.ok(svg.includes("枇杷树"));
  assert.ok(svg.includes("&lt;img"));
  assert.ok(svg.includes(FOOTER_TEXT));
});

test("dark and light backgrounds differ", () => {
  const light = layoutShareCard(base({ template: "light" }));
  const dark = layoutShareCard(base({ template: "dark" }));
  const fill = (layout: ReturnType<typeof layoutShareCard>) =>
    layout.paints.find((paint) => paint.kind === "rect" && paint.w === layout.width)?.fill;
  assert.notEqual(fill(light), fill(dark));
});

test("chapter is optional and parsed from a card source line", () => {
  const layout = layoutShareCard(base({ chapter: null, author: "归有光" }));
  const author = layout.paints.find((paint) => paint.kind === "text" && paint.role === "author");
  assert.equal(author && author.kind === "text" ? author.text : "", "归有光");
  assert.equal(chapterFromSource("项脊轩志 · 正文", "项脊轩志"), "正文");
  assert.equal(chapterFromSource("项脊轩志 · 卷一 · 手植", "项脊轩志"), "卷一 · 手植");
  assert.equal(chapterFromSource("只有书名", "只有书名"), null);
});

test("select all quotes leaves notes unticked", () => {
  const choice = selectAllQuoteIds(["h1", "h2"]);
  assert.deepEqual(choice.quoteIds, ["h1", "h2"]);
  assert.deepEqual(choice.noteIds, []);
});

test("card notes match the book and the quote, newest body wins", () => {
  const cards = [
    { quote: "  甲\n乙  ", body: "旧笔记", sourceBookId: "b1", updatedAt: 1 },
    { quote: "甲 乙", body: "新笔记", sourceBookId: "b1", updatedAt: 5 },
    { quote: "甲 乙", body: "别人的书", sourceBookId: "b2", updatedAt: 9 },
    { quote: "别的句子", body: PRIVATE_NOTE, sourceBookId: "b1", updatedAt: 9 },
  ];
  assert.equal(normalizeQuote(" 甲\n乙 "), "甲 乙");
  assert.equal(findCardNote(cards, "b1", "甲 乙"), "新笔记");
  assert.equal(findCardNote(cards, "b1", "没有"), null);
  assert.equal(findCardNote(cards, "b2", "甲 乙"), "别人的书");
});
