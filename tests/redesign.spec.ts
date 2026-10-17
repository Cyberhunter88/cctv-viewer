import {test,expect,savePreferences,type Page} from './fixtures';

test.use({hasTouch:true});
test.beforeEach(async({context},info)=>{
 const hash=[...info.testId].reduce((n,c)=>(Math.imul(n,31)+c.charCodeAt(0))>>>0,0);
 await context.setExtraHTTPHeaders({'X-Forwarded-For':`2001:db8:1::${(hash>>>16).toString(16)}:${(hash&65535).toString(16)}`});
});
async function login(page:Page){
 await page.goto('/');
 await page.locator('#login-form input[name=username]').fill('browser');
 await page.locator('#login-form input[name=password]').fill('synthetic browser test password');
 await page.locator('#login-form button').click();
 await expect(page.locator('#app')).toBeVisible();
 await page.locator('#date').fill('2026-10-14');
 await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
}

test('archive and all main dialogs fit phones, landscape, tablets and desktop',async({page},info)=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.setViewportSize({width:360,height:780});
 await page.goto('/');await expect(page.locator('#login')).toBeVisible();
 await page.screenshot({path:info.outputPath('login-360.png'),fullPage:true});
 await login(page);
 for(const theme of ['dark','light']){
  await page.locator('#preferences-button').click();await page.locator('#theme').selectOption(theme);await savePreferences(page);
  await expect(page.locator('#toast')).toBeHidden({timeout:10000});
  for(const [width,height] of [[360,780],[390,844],[844,390],[768,1024],[1366,768],[1920,1080]]){
   await page.setViewportSize({width,height});
   await page.locator('.thumb').first().tap();
   await expect.poll(()=>page.locator('video').first().evaluate(v=>v.readyState)).toBeGreaterThanOrEqual(2);
   await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   const grid=await page.locator('#video-grid').boundingBox(),controls=await page.locator('#playback-controls').boundingBox();
   expect(controls!.y).toBeGreaterThanOrEqual(grid!.y+grid!.height);
   await expect(page.locator('#timeline-section > #playback-controls')).toHaveCount(1);
   await expect(page.locator('#playback-controls')).toHaveCSS('position','static');
   if(width<=700){
    await expect(page.locator('#search-button > span')).toBeVisible();
    await expect(page.locator('#preferences-button > span')).toBeVisible();
    expect(await page.locator('.camera-group').first().evaluate(el=>el.clientWidth)).toBeGreaterThanOrEqual(300);
   }
   const sizes=await page.locator('#app button:not([hidden]),#app .rail-item').filter({visible:true}).evaluateAll(elements=>elements.map(el=>({name:el.getAttribute('aria-label')||el.textContent,width:el.getBoundingClientRect().width,height:el.getBoundingClientRect().height})));
   for(const size of sizes){expect(size.width,size.name||'touch width').toBeGreaterThanOrEqual(44);expect(size.height,size.name||'touch height').toBeGreaterThanOrEqual(44)}
   if(width===360){
    await expect(page.locator('#password-button')).toBeHidden();
    await page.locator('#account-menu summary').tap();
    await expect(page.locator('#password-button')).toBeVisible();
    const action=await page.locator('#password-button').boundingBox();
    expect(action!.width).toBeGreaterThanOrEqual(44);expect(action!.height).toBeGreaterThanOrEqual(44);
    await page.keyboard.press('Escape');
    await expect(page.locator('#password-button')).toBeHidden();
   }
   await page.evaluate(()=>window.scrollTo({top:0,behavior:'instant'}));
   await page.screenshot({path:info.outputPath(`archive-${theme}-${width}.png`),fullPage:true});
   for(const [trigger,id,close] of [['preferences-button','preferences-dialog','preferences-close'],['search-button','search-dialog','search-close'],['settings-button','camera-dialog','close-dialog']]){
    await page.locator('#'+trigger).click();
    const dialog=page.locator('#'+id),body=dialog.locator('.dialog-body');
    await expect.poll(()=>body.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
    const box=await dialog.boundingBox();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(width);expect(box!.y).toBeGreaterThanOrEqual(0);expect(box!.y+box!.height).toBeLessThanOrEqual(height);
    await body.evaluate(el=>el.scrollTop=el.scrollHeight);
    await expect(page.locator('#'+close)).toBeInViewport();
    if(width===360||width===1366)await page.screenshot({path:info.outputPath(`${id}-${theme}-${width}.png`)});
    await page.locator('#'+close).click();
    await expect(page.locator('#'+trigger)).toBeFocused();
   }
  }
 }
 expect(errors).toEqual([]);
});

test.describe('mobile browser',()=>{
 test.use({isMobile:true});
 test('phone touch can seek and zoom the timeline and operate playback',async({page})=>{
 await page.setViewportSize({width:360,height:780});await login(page);
 await page.locator('#timeline').scrollIntoViewIfNeeded();
 const timeline=await page.locator('#timeline').boundingBox();
 await page.touchscreen.tap(timeline!.x+85+(timeline!.width-100)*.5,timeline!.y+55);
 await expect(page.locator('#clock')).toHaveText('12:00:00');
 await page.locator('#zoom-in').tap();await expect(page.locator('#zoom-label')).not.toHaveText('24 Std.');
 await page.locator('#reset-zoom').tap();await expect(page.locator('#zoom-label')).toHaveText('24 Std.');
 await page.locator('#skip-back').tap();await expect(page.locator('#clock')).not.toHaveText('12:00:00');
 await page.locator('.thumb').first().tap();
 await page.locator('#play').tap();await expect(page.locator('#play')).toHaveAttribute('aria-label','Pause');
 await page.locator('#play').tap();await expect(page.locator('#play')).toHaveAttribute('aria-label','Abspielen');
 await expect(page.locator('.timeline-help-touch')).toBeVisible();await expect(page.locator('.timeline-help-pointer')).toBeHidden();
 await page.locator('#search-button').tap();await expect(page.locator('#search-dialog')).toBeVisible();await page.locator('#search-close').tap();
 await page.locator('#settings-button').tap();await page.locator('#open-management').tap();
 await expect(page.locator('#management-dialog')).toBeVisible();await page.locator('[data-tab=operation]').tap();
 await expect.poll(()=>page.locator('#management-dialog .dialog-body').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
 await page.locator('#management-close').tap();
 });
});
