import {test,expect,openPage,type Page} from './fixtures';
test.beforeEach(async({context},info)=>{const hash=[...info.testId].reduce((n,c)=>(Math.imul(n,31)+c.charCodeAt(0))>>>0,0);await context.setExtraHTTPHeaders({'X-Forwarded-For':`2001:db8:2::${(hash>>>16).toString(16)}:${(hash&65535).toString(16)}`})});
async function login(page:Page){await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible()}

test('sidebar collapses to usable icons, persists and expands at tablet width',async({page})=>{
 await page.setViewportSize({width:1366,height:800});await login(page);
 const rail=page.locator('#app .rail'),workspace=page.locator('#app .workspace');
 await expect(page.locator('#rail-toggle')).toHaveAttribute('aria-expanded','true');
 const saved=page.waitForResponse(response=>response.url().endsWith('/api/preferences')&&response.request().method()==='PATCH'&&response.request().postDataJSON().railCollapsed===true&&response.ok());
 await page.locator('#rail-toggle').click();await saved;
 await expect(page.locator('#rail-toggle')).toHaveAttribute('aria-expanded','false');
 await expect.poll(()=>rail.evaluate(el=>Math.round(el.getBoundingClientRect().width))).toBe(72);
 await expect.poll(()=>workspace.evaluate(el=>Math.round(parseFloat(getComputedStyle(el).marginLeft)))).toBe(72);
 await expect(page.locator('a[data-page="recordings"] .rail-label')).toBeHidden();
 await expect(page.getByRole('link',{name:'Betrieb'})).toBeHidden();
 await expect(page.locator('#rail-nav .nav-group')).toHaveCount(2);
 await page.locator('#rail-nav .nav-group[data-admin] > summary').click();
 await expect(page.locator('#rail-toggle')).toHaveAttribute('aria-expanded','true');
 await page.getByRole('link',{name:'Betrieb'}).click();await expect(page).toHaveURL(/#admin\/operation$/);
 await expect(page.locator('#operation-panel')).toBeVisible();
 await page.reload();await expect(page.locator('#app')).toBeVisible();
 await expect.poll(()=>rail.evaluate(el=>Math.round(el.getBoundingClientRect().width))).toBeGreaterThan(200);
 await page.setViewportSize({width:768,height:800});
 await expect.poll(()=>rail.evaluate(el=>Math.round(el.getBoundingClientRect().width))).toBeGreaterThan(200);
 await expect(page.locator('a[data-page="recordings"] .rail-label')).toBeVisible();
 await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test('settings and administration pages use matching menu names and back actions',async({page})=>{
 await login(page);
 for(const [path,heading] of [['settings/appearance','Darstellung'],['settings/playback','Wiedergabe'],['settings/account','Mein Konto'],['settings/sessions','Meine Sitzungen'],['admin/cameras','Kameras verwalten'],['admin/users','Benutzer'],['admin/sessions','Alle Sitzungen'],['admin/oidc','Anmeldung / OIDC'],['admin/operation','Betrieb']] as const){
  await openPage(page,path);await expect(page.locator('a[data-page="'+path+'"]').first()).toHaveAttribute('aria-current','page');
  await expect(page.locator('.content-page:visible h2').first()).toHaveText(heading);
  await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 }
 await openPage(page,'settings/sessions');await page.locator('#sessions-close').click();await expect(page).toHaveURL(/#recordings$/);
});

test('direct links, history, draft guards and paused playback',async({page})=>{
 await login(page);await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen');await page.locator('.thumb').first().click();await page.locator('#play').click();await expect(page.locator('#play')).toHaveAttribute('aria-label','Pause');
 const selected=await page.locator('#camera-filters .selected').allTextContents();
 await openPage(page,'search');const position=await page.locator('#clock').textContent();await expect(page.locator('dialog[open]')).toHaveCount(0);await expect.poll(()=>page.locator('video').evaluateAll(videos=>videos.every(v=>(v as HTMLVideoElement).paused))).toBe(true);
 await page.goBack();await expect(page).toHaveURL(/#recordings$/);await expect(page.locator('#play')).toHaveAttribute('aria-label','Abspielen');await expect(page.locator('#clock')).toHaveText(position!);expect(await page.locator('#camera-filters .selected').allTextContents()).toEqual(selected);await expect(page.locator('#date')).toHaveValue('2026-10-14');
 await page.goForward();await expect(page.locator('#search-dialog')).toBeVisible();await page.reload();await expect(page.locator('#search-dialog')).toBeVisible();
 await openPage(page,'settings/appearance');await page.locator('#pref-density').selectOption('compact');await openPage(page,'settings/playback');await expect(page.locator('#pref-density')).toHaveValue('compact');await expect(page.locator('#preferences-save')).toBeEnabled();
 await page.goBack();await expect(page).toHaveURL(/#settings\/appearance$/);page.once('dialog',d=>d.dismiss());await page.goBack();await expect(page).toHaveURL(/#settings\/appearance$/);await expect(page.locator('#pref-density')).toHaveValue('compact');await openPage(page,'settings/playback');
 page.once('dialog',d=>d.dismiss());await page.locator('a[data-page=search]').click();await expect(page).toHaveURL(/#settings\/playback$/);await expect(page.locator('#pref-density')).toHaveValue('compact');
 page.once('dialog',d=>d.accept());await page.locator('a[data-page=search]').click();await expect(page).toHaveURL(/#search$/);
 await page.goto('/#admin/operation');await expect(page.locator('#operation-panel')).toBeVisible();await expect(page.locator('#operation-form input[name=cacheMB]')).not.toHaveValue('');
 await page.goto('/#unknown');await expect(page).toHaveURL(/#recordings$/);
});

test('local user lifecycle and session revocation through the GUI',async({page,browser})=>{
 await login(page);const username='synthetic-ui-reader';const password='synthetic user lifecycle password';let created=false;
 const secondContext=await browser.newContext({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.201'}});const second=await secondContext.newPage();
 try{
  await openPage(page,'admin/users');await page.locator('#user-form input[name=username]').fill(username);await page.locator('#user-form input[name=password]').fill(password);await page.locator('#user-form button[type=submit]').click();await expect(page.locator('#user-list')).toContainText(username);created=true;
  await page.locator('#user-search').fill('synthetic-ui-reader');await expect(page.locator('#user-list .camera-entry')).toHaveCount(1);
  await second.goto('/');await second.locator('#login-form input[name=username]').fill(username);await second.locator('#login-form input[name=password]').fill(password);await second.locator('#login-form button').click();await expect(second.locator('#app')).toBeVisible();await expect(second.locator('[data-admin]')).toBeHidden();
  await second.goto('/#admin/users');await expect(second).toHaveURL(/#recordings$/);await openPage(second,'settings/sessions');await expect(second.locator('#session-list')).toContainText('Diese Sitzung');await expect(second.locator('#session-list .session-entry')).toHaveCount(1);
  await openPage(page,'admin/sessions');const row=page.locator('.session-entry').filter({hasText:username});await expect(row).toHaveCount(1);page.once('dialog',d=>d.accept());await row.getByRole('button',{name:'Sitzung beenden'}).click();await expect(row).toHaveCount(0);
  await second.reload();await expect(second.locator('#login')).toBeVisible();
  await openPage(page,'admin/users');await page.locator('#user-search').fill(username);await page.locator('#user-list .camera-entry button').first().click();await page.locator('#user-form input[name=disabled]').check();await page.locator('#user-form button[type=submit]').click();await expect(page.locator('#user-list')).toContainText('gesperrt');
  await page.locator('#user-list .camera-entry button').first().click();await page.locator('#user-form input[name=disabled]').uncheck();await page.locator('#user-form select[name=role]').selectOption('admin');await page.locator('#user-form input[name=password]').fill('synthetic reset lifecycle password');await page.locator('#user-form button[type=submit]').click();await expect(page.locator('#user-list')).toContainText('Administrator');
  page.once('dialog',d=>d.accept());await page.locator('#user-list').getByRole('button',{name:'Löschen'}).click();await expect(page.locator('#user-list .camera-entry')).toHaveCount(0);created=false;
 }finally{await secondContext.close();if(created){const s=await(await page.request.get('/api/session')).json();await page.request.delete('/api/admin/users/'+username,{headers:{Origin:new URL(page.url()).origin,'X-CSRF-Token':s.csrf}})}}
});

test('own sessions can be ended together',async({page,browser})=>{
 await login(page);const secondContext=await browser.newContext({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.202'}});const second=await secondContext.newPage();
 try{await login(second);await openPage(page,'settings/sessions');await expect(page.locator('#session-list')).toContainText('Diese Sitzung');page.once('dialog',d=>d.accept());await page.locator('#sessions-end-own').click();await expect(page.locator('#login')).toBeVisible();await second.reload();await expect(second.locator('#login')).toBeVisible()}finally{await secondContext.close()}
});
