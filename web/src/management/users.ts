import {confirmAction} from '../confirm-dialog.ts';
import {$, field, roleLabel, type Management} from './shared.ts';

type User = {username: string; role: string; disabled: boolean; totp?: boolean};

export const usersPanel = `
<section id="users-panel">
  <h3>Lokale Benutzer</h3>
  <div class="user-toolbar">
    <label>Benutzer suchen<input id="user-search" type="search" placeholder="Name, Rolle oder Status"></label>
    <button type="button" id="new-user" class="button primary">Neuer Benutzer</button>
  </div>
  <p id="user-count" role="status"></p>
  <div id="user-list"></div>
  <h3 id="user-editor-title">Neuer Benutzer</h3>
  <form id="user-form" class="settings-form">
    <input name="create" type="hidden" value="true">
    <label>Benutzername<input name="username" required maxlength="100" autocomplete="off"></label>
    <label>Rolle<select name="role"><option value="viewer">Betrachter</option><option value="admin">Administrator</option></select></label>
    <label><input name="disabled" type="checkbox"> Konto gesperrt</label>
    <label>Neues Passwort<input name="password" type="password" minlength="14" maxlength="1024" autocomplete="new-password" required><small>Bei bestehenden Konten leer lassen, um das Passwort beizubehalten.</small></label>
    <div class="dialog-actions">
      <button type="submit" class="button primary">Benutzer speichern</button>
    </div>
  </form>
</section>`;

export function initUsers(m: Management) {
  const {c, guards, run, actionButton} = m;
  const form = $<HTMLFormElement>('user-form');
  const guard = guards['user-form'];
  let users: User[] = [];

  const isOwnLocalAccount = (username: string) =>
    username === c.getSession().name && c.getSession().kind === 'local';

  function newUser() {
    form.reset();
    field(form, 'create').value = 'true';
    (field(form, 'username') as HTMLInputElement).readOnly = false;
    (field(form, 'password') as HTMLInputElement).required = true;
    $('user-editor-title').textContent = 'Neuer Benutzer';
    guard.clean();
  }

  function editUser(user: User) {
    if (m.isBusy() || !guard.leave()) return;
    form.reset();
    field(form, 'username').value = user.username;
    (field(form, 'username') as HTMLInputElement).readOnly = true;
    field(form, 'role').value = user.role;
    (field(form, 'disabled') as HTMLInputElement).checked = user.disabled;
    field(form, 'create').value = 'false';
    (field(form, 'password') as HTMLInputElement).required = false;
    $('user-editor-title').textContent = 'Benutzer bearbeiten: ' + user.username;
    guard.clean();
    field(form, 'role').focus();
  }

  async function loadUsers() {
    users = await c.api<User[]>('/api/admin/users');
    renderUsers();
  }

  const describe = (user: User) =>
    user.username + ' · ' + roleLabel(user.role) + (user.disabled ? ' · gesperrt' : ' · aktiv');

  function renderUsers() {
    const query = $<HTMLInputElement>('user-search').value.toLocaleLowerCase('de');
    const shown = users.filter(user => {
      const text = user.username + ' ' + roleLabel(user.role) + ' ' + (user.disabled ? 'gesperrt' : 'aktiv');
      return text.toLocaleLowerCase('de').includes(query);
    });
    $('user-list').replaceChildren();
    $('user-count').textContent = shown.length + ' von ' + users.length + ' Konten';
    for (const user of shown) {
      const row = document.createElement('div');
      row.className = 'camera-entry';
      const label = describe(user) + (user.totp ? ' · zweiter Faktor' : '');
      row.append(
        actionButton(label, () => editUser(user)),
        actionButton('Löschen', () => deleteUser(user)),
      );
      if (user.totp) row.append(actionButton('Zweiten Faktor zurücksetzen', () => resetSecondFactor(user)));
      $('user-list').append(row);
    }
  }

  const userURL = (user: User) => '/api/admin/users/' + encodeURIComponent(user.username);

  function deleteUser(user: User) {
    return run('settings-error', async () => {
      if (!guard.leave()) return;
      const accepted = await confirmAction({
        title: 'Benutzer löschen',
        message: 'Benutzer „' + user.username + '“ wird gelöscht. Das lässt sich nicht rückgängig machen.',
        confirm: 'Benutzer löschen',
        danger: true,
      });
      if (!accepted) return;
      await c.api(userURL(user), {method: 'DELETE'});
      if (isOwnLocalAccount(user.username)) {
        guard.clean();
        location.reload();
        return;
      }
      newUser();
      await loadUsers();
      c.notify('Benutzer gelöscht.');
    });
  }

  function resetSecondFactor(user: User) {
    return run('settings-error', async () => {
      const accepted = await confirmAction({
        title: 'Zweiten Faktor zurücksetzen',
        message: 'Der zweite Faktor von „' + user.username + '“ wird entfernt. Die Anmeldung ist danach wieder nur mit Passwort möglich.',
        confirm: 'Zurücksetzen',
        danger: true,
      });
      if (!accepted) return;
      await c.api(userURL(user) + '/totp', {method: 'DELETE'});
      await loadUsers();
      c.notify('Zweiter Faktor zurückgesetzt.');
    });
  }

  $('user-search').oninput = renderUsers;
  $('new-user').onclick = () => {
    if (!guard.leave()) return;
    newUser();
    field(form, 'username').focus();
  };
  form.onsubmit = event => {
    event.preventDefault();
    void run('settings-error', async () => {
      const username = field(form, 'username').value;
      const create = field(form, 'create').value === 'true';
      await c.api('/api/admin/users', {
        method: 'POST',
        body: JSON.stringify({
          username,
          role: field(form, 'role').value,
          disabled: (field(form, 'disabled') as HTMLInputElement).checked,
          password: field(form, 'password').value,
          create,
        }),
      });
      if (isOwnLocalAccount(username) && !create) {
        guard.clean();
        location.reload();
        return;
      }
      newUser();
      await loadUsers();
      c.notify('Benutzer gespeichert.');
    });
  };

  return {
    async enter() {
      newUser();
      await loadUsers();
    },
  };
}
