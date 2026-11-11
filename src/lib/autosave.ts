/** 合并连续编辑，串行写入；失败时保留最新内容，供下一次编辑或离开页面时重试。 */
export function createAutosave<T>(
  save: (value: T) => Promise<unknown>,
  onError: (error: unknown) => void,
  delay: number,
) {
  let pending: { value: T } | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let inFlight: Promise<void> | null = null;

  const clearTimer = () => {
    if (timer !== null) clearTimeout(timer);
    timer = null;
  };

  const flush = (): Promise<void> => {
    clearTimer();
    if (inFlight) return inFlight;
    if (!pending) return Promise.resolve();
    inFlight = (async () => {
      while (pending) {
        const entry = pending;
        pending = null;
        try {
          await save(entry.value);
        } catch (error) {
          pending ??= entry;
          onError(error);
          return;
        }
      }
    })().finally(() => {
      inFlight = null;
    });
    return inFlight;
  };

  return {
    schedule(value: T) {
      pending = { value };
      clearTimer();
      timer = setTimeout(() => void flush(), delay);
    },
    flush,
  };
}

/** 失焦或离开页面前提交尚未经过防抖计时的改动。 */
export function flushOnLeave(flush: () => void): () => void {
  const onHidden = () => {
    if (document.visibilityState === "hidden") flush();
  };
  window.addEventListener("blur", flush);
  window.addEventListener("pagehide", flush);
  document.addEventListener("visibilitychange", onHidden);
  return () => {
    window.removeEventListener("blur", flush);
    window.removeEventListener("pagehide", flush);
    document.removeEventListener("visibilitychange", onHidden);
    flush();
  };
}
