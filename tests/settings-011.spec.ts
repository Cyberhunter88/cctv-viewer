import {test,expect,openPage,cameraMutation,type Page} from './fixtures';
async function login(page:Page){await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible()}

test('camera tabs retain independent drafts and import HTTPS credentials safely',async({page})=>{
 await login(page);await openPage(page,'admin/cameras');
 await expect(page.locator('.camera-editor-empty')).toBeVisible();await expect(page.locator('#camera-form')).toBeHidden();
 await page.locator('#camera-list .camera-edit').first().click();
 await page.locator('#camera-form input[name=name]').fill('Synthetic draft');
 await page.locator('#camera-tab-live').click();await expect(page.locator('#live-camera-form')).toBeVisible();
 await page.locator('#live-fast').fill('https://camera.example.org/flv?port=1935&app=bcs&stream=channel0_sub.bcs&user=synthetic&password=a%26b%23c');
 await page.locator('#live-fast').press('Tab');await page.locator('#live-preload-fast').selectOption('always');
 await expect(page.locator('#live-username')).toHaveValue('synthetic');await expect(page.locator('#live-password')).toHaveValue('a&b#c');
 await expect(page.locator('#live-fast')).not.toHaveValue(/password|user=/);
 await page.locator('#live-preload-high').selectOption('off');
 await page.locator('#camera-tab-general').click();await expect(page.locator('#camera-form input[name=name]')).toHaveValue('Synthetic draft');
 await page.locator('#camera-tab-live').click();await expect(page.locator('#live-preload-fast')).toHaveValue('always');await expect(page.locator('#live-preload-high')).toHaveValue('off');
 let confirmations=0;page.on('dialog',async dialog=>{confirmations++;await dialog.accept()});
 await page.locator('#close-dialog').click();await expect(page).toHaveURL(/#recordings$/);expect(confirmations).toBe(2);
 await openPage(page,'admin/cameras');await expect(page.locator('.camera-editor-empty')).toBeVisible();
});

test('per-source HTTPS and preload settings save with secret-free source URLs',async({page})=>{
 let saved:Record<string,any>|undefined;
 const config={enabled:true,username:'synthetic',hasPassword:true,shared:false,sources:{fast:'https://camera.example.org/flv?stream=channel0_sub.bcs',high:'',zoomFast:'',zoomHigh:''},preload:{fast:'idle',high:'idle',zoomFast:'idle',zoomHigh:'idle'},skipTLSVerify:{fast:false}};
 await page.route('**/api/cameras/*/live',async route=>{
  if(route.request().method()==='PUT'){saved=route.request().postDataJSON();Object.assign(config,{...saved,password:undefined,hasPassword:true})}
  await route.fulfill({json:config});
 });
 await page.route('**/api/cameras/*/live/status',route=>route.fulfill({json:{fast:'ready'}}));
 await login(page);await openPage(page,'admin/cameras');await page.locator('#camera-list .camera-entry').first().getByRole('button',{name:/Livequelle einrichten/}).click();
 await expect(page.locator('#live-camera-form')).toBeVisible();
 await page.locator('#live-password').fill('synthetic replacement');
 await page.locator('#live-preload-fast').selectOption('always');await page.locator('#live-preload-high').selectOption('off');
 const advanced=page.locator('.live-source-card').first().locator('details');await advanced.locator('summary').click();await page.locator('#live-tls-fast').check();
 await page.locator('#live-camera-form button[type=submit]').click();
 await expect(page.locator('#live-password')).toHaveValue('');await expect(page.locator('#live-status-fast')).toHaveText('Bereit');
 expect(saved?.preload).toEqual({fast:'always',high:'off',zoomFast:'idle',zoomHigh:'idle'});
 expect(saved?.skipTLSVerify.fast).toBe(true);expect(saved?.sources.fast).not.toContain('password');expect(saved?.password).toBe('synthetic replacement');
});

test('single-view cameras hide zoom controls while preserving saved streams',async({page})=>{
 await login(page);
 const cameras=await(await page.request.get('/api/cameras')).json();
 const session=await(await page.request.get('/api/session')).json();
 const headers={'X-CSRF-Token':session.csrf,Origin:new URL(page.url()).origin};
 const created=await cameraMutation(page,()=>page.request.post('/api/cameras',{headers,data:{...cameras[0],id:'',name:'Zoom-Testkamera'}}));
 expect(created.ok(),await created.text()).toBe(true);
 const camera=await created.json();
 const liveURL=`/api/cameras/${encodeURIComponent(camera.id)}/live`;
 const zoom='rtsp://camera.example.org/zoom';
 try{
 const saved=await page.request.put(liveURL,{headers,data:{enabled:false,username:'',shared:true,sources:{fast:'rtsp://camera.example.org/wide',high:'',zoomFast:zoom,zoomHigh:''},preload:{fast:'off',high:'off',zoomFast:'always',zoomHigh:'off'},skipTLSVerify:{}}});
 expect(saved.ok(),await saved.text()).toBe(true);
 await page.reload();
 const edit=()=>page.locator('#camera-list .camera-entry').filter({hasText:'Zoom-Testkamera'}).locator('.camera-edit');
 await openPage(page,'admin/cameras');await edit().click();
 await expect(page.locator('#view-mode')).toHaveValue('single');await page.locator('#camera-tab-live').click();
 await expect(page.locator('[data-source=zoomFast]')).toBeHidden();await expect(page.locator('[data-source=zoomHigh]')).toBeHidden();
 await page.locator('#camera-tab-general').click();await page.locator('#view-mode').selectOption('dual');await page.locator('#camera-tab-live').click();
 await expect(page.locator('[data-source=zoomFast]')).toBeVisible();await expect(page.locator('#live-zoomFast')).toHaveValue(zoom);
 await page.locator('#camera-tab-general').click();await page.locator('#view-mode').selectOption('single');await page.locator('#camera-tab-live').click();
 await expect(page.locator('[data-source=zoomFast]')).toBeHidden();await expect(page.locator('#live-zoomFast')).toHaveValue(zoom);
 await page.locator('#camera-tab-general').click();await page.locator('#camera-form button[type=submit]').click();
 await expect(page.locator('#camera-error')).toBeEmpty();
 await openPage(page,'admin/live-share');await page.locator('#live-admin-camera').selectOption(camera.id);
 await expect(page.locator('#live-share-urls')).toContainText('/wide/fast');
 await expect(page.locator('#live-share-urls')).not.toContainText('/zoom/');
 page.once('dialog',dialog=>dialog.accept());await openPage(page,'live');const filter=page.locator('#live-filter-'+camera.id);if(await filter.getAttribute('aria-pressed')==='false')await filter.click();
 await expect(page.locator('#live-grid').getByRole('button',{name:'Zoom anzeigen'})).toHaveCount(0);
 const restored=await(await page.request.get(`/api/cameras/${encodeURIComponent(camera.id)}/live`)).json();
 expect(restored.sources.zoomFast).toBe(zoom);expect(restored.preload.zoomFast).toBe('always');
 await openPage(page,'admin/cameras');await edit().click();
 await page.locator('#view-mode').selectOption('dual');await page.locator('#camera-form button[type=submit]').click();
 await expect(page.locator('#camera-error')).toBeEmpty();
 await page.locator('#camera-tab-live').click();await expect(page.locator('[data-source=zoomFast]')).toBeVisible();
 await openPage(page,'admin/live-share');await page.locator('#live-admin-camera').selectOption(camera.id);
 await expect(page.locator('#live-share-urls')).toContainText('/zoom/fast');
 page.once('dialog',dialog=>dialog.accept());await openPage(page,'live');await expect(page.locator('#live-grid').getByRole('button',{name:'Zoom anzeigen'}).first()).toBeVisible();
 }finally{
  const removed=await cameraMutation(page,()=>page.request.delete(`/api/cameras/${encodeURIComponent(camera.id)}`,{headers}));
  expect(removed.ok(),await removed.text()).toBe(true);
 }
});

test('all settings pages and camera source controls fit mobile and desktop themes',async({page},info)=>{
 await login(page);
 for(const width of [360,768,1440])for(const theme of ['light','dark']){
  await page.setViewportSize({width,height:900});await page.emulateMedia({colorScheme:theme as 'light'|'dark'});await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);
  for(const path of ['settings/appearance','settings/playback','settings/account','settings/sessions','admin/cameras','admin/live-share','admin/users','admin/sessions','admin/oidc','admin/operation']){
   await openPage(page,path);await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   if(path==='admin/cameras'){
    await page.locator('#camera-list .camera-edit').first().click();await page.locator('#camera-tab-live').click();await expect(page.locator('#live-camera-form')).toBeVisible();
    await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   }
   if(width===360||width===1440)await page.screenshot({path:info.outputPath(path.replace('/','-')+'-'+theme+'-'+width+'.png'),fullPage:true});
  }
 }
});
