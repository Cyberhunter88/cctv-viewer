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
    case 503: return { kind: 'busy', message: 'Server ausgelastet – neuer Versuch folgt …' };
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
