import {navigate, currentPage} from '../navigation.ts';
import {$, field, type Management} from './shared.ts';

type Settings = {
  version: number;
  values: Record<string, unknown>;
  sources: Record<string, string>;
  secretSet: boolean;
  external: Record<string, unknown>;
};
type Profile = {scale: number; fps: number; bitrate: number};

export const oidcPanel = `
<section id="oidc-panel" hidden>
  <h3>Anmeldung/OIDC</h3>
  <p>SSO-Konten beim Identity Provider verwalten. Änderungen werden erst nach einer erfolgreichen Administrator-Testanmeldung aktiviert.</p>
  <form id="oidc-form" class="settings-form">
    <label><input name="localLogin" type="checkbox"> Lokale Anmeldung aktiv</label>
    <label>OIDC-Issuer<input name="oidcIssuer" type="url" placeholder="https://auth.example.org/"><small>Leer bedeutet OIDC deaktiviert.</small></label>
    <label>Client-ID<input name="oidcClientId" autocomplete="off"></label>
    <label>Neues Client-Secret<input name="oidcSecret" type="password" autocomplete="new-password"><small id="secret-status"></small></label>
    <label>Gruppen-Claim<input name="oidcGroupClaim"></label>
    <label>Erlaubte Gruppen<input name="allowedGroups"><small>Durch Komma getrennte exakte Werte.</small></label>
    <label>Administratorgruppen<input name="adminGroups"></label>
    <label>Erlaubte Subjects<input name="allowedSubjects"></label>
    <label>Administrator-Subjects<input name="adminSubjects"></label>
    <p id="oidc-callback" class="muted"></p>
    <button type="submit" class="button primary">Anmeldung speichern und prüfen</button>
  </form>
  <a id="oidc-test-link" class="button primary" hidden>Administrator-Testanmeldung starten</a>
</section>`;

const profileFields = (legend: string, prefix: string) => `
    <fieldset>
      <legend>${legend}</legend>
      <label>Skalierung (%)<input name="${prefix}Scale" type="number" min="20" max="100" required></label>
      <label>FPS<input name="${prefix}FPS" type="number" min="2" max="30" required></label>
      <label>Bitrate (kbit/s)<input name="${prefix}Bitrate" type="number" min="100" max="8000" required></label>
    </fieldset>`;

export const operationPanel = `
<section id="operation-panel" hidden>
  <h3>Betrieb</h3>
  <section class="operation-status" aria-labelledby="operation-status-title">
    <h4 id="operation-status-title">Aktueller Zustand</h4>
    <dl id="operation-metrics" class="metric-list"></dl>
    <p id="operation-metrics-hint" class="setting-help"></p>
  </section>
  <p class="muted">Konfigurationssicherung: Kameras und Einstellungen ohne Passwörter und Zugangsdaten. <a id="backup-download" href="/api/admin/backup" download>Sicherung herunterladen</a></p>
  <form id="operation-form" class="settings-form">
    <label><input name="autoScan" type="checkbox"> Heute automatisch indexieren</label>
    <label>Indexintervall in Minuten<input name="autoScanMinutes" type="number" min="1" max="10080" required></label>
    <label>Cachebudget in MB<input name="cacheMB" type="number" min="32" max="65536" required></label>
    <label>Gleichzeitige Umwandlungen<input name="maxTranscoders" type="number" min="0" max="64" required></label>
    <label>Temporärer Videospeicher in MiB<input name="hlsTempMB" type="number" min="16" max="4096" required></label>
    <label>Video-Encoder<select name="encoder"><option value="software">Software (libx264)</option><option value="vaapi">VAAPI (Intel/AMD)</option><option value="qsv">Intel Quick Sync</option></select><small>Hardware-Encoder benötigen ein durchgereichtes Gerät unter /dev/dri. Schlägt der Start fehl, übernimmt für zehn Minuten der Software-Encoder.</small></label>
    ${profileFields('Ausgewogen', 'balanced')}
    ${profileFields('Schnell', 'fast')}
    <label>Sitzungsdauer in Minuten<input name="sessionMinutes" type="number" min="15" max="1440" required></label>
    <button type="submit" class="button primary">Betrieb speichern</button>
  </form>
  <section class="storage-info">
    <h3>Geschützte Zugangsdaten</h3>
    <p class="setting-help">Kamerazugangsdaten, Freigabepasswort und gespeicherte Anmeldeseinstellungen werden verschlüsselt gespeichert. Für eine Wiederherstellung Datenbank und Schlüsseldatei gemeinsam sichern.</p>
  </section>
  <h3>Extern konfigurierte Werte</h3>
  <pre id="external-settings"></pre>
</section>`;

const labels: Record<string, string> = {
  localLogin: 'Lokale Anmeldung',
  oidcIssuer: 'OIDC-Issuer',
  oidcClientId: 'Client-ID',
  oidcSecret: 'Client-Secret',
  oidcGroupClaim: 'Gruppen-Claim',
  allowedGroups: 'Erlaubte Gruppen',
  adminGroups: 'Administratorgruppen',
  allowedSubjects: 'Erlaubte Subjects',
  adminSubjects: 'Administrator-Subjects',
  autoScan: 'Automatische Indexierung',
  autoScanMinutes: 'Indexintervall',
  scanInterval: 'Technischer Scanintervall',
  cacheMB: 'Cachebudget',
  maxTranscoders: 'Gleichzeitige Umwandlungen',
  hlsTempMB: 'Temporärer Videospeicher',
  balanced: 'Profil Ausgewogen',
  fast: 'Profil Schnell',
  sessionMinutes: 'Sitzungsdauer',
  encoder: 'Video-Encoder',
};

const externalLabels: Record<string, string> = {
  baseURL: 'Öffentliche URL',
  recordingsDir: 'Aufnahmeverzeichnis im Container',
  dataDir: 'Persistentes Datenverzeichnis',
  address: 'Interne Serveradresse',
  trustedProxies: 'Vertrauenswürdige Proxys',
};

const listKeys = ['allowedGroups', 'adminGroups', 'allowedSubjects', 'adminSubjects'];
const profiles = ['balanced', 'fast'];
const profileKeys = [['Scale', 'scale'], ['FPS', 'fps'], ['Bitrate', 'bitrate']] as const;
const settingsForms = ['oidc-form', 'operation-form'];

const isOIDCSetting = (key: string) => key === 'localLogin' || key.startsWith('oidc') || listKeys.includes(key);
const listText = (value: unknown) => (Array.isArray(value) ? value.join(', ') : String(value ?? ''));

export function initRuntimeSettings(m: Management) {
  const {c, guards, run, actionButton} = m;
  const admin = $('management-dialog');
  let settings: Settings;

  function fillForms() {
    for (const id of settingsForms) {
      const form = $<HTMLFormElement>(id);
      for (const [key, value] of Object.entries(settings.values)) {
        const element = form.elements.namedItem(key);
        if (element instanceof HTMLSelectElement) element.value = String(value ?? '');
        else if (element instanceof HTMLInputElement) {
          if (element.type === 'checkbox') element.checked = !!value;
          else element.value = listText(value);
        }
      }
    }
    const operation = $<HTMLFormElement>('operation-form');
    for (const profile of profiles) {
      const values = settings.values[profile] as Profile;
      for (const [suffix, key] of profileKeys) field(operation, profile + suffix).value = String(values[key]);
    }
    field($<HTMLFormElement>('oidc-form'), 'oidcSecret').value = '';
    $('secret-status').textContent = settings.secretSet
      ? 'Secret vorhanden (' + settings.sources.oidcSecret + '); leer lassen, um es beizubehalten.'
      : 'Noch kein Secret vorhanden.';
  }

  function renderExternal() {
    $('external-settings').textContent = Object.entries(settings.external)
      .map(([key, value]) => (externalLabels[key] || key) + ': ' + listText(value))
      .join('\n');
    $('oidc-callback').textContent = 'Callback-URL: ' + settings.external.baseURL + '/auth/oidc/callback';
  }

  // Each value shows where it comes from and can fall back to its environment value.
  function renderSources() {
    $('settings-sources').replaceChildren();
    for (const [key, source] of Object.entries(settings.sources)) {
      const row = document.createElement('div');
      row.className = 'source-row';
      row.dataset.settingsSection = isOIDCSetting(key) ? 'oidc' : 'operation';
      const text = document.createElement('span');
      text.textContent = (labels[key] || key) + ' · ' + source;
      const reset = actionButton('Auf Umgebung zurücksetzen', async () => {
        const form = currentPage() === 'admin/oidc' ? 'oidc-form' : 'operation-form';
        if (!guards[form].leave()) return;
        await run('settings-error', () => save({values: {}, reset: [key]}));
      });
      row.append(text, reset);
      $('settings-sources').append(row);
    }
    const section = currentPage().split('/')[1];
    for (const row of admin.querySelectorAll<HTMLElement>('[data-settings-section]')) {
      row.hidden = row.dataset.settingsSection !== section;
    }
  }

  async function loadSettings() {
    settings = await c.api<Settings>('/api/admin/settings');
    fillForms();
    renderExternal();
    renderSources();
    for (const id of settingsForms) guards[id].clean();
  }

  async function save(change: {values: Record<string, unknown>; reset?: string[]}) {
    const result = await c.api<{testRequired?: boolean; testURL?: string}>('/api/admin/settings', {
      method: 'PUT',
      body: JSON.stringify({version: settings.version, ...change}),
    });
    const link = $<HTMLAnchorElement>('oidc-test-link');
    if (result.testRequired) {
      link.href = result.testURL!;
      link.hidden = false;
      field($<HTMLFormElement>('oidc-form'), 'oidcSecret').value = '';
      guards['oidc-form'].clean();
      if (currentPage() !== 'admin/oidc') navigate('admin/oidc');
      c.notify('Bisherige Anmeldung bleibt aktiv. Jetzt die OIDC-Testanmeldung abschließen.');
    } else {
      link.hidden = true;
      await loadSettings();
      c.notify('Einstellungen gespeichert.');
    }
  }

  function formValues(form: HTMLFormElement) {
    const values: Record<string, unknown> = {};
    for (const entry of Array.from(form.elements)) {
      if (entry instanceof HTMLSelectElement) {
        values[entry.name] = entry.value;
        continue;
      }
      if (!(entry instanceof HTMLInputElement)) continue;
      const key = entry.name;
      if (key === 'oidcSecret' && !entry.value) continue;
      if (entry.type === 'checkbox') values[key] = entry.checked;
      else if (entry.type === 'number') values[key] = Number(entry.value);
      else if (listKeys.includes(key)) values[key] = entry.value.split(',').map(value => value.trim()).filter(Boolean);
      else values[key] = entry.value.trim();
    }
    if (form.id === 'operation-form') {
      for (const profile of profiles) {
        const value: Record<string, unknown> = {};
        for (const [suffix, key] of profileKeys) {
          value[key] = values[profile + suffix];
          delete values[profile + suffix];
        }
        values[profile] = value;
      }
    }
    return values;
  }

  for (const id of settingsForms) {
    $<HTMLFormElement>(id).onsubmit = event => {
      event.preventDefault();
      const form = event.currentTarget as HTMLFormElement;
      void run('settings-error', async () => save({values: formValues(form)}));
    };
  }

  return {loadSettings};
}
