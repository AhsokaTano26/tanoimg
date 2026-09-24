export function createUploadQueue({ concurrency, upload, onProgress, onResult, onIdle }) {
  if (!Number.isInteger(concurrency) || concurrency < 1) throw new Error('concurrency must be positive');
  const items = [];
  const controllers = new Set();
  let head = 0;
  let active = 0;
  let total = 0;
  let completed = 0;
  let failed = 0;
  let cancelled = 0;
  let stopped = false;
  let batchOpen = false;

  const snapshot = () => ({ total, completed, failed, cancelled, active, pending: items.length - head, stopped });
  const emitProgress = () => onProgress?.(snapshot());

  function pump() {
    while (!stopped && active < concurrency && head < items.length) {
      const file = items[head];
      items[head++] = undefined; // Release each File reference as soon as it starts.
      const controller = new AbortController();
      controllers.add(controller);
      active++;
      Promise.resolve().then(() => upload(file, controller.signal)).then(
        value => {
          if (controller.signal.aborted) cancelled++;
          else onResult?.(file, value, null);
        },
        error => {
          if (controller.signal.aborted) cancelled++;
          else { failed++; onResult?.(file, null, error); }
        },
      ).finally(() => {
        controllers.delete(controller);
        active--;
        completed++;
        emitProgress();
        pump();
      });
    }
    if (batchOpen && active === 0 && head === items.length) {
      batchOpen = false;
      items.length = 0;
      head = 0;
      onIdle?.(snapshot());
    }
  }

  return {
    add(files) {
      if (stopped && batchOpen) return false;
      if (!batchOpen) {
        total = completed = failed = cancelled = 0;
        stopped = false;
        batchOpen = true;
      }
      for (const file of files) { items.push(file); total++; }
      emitProgress();
      pump();
      return true;
    },
    cancel() {
      if (!batchOpen || stopped) return;
      stopped = true;
      const dropped = items.length - head;
      for (let index = head; index < items.length; index++) items[index] = undefined;
      head = items.length;
      completed += dropped;
      cancelled += dropped;
      for (const controller of controllers) controller.abort();
      emitProgress();
      pump();
    },
    snapshot,
  };
}
