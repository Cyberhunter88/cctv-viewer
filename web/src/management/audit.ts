import {navigate, registerPage} from '../navigation.ts';
import {$, formatDateTime, type Management} from './shared.ts';

type AuditEntry = {time: number; actor: string; action: string; target: string; ip: string; ok: boolean};

export const auditPage = `
<section id="audit-page" class="content-page" role="main" hidden aria-labelledby="audit-title">
  <div class="dialog-heading">
    <h2 id="audit-title">Protokoll</h2>
    <button id="audit-close" class="button secondary" type="button" aria-label="Zurück zu Aufnahmen">← Aufnahmen</button>
  </div>
  <p class="muted">Anmeldungen und Änderungen durch Administratoren. Einträge werden nach 180 Tagen gelöscht.</p>
  <div class="dialog-actions">
    <button id="audit-refresh" class="button secondary" type="button">Aktualisieren</button>
    <button id="audit-more" class="button secondary" type="button" hidden>Ältere Einträge</button>
  </div>
  <p id="audit-error" class="error" role="alert"></p>
  <div id="audit-list"></div>
</section>`;

const auditActions: Record<string, string> = {
  'login.local': 'Lokale Anmeldung',
  'login.oidc': 'SSO-Anmeldung',
  logout: 'Abmeldung',
  'user.save': 'Benutzer gespeichert',
  'user.delete': 'Benutzer gelöscht',
  'password.change': 'Passwort geändert',
  'settings.save': 'Einstellungen gespeichert',
  'session.revoke': 'Sitzung beendet',
  'session.revoke-own': 'Eigene Sitzungen beendet',
  'camera.save': 'Kamera gespeichert',
  'camera.batch': 'Kameras gespeichert',
  'camera.delete': 'Kamera gelöscht',
  'alert.test': 'Testwarnung gesendet',
  'backup.export': 'Sicherung exportiert',
  'backup.import': 'Sicherung importiert',
  'totp.enable': 'Zweiten Faktor aktiviert',
  'totp.disable': 'Zweiten Faktor entfernt',
  'totp.reset': 'Zweiten Faktor zurückgesetzt',
};

const describe = (entry: AuditEntry) =>
  formatDateTime(entry.time) +
  ' · ' +
  (auditActions[entry.action] || entry.action) +
  (entry.target ? ' · ' + entry.target : '') +
  ' · ' +
  (entry.actor || 'unbekannt') +
  ' · ' +
  entry.ip +
  (entry.ok ? '' : ' · fehlgeschlagen');

export function initAudit(m: Management) {
  const {c, run} = m;
  let offset = 0;

  async function loadAudit(more: boolean) {
    await run('audit-error', async () => {
      const list = $('audit-list');
      if (!more) {
        offset = 0;
        list.replaceChildren();
      }
      const result = await c.api<{entries: AuditEntry[]; hasMore: boolean}>(
        '/api/admin/audit?limit=100&offset=' + offset,
      );
      offset += result.entries.length;
      if (!offset) list.textContent = 'Keine Einträge.';
      for (const entry of result.entries) {
        const row = document.createElement('p');
        row.className = 'session-entry';
        row.textContent = describe(entry);
        list.append(row);
      }
      $('audit-more').hidden = !result.hasMore;
    });
  }

  registerPage('admin/audit', {
    element: $('audit-page'),
    title: 'Protokoll',
    admin: true,
    leave: () => !m.isBusy(),
    enter: () => loadAudit(false),
  });
  $('audit-close').onclick = () => navigate('recordings');
  $('audit-refresh').onclick = () => void loadAudit(false);
  $('audit-more').onclick = () => void loadAudit(true);
}
