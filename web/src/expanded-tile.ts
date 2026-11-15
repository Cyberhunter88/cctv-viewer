// Fallback "expanded" camera tile when native fullscreen is unavailable.
// The overlay behaves like a modal view: the rest of the page is inert,
// focus stays inside the tile, and closing returns focus to the trigger.
let expanded: HTMLElement | undefined, opener: HTMLElement | undefined;
let inertNodes: HTMLElement[] = [];
const keepActive = (node: Element) => node.id === 'shortcut-help' || node.id === 'toast' || node.id === 'toast-region' || node.id === 'toast-alert';

export function expandedTile(): HTMLElement | undefined {
  return expanded?.isConnected ? expanded : undefined;
}

export function expandTile(tile: HTMLElement, trigger?: HTMLElement) {
  collapseTile(false);
  expanded = tile;
  opener = trigger;
  tile.classList.add('expanded');
  for (let node: HTMLElement = tile; node.parentElement && node !== document.body; node = node.parentElement) {
    for (const sibling of node.parentElement.children) {
      if (sibling === node || !(sibling instanceof HTMLElement) || sibling.inert || keepActive(sibling)) continue;
      sibling.inert = true;
      inertNodes.push(sibling);
    }
  }
  if (!trigger || !tile.contains(trigger)) {
    tile.tabIndex = -1;
    tile.focus({ preventScroll: true });
  }
  document.dispatchEvent(new CustomEvent('tileexpandchange', { detail: tile }));
}

/**
 * Shared by recordings and live tiles: closes an open fullscreen or overlay, otherwise
 * opens native fullscreen and falls back to the overlay when the browser refuses it.
 * Focus moves into a fullscreen tile so Space does not re-press the trigger button.
 */
export async function toggleEnlarged(tile: HTMLElement, trigger?: HTMLElement) {
  if (document.fullscreenElement) { await document.exitFullscreen(); return; }
  if (expandedTile() === tile) { collapseTile(); return; }
  if (document.fullscreenEnabled && tile.requestFullscreen) {
    try {
      await tile.requestFullscreen();
      tile.tabIndex = -1;
      tile.focus({ preventScroll: true });
      return;
    } catch { /* falls back to the overlay below */ }
  }
  expandTile(tile, trigger);
}

/** Returns true if an expanded tile was closed. */
export function collapseTile(restoreFocus = true): boolean {
  const tile = expanded;
  for (const node of inertNodes) node.inert = false;
  inertNodes = [];
  expanded = undefined;
  const stale = [...document.querySelectorAll<HTMLElement>('.camera-tile.expanded')];
  if (tile) stale.push(tile);
  if (!stale.length) return false;
  for (const node of stale) node.classList.remove('expanded');
  const target = opener;
  opener = undefined;
  if (restoreFocus && target?.isConnected) target.focus({ preventScroll: true });
  document.dispatchEvent(new CustomEvent('tileexpandchange', { detail: null }));
  return true;
}
