import {test,expect} from '@playwright/test';
test('fast compressed playback with 250 ms latency and constrained bandwidth',async({page,browserName})=>{
 test.skip(browserName!=='chromium','Bandwidth emulation uses Chromium CDP; HLS is exercised separately in WebKit.');
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
 const cdp=await page.context().newCDPSession(page);await cdp.send('Network.enable');await cdp.send('Network.emulateNetworkConditions',{offline:false,latency:250,downloadThroughput:128*1024,uploadThroughput:64*1024});
 await page.locator('#quality').selectOption('fast');await page.locator('#play').click();await expect.poll(()=>page.locator('video').first().evaluate(v=>v.currentTime),{timeout:25000}).toBeGreaterThan(3);await expect(page.locator('.tile-message.error')).toHaveCount(0);await page.locator('#play').click();await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));await cdp.detach();
});
