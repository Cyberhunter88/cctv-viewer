// Shared presentation for existing settings pages, without changing their APIs.
export function initSettingsLayout(){
 const help:Record<string,string>={
  'management-dialog':'Einstellungen für Benutzer, Anmeldung und den Betrieb des Viewers.',
  'password-dialog':'Dein Konto und dein Passwort verwalten.',
  'sessions-page':'Aktive Anmeldungen prüfen und bei Bedarf beenden.',
  'preferences-dialog':'Darstellung und Wiedergabe an deine Arbeitsweise anpassen.',
  'batch-dialog':'Mehrere Aufnahmeordner auswählen und gemeinsam als Kameras hinzufügen.'
 };
 for(const page of document.querySelectorAll<HTMLElement>('.content-page')){
  if(page.dataset.settingsLayout)continue;page.dataset.settingsLayout='true';page.classList.add('settings-page');
  const heading=page.querySelector('.dialog-heading');
  if(heading&&help[page.id]){const text=document.createElement('p');text.className='settings-intro';text.textContent=help[page.id];heading.after(text)}
  for(const form of page.querySelectorAll<HTMLFormElement>('form')){
   form.classList.add('settings-form');
   for(const button of form.querySelectorAll<HTMLButtonElement>('button[type=submit]'))button.classList.add('settings-submit');
  }
 }
 const nav=document.getElementById('rail-nav');
 if(nav&&!nav.querySelector('[data-page="admin/live-share"]')){
  const camera=nav.querySelector('[data-page="admin/cameras"]')!;
  const link=document.createElement('a');link.href='#admin/live-share';link.dataset.page='admin/live-share';link.className='rail-item';link.setAttribute('aria-label','Streamfreigabe');link.title='Streamfreigabe';link.innerHTML='<span class="rail-symbol" aria-hidden="true">↗</span><span class="rail-label">Streamfreigabe</span>';camera.after(link);
  const title=document.createElement('span');title.className='nav-section-label';title.textContent='Ansichten';nav.prepend(title);
  const personal=nav.querySelector('details:not([data-admin]) summary .rail-label');if(personal)personal.textContent='Meine Einstellungen';
 }
}
