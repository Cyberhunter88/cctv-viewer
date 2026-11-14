// Single-pointer alternative to dragging camera chips (WCAG 2.5.7).
// In reorder mode every chip gets "move left" / "move right" buttons.
export function wrapChipWithMoveButtons(chip: HTMLButtonElement, name: string, index: number, count: number, move: (delta: -1 | 1) => void): HTMLElement {
  const group = document.createElement('span');
  group.className = 'chip-move';
  group.setAttribute('role', 'group');
  group.setAttribute('aria-label', name);
  const button = (delta: -1 | 1) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'icon-button chip-move-button';
    b.textContent = delta < 0 ? '‹' : '›';
    b.setAttribute('aria-label', `${name} nach ${delta < 0 ? 'links' : 'rechts'} verschieben`);
    b.id = `${chip.id}-move-${delta < 0 ? 'left' : 'right'}`;
    b.disabled = delta < 0 ? index === 0 : index === count - 1;
    b.onclick = () => move(delta);
    return b;
  };
  group.append(button(-1), chip, button(1));
  return group;
}

/** Creates the toggle that switches the chips into reorder mode. */
export function createReorderToggle(id: string, active: () => boolean, toggle: () => void): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.id = id;
  b.className = 'text-button';
  b.textContent = 'Reihenfolge ändern';
  const sync = () => b.setAttribute('aria-pressed', String(active()));
  b.onclick = () => { toggle(); sync(); };
  sync();
  return b;
}
