// Entry point of the administration and account pages. The build loads it as its own
// chunk after sign-in; the pages themselves live in ./management/.
import {navigate, registerPage} from './navigation.ts';
import {$, createManagement, type ManagementContext} from './management/shared.ts';
import {usersPanel, initUsers} from './management/users.ts';
import {oidcPanel, operationPanel, initRuntimeSettings} from './management/runtime-settings.ts';
import {initMetrics} from './management/metrics.ts';
import {accountPage, initAccount} from './management/account.ts';
import {sessionsPage, initSessions} from './management/sessions.ts';
import {auditPage, initAudit} from './management/audit.ts';

const adminPage = `
<section id="management-dialog" class="content-page settings-page" role="main" hidden>
  <div class="dialog-heading">
    <h2 id="management-title">Verwaltung</h2>
    <button type="button" id="management-close" class="button secondary" aria-label="Zurück zu Aufnahmen">← Aufnahmen</button>
  </div>
  <p class="settings-intro">Einstellungen für Benutzer, Anmeldung und den Betrieb des Viewers.</p>
  ${usersPanel}
  ${oidcPanel}
  ${operationPanel}
  <p id="settings-error" class="error" role="alert"></p>
  <details>
    <summary>Herkunft und Umgebungswerte</summary>
    <div id="settings-sources"></div>
  </details>
</section>`;

const tabs = {
  users: {title: 'Benutzer', form: 'user-form'},
  oidc: {title: 'Anmeldung / OIDC', form: 'oidc-form'},
  operation: {title: 'Betrieb', form: 'operation-form'},
} as const;
type Tab = keyof typeof tabs;

export function initManagement(c: ManagementContext) {
  const root = document.createElement('div');
  root.innerHTML = adminPage + accountPage + auditPage + sessionsPage;
  document.querySelector('.workspace')!.append(...Array.from(root.children));

  const m = createManagement(c, ['user-form', 'oidc-form', 'operation-form', 'password-form']);
  const users = initUsers(m);
  const runtime = initRuntimeSettings(m);
  const metrics = initMetrics(c);
  initAccount(m);
  initSessions(m);
  initAudit(m);

  // Users, sign-in and operation share one page element and switch panels.
  function showTab(tab: Tab) {
    $('management-title').textContent = tabs[tab].title;
    for (const name of Object.keys(tabs)) $(name + '-panel').hidden = name !== tab;
    void metrics.loadMetrics();
    $('settings-sources').closest('details')!.hidden = tab === 'users';
    $('settings-error').textContent = '';
  }

  for (const tab of Object.keys(tabs) as Tab[]) {
    registerPage('admin/' + tab, {
      element: $('management-dialog'),
      title: tabs[tab].title,
      admin: true,
      leave: () => !m.isBusy() && m.guards[tabs[tab].form].leave(),
      enter: async () => {
        showTab(tab);
        await m.run('settings-error', async () => {
          await runtime.loadSettings();
          if (tab === 'users') await users.enter();
        });
      },
    });
  }
  $('management-close').onclick = () => navigate('recordings');

  m.cleanAll();
  if (location.search === '?settings=confirmed') {
    c.notify('OIDC-Test erfolgreich. Einstellungen aktiviert.');
    history.replaceState(null, '', '/#admin/oidc');
  }
}
