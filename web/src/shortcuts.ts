// One list of keyboard shortcuts for the settings page and the "?" overview.
export const shortcuts: ReadonlyArray<readonly [string, string]> = [
  ['Leertaste / K', 'Abspielen / Pause'],
  ['← / →', 'Gewählte Sprungweite (auf der Zeitleiste immer aktiv)'],
  ['Umschalt + ← / →', '60 Sekunden springen'],
  ['J / L', '10 Sekunden zurück / vor'],
  ['P / N', 'Vorherige / nächste Aufnahmegrenze'],
  ['Pos1 / Ende', 'Tagesanfang / Tagesende'],
  [', / .', 'Einzelbild zurück / vor (hält die Wiedergabe an)'],
  ['↑ / ↓', 'Schneller / langsamer abspielen'],
  ['M', 'Ton der gewählten Kamera ein / aus'],
  ['F / Escape', 'Kamerabild vergrößern / schließen'],
  ['Klick aufs Bild im Vollbild', 'Abspielen / Pause'],
  ['+ / − auf der Zeitleiste', 'Vergrößern / verkleinern'],
  ['?', 'Diese Übersicht öffnen'],
];

export function renderShortcutList(list: HTMLElement) {
  list.replaceChildren(...shortcuts.flatMap(([key, action]) => {
    const term = document.createElement('dt'), description = document.createElement('dd');
    term.textContent = key;
    description.textContent = action;
    return [term, description];
  }));
}

/** Opens the shortcut overview; it is a modal dialog, so Escape and the close button end it. */
export function initShortcutHelp() {
  const dialog = document.getElementById('shortcut-help') as HTMLDialogElement;
  renderShortcutList(dialog.querySelector<HTMLElement>('.shortcut-list')!);
  for (const list of document.querySelectorAll<HTMLElement>('#preferences-form .shortcut-list')) renderShortcutList(list);
  dialog.querySelector('#shortcut-help-close')!.addEventListener('click', () => dialog.close());
  dialog.addEventListener('click', event => { if (event.target === dialog) dialog.close(); });
  return {
    open() { if (!dialog.open) dialog.showModal(); },
    get isOpen() { return dialog.open; },
  };
}
