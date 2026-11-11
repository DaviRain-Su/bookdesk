import { expect, test } from "bun:test";
import { pageTurnForKey } from "../src/lib/pageKeys";

const key = (value: string, options: Partial<KeyboardEvent> = {}) =>
  ({ key: value, ...options }) as KeyboardEvent;

test("standard page keys and shifted space", () => {
  for (const value of ["ArrowRight", "PageDown", " ", "Spacebar"]) {
    expect(pageTurnForKey(key(value))).toBe("next");
  }
  for (const value of ["ArrowLeft", "PageUp"]) {
    expect(pageTurnForKey(key(value))).toBe("prev");
  }
  expect(pageTurnForKey(key(" ", { shiftKey: true }))).toBe("prev");
});

test("modifiers, composition and consumed keys keep their existing behavior", () => {
  for (const options of [
    { metaKey: true }, { ctrlKey: true }, { altKey: true }, { shiftKey: true },
    { isComposing: true }, { defaultPrevented: true },
  ]) {
    expect(pageTurnForKey(key("ArrowRight", options))).toBeNull();
  }
  expect(pageTurnForKey(key("Enter"))).toBeNull();
});
