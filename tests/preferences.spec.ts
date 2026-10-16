import {test,expect,type Page} from '@playwright/test';
test.use({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.40'}});
async function login(page:Page){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
}
async function save(page:Page){if(await page.locator('#preferences-save').isEnabled())await page.locator('#preferences-save').click();else await page.locator('#preferences-close').click()}
async function open(page:Page){await page.locator('#preferences-button').click();await expect(page.locator('#preferences-dialog')).toBeVisible()}
test('legacy settings migrate, personal preferences persist and reset',async({page})=>{
 await login(page);const session=await(await page.request.get('/api/session')).json();
 await page.evaluate(key=>localStorage.setItem('cctv-view:'+key,JSON.stringify({layout:'4',quality:'compatible',cameras:['Testkamera-B'],order:['Testkamera-B','Testkamera-A'],timelineHeight:300})),session.preferenceKey);await page.reload();
 await expect(page.locator('#layout')).toHaveValue('2x2');await expect(page.locator('#quality')).toHaveValue('native');await expect(page.locator('#camera-filters .filter').first()).toHaveText('Testkamera-B');await open(page);
 await expect(page.locator('#pref-showNames')).toBeChecked();await page.locator('#pref-density').selectOption('compact');await page.locator('#pref-showNames').uncheck();await page.locator('#pref-showTimestamps').uncheck();await page.locator('#pref-showCodec').uncheck();await page.locator('#pref-showOverview').uncheck();await page.locator('#pref-showThumbnails').uncheck();await page.locator('#pref-shortcuts').uncheck();await page.locator('#pref-seekStep').selectOption('30');await page.locator('#pref-timelineHeight').fill('400');await page.locator('#pref-timelineHeight').dispatchEvent('change');await page.locator('#theme').selectOption('dark');await save(page);
 const thumbs:string[]=[];page.on('request',r=>{if(r.url().includes('/thumbnail'))thumbs.push(r.url())});await page.reload();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen');
 await expect(page.locator('html')).toHaveAttribute('data-density','compact');await expect(page.locator('html')).toHaveAttribute('data-theme','dark');await expect(page.locator('#overview')).toBeHidden();await expect(page.locator('#thumbnails')).toBeHidden();await expect(page.locator('.thumb')).toHaveCount(0);await expect(page.locator('#timeline-viewport')).toHaveCSS('height','400px');expect(thumbs).toEqual([]);
 await expect(page.locator('.camera-tile').first()).toHaveAccessibleName('Testkamera-B');await open(page);await expect(page.locator('#pref-seekStep')).toHaveValue('30');await expect(page.locator('#pref-shortcuts')).not.toBeChecked();
 page.once('dialog',d=>d.dismiss());await page.locator('#preferences-reset').click();await expect(page.locator('#pref-density')).toHaveValue('compact');
 page.once('dialog',d=>d.accept());await page.locator('#preferences-reset').click();await expect(page.locator('#pref-density')).toHaveValue('comfortable');await expect(page.locator('#pref-showNames')).toBeChecked();await expect(page.locator('#pref-shortcuts')).toBeChecked();await expect(page.locator('#theme')).toHaveValue('system');await expect(page.locator('#pref-seekStep')).toHaveValue('5');await expect(page.locator('#layout')).toHaveValue('2x2');await expect(page.locator('#pref-timelineHeight')).toHaveValue('240');await save(page);await expect(page.locator('#layout')).toHaveValue('auto');await expect(page.locator('#quality')).toHaveValue('native');await expect(page.locator('#camera-filters .filter.selected')).toHaveCount(2);
});
test('shortcuts share the configured step and never interfere with forms',async({page})=>{
 await login(page);await open(page);await page.locator('#pref-seekStep').selectOption('30');await save(page);await page.locator('#timeline').focus();await expect(page.locator('#clock')).toHaveText('08:30:00');await page.keyboard.press('ArrowRight');await expect(page.locator('#clock')).toHaveText('08:30:30');await page.keyboard.press('Shift+ArrowLeft');await expect(page.locator('#clock')).toHaveText('08:29:30');await page.keyboard.press('Shift+ArrowRight');await expect(page.locator('#clock')).toHaveText('08:30:30');const after=await page.locator('#clock').textContent();
 await open(page);await page.locator('#pref-shortcuts').uncheck();await save(page);await page.locator('#timeline').focus();await page.keyboard.press('ArrowRight');await expect(page.locator('#clock')).toHaveText(after!);await page.locator('#overview').focus();await page.keyboard.press('ArrowRight');await expect(page.locator('#clock')).toHaveText(after!);
 await open(page);await page.locator('#pref-shortcuts').check();await page.locator('#pref-timelineHeight').focus();await page.keyboard.press('ArrowDown');await expect(page.locator('#clock')).toHaveText(after!);page.once('dialog',d=>d.accept());await page.keyboard.press('Escape');await expect(page.locator('#preferences-dialog')).toBeHidden();await expect(page.locator('#preferences-button')).toBeFocused();
 await page.locator('#select-none').click();await expect(page.locator('#video-grid')).toContainText('Keine Kameras ausgewählt');await expect(page.locator('#camera-range')).toHaveText('Keine Kamera ausgewählt');await page.locator('#select-all').click();await expect(page.locator('.camera-tile')).toHaveCount(2);
});
for(const role of ['admin','viewer'])test(`${role} responsive shell and settings in both themes`,async({page},info)=>{
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message));
 if(role==='viewer')await page.route('**/api/session',async route=>{const response=await route.fetch();if(response.status()!==200)return route.fulfill({response});const session=await response.json();await route.fulfill({response,json:{...session,role:'viewer',kind:'oidc',preferenceKey:'viewer-redesign'}})});
 await login(page);if(role==='viewer')await expect(page.locator('#settings-button')).toBeHidden();else await expect(page.locator('#settings-button')).toBeVisible();
 const navigation=page.getByRole('navigation',{name:'Hauptnavigation'});
 await expect(navigation.getByRole('button',{name:'Aufnahmen suchen'})).toHaveCount(1);
 await expect(navigation.getByRole('button',{name:'Persönliche Einstellungen'})).toHaveCount(1);
 await expect(page.locator('#header-preferences, #mobile-toolbar')).toHaveCount(0);
 for(const theme of ['light','dark']){
  for(const width of [390,768,1440,1920]){
   await page.setViewportSize({width,height:1000});await open(page);await page.locator('#theme').selectOption(theme);await save(page);await open(page);
   await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   const box=await page.locator('#preferences-dialog').boundingBox();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(width);
   await page.screenshot({path:info.outputPath(`${theme}-${width}-settings.png`)});await page.locator('#preferences-close').click();
   expect(await page.locator('.rail-item').evaluateAll(items=>items.filter(item=>!item.hasAttribute('hidden')).every(item=>{const button=item.getBoundingClientRect(),icon=item.querySelector('svg')!.getBoundingClientRect();return Math.abs(button.y+button.height/2-icon.y-icon.height/2)<1}))).toBe(true);
   const icon=await page.locator('.rail-logo svg').boundingBox(),logo=await page.locator('.rail-logo').boundingBox();expect(Math.abs(icon!.y+icon!.height/2-logo!.y-logo!.height/2)).toBeLessThan(1);
   await expect(page.locator('#search-button')).toHaveAccessibleName('Aufnahmen suchen');await expect(page.locator('#play')).toBeVisible();
   await page.locator('#account-menu summary').click({force:role==='viewer'});await expect(page.locator('#logout')).toBeVisible();if(role==='viewer'){await expect(page.locator('#password-button')).toBeHidden();await expect(page.locator('.account-actions')).toBeHidden();await expect(page.locator('.account-chevron')).toBeHidden();await expect(page.locator('#account-menu')).not.toHaveAttribute('open','')}else await expect(page.locator('#password-button')).toBeVisible();await page.keyboard.press('Escape');
   await page.screenshot({path:info.outputPath(`${theme}-${width}-recordings.png`),fullPage:true});
   await page.evaluate(()=>window.scrollTo({top:document.documentElement.scrollHeight,behavior:'instant'}));const transport=await page.locator('#playback-controls').boundingBox();expect(transport!.y).toBeGreaterThanOrEqual(0);expect(transport!.y+transport!.height).toBeLessThanOrEqual(1000);await page.evaluate(()=>window.scrollTo({top:0,behavior:'instant'}));
  }
 }
 expect(errors).toEqual([]);
});

test('original migration, collapsible rail, account placement and laptop controls',async({page},info)=>{
 await login(page);const session=await(await page.request.get('/api/session')).json();
 await page.evaluate(key=>localStorage.setItem('cctv-view:'+key,JSON.stringify({quality:'fast',density:'comfortable'})),session.preferenceKey);
 await page.reload();await expect(page.locator('#quality')).toHaveValue('native');
 await page.locator('#quality').selectOption('balanced');await page.reload();await expect(page.locator('#quality')).toHaveValue('balanced');
 await page.locator('#quality').selectOption('native');
 await page.setViewportSize({width:1366,height:768});
 const brand=await page.locator('.rail-logo').boundingBox(),collapse=await page.locator('#rail-toggle').boundingBox();expect(Math.abs(brand!.y+brand!.height/2-collapse!.y-collapse!.height/2)).toBeLessThan(1);
 const toggle=page.locator('#rail-toggle');await expect(toggle).toHaveAttribute('aria-expanded','true');
 await toggle.click();await expect(toggle).toHaveAttribute('aria-expanded','false');
 await expect(page.locator('.workspace')).toHaveCSS('margin-left','72px');
 await page.reload();await expect(toggle).toHaveAttribute('aria-expanded','false');
 await toggle.focus();await page.keyboard.press('Enter');await expect(toggle).toHaveAttribute('aria-expanded','true');
 await expect(page.locator('.page-heading #account-menu')).toHaveCount(0);
 const account=page.locator('#account-menu');await expect(page.locator('.rail-bottom #account-menu')).toHaveCount(1);
 await account.locator('summary').click();await expect(page.locator('#logout')).toBeVisible();
 await page.keyboard.press('Escape');await expect(account).not.toHaveAttribute('open','');await expect(account.locator('summary')).toBeFocused();
 for(const theme of ['light','dark'])for(const width of [1366,1440,768,390]){
  await page.setViewportSize({width,height:width===1366?768:900});
  await open(page);await page.locator('#theme').selectOption(theme);await save(page);
  await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  if(width>=1366){const top=await page.locator('.view-toolbar').boundingBox(),bottom=await page.locator('#playback-controls').boundingBox();expect(bottom!.y-top!.y).toBeLessThanOrEqual(160)}
  await account.locator('summary').click();await expect(page.locator('#logout')).toBeVisible();
  const menu=await account.locator('.account-actions').boundingBox();expect(menu!.x).toBeGreaterThanOrEqual(0);expect(menu!.y).toBeGreaterThanOrEqual(0);expect(menu!.x+menu!.width).toBeLessThanOrEqual(width);expect(menu!.y+menu!.height).toBeLessThanOrEqual(width===1366?768:900);
  await page.screenshot({path:info.outputPath(`account-${theme}-${width}.png`)});await page.keyboard.press('Escape');
 }
});

test('native fullscreen uses the actual tile and preserves original playback',async({page})=>{
 await login(page);await page.setViewportSize({width:1366,height:768});
 await page.locator('.thumb').first().click();const tile=page.locator('.camera-tile').first(),video=tile.locator('video');
 await expect.poll(()=>video.evaluate(v=>v.readyState)).toBeGreaterThanOrEqual(2);
 await page.locator('#play').click();await expect.poll(()=>video.evaluate(v=>v.currentTime)).toBeGreaterThan(1);
 const src=await video.getAttribute('src');expect(src).toMatch(/\/api\/recordings\/.+\/file$/);
 await tile.locator('.tile-expand').click();await expect.poll(()=>tile.evaluate(el=>document.fullscreenElement===el)).toBe(true);
 await expect(tile.locator('.tile-expand')).toHaveAttribute('aria-label','Vollbild schließen');
 expect(await video.getAttribute('src')).toBe(src);await expect.poll(()=>video.evaluate(v=>!v.paused)).toBe(true);
 await tile.locator('.tile-expand').click();await expect.poll(()=>page.evaluate(()=>document.fullscreenElement===null)).toBe(true);
 await video.dblclick();await expect.poll(()=>tile.evaluate(el=>document.fullscreenElement===el)).toBe(true);
 await page.keyboard.press('Escape');await expect.poll(()=>page.evaluate(()=>document.fullscreenElement===null)).toBe(true);
 await page.locator('.page-heading h1').click();await page.keyboard.press('f');await expect.poll(()=>tile.evaluate(el=>document.fullscreenElement===el)).toBe(true);
 await page.evaluate(()=>document.exitFullscreen());
});

test('menus scroll in short windows and keep close controls reachable',async({page},info)=>{
 await login(page);
 for(const width of [1366,390]){
  await page.setViewportSize({width,height:500});
  for(const [trigger,id,close] of [['preferences-button','preferences-dialog','preferences-close'],['settings-button','camera-dialog','close-dialog'],['search-button','search-dialog','search-close']]){
   await page.locator('#'+trigger).click();const dialog=page.locator('#'+id);await expect(dialog).toBeVisible();
   await dialog.locator('.dialog-body').evaluate(el=>el.scrollTo(0,el.scrollHeight));
   const metrics=await dialog.locator('.dialog-body').evaluate(el=>({top:el.scrollTop,remaining:el.scrollHeight-el.clientHeight,overflow:el.scrollWidth>el.clientWidth+1}));
   expect(metrics.overflow).toBe(false);if(metrics.remaining>1)expect(metrics.top).toBeGreaterThan(0);
   const button=page.locator('#'+close);await expect(button).toBeInViewport();
   await page.screenshot({path:info.outputPath(`${id}-${width}.png`)});
   await button.click();await expect(dialog).toBeHidden();
  }
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 }
});


test('tile controls retain keyboard focus when the responsive grid rebuilds',async({page})=>{
 await login(page);
 await page.locator('#video-grid').evaluate(el=>{
  el.dataset.testRenders='0';
  new MutationObserver(()=>el.dataset.testRenders=String(Number(el.dataset.testRenders)+1)).observe(el,{childList:true});
 });
 for(const [index,selector] of ['.tile-audio','.tile-title','.tile-expand'].entries()){
  const control=page.locator(selector).first();await control.focus();await expect(control).toBeFocused();
  const before=Number(await page.locator('#video-grid').getAttribute('data-test-renders'));
  await page.setViewportSize({width:index%2?1440:1366,height:1000});
  await expect.poll(async()=>Number(await page.locator('#video-grid').getAttribute('data-test-renders'))).toBeGreaterThan(before);
  await expect(control).toBeFocused();
 }
});
