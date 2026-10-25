import {test,expect,openPage,cameraMutation,type Page} from './fixtures';
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
 const first=await page.locator('#live-camera-filters .filter').first().textContent();
 await page.locator('#live-camera-filters .filter').first().focus();await page.keyboard.press('Alt+ArrowRight');
 await expect(page.locator('#live-camera-filters .filter').last()).toHaveText(first!);
 await page.locator('#live-camera-filters .filter').first().click();await expect(page.locator('#live-camera-filters .selected')).toHaveCount(1);
});

test('live camera administration protects unsaved edits and does not disclose passwords',async({page})=>{
 await login(page);await openPage(page,'admin/cameras');await page.locator('#camera-list .camera-entry').first().getByRole('button',{name:/Livequelle einrichten/}).click();
 await expect(page.locator('#live-admin')).toBeVisible();await expect(page.locator('#live-password')).toHaveValue('');
 await page.locator('#live-fast').fill('rtsp://camera.example.org/sub');
 page.once('dialog',dialog=>dialog.dismiss());await page.locator('#close-dialog').click();await expect(page.locator('#live-admin')).toBeVisible();await expect(page.locator('#live-fast')).toHaveValue('rtsp://camera.example.org/sub');
 page.once('dialog',dialog=>dialog.accept());await page.locator('#close-dialog').click();await expect(page).toHaveURL(/#recordings$/);
});

test('Home Assistant server URL persists in the form and generates copyable links',async({page})=>{
 let serverUrl='';
 await page.route('**/api/admin/live-share',route=>{if(route.request().method()==='PUT')serverUrl=route.request().postDataJSON().serverUrl;return route.fulfill({json:{enabled:false,username:'homeassistant',password:'',hasPassword:true,address:'0.0.0.0:8554',listening:false,serverUrl}})});
 await page.route('**/api/cameras/*/live',route=>route.fulfill({json:{enabled:true,username:'',hasPassword:false,shared:true,sources:{fast:'rtsp://camera.example/sub',high:'',zoomFast:'',zoomHigh:''}}}));
 await login(page);await openPage(page,'admin/live-share');
 const id=await page.locator('#live-admin-camera').inputValue();
 await page.locator('#live-share-server-url').fill('rtsp://viewer.example:8554');
 await expect(page.locator('#live-share-urls')).toContainText(`rtsp://viewer.example:8554/live/${id}/wide/fast`);
 await page.locator('#live-share-form button[type=submit]').click();
 await expect(page.locator('#live-share-error')).toBeEmpty();
 await page.reload();await openPage(page,'admin/live-share');
 await expect(page.locator('#live-share-server-url')).toHaveValue('rtsp://viewer.example:8554');
 await expect(page.locator('#live-share-urls')).toContainText(`rtsp://viewer.example:8554/live/${id}/wide/fast`);
});

test('real live stream plays through HTTPS fallback, supports zoom and releases on leave',async({page})=>{
 test.skip(!process.env.CTV_TEST_GO2RTC,'Pinned go2rtc required');
 await page.addInitScript(()=>Object.defineProperty(window,'RTCPeerConnection',{value:class{constructor(){throw new Error('Synthetic blocked WebRTC transport')}}}));
 await login(page);
 const cameras=await(await page.request.get('/api/cameras')).json(),session=await(await page.request.get('/api/session')).json();
 const headers={'X-CSRF-Token':session.csrf,Origin:new URL(page.url()).origin};
 const saved=await cameraMutation(page,()=>page.request.post('/api/cameras',{headers,data:{...cameras[0],viewMode:'dual',wideMarker:'_00_',zoomMarker:'_01_'}}));
 expect(saved.ok(),await saved.text()).toBe(true);
 try{
 await page.reload();await openPage(page,'live');
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
 }finally{
  const restored=await cameraMutation(page,()=>page.request.post('/api/cameras',{headers,data:cameras[0]}));
  expect(restored.ok(),await restored.text()).toBe(true);
  expect((await restored.json()).viewMode).toBe(cameras[0].viewMode);
 }
});

test('live controls fit mobile light and dark layouts',async({page})=>{
 await page.setViewportSize({width:360,height:800});await login(page);await openPage(page,'live');
 for(const theme of ['light','dark']){await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await expect(page.locator('#live-select-all')).toBeVisible()}
});
