import {formGuard} from '../navigation.ts';

// Shared plumbing for the administration and account pages, which load as one chunk after sign-in.

export type ManagementContext = {
  api: <T>(url: string, options?: RequestInit) => Promise<T>;
  getSession: () => {name: string; role: string; kind: string};
  getCameras: () => {id: string; name: string}[];
  getCursor: () => number;
  notify: (message: string) => void;
};

export type Guard = ReturnType<typeof formGuard>;

export const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

export const field = (form: HTMLFormElement, name: string) =>
  form.elements.namedItem(name) as HTMLInputElement | HTMLSelectElement;

export const roleLabel = (role: string) => (role === 'admin' ? 'Administrator' : 'Betrachter');

export function formatDateTime(epoch: number): string {
  return new Date(epoch * 1000).toLocaleString('de-DE');
}

// The error element of an action decides which page is marked busy while it runs.
const busySurface: Record<string, string> = {
  'sessions-error': 'sessions-page',
  'audit-error': 'audit-page',
  'password-error': 'password-dialog',
  'totp-error': 'password-dialog',
};

export type Management = {
  c: ManagementContext;
  guards: Record<string, Guard>;
  isBusy: () => boolean;
  run: (errorId: string, action: () => Promise<void>) => Promise<void>;
  report: (errorId: string, error: unknown) => void;
  actionButton: (text: string, action: () => void | Promise<void>) => HTMLButtonElement;
  cleanAll: () => void;
};

export function createManagement(c: ManagementContext, formIds: string[]): Management {
  const guards = Object.fromEntries(formIds.map(id => [id, formGuard($<HTMLFormElement>(id))]));
  let busy = false;
  const report = (errorId: string, error: unknown) => {
    $(errorId).textContent = (error as Error).message;
  };

  // Only one action runs at a time; its page is disabled until it settles.
  async function run(errorId: string, action: () => Promise<void>) {
    if (busy) return;
    busy = true;
    $(errorId).textContent = '';
    const surface = $(busySurface[errorId] ?? 'management-dialog');
    const controls = [...surface.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLButtonElement>('input,select,button')];
    const states = controls.map(control => control.disabled);
    surface.setAttribute('aria-busy', 'true');
    try {
      const pending = action();
      controls.forEach(control => (control.disabled = true));
      await pending;
    } catch (error) {
      report(errorId, error);
    } finally {
      controls.forEach((control, index) => (control.disabled = states[index]));
      busy = false;
      surface.setAttribute('aria-busy', 'false');
    }
  }

  function actionButton(text: string, action: () => void | Promise<void>) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'button secondary';
    button.textContent = text;
    button.onclick = () => void Promise.resolve(action()).catch(error => alert(error.message));
    return button;
  }

  // Every form offers to discard its own changes next to the submit button.
  for (const [id, guard] of Object.entries(guards)) {
    const discard = document.createElement('button');
    discard.type = 'button';
    discard.className = 'button secondary settings-discard';
    discard.textContent = 'Änderungen verwerfen';
    discard.onclick = () => {
      if (!busy && guard.leave()) c.notify('Änderungen verworfen.');
    };
    $<HTMLFormElement>(id).querySelector('button[type=submit]')!.before(discard);
  }

  return {
    c,
    guards,
    isBusy: () => busy,
    run,
    report,
    actionButton,
    cleanAll: () => Object.values(guards).forEach(guard => guard.clean()),
  };
}
