import {test,expect} from './fixtures';
// A recording the camera still writes answers 409; the tile waits and starts by itself once the file settles.
test('a recording that is still being written shows a waiting state and then plays',async({page})=>{
 let conflicts=0;
 await page.route('**/api/recordings/*/file',async route=>{
  const records=await(await page.request.get('/api/day?date=2026-10-14')).json();
  const first=records.recordings.find((r:{cameraId:string})=>r.cameraId==='Testkamera-A');
  if(first&&route.request().url().includes(first.id)&&conflicts<2){conflicts++;await route.fulfill({status:409,headers:{'Retry-After':'5'},contentType:'application/json',body:'{"error":"Aufnahme wird gerade geschrieben oder wurde geändert"}'});return}
  await route.continue();
 });
 await page.goto('/');
 await page.locator('#login-form input[name=username]').fill('browser');
 await page.locator('#login-form input[name=password]').fill('synthetic browser test password');
 await page.locator('#login-form button').click();
 await expect(page.locator('#app')).toBeVisible();
 await page.locator('#date').fill('2026-10-14');
 await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
 await page.locator('#layout').selectOption('2x2');
 await page.locator('#quality').selectOption('native');
 await page.locator('#play').click();
 const tile=page.locator('.camera-tile').filter({has:page.locator('.tile-title').filter({hasText:'Testkamera-A'})});
 await expect(tile.locator('.tile-message')).toContainText('noch geschrieben',{timeout:15000});
 await expect(tile.locator('.tile-message.error')).toHaveCount(0);
 await expect.poll(()=>tile.locator('video').evaluate(v=>(v as HTMLVideoElement).currentTime),{timeout:30000}).toBeGreaterThan(.5);
 await expect(tile.locator('.tile-message')).toBeHidden();
 expect(conflicts).toBeGreaterThanOrEqual(2);
});
