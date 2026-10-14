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
