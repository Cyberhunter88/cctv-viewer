import {$, type ManagementContext} from './shared.ts';

const megabytes = (bytes: number) =>
  (bytes / 1048576).toLocaleString('de-DE', {maximumFractionDigits: 0}) + ' MiB';

function duration(seconds: number) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return (days ? days + ' T ' : '') + hours + ' Std. ' + minutes + ' Min.';
}

function hint(m: Record<string, number>) {
  if (m.liveGatewayDrops > 0) {
    return 'Home Assistant liest langsamer, als die Kamera sendet; verworfene Pakete führen zu Bildfehlern. Siehe „Flüssige Wiedergabe in Home Assistant“ in docs/LIVE.md.';
  }
  if (m.liveUpstreamLost > 0) {
    return 'Pakete fehlen schon zwischen Kamera, go2rtc und Freigabe; Kamera und Netz prüfen. Siehe docs/LIVE.md.';
  }
  if (m.encoderFallbacks > 0) return 'Der Hardware-Encoder ist ausgefallen; der Software-Encoder hat übernommen.';
  return '';
}

// Live operating figures, refreshed every five seconds while the operation page is shown.
export function initMetrics(c: ManagementContext) {
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function loadMetrics() {
    clearTimeout(timer);
    // Polling stops as soon as the panel is not rendered, also after leaving the administration page.
    if ($('operation-panel').offsetParent === null || document.hidden) return;
    try {
      const m = await c.api<Record<string, number>>('/api/admin/metrics');
      const rows: [string, string][] = [
        ['Aktive Umwandlungen', `${m.encoding} von ${m.encodingLimit || 'unbegrenzt'}`],
        ['Wiedergabesitzungen', String(m.streamSessions)],
        ['Temporärer Videospeicher', `${megabytes(m.temporaryBytes)} von ${megabytes(m.temporaryBudget)}`],
        ['Encoder-Ausweichstarts', String(m.encoderFallbacks)],
        ['Verlorene Pakete der Freigabe', String(m.liveGatewayDrops)],
        ['Vor der Freigabe verlorene Pakete', String(m.liveUpstreamLost ?? 0)],
        ['Fehlgeschlagene Umwandlungen', String(m.streamFailures)],
        ['Laufzeit', duration(m.uptimeSeconds)],
      ];
      $('operation-metrics').replaceChildren(
        ...rows.flatMap(([label, value]) => {
          const term = document.createElement('dt');
          const detail = document.createElement('dd');
          term.textContent = label;
          detail.textContent = value;
          return [term, detail];
        }),
      );
      $('operation-metrics-hint').textContent = hint(m);
    } catch {
      // A failed refresh keeps the last figures; the next attempt follows shortly.
    }
    timer = setTimeout(() => void loadMetrics(), 5000);
  }

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) void loadMetrics();
  });
  return {loadMetrics};
}
