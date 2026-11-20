import {navigate, registerPage} from '../navigation.ts';
import {$, field, roleLabel, type Management} from './shared.ts';

export const accountPage = `
<section id="password-dialog" class="content-page" role="main" hidden aria-labelledby="password-title">
  <div class="dialog-heading">
    <h2 id="password-title">Mein Konto</h2>
    <button type="button" id="password-close" class="button secondary" aria-label="Zurück zu Aufnahmen">← Aufnahmen</button>
  </div>
  <p id="account-info" class="muted"></p>
  <form id="password-form">
    <label>Bisheriges Passwort<input name="current" type="password" autocomplete="current-password" required maxlength="1024"></label>
    <label>Neues Passwort<input name="password" type="password" autocomplete="new-password" required minlength="14" maxlength="1024"></label>
    <button type="submit" class="button primary">Passwort ändern und abmelden</button>
  </form>
  <p id="password-error" class="error" role="alert"></p>
  <section id="totp-section" class="storage-info" hidden aria-labelledby="totp-title">
    <h3 id="totp-title">Zweiter Faktor</h3>
    <p id="totp-status" class="setting-help" role="status"></p>
    <button id="totp-start" type="button" class="button secondary" hidden>Zweiten Faktor einrichten</button>
    <form id="totp-enable-form" hidden>
      <p class="setting-help">Schlüssel in der Authenticator-App eintragen (zeitbasiert, 6 Stellen, 30 Sekunden) und den angezeigten Code bestätigen.</p>
      <label>Schlüssel<input id="totp-secret" readonly autocomplete="off" spellcheck="false"></label>
      <label>Einrichtungsadresse<input id="totp-uri" readonly autocomplete="off" spellcheck="false"></label>
      <label>Bestätigungscode<input name="code" inputmode="numeric" autocomplete="one-time-code" maxlength="7" pattern="[0-9 ]{6,7}" required></label>
      <button type="submit" class="button primary">Zweiten Faktor aktivieren</button>
    </form>
    <form id="totp-disable-form" hidden>
      <label>Passwort zur Bestätigung<input name="password" type="password" autocomplete="current-password" required maxlength="1024"></label>
      <button type="submit" class="button secondary">Zweiten Faktor entfernen</button>
    </form>
    <p id="totp-error" class="error" role="alert"></p>
  </section>
</section>`;

type Setup = {secret: string; uri: string};

export function initAccount(m: Management) {
  const {c, guards, run, report} = m;
  const passwordForm = $<HTMLFormElement>('password-form');
  const enableForm = $<HTMLFormElement>('totp-enable-form');
  const disableForm = $<HTMLFormElement>('totp-disable-form');

  // The second factor of the own local account. A started setup stays inactive until a code confirms it.
  function showTOTP(enabled: boolean, setup?: Setup) {
    $('totp-status').textContent = enabled
      ? 'Aktiv: Bei der Anmeldung wird zusätzlich ein Code aus der Authenticator-App abgefragt.'
      : setup
        ? 'Einrichtung begonnen, noch nicht aktiv.'
        : 'Nicht eingerichtet.';
    $('totp-start').hidden = enabled || !!setup;
    enableForm.hidden = !setup;
    disableForm.hidden = !enabled;
    enableForm.reset();
    disableForm.reset();
    $<HTMLInputElement>('totp-secret').value = setup?.secret.replace(/(.{4})/g, '$1 ').trim() || '';
    $<HTMLInputElement>('totp-uri').value = setup?.uri || '';
  }

  async function loadTOTP() {
    $('totp-error').textContent = '';
    try {
      showTOTP((await c.api<{enabled: boolean}>('/api/totp')).enabled);
    } catch (error) {
      report('totp-error', error);
    }
  }

  registerPage('settings/account', {
    element: $('password-dialog'),
    title: 'Mein Konto',
    leave: () => !m.isBusy() && guards['password-form'].leave(),
    enter: () => {
      const session = c.getSession();
      const local = session.kind === 'local';
      passwordForm.hidden = !local;
      $('account-info').textContent =
        session.name +
        ' · ' +
        roleLabel(session.role) +
        (local ? ' · Lokales Konto' : ' · SSO: Passwort und Konto beim Identity Provider verwalten.');
      passwordForm.reset();
      $('password-error').textContent = '';
      guards['password-form'].clean();
      $('totp-section').hidden = !local;
      if (local) void loadTOTP();
    },
  });

  $('password-button').onclick = () => navigate('settings/account');
  $('password-close').onclick = () => navigate('recordings');

  $('totp-start').onclick = () => {
    void run('totp-error', async () => {
      showTOTP(false, await c.api<Setup>('/api/totp/setup', {method: 'POST'}));
      field(enableForm, 'code').focus();
    });
  };
  enableForm.onsubmit = event => {
    event.preventDefault();
    void run('totp-error', async () => {
      await c.api('/api/totp/enable', {method: 'POST', body: JSON.stringify({code: field(enableForm, 'code').value})});
      showTOTP(true);
      c.notify('Zweiter Faktor aktiviert.');
    });
  };
  disableForm.onsubmit = event => {
    event.preventDefault();
    void run('totp-error', async () => {
      await c.api('/api/totp/disable', {
        method: 'POST',
        body: JSON.stringify({password: field(disableForm, 'password').value}),
      });
      showTOTP(false);
      c.notify('Zweiter Faktor entfernt.');
    });
  };
  passwordForm.onsubmit = event => {
    event.preventDefault();
    void run('password-error', async () => {
      await c.api('/api/password', {method: 'POST', body: JSON.stringify(Object.fromEntries(new FormData(passwordForm)))});
      passwordForm.reset();
      guards['password-form'].clean();
      location.reload();
    });
  };
}
