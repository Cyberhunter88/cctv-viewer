import {test,expect,type Page} from './fixtures';
test.use({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.81'}});
async function login(page:Page){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();
}
async function openPreferences(page:Page){await page.locator('#preferences-button').click()}
async function persistPreferences(page:Page){if(await page.locator('#preferences-save').isEnabled())await page.locator('#preferences-save').click();else await page.locator('#preferences-close').click()}

test('preferences are drafts until saved, including theme, reset and validation',async({page})=>{
 await page.emulateMedia({colorScheme:'light'});await login(page);
 const session=await(await page.request.get('/api/session')).json();
 const stored=()=>page.evaluate(key=>localStorage.getItem('cctv-view:'+key),session.preferenceKey);
 await openPreferences(page);await expect(page.locator('#preferences-save')).toBeDisabled();
 const before=await stored();
 await page.locator('#pref-density').selectOption('compact');await page.locator('#theme').selectOption('dark');await page.locator('#aspect').selectOption('4/3');await page.locator('#pref-showNames').uncheck();
 await expect(page.locator('html')).toHaveAttribute('data-density','comfortable');await expect(page.locator('html')).toHaveAttribute('data-theme','light');await expect(page.locator('body')).not.toHaveClass(/hide-showNames/);expect(await stored()).toBe(before);
 // System theme updates must not overwrite a user's unsaved selection.
 await page.emulateMedia({colorScheme:'dark'});await page.emulateMedia({colorScheme:'light'});await page.evaluate(()=>window.dispatchEvent(new StorageEvent('storage',{key:'cctv-theme',newValue:null})));await expect(page.locator('#theme')).toHaveValue('dark');
 page.once('dialog',d=>d.dismiss());await page.keyboard.press('Escape');await expect(page.locator('#preferences-dialog')).toBeVisible();await expect(page.locator('#pref-density')).toHaveValue('compact');
 page.once('dialog',d=>d.accept());await page.locator('#preferences-cancel').click();await expect(page.locator('#preferences-dialog')).toBeHidden();
 await openPreferences(page);await expect(page.locator('#pref-density')).toHaveValue('comfortable');await expect(page.locator('#aspect')).toHaveValue('auto');await expect(page.locator('#theme')).toHaveValue('system');await expect(page.locator('#preferences-save')).toBeDisabled();
 await page.locator('#pref-timelineHeight').fill('20');await page.locator('#preferences-save').click();await expect(page.locator('#preferences-dialog')).toBeVisible();expect(await stored()).toBe(before);
 await page.locator('#pref-timelineHeight').fill('333');await page.locator('#pref-density').selectOption('compact');await page.locator('#theme').selectOption('dark');await page.locator('#aspect').selectOption('4/3');await persistPreferences(page);
 await expect(page.locator('html')).toHaveAttribute('data-density','compact');await expect(page.locator('html')).toHaveAttribute('data-theme','dark');await page.reload();await expect(page.locator('#app')).toBeVisible();
 await openPreferences(page);await expect(page.locator('#aspect')).toHaveValue('4/3');await expect(page.locator('#pref-timelineHeight')).toHaveValue('333');await expect(page.locator('#preferences-save')).toBeDisabled();
 page.once('dialog',d=>d.accept());await page.locator('#preferences-reset').click();await expect(page.locator('html')).toHaveAttribute('data-density','compact');await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
 page.once('dialog',d=>d.accept());await page.locator('#preferences-close').click();await openPreferences(page);await expect(page.locator('#aspect')).toHaveValue('4/3');await expect(page.locator('#pref-density')).toHaveValue('compact');
 page.once('dialog',d=>d.accept());await page.locator('#preferences-reset').click();await persistPreferences(page);await expect(page.locator('html')).toHaveAttribute('data-density','comfortable');await expect(page.locator('html')).toHaveAttribute('data-theme','light');
 await page.locator('#quality').selectOption('balanced');await page.reload();await expect(page.locator('#quality')).toHaveValue('balanced');
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
 await page.reload();await expect(page.locator('#app')).toBeVisible();await page.locator('#settings-button').click();
 await expect(page.locator('#camera-list .camera-entry')).toHaveCount(6);await expect(page.locator('#camera-list')).toContainText('Quelle offline');
 await page.locator('#camera-list').getByRole('button',{name:'Testkamera-A',exact:true}).click();
 await page.locator('#camera-form input[name=name]').fill('Synthetischer Entwurf');
 page.once('dialog',d=>d.dismiss());await page.locator('#camera-list').getByRole('button',{name:'Testkamera-B',exact:true}).click();await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Synthetischer Entwurf');
 page.once('dialog',d=>d.dismiss());await page.locator('#close-dialog').click();await expect(page.locator('#camera-dialog')).toBeVisible();
 page.once('dialog',d=>d.accept());await page.locator('#camera-list').getByRole('button',{name:'Testkamera-B',exact:true}).click();await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Testkamera-B');expect(posts).toBe(0);
 await page.locator('#camera-form input[name=name]').fill('Synthetisch gespeichert');await page.locator('#camera-form button[type=submit]').click();await expect(page.locator('#camera-error')).toContainText('Synthetischer Speicherfehler');await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Synthetisch gespeichert');
 failSave=false;await page.locator('#camera-form button[type=submit]').click();await expect(page.locator('#camera-list .is-selected')).toContainText('Synthetisch gespeichert');await expect(page.locator('#camera-error')).toBeEmpty();
 await page.locator('#close-dialog').click();await page.locator('#settings-button').click();
 for(const theme of ['light','dark'])for(const width of [390,768,1366,1920]){
  await page.locator('#close-dialog').click();await page.setViewportSize({width,height:width===1366?768:900});await openPreferences(page);await page.locator('#theme').selectOption(theme);await persistPreferences(page);await page.locator('#settings-button').click();
  await expect(page.locator('#camera-list')).toBeVisible();await expect(page.locator('#camera-dialog .dialog-body')).toBeVisible();
  await page.screenshot({path:info.outputPath(`cameras-${theme}-${width}.png`)});
  await page.locator('#camera-list').getByRole('button',{name:'Synthetisch gespeichert',exact:true}).click();await expect(page.locator('#camera-form')).toBeVisible();
  if(width<900){await expect(page.locator('.camera-overview')).toBeHidden();await expect(page.locator('#camera-back')).toBeInViewport()}
  await page.locator('#camera-advanced summary').click();await page.locator('#view-mode').selectOption('dual');await expect(page.locator('#dual-fields')).toBeVisible();await page.locator('#view-mode').selectOption('single');
  await expect.poll(()=>page.locator('#camera-dialog .dialog-body').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
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
 await page.locator('#settings-button').click();await expect(page.locator('#camera-list')).toContainText('Noch keine Kameras');await page.setViewportSize({width:390,height:700});await page.locator('#new-camera').click();await expect(page.locator('#camera-form')).toBeVisible();
 await page.locator('#browse-folder').click();await expect(page.locator('#folder-browser')).toBeVisible();await page.locator('#camera-cancel').click();await expect(page.locator('#camera-list')).toBeVisible();await page.locator('#close-dialog').click();
 for(const width of [390,768,1366]){await page.setViewportSize({width,height:800});await expect(page.locator('#account-menu')).not.toHaveAttribute('open','');await expect(page.getByRole('button',{name:'Abmelden',exact:true})).toBeVisible()}
 await page.locator('#rail-toggle').click();await expect(page.locator('#logout')).toBeVisible();await page.locator('#logout').click();await expect(page.locator('#login')).toBeVisible();
});

for(const dpr of [1,2])test.describe(`dialog corners at device scale ${dpr}`,()=>{
 test.use({deviceScaleFactor:dpr});
 test('short dialogs retain four rounded corners, inner scrolling and reachable close buttons',async({page},info)=>{
  await login(page);
  for(const theme of ['light','dark']){
   await openPreferences(page);await page.locator('#theme').selectOption(theme);await persistPreferences(page);
   for(const width of [390,1366]){
    await page.setViewportSize({width,height:500});
    for(const [trigger,id,close] of [['preferences-button','preferences-dialog','preferences-close'],['settings-button','camera-dialog','close-dialog'],['search-button','search-dialog','search-close']]){
     await page.locator('#'+trigger).click();const dialog=page.locator('#'+id),body=dialog.locator('.dialog-body');
     await body.evaluate(el=>el.scrollTop=el.scrollHeight);
     const m=await dialog.evaluate(el=>{const c=getComputedStyle(el),b=el.querySelector('.dialog-body')!;return {radii:[c.borderTopLeftRadius,c.borderTopRightRadius,c.borderBottomLeftRadius,c.borderBottomRightRadius],overflow:c.overflow,bodyOverflow:b.scrollWidth>b.clientWidth+1}});
     expect(m.radii.every(r=>r==='14px')).toBe(true);expect(m.overflow).toBe('hidden');expect(m.bodyOverflow).toBe(false);await expect(page.locator('#'+close)).toBeInViewport();
     await page.screenshot({path:info.outputPath(`${id}-${theme}-${width}-dpr${dpr}.png`)});await page.locator('#'+close).click();
    }
    await page.locator('#settings-button').click();await page.locator('#open-management').click();await expect(page.locator('#management-dialog')).toBeVisible();await expect(page.locator('#management-dialog')).toHaveCSS('overflow','hidden');
    await page.locator('#management-dialog .dialog-body').evaluate(el=>el.scrollTop=el.scrollHeight);await expect(page.locator('#management-close')).toBeInViewport();await page.screenshot({path:info.outputPath(`management-${theme}-${width}-dpr${dpr}.png`)});await page.locator('#management-close').click();
    await page.locator('#account-menu summary').click();await page.locator('#password-button').click();await expect(page.locator('#password-dialog .dialog-body')).toBeVisible();await page.locator('#password-close').click();
   }
  }
 });
});
