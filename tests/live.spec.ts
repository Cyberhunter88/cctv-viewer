import {test,expect,openPage,type Page} from './fixtures';
async function login(page:Page){await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible()}

test('live navigation and empty selection persist independently from recordings',async({page})=>{
 await login(page);const recorded=await page.locator('#camera-filters .selected').allTextContents();
 await openPage(page,'live');await expect(page.locator('#live-page h1')).toHaveText('Liveansicht');
 const saved=page.waitForResponse(r=>r.url().endsWith('/api/preferences')&&r.request().method()==='PATCH'&&Array.isArray(r.request().postDataJSON().liveCameras)&&r.request().postDataJSON().liveCameras.length===0&&r.ok());
 await page.locator('#live-select-none').click();await saved;
 await expect(page.locator('#live-empty')).toContainText('Keine Kameras ausgewählt');await expect(page.locator('#live-grid video')).toHaveCount(0);
 await page.reload();await expect(page.locator('#live-empty')).toContainText('Keine Kameras ausgewählt');
 await openPage(page,'recordings');expect(await page.locator('#camera-filters .selected').allTextContents()).toEqual(recorded);
 await openPage(page,'live');await page.locator('#live-select-all').click();await expect(page.locator('#live-camera-filters .selected')).toHaveCount(2);
 await page.locator('#live-camera-filters .filter').first().click();await expect(page.locator('#live-camera-filters .selected')).toHaveCount(1);
});

test('live camera administration protects unsaved edits and does not disclose passwords',async({page})=>{
 await login(page);await openPage(page,'admin/cameras');await page.locator('#camera-list .camera-entry').first().getByRole('button',{name:/Livequelle einrichten/}).click();
 await expect(page.locator('#live-admin')).toBeVisible();await expect(page.locator('#live-password')).toHaveValue('');
 await page.locator('#live-fast').fill('rtsp://camera.example.org/sub');
 page.once('dialog',dialog=>dialog.dismiss());await page.locator('#close-dialog').click();await expect(page.locator('#live-admin')).toBeVisible();await expect(page.locator('#live-fast')).toHaveValue('rtsp://camera.example.org/sub');
 page.once('dialog',dialog=>dialog.accept());await page.locator('#close-dialog').click();await expect(page).toHaveURL(/#recordings$/);
});

test('real live stream plays through HTTPS fallback, supports zoom and releases on leave',async({page})=>{
 test.skip(!process.env.CTV_TEST_GO2RTC,'Pinned go2rtc required');
 await page.addInitScript(()=>Object.defineProperty(window,'RTCPeerConnection',{value:class{constructor(){throw new Error('Synthetic blocked WebRTC transport')}}}));
 await login(page);await openPage(page,'live');
 const video=page.locator('#live-grid video').first();await expect.poll(()=>video.evaluate(v=>(v as HTMLVideoElement).readyState>=2?(v as HTMLVideoElement).currentTime:0),{timeout:25000}).toBeGreaterThan(.5);
 await expect(page.locator('#live-grid .live-tile-status').first()).toHaveText('Live · HTTPS');
 await expect(video).toHaveJSProperty('muted',true);
 const original=await video.elementHandle();
 await page.locator('#live-grid').getByRole('button',{name:'Zoom anzeigen'}).first().click();
 expect(await video.evaluate((node,old)=>node===old,original)).toBe(true);await expect(page.locator('#live-grid video')).toHaveCount(3);
 await expect.poll(()=>page.locator('#live-grid video').nth(1).evaluate(v=>(v as HTMLVideoElement).readyState>=2?(v as HTMLVideoElement).currentTime:0),{timeout:25000}).toBeGreaterThan(0);
 await page.locator('#live-grid').getByRole('button',{name:'Ton einschalten'}).first().click();await expect(page.locator('#live-grid video').first()).toHaveJSProperty('muted',false);
 await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'))});await expect(page.locator('#live-grid video')).toHaveCount(0);
 await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});document.dispatchEvent(new Event('visibilitychange'))});await expect.poll(()=>page.locator('#live-grid video').first().evaluate(v=>(v as HTMLVideoElement).readyState>=2?(v as HTMLVideoElement).currentTime:0),{timeout:25000}).toBeGreaterThan(.5);
 await openPage(page,'recordings');await expect.poll(()=>page.locator('#live-grid video').evaluateAll(videos=>videos.every(v=>(v as HTMLVideoElement).paused&&!v.getAttribute('src')&&!(v as HTMLVideoElement).srcObject))).toBe(true);
});

test('live controls fit mobile light and dark layouts',async({page})=>{
 await page.setViewportSize({width:360,height:800});await login(page);await openPage(page,'live');
 for(const theme of ['light','dark']){await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await expect(page.locator('#live-select-all')).toBeVisible()}
});
