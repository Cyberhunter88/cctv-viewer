import {test,expect} from './fixtures';
// Guards the tile grid (video and footer span the tile) and the camera chips (one line each).
for(const [name,width,height] of [['desktop',1440,900],['phone',390,844]] as const){
 test(`playing tiles fill their width and camera chips stay on one line on ${name}`,async({page})=>{
  await page.setViewportSize({width,height});
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
  await expect.poll(()=>tile.locator('video').evaluate(v=>(v as HTMLVideoElement).currentTime),{timeout:30000}).toBeGreaterThan(.5);
  const box=await tile.boundingBox(),video=await tile.locator('video').boundingBox(),footer=await tile.locator('.tile-footer').boundingBox();
  expect(box&&video&&footer).toBeTruthy();
  expect(video!.width,'video spans the tile').toBeGreaterThan(box!.width*.95);
  expect(footer!.width,'footer spans the tile').toBeGreaterThan(box!.width*.95);
  for(const chip of await page.locator('.camera-filters .filter').all()){
   const chipBox=await chip.boundingBox();
   expect(chipBox!.height,'chip text must not wrap').toBeLessThan(52);
  }
 });
}
