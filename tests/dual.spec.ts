import {test,expect,type Page} from '@playwright/test';
// The disposable server trusts loopback as a synthetic proxy. Give this test
// its own client IP so adding a ninth test does not exhaust the login quota.
test.use({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.10'}});
async function login(page:Page){await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});await expect(page.locator('#rescan')).toBeEnabled({timeout:20000})}

test('batch setup, paired views, offset sync, enlargement and cleanup',async({page})=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));await login(page);
 await page.locator('#settings-button').click();await page.locator('#new-camera').click();
 await page.locator('#view-mode').selectOption('dual');
 await expect(page.locator('#camera-form input[name=wideMarker]')).toHaveValue('_00_');await expect(page.locator('#camera-form input[name=zoomMarker]')).toHaveValue('_01_');
 await page.locator('#camera-form input[name=wideMarker]').fill('_wide');await page.locator('#camera-form input[name=zoomMarker]').fill('_zoom');
 await page.locator('#camera-form input[name=zoomOffset]').fill('-5');
 await page.locator('#batch-open').click();
 for(const folder of ['DualFront','DualBack'])await page.getByRole('checkbox',{name:'Kameraordner '+folder+' auswählen',exact:true}).check();
 await expect(page.locator('#batch-selected input')).toHaveCount(2);
 await page.locator('#batch-save').click();await expect(page.locator('#batch-dialog')).not.toBeVisible();
 await expect(page.locator('#recording-count')).toContainText('6 Aufnahmen',{timeout:20000});await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
 await expect(page.locator('.camera-group.dual')).toHaveCount(2);
 for(const group of await page.locator('.camera-group.dual').all()){
  await expect(group.locator('video')).toHaveCount(2);await expect(group.locator('.tile-title').first()).toContainText('Weitwinkel');await expect(group.locator('.tile-title').last()).toContainText('Zoom');
 }
 await page.locator('.thumb').first().click();await page.locator('#play').click();
 await expect.poll(()=>page.locator('.camera-group.dual video').first().evaluate(v=>v.currentTime)).toBeGreaterThan(1);
 const times=await page.locator('.camera-group.dual video').evaluateAll(v=>v.map(x=>x.currentTime));expect(Math.abs(times[0]-times[1])).toBeLessThan(1);
 await page.locator('#play').click();await page.locator('.camera-group.dual video').first().dblclick();await expect(page.locator('.camera-tile.expanded')).toHaveCount(1);await page.keyboard.press('Escape');await expect(page.locator('.camera-tile.expanded')).toHaveCount(0);
 await page.screenshot({path:'/tmp/cctv-dual-ui.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:'/tmp/cctv-dual-mobile.png',fullPage:true});
 for(const width of [980,1440,390]){
  await page.setViewportSize({width,height:844});
  await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
  await expect(page.locator('.camera-group.dual')).toHaveCount(2);
  await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 }
 await page.evaluate(async()=>{const session=await(await fetch('/api/session')).json();const cameras=await(await fetch('/api/cameras')).json();for(const c of cameras.filter((c:{name:string})=>c.name==='DualFront'||c.name==='DualBack')){const r=await fetch('/api/cameras/'+c.id,{method:'DELETE',headers:{'X-CSRF-Token':session.csrf}});if(!r.ok)throw new Error('cleanup failed')}});
 expect(errors).toEqual([]);
});

test('a failed or missing zoom leaves wide playback running without repeated requests or DOM churn',async({page})=>{
 await page.addInitScript(()=>{
  const play=HTMLMediaElement.prototype.play;
  HTMLMediaElement.prototype.play=function(){(this as any).testPlayCalls=((this as any).testPlayCalls||0)+1;return play.call(this)};
 });
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));await login(page);
 const session=await(await page.request.get('/api/session')).json();
 const headers={'X-CSRF-Token':session.csrf,Origin:'http://127.0.0.1:18080'};
 const response=await page.request.post('/api/cameras',{headers,data:{name:'Dual isolation',folder:'DualFront',format:'automatic',indexingMode:'full',timezone:'Europe/Berlin',offset:0,viewMode:'dual',wideMarker:'_wide',zoomMarker:'_zoom',zoomOffset:-5}});
 expect(response.ok(),await response.text()).toBe(true);const camera=await response.json();
 try{
  await page.reload();await page.locator('#date').fill('2026-10-14');
  await expect(page.locator('#recording-count')).toContainText('4 Aufnahmen',{timeout:20000});
  for(const name of await page.locator('#camera-filters .filter.selected').allTextContents())if(name!=='Dual isolation')await page.locator('#camera-filters').getByRole('button',{name,exact:true}).click();
  const group=page.locator('.camera-group.dual'),wide=group.locator('.camera-tile').first(),zoom=group.locator('.camera-tile').last();
  const day=await(await page.request.get('/api/day?date=2026-10-14')).json();
  const zoomRecord=day.recordings.find((r:{cameraId:string;view:string})=>r.cameraId===camera.id&&r.view==='zoom');
  let zoomRequests=0;
  await page.route('**/api/streams',async route=>{
   if(route.request().method()==='POST'&&route.request().postDataJSON().recording===zoomRecord.id){zoomRequests++;await route.fulfill({status:422,contentType:'application/json',body:'{"error":"synthetic zoom failure"}'});return}
   await route.continue();
  });
  await page.locator('.thumb').first().click();await page.locator('#play').click();await page.locator('#quality').selectOption('fast');
  await expect(zoom.locator('.tile-message.error')).toBeVisible();
  await expect.poll(()=>wide.locator('video').evaluate(v=>v.currentTime),{timeout:20000}).toBeGreaterThan(1);
  expect(zoomRequests).toBe(4);await expect(wide.locator('.tile-message.error')).toHaveCount(0);
  await page.locator('#play').click();await page.unroute('**/api/streams');
  await page.route('**/api/day?*',async route=>{
   const response=await route.fetch(),data=await response.json();
   const first=data.recordings.find((r:{cameraId:string;view:string})=>r.cameraId===camera.id&&r.view==='wide');
   data.recordings=data.recordings.filter((r:{cameraId:string;view:string})=>r.cameraId!==camera.id||r.view!=='zoom');
   data.recordings.push({...first,id:'synthetic-unknown',view:'unknown',start:first.start-60,duration:10});
   await route.fulfill({response,json:data});
  });
  await page.locator('#quality').selectOption('native');await page.locator('#date').dispatchEvent('change');
  await expect(zoom.locator('.tile-message')).toHaveText('▧Keine Aufnahme zu diesem Zeitpunkt');
  await expect(page.locator('#clock')).toHaveText('08:30:00');
  await zoom.locator('.tile-message').evaluate(el=>{
   (window as any).zoomMessageMutations=0;
   new MutationObserver(entries=>(window as any).zoomMessageMutations+=entries.length).observe(el,{childList:true,subtree:true,attributes:true});
  });
  const playCalls=await wide.locator('video').evaluate(v=>(v as any).testPlayCalls||0);
  await page.locator('#play').click();await expect.poll(()=>wide.locator('video').evaluate(v=>v.currentTime)).toBeGreaterThan(1);
  expect(await wide.locator('video').evaluate(v=>(v as any).testPlayCalls||0)-playCalls).toBeLessThanOrEqual(2);
  expect(await page.evaluate(()=>(window as any).zoomMessageMutations)).toBe(0);
  expect(errors).toEqual([]);
 }finally{
  if(!page.isClosed())await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));
  await page.request.delete('/api/cameras/'+camera.id,{headers});
 }
});
