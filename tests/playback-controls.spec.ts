import {test,expect,seekToRecording,type Page} from './fixtures';
// Each test logs in from its own synthetic address, so login rate limits never interfere.
test.beforeEach(async({context},info)=>{const hash=[...info.testId].reduce((n,c)=>(Math.imul(n,31)+c.charCodeAt(0))>>>0,0);await context.setExtraHTTPHeaders({'X-Forwarded-For':`2001:db8:5::${(hash>>>16).toString(16)}:${(hash&65535).toString(16)}`})});

async function login(page:Page){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();
 await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
}
async function enterFullscreen(page:Page){
 await seekToRecording(page);const tile=page.locator('.camera-tile').first();
 await tile.locator('.tile-expand').click();await expect.poll(()=>tile.evaluate(el=>document.fullscreenElement===el)).toBe(true);
 return tile;
}

test('question mark opens the shortcut overview and new keys control speed, sound and frames',async({page})=>{
 await login(page);await seekToRecording(page);
 // Page shortcuts apply without focus; a focused timeline keeps its own arrow keys (WebKit keeps that focus on clicks).
 await page.evaluate(()=>(document.activeElement as HTMLElement|null)?.blur());
 await page.keyboard.press('?');await expect(page.locator('#shortcut-help')).toBeVisible();
 await expect(page.locator('#shortcut-help .shortcut-list')).toContainText('Einzelbild zurück / vor');
 await page.keyboard.press('Escape');await expect(page.locator('#shortcut-help')).toBeHidden();await expect(page).toHaveURL(/#recordings$/);await page.evaluate(()=>(document.activeElement as HTMLElement|null)?.blur());
 await expect(page.locator('#speed')).toHaveValue('1');
 await page.keyboard.press('ArrowUp');await expect(page.locator('#speed')).toHaveValue('2');
 await page.keyboard.press('ArrowDown');await page.keyboard.press('ArrowDown');await expect(page.locator('#speed')).toHaveValue('0.5');
 await page.keyboard.press('ArrowUp');
 const audio=page.locator('.camera-tile').first().locator('.tile-audio');
 await page.keyboard.press('m');await expect(audio).toHaveAttribute('aria-pressed','true');
 await page.keyboard.press('m');await expect(audio).toHaveAttribute('aria-pressed','false');
 // A frame step pauses playback and moves the clock by a fraction of a second.
 await page.locator('#play').click();await expect(page.locator('#play')).toHaveAttribute('aria-label','Pause');
 await page.evaluate(()=>(document.activeElement as HTMLElement|null)?.blur());
 const before=await page.locator('.camera-tile video').first().evaluate(v=>(v as HTMLVideoElement).currentTime);
 await page.keyboard.press('.');await expect(page.locator('#play')).toHaveAttribute('aria-label','Abspielen');
 await expect.poll(()=>page.locator('.camera-tile video').first().evaluate(v=>(v as HTMLVideoElement).currentTime)).not.toBe(before);
 // The settings page shows the same list and opens the overview from a button.
 await page.goto('/#settings/playback');await expect(page.locator('#preferences-form .shortcut-list')).toContainText('Schneller / langsamer abspielen');
 await page.locator('#shortcut-help-open').click();await expect(page.locator('#shortcut-help')).toBeVisible();
 await page.locator('#shortcut-help-close').click();await expect(page.locator('#shortcut-help')).toBeHidden();
});

test('fullscreen controls fade out while playing and return on movement',async({page})=>{
 await login(page);const tile=await enterFullscreen(page);
 await tile.locator('.tile-play').click();await expect(page.locator('#play')).toHaveAttribute('aria-label','Pause');
 await expect(tile).toHaveClass(/idle/,{timeout:5000});await expect(tile.locator('.tile-footer')).toHaveCSS('opacity','0');
 await page.mouse.move(200,200);await page.mouse.move(220,220);await expect(tile).not.toHaveClass(/idle/);
 // Paused playback keeps the controls visible.
 await tile.locator('.tile-play').click();await page.waitForTimeout(3000);await expect(tile).not.toHaveClass(/idle/);
 await page.evaluate(()=>document.exitFullscreen());
});

test('tiles offer picture-in-picture and a visible hotspot marker',async({page})=>{
 await login(page);
 const tile=page.locator('.camera-tile').first(),pip=tile.locator('.tile-pip');
 if(await page.evaluate(()=>document.pictureInPictureEnabled))await expect(pip).toHaveAttribute('aria-label',/^Bild-in-Bild · /);else await expect(pip).toBeHidden();
 await expect(tile.locator('.tile-hotspot-marker')).toBeHidden();
 await page.locator('#layout').selectOption('hotspot');
 await expect(page.locator('.hotspot-primary .tile-hotspot-marker')).toBeVisible();
});

test('original quality falls back to a full-resolution conversion when the browser cannot decode it',async({page})=>{
 // The browser reports no support for the recording codec, as Chrome on Linux does for H.265.
 await page.addInitScript(()=>{const original=HTMLMediaElement.prototype.canPlayType;HTMLMediaElement.prototype.canPlayType=function(type:string){return /avc1|hvc1|hev1/.test(type)?'':original.call(this,type)} as typeof original});
 await login(page);
 const profiles:string[]=[],originals:string[]=[];
 page.on('request',request=>{if(request.url().endsWith('/api/streams')&&request.method()==='POST')profiles.push(request.postDataJSON().profile);if(request.url().endsWith('/file'))originals.push(request.method())});
 await expect(page.locator('#quality')).toHaveValue('native');
 await seekToRecording(page);await page.locator('#play').click();
 await expect.poll(()=>profiles.includes('full'),{timeout:20000}).toBe(true);
 await expect(page.locator('#toast-text')).toContainText('voller Auflösung');
 await expect.poll(()=>page.locator('.camera-tile video').first().evaluate(v=>(v as HTMLVideoElement).readyState),{timeout:25000}).toBeGreaterThanOrEqual(2);
 await expect(page.locator('.camera-tile .tile-header small').first()).toContainText('umgewandelt');
 expect(profiles.every(profile=>profile==='full')).toBe(true);expect(originals).toEqual([]);
 // Release the conversion: the shared test server counts streams across tests.
 await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));
 await expect.poll(async()=>(await(await page.request.get('/api/playback-status')).json()).sessions,{timeout:15000}).toBe(0);
});

test('boundary and day keys move the cursor and the help button opens the overview',async({page})=>{
 await login(page);await page.evaluate(()=>(document.activeElement as HTMLElement|null)?.blur());
 await page.keyboard.press('Home');await expect(page.locator('#clock')).toHaveText('00:00:00');
 await page.keyboard.press('n');const first=await page.locator('#clock').textContent();expect(first).not.toBe('00:00:00');
 await page.keyboard.press('n');await expect(page.locator('#clock')).not.toHaveText(first!);
 await page.keyboard.press('p');await expect(page.locator('#clock')).toHaveText(first!);
 await page.keyboard.press('End');await expect(page.locator('#clock')).toHaveText('23:59:59');
 await page.locator('#shortcut-help-button').click();await expect(page.locator('#shortcut-help')).toBeVisible();
 await expect(page.locator('#shortcut-help .shortcut-list')).toContainText('Vorherige / nächste Aufnahmegrenze');
 await page.keyboard.press('Escape');await expect(page.locator('#shortcut-help')).toBeHidden();
});

test('a preferences reset can be undone from its message',async({page})=>{
 await login(page);
 const saved=(check:(body:Record<string,unknown>)=>boolean)=>page.waitForResponse(r=>r.url().endsWith('/api/preferences')&&r.request().method()==='PATCH'&&check(r.request().postDataJSON())&&r.ok());
 await page.goto('/#settings/appearance');let write=saved(body=>body.density==='compact');await page.locator('#pref-density').selectOption('compact');await write;
 page.once('dialog',d=>d.accept());write=saved(body=>body.density==='comfortable');await page.locator('#preferences-reset').click();await write;
 await expect(page.locator('html')).toHaveAttribute('data-density','comfortable');
 await expect(page.locator('#toast-action')).toHaveText('Rückgängig');
 write=saved(body=>body.density==='compact');await page.locator('#toast-action').click();await write;
 await expect(page.locator('html')).toHaveAttribute('data-density','compact');await expect(page.locator('#pref-density')).toHaveValue('compact');
 await page.reload();await expect(page.locator('#app')).toBeVisible();await expect(page.locator('html')).toHaveAttribute('data-density','compact');
});
