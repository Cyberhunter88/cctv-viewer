import {test,expect} from './fixtures';
// A recording the camera still writes answers 409; the tile waits and starts by itself once the file settles.
test('a recording that is still being written shows a waiting state and then plays',async({page})=>{
 let conflicts=0;
 const growing=new Set<string>();
 // Any recording of the camera may be the one under the cursor, so all of them answer 409 at first.
 await page.route('**/api/recordings/*/file',async route=>{
  const id=route.request().url().split('/api/recordings/')[1].split('/')[0];
  if(growing.has(id)&&conflicts<2){conflicts++;await route.fulfill({status:409,headers:{'Retry-After':'5'},contentType:'application/json',body:'{"error":"Aufnahme wird gerade geschrieben oder wurde geändert"}'});return}
  await route.continue();
 });
 // The recordings are marked as growing before the app loads them, so no early request consumes a conflict.
 await page.goto('/');await expect(page.locator('#login')).toBeVisible();
 const signedIn=await page.request.post('/auth/local',{headers:{Origin:new URL(page.url()).origin},data:{username:'browser',password:'synthetic browser test password'}});
 expect(signedIn.ok(),await signedIn.text()).toBe(true);
 // The day may still be indexing, as the interface waits for "2 Aufnahmen" further below.
 await expect.poll(async()=>{const day=await(await page.request.get('/api/day?date=2026-10-14')).json();for(const r of day.recordings||[])if(r.cameraId==='Testkamera-A')growing.add(r.id);return growing.size},{timeout:20000}).toBeGreaterThan(0);
 await page.reload();
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
