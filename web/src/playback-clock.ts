interface PlaybackClockOptions {
  advance: (seconds: number) => void;
  synchronize: () => void;
  render: () => void;
  now?: () => number;
  requestFrame?: (callback: FrameRequestCallback) => number;
  cancelFrame?: (id: number) => void;
}

export function createPlaybackClock(options: PlaybackClockOptions) {
  const now = options.now ?? (() => performance.now());
  const request = options.requestFrame ?? requestAnimationFrame;
  const cancel = options.cancelFrame ?? cancelAnimationFrame;
  let running = false;
  let frame: number | undefined;
  let previous = 0;
  let synchronized = 0;
  let rendered = 0;

  function tick(time: number) {
    frame = undefined;
    if (!running) return;
    const seconds = Math.max(0, time - previous) / 1000;
    previous = time;
    options.advance(seconds);
    if (!running) return;
    if (time - synchronized >= 50) {
      synchronized = time;
      options.synchronize();
    }
    if (!running) return;
    if (time - rendered >= 100) {
      rendered = time;
      options.render();
    }
    if (running) frame = request(tick);
  }

  return {
    start() {
      if (running) return;
      running = true;
      previous = synchronized = rendered = now();
      frame = request(tick);
    },
    stop() {
      running = false;
      if (frame !== undefined) cancel(frame);
      frame = undefined;
    },
    reset() {
      previous = synchronized = rendered = now();
    },
  };
}
