// 真正渲染生成的 EPUB，使用实际书卡、全屏阅读和 epub.js；不访问用户书库。
import React from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import JSZip from "jszip";
import "../src/styles.css";
import { DEFAULT_READER_SETTINGS } from "../src/components/reader/settings";

// 嵌入式测试浏览器的面板收起时视口为零，不产生动画帧。
// 保留真实 DOM 排版，仅让这种环境中的 epub.js 队列仍能推进。
if (document.documentElement.clientWidth === 0) {
  window.requestAnimationFrame = (callback) => window.setTimeout(() => callback(performance.now()), 0);
  window.cancelAnimationFrame = (id) => window.clearTimeout(id);
}
window.addEventListener("unhandledrejection", (event) => {
  window.regression = { done: true, error: String(event.reason) };
  document.getElementById("results").textContent = JSON.stringify(window.regression);
});

const source = new URLSearchParams(location.search).get("baseline")
  ? "/.mygo/qa/baseline/src" : "/src";
const { default: ReaderView } = await import(/* @vite-ignore */ `${source}/components/reader/ReaderView.tsx`);
const { default: CanvasBoard } = await import(/* @vite-ignore */ `${source}/components/CanvasBoard.tsx`);

const zip = new JSZip();
zip.file("mimetype", "application/epub+zip", { compression: "STORE" });
zip.file("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`);
zip.file("book.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="id">keyboard-test</dc:identifier><dc:title>翻页测试</dc:title><dc:language>zh</dc:language></metadata><manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter"/></spine></package>`);
zip.file("nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>目录</title></head><body><nav epub:type="toc"><ol><li><a href="chapter.xhtml">翻页测试</a></li></ol></nav></body></html>`);
zip.file("chapter.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>翻页测试</title></head><body><h1>翻页测试</h1>${Array.from({ length: 100 }, (_, i) => `<p>第${i + 1}段。${"这是用于检查键盘翻页的正文，翻页后阅读位置应当发生变化。".repeat(6)}</p>`).join("")}</body></html>`);
const url = URL.createObjectURL(await zip.generateAsync({ type: "blob", mimeType: "application/epub+zip" }));
window.mygo = {
  on() { return () => {}; },
  async call(method) {
    if (method === "Library.EpubURL") return url;
    if (method === "Notes.ListHighlights" || method === "Characters.List") return [];
    if (method === "Board.Save") return;
    throw new Error(`未声明的测试调用：${method}`);
  },
};

const positions = new Map();
window.addEventListener("bookdesk:book-progress", (event) => {
  positions.set(event.detail.nodeId ?? "reader", event.detail.cfi);
});
const root = createRoot(document.getElementById("root"));
const frame = () => new Promise((resolve) => setTimeout(resolve, 0));
const assert = (condition, message) => { if (!condition) throw new Error(message); };
async function until(predicate, label) {
  const deadline = performance.now() + 10000;
  while (!predicate()) {
    if (performance.now() > deadline) throw new Error(`等待失败：${label}`);
    await frame();
  }
}
const node = (id, x, selected) => ({
  id, type: "book", selected, position: { x, y: 0 }, style: { width: 640 },
  data: { bookId: id, title: `测试书 ${id}` },
});
const cardProps = {
  tool: "select", board: { id: "keyboard-board", name: "键盘测试" },
  initialNodes: [node("book-a", 0, true), node("book-b", 700, false)],
  initialEdges: [], initialViewport: { x: 0, y: 0, zoom: 0.8 },
  hasStoredViewport: true, hasBooks: true,
  onAddBook() {}, onOpenLibrary() {}, onSaveError(message) { throw new Error(message); },
};
function Scene({ fullscreen = false }) {
  return React.createElement("div", { className: "app" },
    React.createElement("div", { className: "app-body", style: { position: "relative" } },
      React.createElement(CanvasBoard, cardProps),
      fullscreen ? React.createElement(ReaderView, {
        book: { id: "book-a", title: "翻页测试", author: null },
        settings: DEFAULT_READER_SETTINGS, initialCfi: null, initialSpineIndex: 0,
        initialPercent: 0, initialReveal: null, onSettingsChange() {}, onClose() {},
        onProgress(progress) { positions.set("reader", progress.cfi); },
        onOpenExcerpts() {}, onOpenSearch() {},
      }) : null,
    ),
  );
}
const render = (fullscreen) => flushSync(() => root.render(React.createElement(Scene, { fullscreen })));
const card = () => document.querySelector('[data-id="book-a"]');
const pageDoc = (scope) => scope.querySelector("iframe").contentDocument;
function key(target, value, options = {}) {
  const event = new (target.ownerDocument?.defaultView ?? window).KeyboardEvent("keydown", {
    key: value, bubbles: true, cancelable: true, ...options,
  });
  target.dispatchEvent(event);
  return event.defaultPrevented;
}
async function changed(id, action) {
  const previous = positions.get(id);
  action();
  await until(() => positions.get(id) && positions.get(id) !== previous, `${id} 翻页`);
  return positions.get(id);
}
const results = [];
async function check(name, run) {
  try { await run(); results.push({ name, ok: true }); }
  catch (error) { results.push({ name, ok: false, error: String(error) }); }
  document.getElementById("results").textContent = JSON.stringify(results, null, 2);
}

render(false);
await until(() => document.querySelectorAll(".book-card .page-button:not(:disabled)").length === 4, "两本真实 EPUB 就绪");
await frame();

await check("点过翻页按钮后方向键继续翻页，且每次只翻一页", async () => {
  const start = positions.get("book-a");
  const expected = await changed("book-a", () => card().querySelector(".page-button:last-child").click());
  await changed("book-a", () => card().querySelector(".page-button:first-child").click());
  card().querySelector(".page-button:last-child").focus();
  const other = positions.get("book-b");
  const actual = await changed("book-a", () => assert(key(document.activeElement, "ArrowRight"), "方向键未接住"));
  assert(actual === expected, "一次方向键翻了多页");
  await changed("book-a", () => key(document.activeElement, "ArrowLeft"));
  assert(positions.get("book-a") === start, "左键没有回到原页");
  assert(positions.get("book-b") === other, "另一本书被翻页");
});

await check("画布节点获得焦点时翻页，不移动节点", async () => {
  const before = card().getAttribute("style");
  card().focus();
  await changed("book-a", () => assert(key(card(), "ArrowRight"), "节点焦点未接住"));
  assert(card().getAttribute("style") === before, "方向键移动了书卡");
  await changed("book-a", () => key(card(), "ArrowLeft"));
});

await check("书页 iframe 内方向键、分页键和空格都有效", async () => {
  for (const [forward, backward, backOptions] of [
    ["ArrowRight", "ArrowLeft", {}], ["PageDown", "PageUp", {}], [" ", " ", { shiftKey: true }],
  ]) {
    const start = positions.get("book-a");
    await changed("book-a", () => assert(key(pageDoc(card()).body, forward), "书页按键未处理"));
    await changed("book-a", () => key(pageDoc(card()).body, backward, backOptions));
    assert(positions.get("book-a") === start, "反向按键没有回到原页");
  }
});

await check("输入、选字、组合键和按钮空格保持原有行为", async () => {
  const before = positions.get("book-a");
  const doc = pageDoc(card());
  const input = doc.createElement("input");
  const editable = doc.createElement("div");
  editable.contentEditable = "true";
  const child = doc.createElement("span");
  editable.append(child);
  doc.body.append(input, editable);
  assert(!key(input, "ArrowRight"), "输入框误翻页");
  assert(!key(child, "ArrowLeft"), "可编辑内容误翻页");
  assert(!key(doc.body, "ArrowRight", { shiftKey: true }), "Shift 选字被抢占");
  assert(!key(doc.body, "ArrowRight", { metaKey: true }), "系统组合键被抢占");
  assert(!key(card().querySelector(".page-button"), " "), "按钮空格被抢占");
  // 等待 epub.js 报告位置，防止只保留默认行为却仍偷偷翻页。
  await new Promise((resolve) => setTimeout(resolve, 150));
  assert(positions.get("book-a") === before, "输入或组合键改变了阅读位置");
  input.remove(); editable.remove();
});

await check("全屏外层无需点书页即可翻页，后台书卡不动", async () => {
  render(true);
  await until(() => document.querySelectorAll(".reader-overlay .page-button:not(:disabled)").length === 2 && positions.has("reader"), "全屏阅读就绪");
  const other = positions.get("book-a");
  const overlay = document.querySelector(".reader-overlay");
  overlay.focus();
  await changed("reader", () => assert(key(overlay, "ArrowRight"), "全屏方向键未处理"));
  await changed("reader", () => key(document.body, "PageDown"));
  await changed("reader", () => key(document.body, " "));
  await changed("reader", () => key(document.body, " ", { shiftKey: true }));
  await changed("reader", () => key(pageDoc(overlay).body, "ArrowLeft"));
  assert(positions.get("book-a") === other, "全屏翻页影响了后台书卡");
});

await check("设置和对话面板打开时不会误翻页", async () => {
  const overlay = document.querySelector(".reader-overlay");
  const button = overlay.querySelector('[title="阅读设置"]');
  flushSync(() => button.click());
  assert(!key(document.body, "ArrowRight"), "阅读设置打开时误翻页");
  flushSync(() => button.click());
  const drawer = document.createElement("aside");
  drawer.className = "drawer is-open";
  document.body.append(drawer);
  assert(!key(pageDoc(overlay).body, "ArrowRight"), "对话打开时书页仍翻页");
  drawer.remove();
});

flushSync(() => root.unmount());
URL.revokeObjectURL(url);
window.regression = { done: true, passed: results.filter((r) => r.ok).length, total: results.length, results };
document.getElementById("results").textContent = JSON.stringify(window.regression, null, 2);
