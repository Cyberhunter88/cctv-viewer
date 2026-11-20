import {clock} from './timeline.ts';

export type RequestHooks = {
  // CSRF token of the signed-in session; requests before sign-in send none.
  csrf?: string;
  // Called once when the server reports an expired sign-in.
  onUnauthorized: () => void;
};

export type ApiError = Error & {indexing: boolean; status: number};

type ErrorBody = {error?: string; gaps?: number[][]; indexing?: boolean};

// The server explains failures in JSON; gaps list the missing ranges of a recording.
export function errorMessage(body: ErrorBody, status: number): string {
  const message = body.error || `Anfrage fehlgeschlagen (${status})`;
  if (!body.gaps) return message;
  return message + '\n' + body.gaps.map(gap => clock(gap[0]) + ' – ' + clock(gap[1])).join('\n');
}

export async function requestJSON<T>(url: string, options: RequestInit, hooks: RequestHooks): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body) headers.set('Content-Type', 'application/json');
  if (hooks.csrf) headers.set('X-CSRF-Token', hooks.csrf);
  const response = await fetch(url, {...options, headers});
  if (response.status === 401) {
    hooks.onUnauthorized();
    throw new Error('Anmeldung abgelaufen');
  }
  const text = await response.text();
  let body: ErrorBody & Record<string, unknown> = {};
  try {
    body = text ? JSON.parse(text) : {};
  } catch {
    body = {error: text || `Serverfehler (${response.status})`};
  }
  if (!response.ok) {
    throw Object.assign(new Error(errorMessage(body, response.status)), {
      indexing: body.indexing === true,
      status: response.status,
    }) as ApiError;
  }
  return body as T;
}
