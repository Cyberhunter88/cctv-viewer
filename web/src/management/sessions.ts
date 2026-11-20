import {navigate, registerPage, currentPage} from '../navigation.ts';
import {confirmAction} from '../confirm-dialog.ts';
import {$, formatDateTime, roleLabel, type Management} from './shared.ts';

type ActiveSession = {id: string; name: string; kind: string; role: string; expires: number; current: boolean};

export const sessionsPage = `
<section id="sessions-page" class="content-page settings-page" role="main" hidden aria-labelledby="sessions-title">
  <div class="dialog-heading">
    <h2 id="sessions-title">Meine Sitzungen</h2>
    <button id="sessions-close" class="button secondary" type="button" aria-label="Zurück zu Aufnahmen">← Aufnahmen</button>
  </div>
  <p class="settings-intro">Aktive Anmeldungen prüfen und bei Bedarf beenden.</p>
  <p class="muted">Aktive Anmeldungen verwalten. Nach dem Beenden einer Sitzung ist eine neue Anmeldung erforderlich.</p>
  <div class="dialog-actions">
    <button id="sessions-refresh" class="button secondary" type="button">Aktualisieren</button>
    <button id="sessions-end-own" class="button secondary" type="button">Alle eigenen Sitzungen beenden</button>
  </div>
  <p id="sessions-error" class="error" role="alert"></p>
  <div id="session-list"></div>
</section>`;

// One page lists the own sessions under settings and every session under administration.
export function initSessions(m: Management) {
  const {c, run, actionButton} = m;

  const describe = (entry: ActiveSession) =>
    entry.name +
    ' · ' +
    (entry.kind === 'local' ? 'Lokal' : 'SSO') +
    ' · ' +
    roleLabel(entry.role) +
    ' · gültig bis ' +
    formatDateTime(entry.expires) +
    (entry.current ? ' · Diese Sitzung' : '');

  async function loadSessions() {
    await run('sessions-error', async () => {
      const all = currentPage() === 'admin/sessions';
      const base = all ? '/api/admin/sessions' : '/api/sessions';
      const sessions = await c.api<ActiveSession[]>(base);
      const list = $('session-list');
      list.replaceChildren();
      if (!sessions.length) list.textContent = 'Keine aktiven Sitzungen.';
      for (const entry of sessions) {
        const row = document.createElement('div');
        row.className = 'session-entry';
        const text = document.createElement('p');
        text.textContent = describe(entry);
        row.append(text, actionButton('Sitzung beenden', () => endSession(base, entry)));
        list.append(row);
      }
    });
  }

  function endSession(base: string, entry: ActiveSession) {
    return run('sessions-error', async () => {
      const accepted = await confirmAction({
        title: 'Sitzung beenden',
        message: entry.current
          ? 'Das ist deine aktuelle Sitzung. Danach ist eine neue Anmeldung erforderlich.'
          : 'Die Sitzung von „' + entry.name + '“ wird beendet. Danach ist dort eine neue Anmeldung erforderlich.',
        confirm: 'Sitzung beenden',
        danger: true,
      });
      if (!accepted) return;
      await c.api(base + '/' + entry.id, {method: 'DELETE'});
      if (entry.current) {
        m.guards['user-form'].clean();
        location.reload();
        return;
      }
      c.notify('Sitzung beendet.');
      setTimeout(() => void loadSessions(), 0);
    });
  }

  for (const scope of ['settings', 'admin']) {
    const title = scope === 'admin' ? 'Alle Sitzungen' : 'Meine Sitzungen';
    registerPage(scope + '/sessions', {
      element: $('sessions-page'),
      title,
      admin: scope === 'admin',
      leave: () => !m.isBusy(),
      enter: () => {
        $('sessions-title').textContent = title;
        return loadSessions();
      },
    });
  }

  $('sessions-close').onclick = () => navigate('recordings');
  $('sessions-refresh').onclick = () => void loadSessions();
  $('sessions-end-own').onclick = () =>
    void run('sessions-error', async () => {
      const accepted = await confirmAction({
        title: 'Alle eigenen Sitzungen beenden',
        message: 'Alle deine Anmeldungen werden beendet, auch diese. Danach ist eine neue Anmeldung erforderlich.',
        confirm: 'Alle beenden und abmelden',
        danger: true,
      });
      if (!accepted) return;
      await c.api('/api/sessions', {method: 'DELETE'});
      location.reload();
    });
}
