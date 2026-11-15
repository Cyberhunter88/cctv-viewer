import {openPage,openNavigation,preferenceControl} from './fixtures';
import {test,expect,savePreferences as persistPreferences,type Page} from './fixtures';
test.use({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.81'}});
async function login(page:Page){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();
}
async function openPreferences(page:Page){await openNavigation(page,'preferences-button')}

test('preferences apply and save immediately, including theme, reset and validation',async({page})=>{
 await page.emulateMedia({colorScheme:'light'});await login(page);
 // Delay account writes so UI updates cannot stand in for durable storage.
 await page.route('**/api/preferences',async route=>{
  if(route.request().method()!=='PATCH')return route.continue();
  const response=await route.fetch(),body=await response.body(),status=response.status(),headers=response.headers();await new Promise(resolve=>setTimeout(resolve,250));await route.fulfill({status,headers,body});
 });
 const session=await(await page.request.get('/api/session')).json();
 const account=async()=>(await(await page.request.get('/api/preferences')).json())||{};
 // A custom grid shows the column field, the remaining numeric preference for the validation check.
 const customSaved=page.waitForResponse(response=>response.url().endsWith('/api/preferences')&&response.request().method()==='PATCH'&&response.ok());await page.locator('#layout').selectOption('custom');await customSaved;
 await openPreferences(page);await expect(page.locator('#preferences-save')).toHaveCount(0);await expect(page.locator('#preferences-cancel')).toHaveCount(0);
 const saved=(check:(body:Record<string,unknown>)=>boolean)=>page.waitForResponse(response=>response.url().endsWith('/api/preferences')&&response.request().method()==='PATCH'&&check(response.request().postDataJSON())&&response.ok());
 let write=saved(body=>body.density==='compact');await (await preferenceControl(page,'pref-density')).selectOption('compact');
 await expect(page.locator('html')).toHaveAttribute('data-density','compact');await write;
 write=saved(body=>body.theme==='dark');await (await preferenceControl(page,'theme')).selectOption('dark');await expect(page.locator('html')).toHaveAttribute('data-theme','dark');await write;
 write=saved(body=>body.showTimestamps===false);await (await preferenceControl(page,'aspect')).selectOption('4/3');await (await preferenceControl(page,'pref-showTimestamps')).uncheck();await write;
 await expect(page.locator('body')).toHaveClass(/hide-showTimestamps/);await expect(page.locator('#preference-status')).toHaveText('Alle Änderungen gespeichert.');
 // System theme updates must not overwrite the saved choice.
 await page.emulateMedia({colorScheme:'dark'});await page.emulateMedia({colorScheme:'light'});await expect(page.locator('#theme')).toHaveValue('dark');await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
 // Leaving never asks for confirmation; there is no draft to lose.
 let asked=false;const onDialog=(dialog:import('@playwright/test').Dialog)=>{asked=true;void dialog.dismiss()};page.on('dialog',onDialog);
 await page.keyboard.press('Escape');await expect(page.locator('#preferences-dialog')).toBeHidden();page.off('dialog',onDialog);expect(asked).toBe(false);
 // Invalid numbers are reported and never stored; a valid value is saved after typing pauses.
 await (await preferenceControl(page,'grid-columns')).fill('20');await expect(page.locator('#preference-status')).toContainText('Ungültiger Wert',{timeout:3000});expect((await account()).columns).not.toBe(20);
 write=saved(body=>body.columns===3);await (await preferenceControl(page,'grid-columns')).fill('3');await write;
 await page.reload();await expect(page.locator('#app')).toBeVisible();await expect(page.locator('html')).toHaveAttribute('data-density','compact');await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
 await openPreferences(page);await expect(page.locator('#aspect')).toHaveValue('4/3');await expect(page.locator('#grid-columns')).toHaveValue('3');
 // Declining the reset keeps everything; accepting applies and saves the defaults at once.
 page.once('dialog',d=>d.dismiss());await page.locator('#preferences-reset').click();await expect(page.locator('html')).toHaveAttribute('data-density','compact');
 write=saved(body=>body.density==='comfortable'&&body.theme==='system');page.once('dialog',d=>d.accept());await page.locator('#preferences-reset').click();await write;
 await expect(page.locator('html')).toHaveAttribute('data-density','comfortable');await expect(page.locator('html')).toHaveAttribute('data-theme','light');await expect(page.locator('#aspect')).toHaveValue('auto');
 expect(session.preferenceKey).toBeTruthy();
 const qualitySaved=page.waitForResponse(response=>response.url().endsWith('/api/preferences')&&response.request().method()==='PATCH'&&response.request().postDataJSON().quality==='balanced'&&response.ok());
 await persistPreferences(page);await page.locator('#quality').selectOption('balanced');await qualitySaved;await page.reload();await expect(page.locator('#quality')).toHaveValue('balanced');
 await page.unrouteAll({behavior:'wait'});
});

test('six-camera editor preserves drafts and failed saves, and adapts to narrow windows',async({page},info)=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));await login(page);
 const original=await(await page.request.get('/api/cameras')).json();
 let cameras=[...original.filter((c:{id:string})=>['Testkamera-A','Testkamera-B'].includes(c.id))];
 for(let i=0;i<4;i++)cameras.push({...cameras[0],id:'synthetic-ui-'+i,name:i===0?'Synthetische Kamera mit einem sehr langen Anzeigenamen im Eingangsbereich':'Synthetische Kamera '+i,sourceStatus:i===0?'offline':'online',viewMode:i%2?'dual':'single'});
 let failSave=true,posts=0;
 await page.route('**/api/cameras',async route=>{
  if(route.request().method()==='GET')return route.fulfill({json:cameras});
  if(route.request().method()==='POST'){
   posts++;if(failSave)return route.fulfill({status:400,json:{error:'Synthetischer Speicherfehler'}});
   const value=route.request().postDataJSON();cameras=cameras.map(c=>c.id===value.id?{...c,...value}:c);return route.fulfill({json:cameras.find(c=>c.id===value.id)});
  }
  return route.continue();
 });
 await page.reload();await expect(page.locator('#app')).toBeVisible();await openNavigation(page,'settings-button');
 await expect(page.locator('#camera-list .camera-entry')).toHaveCount(6);await expect(page.locator('#camera-list')).toContainText('Quelle offline');
 await page.locator('#camera-list .camera-edit').filter({has:page.locator('.camera-name').getByText('Testkamera-A',{exact:true})}).click();
 await page.locator('#camera-form input[name=name]').fill('Synthetischer Entwurf');
 page.once('dialog',d=>d.dismiss());await page.locator('#camera-list .camera-edit').filter({has:page.locator('.camera-name').getByText('Testkamera-B',{exact:true})}).click();await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Synthetischer Entwurf');
 page.once('dialog',d=>d.dismiss());await page.locator('#close-dialog').click();await expect(page.locator('#camera-dialog')).toBeVisible();
 page.once('dialog',d=>d.accept());await page.locator('#camera-list .camera-edit').filter({has:page.locator('.camera-name').getByText('Testkamera-B',{exact:true})}).click();await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Testkamera-B');expect(posts).toBe(0);
 await page.locator('#camera-form input[name=name]').fill('Synthetisch gespeichert');await page.locator('#camera-form button[type=submit]').click();await expect(page.locator('#camera-error')).toContainText('Synthetischer Speicherfehler');await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Synthetisch gespeichert');
 failSave=false;await page.locator('#camera-form button[type=submit]').click();await expect(page.locator('#camera-list .is-selected')).toContainText('Synthetisch gespeichert');await expect(page.locator('#camera-error')).toBeEmpty();
 await page.locator('#close-dialog').click();await openNavigation(page,'settings-button');
 for(const theme of ['light','dark'])for(const width of [390,768,1366,1920]){
  await page.locator('#close-dialog').click();await page.setViewportSize({width,height:width===1366?768:900});await openPreferences(page);await (await preferenceControl(page,'theme')).selectOption(theme);await persistPreferences(page);await openNavigation(page,'settings-button');
  await expect(page.locator('#camera-list')).toBeVisible();await expect(page.locator('#camera-dialog')).toBeVisible();
  await page.screenshot({path:info.outputPath(`cameras-${theme}-${width}.png`)});
  await page.locator('#camera-list .camera-edit').filter({has:page.locator('.camera-name').getByText('Synthetisch gespeichert',{exact:true})}).click();await expect(page.locator('#camera-form')).toBeVisible();
  if(width<900){await expect(page.locator('.camera-overview')).toBeVisible();await expect(page.locator('#camera-back')).toBeInViewport()}
  await page.locator('#view-mode').selectOption('dual');await page.locator('#camera-tab-recordings').click();await page.locator('#camera-advanced summary').click();await expect(page.locator('#dual-fields')).toBeVisible();await page.locator('#camera-tab-general').click();await page.locator('#view-mode').selectOption('single');
  await expect.poll(()=>page.locator('#camera-dialog').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
  await page.screenshot({path:info.outputPath(`editor-${theme}-${width}.png`)});
  await page.locator('#camera-cancel').click();
 }
 await page.setViewportSize({width:1366,height:900});
 const entries=page.locator('#camera-list .camera-entry');await entries.nth(1).getByRole('button',{name:/Nach oben verschieben/}).click();await expect(entries.first()).toContainText('Synthetisch gespeichert');
 await entries.first().dragTo(entries.nth(1));await expect(entries.first()).toContainText('Testkamera-A');
 const session=await(await page.request.get('/api/session')).json();const order=await page.evaluate(key=>JSON.parse(localStorage.getItem('cctv-view:'+key)!).order,session.preferenceKey);expect(order.slice(0,2)).toEqual(['Testkamera-A','Testkamera-B']);
 await page.locator('#close-dialog').click();expect(errors).toEqual([]);
});

test('empty camera list and direct logout are accessible without opening the account menu',async({page})=>{
 await login(page);await page.route('**/api/cameras',route=>route.fulfill({json:[]}));await page.reload();await expect(page.locator('#app')).toBeVisible();
 await openNavigation(page,'settings-button');await expect(page.locator('#camera-list')).toContainText('Noch keine Kameras');await page.setViewportSize({width:390,height:700});await page.locator('#new-camera').click();await expect(page.locator('#camera-form')).toBeVisible();
 await page.locator('#camera-tab-recordings').click();await page.locator('#browse-folder').click();await expect(page.locator('#folder-browser')).toBeVisible();await page.locator('#camera-cancel').click();await expect(page.locator('#camera-list')).toBeVisible();await page.locator('#close-dialog').click();
 for(const width of [390,768,1366]){await page.setViewportSize({width,height:800});await expect(page.locator('#account-menu')).not.toHaveAttribute('open','');await expect(page.getByRole('button',{name:'Abmelden',exact:true})).toBeVisible()}
 await page.locator('#rail-toggle').click();await expect(page.locator('#logout')).toBeVisible();await page.locator('#logout').click();await expect(page.locator('#login')).toBeVisible();
});

for(const dpr of [1,2])test.describe(`page layout at device scale ${dpr}`,()=>{
 test.use({deviceScaleFactor:dpr});
 test('pages use document scrolling and utility dialogs retain their own scroll area',async({page},info)=>{
  await login(page);
  for(const theme of ['light','dark']){
   await openPreferences(page);await (await preferenceControl(page,'theme')).selectOption(theme);await persistPreferences(page);
   for(const width of [390,1366]){
    await page.setViewportSize({width,height:500});
    for(const path of ['settings/appearance','settings/playback','admin/cameras','admin/users','admin/oidc','admin/operation','settings/account','settings/sessions','admin/sessions','search']){
     await openPage(page,path);await expect(page.locator('dialog[open]')).toHaveCount(0);
     await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
     await page.screenshot({path:info.outputPath(`${path.replace('/','-')}-${theme}-${width}-dpr${dpr}.png`),fullPage:true});
    }
    await openPage(page,'recordings');
   }
  }
 });
});
