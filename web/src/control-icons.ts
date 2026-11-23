// Line icons of the playback controls, drawn as inline SVG so they follow the text colour.
export type ControlIcon = 'expand' | 'collapse' | 'sound' | 'muted' | 'play' | 'pause' | 'back' | 'forward' | 'reconnect' | 'pip' | 'target';
export function setControlIcon(button: Element, name: ControlIcon) {
  const paths: Record<ControlIcon, string> = { expand: 'M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5', collapse: 'M3 8h5V3m13 5h-5V3M8 21v-5H3m13 5v-5h5', sound: 'M11 5 6 9H3v6h3l5 4V5Zm4 3a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14', muted: 'M11 5 6 9H3v6h3l5 4V5Zm5 4 5 6m0-6-5 6', play: 'M7 5v14l12-7L7 5Z', pause: 'M8 5v14m8-14v14', back: 'M4 12a8 8 0 1 0 2.3-5.7M4 4v4h4', forward: 'M20 12a8 8 0 1 1-2.3-5.7M20 4v4h-4', reconnect: 'M20 11a8 8 0 0 0-14.6-4M4 4v4h4m-4 5a8 8 0 0 0 14.6 4M20 20v-4h-4', pip: 'M3 5h18v14H3zM12 12h7v5h-7z', target: 'M12 3v4m0 10v4M3 12h4m10 0h4M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z' };
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '1.7');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  const path = document.createElementNS(svg.namespaceURI, 'path');
  path.setAttribute('d', paths[name]);
  svg.append(path);
  button.replaceChildren(svg);
}
