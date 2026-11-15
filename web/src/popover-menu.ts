// Disclosure menus close on Escape, on an outside click and after an action was chosen.
export function popoverMenu(menu: HTMLDetailsElement) {
  const summary = menu.querySelector('summary')!;
  document.addEventListener('keydown', e => { if (e.key === 'Escape' && menu.open) { e.preventDefault(); menu.open = false; summary.focus(); } });
  document.addEventListener('click', e => { if (e.target instanceof Node && !menu.contains(e.target)) menu.open = false; });
  menu.addEventListener('click', e => { if (e.target instanceof Element && e.target.closest('summary') === null && e.target.closest('button')) menu.open = false; });
}
