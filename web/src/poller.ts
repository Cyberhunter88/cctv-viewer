interface PollerOptions {
  request: (signal: AbortSignal) => Promise<void>;
  onError: (error: unknown) => void;
  schedule?: (callback: () => void, delay: number) => ReturnType<typeof setTimeout>;
  cancel?: (timer: ReturnType<typeof setTimeout>) => void;
}

// Keep even an aborted request in flight until it settles: a quick hide/show
// must not start another request while its predecessor is still unwinding.
export function createPoller(options: PollerOptions) {
  const schedule = options.schedule ?? setTimeout;
  const cancel = options.cancel ?? clearTimeout;
  let enabled = false;
  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let request: AbortController | undefined;
  let failures = 0;
  let lastError = '';

  async function poll() {
    timer = undefined;
    if (!enabled || request) return;
    const own = generation;
    const controller = request = new AbortController();
    let delay = 4000;
    try {
      await options.request(controller.signal);
      if (controller.signal.aborted || own !== generation) return;
      failures = 0;
      lastError = '';
    } catch (error) {
      if (controller.signal.aborted || own !== generation) return;
      delay = Math.min(30000, 4000 * 2 ** Math.min(++failures, 3));
      const message = error instanceof Error ? error.message : String(error);
      if (message !== lastError) {
        lastError = message;
        options.onError(error);
      }
    } finally {
      request = undefined;
      if (enabled) timer = schedule(() => void poll(), own === generation ? delay : 0);
    }
  }

  return {
    start() {
      if (enabled) return;
      enabled = true;
      generation++;
      failures = 0;
      lastError = '';
      if (!request) void poll();
    },
    stop() {
      enabled = false;
      generation++;
      if (timer !== undefined) cancel(timer);
      timer = undefined;
      request?.abort();
    },
  };
}
