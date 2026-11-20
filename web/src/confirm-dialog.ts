// Themed confirmation for actions that delete or reset something. It replaces the
// browser's confirm(), which ignores the theme and looks the same for every action.
// Unsaved-change guards stay synchronous and keep the native dialog.

export type ConfirmOptions = {
  title: string;
  message: string;
  // Label of the confirming button; it names the action, for example "Löschen".
  confirm: string;
  // Destructive actions get a red button, and focus starts on "Abbrechen".
  danger?: boolean;
};

let pending: ((accepted: boolean) => void) | undefined;

export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  const dialog = document.getElementById('confirm-dialog') as HTMLDialogElement;
  const accept = document.getElementById('confirm-accept') as HTMLButtonElement;
  const cancel = document.getElementById('confirm-cancel') as HTMLButtonElement;
  // A second request while one is open counts as declined instead of replacing the first.
  if (pending || dialog.open) return Promise.resolve(false);

  document.getElementById('confirm-title')!.textContent = options.title;
  document.getElementById('confirm-message')!.textContent = options.message;
  accept.textContent = options.confirm;
  accept.className = options.danger ? 'button danger' : 'button primary';
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;

  return new Promise(resolve => {
    const finish = (accepted: boolean) => {
      pending = undefined;
      accept.onclick = cancel.onclick = dialog.oncancel = null;
      dialog.close();
      if (opener?.isConnected) opener.focus({preventScroll: true});
      resolve(accepted);
    };
    pending = finish;
    accept.onclick = () => finish(true);
    cancel.onclick = () => finish(false);
    // Escape closes the dialog as "Abbrechen".
    dialog.oncancel = event => {
      event.preventDefault();
      finish(false);
    };
    dialog.showModal();
    (options.danger ? cancel : accept).focus();
  });
}
