import { describe, expect, test } from "bun:test";
import { createAutosave } from "../src/lib/autosave";

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

describe("autosave", () => {
  test("flush saves the latest edit before its debounce expires", async () => {
    const saved: number[] = [];
    const saver = createAutosave(async (value: number) => { saved.push(value); }, () => {}, 500);
    saver.schedule(1);
    saver.schedule(2);
    await saver.flush();
    expect(saved).toEqual([2]);
    await saver.flush();
    expect(saved).toEqual([2]);
  });

  test("overlapping flushes serialize writes and keep the latest edit", async () => {
    const first = deferred();
    const saved: number[] = [];
    const saver = createAutosave(async (value: number) => {
      saved.push(value);
      if (value === 1) await first.promise;
    }, () => {}, 500);
    saver.schedule(1);
    const flushing = saver.flush();
    saver.schedule(2);
    saver.schedule(3);
    const overlapping = saver.flush();
    expect(saved).toEqual([1]);
    first.resolve();
    await Promise.all([flushing, overlapping]);
    expect(saved).toEqual([1, 3]);
  });

  test("failed edits remain pending for retry", async () => {
    const saved: string[] = [];
    const errors: unknown[] = [];
    let fail = true;
    const saver = createAutosave(async (value: string) => {
      if (fail) throw new Error("disk full");
      saved.push(value);
    }, (error) => errors.push(error), 500);
    saver.schedule("笔记");
    await saver.flush();
    expect(errors).toHaveLength(1);
    expect(saved).toEqual([]);
    fail = false;
    await saver.flush();
    expect(saved).toEqual(["笔记"]);
  });

  test("a failed older write cannot replace a newer pending edit", async () => {
    const first = deferred();
    const saved: string[] = [];
    const saver = createAutosave(async (value: string) => {
      saved.push(value);
      if (value === "old") await first.promise;
    }, () => {}, 500);
    saver.schedule("old");
    const flushing = saver.flush();
    saver.schedule("new");
    first.reject(new Error("disk full"));
    await flushing;
    await saver.flush();
    expect(saved).toEqual(["old", "new"]);
  });
});
