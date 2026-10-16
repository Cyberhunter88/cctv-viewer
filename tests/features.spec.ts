import {test,expect} from './fixtures';
// The disposable server trusts loopback as a proxy; isolate this suite's client IP from other browser suites.
async function login(page:any){await page.setExtraHTTPHeaders({'X-Forwarded-For':'192.0.2.1'});await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});await expect(page.locator('#rescan')).toBeEnabled({timeout:20000})}
test('search filters open recordings; removed functions and endpoints are absent',async({page})=>{
 await login(page);await page.locator('#search-button').click();await page.locator('#search-form input[name=q]').fill('Testkamera-B');await page.locator('#search-form button[type=submit]').click();await expect(page.locator('.search-result')).toHaveCount(1);await expect(page.locator('.search-result').first()).toHaveCSS('border-top-style','solid');await page.locator('.search-result').click();await expect(page.locator('#clock')).toHaveText('08:30:05');
 for(const id of ['export-button','range-mode','image-events','bookmarks-button','time-link','camera-status-button'])await expect(page.locator('#'+id)).toHaveCount(0);
 for(const route of ['/api/bookmarks','/api/exports','/api/calendar','/api/recorded-day','/api/camera-status'])expect((await page.request.get(route)).status()).toBe(404);
});
test('search loads later pages and reports the full result count',async({page})=>{
 const offsets:number[]=[];
 await page.route('**/api/search*',async route=>{
  const url=new URL(route.request().url()),offset=Number(url.searchParams.get('offset')||0);offsets.push(offset);
  const count=offset===0?100:50,results=Array.from({length:count},(_,i)=>({id:`r-${offset+i}`,cameraId:'Testkamera-A',cameraName:'Testkamera-A',filename:`video-${offset+i}.mp4`,start:1791959400,duration:10}));
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({results,total:150,offset,limit:100,hasMore:offset+count<150})});
 });
 await login(page);await page.locator('#search-button').click();await page.locator('#search-form input[name=q]').fill('video');await page.locator('#search-submit').click();
 await expect(page.locator('.search-result')).toHaveCount(100);await expect(page.locator('#search-count')).toHaveText('100 von 150 Treffern angezeigt');await page.locator('#search-more').click();
 await expect(page.locator('.search-result')).toHaveCount(150);await expect(page.locator('#search-count')).toHaveText('150 von 150 Treffern angezeigt');await expect(page.locator('#search-more')).toBeHidden();expect(offsets).toEqual([0,100]);
});
test('search result opens the right hotspot page',async({page})=>{
 await page.route('**/api/cameras',async route=>{
  if(route.request().method()!=='GET')return route.continue();
  const cameras=Array.from({length:6},(_,i)=>({id:`camera-${i+1}`,name:`Kamera ${i+1}`,folder:`Kamera ${i+1}`,pattern:'',timezone:'Europe/Berlin',offset:0}));
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(cameras)});
 });
 await login(page);
 const day=await(await page.request.get('/api/day?date=2026-10-14')).json();
 await page.route('**/api/search*',route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({results:[{id:'search-hit',cameraId:'camera-6',cameraName:'Kamera 6',filename:'hit.mp4',start:day.recordings[0].start,duration:10}],total:1,offset:0,limit:100,hasMore:false})}));
 await page.locator('#layout').selectOption('hotspot');await page.locator('#search-button').click();await page.locator('#search-form input[name=q]').fill('hit');await page.locator('#search-submit').click();await page.locator('.search-result').click();
 await expect(page.locator('#page-display')).toHaveText('Seite 2 von 2');await expect(page.locator('.camera-group .tile-title').filter({hasText:'Kamera 6'})).toBeVisible();
});
test('keyboard shortcuts leave focused buttons to their native action',async({page})=>{
 await login(page);await page.locator('#search-button').focus();await page.keyboard.press('Space');await expect(page.locator('#search-dialog')).toBeVisible();await expect(page.locator('#play')).toHaveAttribute('aria-label','Abspielen');
});
test('boot error offers a retry that can recover to login',async({page})=>{
 let attempts=0;
 await page.route('**/api/session',async route=>{attempts++;if(attempts===1)await route.fulfill({status:503,contentType:'application/json',body:'{"error":"offline"}'});else await route.continue()});
 await page.goto('/');await expect(page.locator('#boot-error')).toBeVisible();await expect(page.locator('#boot-error-message')).toContainText('503');await page.locator('#boot-retry').click();await expect(page.locator('#login')).toBeVisible();expect(attempts).toBe(2);
});
test('new toast remains visible after an older toast timeout',async({page})=>{
 await page.clock.install({time:new Date('2026-10-14T06:00:00Z')});
 await page.route('**/api/day?date=2026-10-15',route=>route.fulfill({status:500,contentType:'application/json',body:'{"error":"Erster Fehler"}'}));
 await page.route('**/api/day?date=2026-10-16',route=>route.fulfill({status:500,contentType:'application/json',body:'{"error":"Zweiter Fehler"}'}));
 await login(page);await page.locator('#next-day').click();await expect(page.locator('#toast')).toContainText('Erster Fehler');await page.clock.fastForward(5000);await page.locator('#next-day').click();await expect(page.locator('#toast')).toContainText('Zweiter Fehler');await page.clock.fastForward(1100);await expect(page.locator('#toast')).toBeVisible();await expect(page.locator('#toast')).toContainText('Zweiter Fehler');
});
test('local users, resource profiles and unchanged OIDC settings',async({page})=>{
 await login(page);await page.locator('#settings-button').click();await page.locator('#open-management').click();await expect(page.locator('#management-dialog')).toBeVisible();await expect(page.locator('#oidc-callback')).toContainText('/auth/oidc/callback');
 await page.getByRole('button',{name:'Betrieb',exact:true}).click();await expect(page.locator('[name=balancedScale]')).toHaveValue('50');await expect(page.locator('[name=fastBitrate]')).toHaveValue('450');await expect(page.locator('[name=autoScan]')).not.toBeChecked();await page.locator('[name=balancedBitrate]').fill('1000');await page.locator('#operation-form button').click();await expect(page.locator('#toast')).toContainText('Einstellungen gespeichert');await expect(page.locator('#settings-error')).toBeEmpty();
 await page.locator('[name=balancedBitrate]').fill('1200');await page.locator('#operation-form button').click();await expect(page.locator('#toast')).toContainText('Einstellungen gespeichert');await page.locator('#management-close').click();
 await page.locator('#account-menu summary').click();await page.locator('#logout').click();await expect(page.locator('#login')).toBeVisible();
});
test('new automatic Reolink camera indexes videos, ignores JPG and supports scoped scans',async({page})=>{
 await login(page);await page.locator('#settings-button').click();await page.locator('#new-camera').click();await page.locator('#camera-form input[name=name]').fill('Automatic Reolink');await page.locator('#camera-form input[name=folder]').fill('Reolink/Testkamera-A');await expect(page.locator('#camera-format')).toHaveValue('automatic');await page.locator('#preview').click();await expect(page.locator('#preview-result')).toContainText('2026-10-14T04:12:17Z');await expect(page.locator('#preview-result')).not.toContainText('.jpg');await page.locator('#camera-form button[type=submit]').click();await expect(page.locator('#camera-list')).toContainText('Automatic Reolink');await page.locator('#close-dialog').click();await expect(page.locator('#recording-count')).toContainText('4 Aufnahmen',{timeout:20000});await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
 const session=await(await page.request.get('/api/session')).json();const cameras=await(await page.request.get('/api/cameras')).json();const camera=cameras.find((c:{name:string})=>c.name==='Automatic Reolink');const headers={Origin:new URL(page.url()).origin,'X-CSRF-Token':session.csrf};expect((await page.request.post('/api/scan',{headers,data:{date:'2026-10-14',camera:camera.id,all:true}})).status()).toBe(202);await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});await expect.poll(async()=>{const s=await(await page.request.get('/api/scan')).json();return s.running}).toBe(false);expect((await page.request.delete('/api/cameras/'+camera.id,{headers})).ok()).toBe(true);
});
