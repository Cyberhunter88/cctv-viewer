// Pure decisions for the playback tile, kept separate so they can be unit tested.
export const WRITING_RETRY_MS = 5000;
export const WRITING_MAX_RETRIES = 24;

export type LoadIssueKind = 'writing' | 'busy' | 'timeout' | 'generic';
export interface LoadIssue { kind: LoadIssueKind; message: string }

/** Maps an HTTP status from a failed load to a user-facing state. */
export function classifyLoadIssue(status: number | undefined, fallback: string): LoadIssue {
  switch (status) {
    case 409: return { kind: 'writing', message: 'Aufnahme wird noch geschrieben …' };
    case 429:
    case 503: return { kind: 'busy', message: 'Server ausgelastet' };
    case 504: return { kind: 'timeout', message: 'Umwandlung dauert zu lange' };
    default: return { kind: 'generic', message: fallback };
  }
}

export interface WritingStep { retry: boolean; attempt: number; message: string }

/** Decides whether to wait again for a file the recorder still writes. */
export function nextWritingStep(retries: number): WritingStep {
  if (retries >= WRITING_MAX_RETRIES) {
    return { retry: false, attempt: retries, message: 'Aufnahme ist noch nicht fertig geschrieben' };
  }
  const attempt = retries + 1;
  const seconds = Math.round(WRITING_RETRY_MS / 1000);
  return { retry: true, attempt, message: `Aufnahme wird noch geschrieben … nächster Versuch in ${seconds} s (${attempt}/${WRITING_MAX_RETRIES})` };
}

/** Buffering tolerance grows with playback speed, where encoding is slower. */
export function stallTimeoutMs(speed: number): number {
  return 10000 + Math.min(20000, Math.max(0, speed - 1) * 1500);
}

export interface ClipRef { id: string; cameraId: string; view?: string; start: number; duration: number }

/**
 * Finds the clip that follows `current` on the same camera and view, but only
 * when playback is close enough to its end for a warm-up to pay off.
 */
export function prefetchTarget<T extends ClipRef>(records: T[], current: ClipRef, cursor: number, speed: number, leadSeconds = 10): T | undefined {
  const end = current.start + current.duration;
  if ((end - cursor) / Math.max(speed, 0.25) > leadSeconds) {
    return undefined;
  }
  let best: T | undefined;
  for (const r of records) {
    if (r.id === current.id || r.cameraId !== current.cameraId || r.view !== current.view) {
      continue;
    }
    if (r.start >= end - 1 && r.start <= end + 5 && (!best || r.start < best.start)) {
      best = r;
    }
  }
  return best;
}

/**
 * "Original" stays the original whenever the browser can decode it. Otherwise (typically
 * H.265 in Chrome on Linux) the server converts it with the "full" profile, which keeps
 * the resolution and frame rate. Codecs the browser already refused are not tried again.
 */
export function streamQuality(selected: string, codec: string, canPlayType: (type: string) => string, unsupported: ReadonlySet<string>): string {
  if (selected !== 'native') return selected;
  if (unsupported.has(codec)) return 'full';
  return codecPlayable(codec, canPlayType) === false ? 'full' : 'native';
}
const codecTypes: Record<string, string[]> = {
  h264: ['video/mp4; codecs="avc1.42E01E"', 'video/mp4; codecs="avc1.640028"'],
  hevc: ['video/mp4; codecs="hvc1.1.6.L93.B0"', 'video/mp4; codecs="hev1.1.6.L93.B0"'],
};
/** Whether the browser reports it can decode the codec; undefined for codecs it cannot be asked about. */
export function codecPlayable(codec: string, canPlayType: (type: string) => string): boolean | undefined {
  const types = codecTypes[codec];
  return types ? types.some(type => canPlayType(type) !== '') : undefined;
}
/**
 * A native "source not supported" error only means an undecodable codec when the browser
 * does not claim support for it. Otherwise it is transient, for example an aborted request
 * while a new source starts (WebKit), and the original is retried instead of converted.
 */
export function nativeErrorConverts(codec: string, canPlayType: (type: string) => string): boolean {
  return codecPlayable(codec, canPlayType) !== true;
}
