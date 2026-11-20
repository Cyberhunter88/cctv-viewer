// Messages are announced through permanent live regions (a region revealed together
// with its text is often skipped by screen readers). Errors stay until closed or replaced;
// other messages close after six seconds unless pointer or focus rests on them.

export type ToastAction = {label: string; run: () => void};

const $ = (id: string) => document.getElementById(id)!;
let hideTimer: ReturnType<typeof setTimeout> | undefined;
// An optional action (such as "Rückgängig") keeps the message open until it is used or closed.
let pendingAction: (() => void) | undefined;

function hideToast() {
  clearTimeout(hideTimer);
  $('toast').hidden = true;
}

function scheduleHide() {
  clearTimeout(hideTimer);
  const toast = $('toast');
  if (toast.classList.contains('toast-error')) return;
  hideTimer = setTimeout(hideToast, toast.classList.contains('toast-has-action') ? 12000 : 6000);
}

export function notify(message: string, kind: 'info' | 'error' = 'info', action?: ToastAction) {
  const error = kind === 'error';
  const region = $(error ? 'toast-alert' : 'toast-region');
  const other = $(error ? 'toast-region' : 'toast-alert');
  const toast = $('toast');
  pendingAction = action?.run;
  $('toast-action').textContent = action?.label || '';
  $('toast-action').hidden = !action;
  toast.classList.toggle('toast-has-action', !!action);
  $('toast-text').textContent = message;
  toast.classList.toggle('toast-error', error);
  toast.hidden = false;
  other.textContent = '';
  region.textContent = '';
  setTimeout(() => {
    region.textContent = message;
  }, 50);
  scheduleHide();
}

export const notifyError = (message: string) => notify(message, 'error');

export function initToast() {
  const toast = $('toast');
  $('toast-close').addEventListener('click', hideToast);
  $('toast-action').addEventListener('click', () => {
    const run = pendingAction;
    pendingAction = undefined;
    hideToast();
    run?.();
  });
  toast.addEventListener('pointerenter', () => clearTimeout(hideTimer));
  toast.addEventListener('pointerleave', scheduleHide);
  toast.addEventListener('focusin', () => clearTimeout(hideTimer));
  toast.addEventListener('focusout', scheduleHide);
}
