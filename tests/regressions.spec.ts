import {openNavigation,preferenceControl} from './fixtures';
import {test,expect} from './fixtures';
test('delayed login initialization keeps username and password entry in their fields',async({page})=>{
 await page.setExtraHTTPHeaders({'X-Forwarded-For':'192.0.2.181'});
 let release!:()=>void;const ready=new Promise<void>(resolve=>release=resolve);
 await page.route('**/auth/methods',async route=>{const response=await route.fetch();await ready;await route.fulfill({response})});
 await page.goto('/');await expect(page.locator('#login')).toBeHidden();release();
 const username=page.locator('#login-form input[name=username]'),password=page.locator('#login-form input[name=password]');
 await expect(username).toBeVisible();
 await username.fill('browser');await password.fill('synthetic browser test password');
 await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
 await expect(password).toBeFocused();await expect(username).toHaveValue('browser');await expect(password).toHaveValue('synthetic browser test password');
 const submitted=page.waitForResponse(response=>response.url().endsWith('/auth/local')&&response.request().method()==='POST');
 await page.locator('#login-form button').click();expect((await submitted).ok()).toBe(true);await expect(page.locator('#app')).toBeVisible();
});
test('diagnostics download excludes credentials; page hide releases streams',async({page})=>{
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
 await openNavigation(page,'preferences-button');await (await preferenceControl(page,'diagnostics-open')).click();await page.locator('#trace-enabled').check();await page.locator('#diagnostics-close').click();await page.locator('#preferences-close').click();await page.locator('#quality').selectOption('balanced');await page.locator('#play').click();await expect.poll(()=>page.locator('video').first().evaluate(v=>v.readyState),{timeout:25000}).toBeGreaterThanOrEqual(2);await page.locator('#play').click();await openNavigation(page,'preferences-button');await (await preferenceControl(page,'diagnostics-open')).click();await page.locator('#trace-stop').click();const downloaded=page.waitForEvent('download');await page.locator('#trace-download').click();const d=await downloaded;expect(d.suggestedFilename()).toBe('wiedergabediagnose.json');const file=await d.createReadStream();let text='';for await(const chunk of file!)text+=chunk.toString();const data=JSON.parse(text);expect(data.events.length).toBeGreaterThan(0);expect(text).not.toMatch(/csrf|clientSecret|synthetic browser test password|\/recordings\//);
 await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));await expect.poll(async()=>{const s=await(await page.request.get('/api/playback-status')).json();return s.sessions}).toBe(0);
});

test('reloading a loaded player keeps the page and saved playback preferences usable',async({page})=>{
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();
 for(const quality of ['balanced','native','balanced']){
  await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen');
  const saved=page.waitForResponse(response=>response.url().endsWith('/api/preferences')&&response.request().method()==='PATCH'&&response.ok());
  await page.locator('#quality').selectOption(quality);await saved;await page.locator('.thumb').first().click();await page.locator('#play').click();
  await expect.poll(()=>page.locator('video').first().evaluate(video=>video.readyState),{timeout:25000}).toBeGreaterThanOrEqual(2);
  await page.reload();await expect(page.locator('#app')).toBeVisible();await expect(page.locator('#quality')).toHaveValue(quality);
 }
});
