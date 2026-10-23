import {test,expect,openPage,type Page} from './fixtures';
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
