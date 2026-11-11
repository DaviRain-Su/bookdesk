// 在 Vite 中打开 /tests/browser-regression.html；只使用内存中的假数据，不访问本机书库或 AI。
import React from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import "../src/styles.css";
import { DEFAULT_READER_SETTINGS } from "../src/components/reader/settings";

const source = new URLSearchParams(location.search).get("baseline")
  ? "/.mygo/qa/baseline/src"
  : "/src";
const { default: App } = await import(/* @vite-ignore */ `${source}/App.tsx`);
const { default: AskPanel } = await import(/* @vite-ignore */ `${source}/components/AskPanel.tsx`);
const listeners = new Map();
let calls = [];
let turns = [];
let failBoard = false;
let failCard = false;
let failProgress = false;
let boardSequence = 0;
let boards = [];

function emit(name, detail) {
  window.dispatchEvent(new CustomEvent(name, { detail }));
}

window.mygo = {
  on(name, listener) {
    const group = listeners.get(name) ?? new Set();
    listeners.set(name, group);
    group.add(listener);
    return () => group.delete(listener);
  },
  async call(method, ...args) {
    calls.push({ method, args });
    switch (method) {
      case "Ai.Ask":
        return new Promise((resolve, reject) => turns.push({ input: args[0], resolve, reject }));
      case "Ai.Cancel": return;
      case "Library.List": return [{ id: "test-book", title: "回归测试书", author: null, coverRelPath: null, collection: null }];
      case "Library.EpubURL": return new Promise(() => {});
      case "Notes.ListHighlights":
      case "Notes.ListCards":
      case "Characters.List": return [];
      case "Notes.SaveCard":
        if (failCard) throw new Error("模拟卡片写入失败");
        return { ...args[0], id: args[0].id ?? crypto.randomUUID() };
      case "Board.List": return boards.map((b) => ({ ...b, folder: null }));
      case "Board.Load": return structuredClone(boards.find((b) => b.id === args[0]) ?? null);
      case "Board.Save":
        if (failBoard) throw new Error("模拟磁盘写入失败");
        boards = boards.map((b) => b.id === args[0].id ? structuredClone(args[0]) : b);
        return;
      case "Board.Delete": throw new Error("模拟删除失败");
      case "Reading.GetSettings": return { ...DEFAULT_READER_SETTINGS };
      case "Reading.GetState": return { lastMode: "desk", lastBookId: "" };
      case "Reading.GetProgress": return null;
      case "Reading.SaveProgress":
        if (failProgress) throw new Error("模拟进度写入失败");
        return;
      case "Reading.SaveSettings":
      case "Reading.SetLastMode":
      case "Reading.SetLastBook": return;
      default: throw new Error(`未声明的测试调用：${method}`);
    }
  },
};

let root = createRoot(document.getElementById("root"));
const render = (component) => flushSync(() => root.render(component));
const frame = () => new Promise((resolve) => setTimeout(resolve, 0));
async function until(predicate, label) {
  const deadline = performance.now() + 5000;
  do {
    if (predicate()) return;
    await frame();
  } while (performance.now() < deadline);
  throw new Error(`等待失败：${label}`);
}
function assert(condition, message) {
  if (!condition) throw new Error(message);
}
function clicked(selector) {
  const element = document.querySelector(selector);
  assert(element, `找不到 ${selector}`);
  flushSync(() => element.click());
}
const matching = (method) => calls.filter((call) => call.method === method);
function reset() {
  flushSync(() => root.unmount());
  root = createRoot(document.getElementById("root"));
  calls = [];
  turns = [];
  failBoard = false;
  failCard = false;
  failProgress = false;
}
async function mountApp() {
  reset();
  boardSequence += 1;
  boards = [1, 2].map((n) => ({
    id: `test-${boardSequence}-${n}`, name: `测试画布 ${n}`,
    viewportJson: '{"x":0,"y":0,"zoom":1}', nodesJson: "[]", edgesJson: "[]", updatedAt: 0,
  }));
  render(React.createElement(App));
  await until(() => document.querySelector(".react-flow"), "画布挂载");
  await frame();
  flushSync(() => {});
}

const results = [];
async function check(name, run) {
  try {
    await run();
    results.push({ name, ok: true });
  } catch (error) {
    results.push({ name, ok: false, error: String(error) });
  }
  document.getElementById("results").textContent = JSON.stringify(results, null, 2);
}

await check("切换选区后旧回复和旧 finally 不覆盖新请求", async () => {
  const request = (selection, mode) => ({ bookId: "test-book", bookTitle: "测试书", selection, mode });
  render(React.createElement(AskPanel, { request: request("第一段", "explain"), onClose() {} }));
  await until(() => turns.length === 1, "第一个 AI 请求");
  const first = turns[0];
  render(React.createElement(AskPanel, { request: request("第二段", "translate"), onClose() {} }));
  await until(() => turns.length === 2, "第二个 AI 请求自动发送");
  const second = turns[1];
  for (const listener of listeners.get("acp:update") ?? []) {
    listener({ turnId: second.input.turnId, kind: "text", text: "新问题的流式文字" });
  }
  first.resolve({ ok: true, text: "旧问题的回答" });
  await frame();
  assert(!document.querySelector(".ask-drawer").textContent.includes("旧问题的回答"), "旧回复污染新面板");
  assert(document.querySelector(".ask-drawer textarea").disabled, "旧 finally 提前解除了发送状态");
  assert(document.querySelector(".is-streaming")?.textContent === "新问题的流式文字", "旧 finally 清空了新流");
  render(React.createElement(AskPanel, { request: request("第二段", "translate"), onClose() {} }));
  assert(document.querySelector(".ask-drawer textarea").disabled, "相同请求对象重建时丢失发送状态");
  second.resolve({ ok: true, text: "第二段译文" });
  await until(() => document.querySelector(".ask-drawer").textContent.includes("第二段译文"), "新回复完成");
  assert(matching("Ai.Cancel").some((call) => call.args[0] === first.input.turnId), "切换时未取消旧请求");
});

await check("关闭 AI 面板会取消请求，重新打开可再次解释", async () => {
  reset();
  const request = { bookId: "test-book", bookTitle: "测试书", selection: "重新解释", mode: "explain" };
  render(React.createElement(AskPanel, { request, onClose() {} }));
  await until(() => turns.length === 1, "解释请求");
  const old = turns[0];
  render(React.createElement(AskPanel, { request: null, onClose() {} }));
  assert(matching("Ai.Cancel").some((call) => call.args[0] === old.input.turnId), "关闭没有取消请求");
  render(React.createElement(AskPanel, { request, onClose() {} }));
  await until(() => turns.length === 2, "重新解释");
  old.reject(new Error("旧请求迟到的错误"));
  await frame();
  assert(!document.querySelector(".ask-drawer").textContent.includes("迟到"), "迟到的错误污染新会话");
  assert(document.querySelector(".ask-drawer textarea").disabled, "迟到的错误结束了新会话");
  turns[1].resolve({ ok: true, text: "重新解释完成" });
});

await check("画布保存失败有提示，恢复后失焦重试最新内容", async () => {
  await mountApp();
  failBoard = true;
  emit("bookdesk:add-text-card");
  await until(() => document.querySelector(".text-card"), "新卡片");
  window.dispatchEvent(new Event("blur"));
  await until(() => document.querySelector(".toasts").textContent.includes("画布未能保存"), "保存失败提示");
  failBoard = false;
  window.dispatchEvent(new Event("blur"));
  await until(() => JSON.parse(boards[0].nodesJson).length === 1, "画布重试保存");
  failCard = true;
  const node = JSON.parse(boards[0].nodesJson)[0];
  emit("bookdesk:update-node", { nodeId: node.id, patch: { body: "修改笔记" } });
  await until(() => document.querySelector(".toasts").textContent.includes("笔记卡未能保存"), "卡片失败提示");
});

await check("删除失败后当前画布继续保存", async () => {
  await mountApp();
  clicked(".board-title");
  clicked('.board-menu-row [title="删除画布"]');
  clicked(".board-menu-confirm");
  await until(() => document.querySelector(".toasts").textContent.includes("删除画布失败"), "删除失败提示");
  emit("bookdesk:add-text-card");
  await until(() => document.querySelector(".text-card"), "删除失败后编辑");
  window.dispatchEvent(new Event("blur"));
  await until(() => JSON.parse(boards[0].nodesJson).length === 1, "删除失败后的保存恢复");
});

await check("进度在防抖结束前 pagehide 保存，写入失败可重试", async () => {
  await mountApp();
  emit("bookdesk:book-progress", { bookId: "book-progress", cfi: "old", spineIndex: 1 });
  emit("bookdesk:book-progress", { bookId: "book-progress", cfi: "latest", spineIndex: 2 });
  window.dispatchEvent(new Event("pagehide"));
  assert(matching("Reading.SaveProgress").at(-1)?.args[0].cfi === "latest", "离开时未保存最新进度");
  failProgress = true;
  emit("bookdesk:book-progress", { bookId: "book-progress", cfi: "retry", spineIndex: 3 });
  window.dispatchEvent(new Event("blur"));
  await until(() => document.querySelector(".toasts").textContent.includes("阅读进度未能保存"), "进度失败提示");
  failProgress = false;
  const count = matching("Reading.SaveProgress").length;
  window.dispatchEvent(new Event("blur"));
  await until(() => matching("Reading.SaveProgress").length > count, "进度重试");
  assert(matching("Reading.SaveProgress").at(-1).args[0].cfi === "retry", "重试丢失了最新进度");
});

await check("阅读设置和进度在卸载前保存最后一次改动", async () => {
  await mountApp();
  emit("bookdesk:open-reader", { bookId: "test-book" });
  await until(() => document.querySelector('.reader-tool[title="阅读设置"]'), "阅读设置入口");
  clicked('.reader-tool[title="阅读设置"]');
  clicked('[title="大一号"]');
  emit("bookdesk:book-progress", { bookId: "book-progress", cfi: "last", spineIndex: 4 });
  flushSync(() => root.unmount());
  assert(matching("Reading.SaveSettings").at(-1)?.args[0].fontSize === DEFAULT_READER_SETTINGS.fontSize + 1, "最后一次设置被丢弃");
  assert(matching("Reading.SaveProgress").at(-1)?.args[0].cfi === "last", "最后一次进度被丢弃");
  root = createRoot(document.getElementById("root"));
});

reset();
window.regression = { done: true, results, passed: results.filter((r) => r.ok).length, total: results.length };
document.getElementById("results").textContent = JSON.stringify(window.regression, null, 2);
