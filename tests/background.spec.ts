import {test,expect,openPage,type Page} from './fixtures';
async function login(page:Page){
 await page.setExtraHTTPHeaders({'X-Forwarded-For':'192.0.2.199'});
 await page.goto('/');
 await page.locator('#login-form input[name=username]').fill('browser');
 await page.locator('#login-form input[name=password]').fill('synthetic browser test password');
 await page.locator('#login-form button').click();
 await expect(page.locator('#app')).toBeVisible();
 await page.locator('#date').fill('2026-10-14');
 await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
 await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
}
import type {Route} from '@playwright/test';

async function trackSearchAborts(page:Page){
 await page.addInitScript(()=>{
  const signals:{aborted:boolean}[]=[];
  (window as unknown as {searchSignals:typeof signals}).searchSignals=signals;
  const original=window.fetch;
  window.fetch=(input,init)=>{
   if(String(input).startsWith('/api/search?')){
    const entry={aborted:false};signals.push(entry);
    init?.signal?.addEventListener('abort',()=>{entry.aborted=true});
   }
   return original(input,init);
  };
 });
}
const hit=(id:string)=>({id,cameraId:'Testkamera-A',cameraName:'Testkamera-A',filename:id+'.mp4',start:1791959400,duration:30});
const reply=(results:ReturnType<typeof hit>[],total=results.length,hasMore=false)=>({status:200,contentType:'application/json',body:JSON.stringify({results,total,hasMore,nextCursor:hasMore?'next-page':'',limit:100,offset:0})});

test('a new search cancels its predecessor and keeps only the new results',async({page})=>{
 await trackSearchAborts(page);
 let first:Route|undefined;
 await page.route('**/api/search?*',async route=>{
  if(new URL(route.request().url()).searchParams.get('q')==='old'){first=route;return}
  await route.fulfill(reply([hit('new')]));
 });
 await login(page);await openPage(page,'search');
 await page.locator('[name=q]').fill('old');await page.locator('#search-submit').click();
 await expect.poll(()=>!!first).toBe(true);
 await page.locator('[name=q]').fill('new');await page.locator('#search-submit').click();
 await expect(page.locator('.search-result')).toHaveCount(1);
 await expect(page.locator('.search-result')).toContainText('new.mp4');
 await expect.poll(()=>page.evaluate(()=>(window as unknown as {searchSignals:{aborted:boolean}[]}).searchSignals[0].aborted)).toBe(true);
 await first!.fulfill(reply([hit('old')])).catch(()=>{});
 await expect(page.locator('.search-result')).toContainText('new.mp4');
 await expect(page.locator('#search-error')).toBeEmpty();
});

test('leaving search cancels a later page and permits retry without losing results',async({page})=>{
 await trackSearchAborts(page);
 let held:Route|undefined;
 const cursors:string[]=[];
 let calls=0;
 await page.route('**/api/search?*',async route=>{
  calls++;cursors.push(new URL(route.request().url()).searchParams.get('cursor')||'');
  if(calls===1){await route.fulfill(reply([hit('first')],2,true));return}
  if(calls===2){held=route;return}
  await route.fulfill(reply([hit('second')],0));
 });
 await login(page);await openPage(page,'search');await page.locator('#search-submit').click();
 await expect(page.locator('.search-result')).toHaveCount(1);
 await page.locator('#search-more').click();await expect.poll(()=>!!held).toBe(true);
 await page.locator('#search-close').click();
 await expect.poll(()=>page.evaluate(()=>(window as unknown as {searchSignals:{aborted:boolean}[]}).searchSignals[1].aborted)).toBe(true);
 await held!.fulfill(reply([hit('stale')],0)).catch(()=>{});
 await openPage(page,'search');await expect(page.locator('.search-result')).toHaveCount(1);
 await expect(page.locator('#search-more')).toBeEnabled();await page.locator('#search-more').click();
 await expect(page.locator('.search-result')).toHaveCount(2);
 await expect(page.locator('#search-count')).toHaveText('2 von 2 Treffern angezeigt');
 expect(cursors).toEqual(['','next-page','next-page']);
 await expect(page.locator('#search-error')).toBeEmpty();
});

test('an interrupted initial search resumes when its page is reopened',async({page})=>{
 let held:Route|undefined,calls=0;
 await page.route('**/api/search?*',async route=>{
  if(++calls===1){held=route;return}
  await route.fulfill(reply([hit('resumed')]));
 });
 await login(page);await openPage(page,'search');await page.locator('#search-submit').click();
 await expect.poll(()=>!!held).toBe(true);await page.locator('#search-close').click();
 await held!.fulfill(reply([hit('stale')])).catch(()=>{});
 await openPage(page,'search');await expect(page.locator('.search-result')).toContainText('resumed.mp4');
 await expect(page.locator('#search-error')).toBeEmpty();
});

test('status polling suspends while hidden and immediately resumes when visible',async({page})=>{
 await login(page);
 await page.evaluate(()=>{
  Object.defineProperty(document,'hidden',{configurable:true,get:()=>true});
  document.dispatchEvent(new Event('visibilitychange'));
 });
 let calls=0;
 await page.route('**/api/scan',async route=>{calls++;await route.continue()});
 await page.waitForTimeout(4500);expect(calls).toBe(0);
 await page.evaluate(()=>{
  Object.defineProperty(document,'hidden',{configurable:true,get:()=>false});
  document.dispatchEvent(new Event('visibilitychange'));
 });
 await expect.poll(()=>calls,{timeout:2000}).toBe(1);
 await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));
 await page.waitForTimeout(200);expect(calls).toBe(1);
});

test('paused playback and other pages keep no continuous animation loop',async({page})=>{
 await page.addInitScript(()=>{
  const metrics={frames:0};(window as unknown as {frameMetrics:typeof metrics}).frameMetrics=metrics;
  const request=window.requestAnimationFrame;
  window.requestAnimationFrame=callback=>request.call(window,time=>{metrics.frames++;callback(time)});
 });
 await login(page);await page.waitForTimeout(300);
 const count=()=>page.evaluate(()=>(window as unknown as {frameMetrics:{frames:number}}).frameMetrics.frames);
 const paused=await count();await page.waitForTimeout(300);expect(await count()-paused).toBeLessThan(3);
 await page.locator('#play').click();await expect.poll(count).toBeGreaterThan(paused+5);
 await openPage(page,'search');await page.waitForTimeout(300);
 const away=await count();await page.waitForTimeout(300);expect(await count()-away).toBeLessThan(3);
 await openPage(page,'recordings');await expect(page.locator('#play')).toHaveAttribute('aria-label','Abspielen');
});

test('a scan refresh cannot supersede an explicit date change',async({page})=>{
 await login(page);
 const day=await(await page.request.get('/api/day?date=2026-10-14')).json();
 let held:Route|undefined,dayRequests=0,scanRequests=0;
 await page.route('**/api/day?date=2026-10-15',route=>{dayRequests++;held=route});
 await page.route('**/api/scan',route=>{scanRequests++;return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({...day.scan,running:false,completion:999999})})});
 await page.locator('#date').fill('2026-10-15');await expect.poll(()=>!!held).toBe(true);
 await page.evaluate(()=>{
  Object.defineProperty(document,'hidden',{configurable:true,get:()=>true});document.dispatchEvent(new Event('visibilitychange'));
  Object.defineProperty(document,'hidden',{configurable:true,get:()=>false});document.dispatchEvent(new Event('visibilitychange'));
 });
 await expect.poll(()=>scanRequests).toBeGreaterThan(0);await page.waitForTimeout(150);
 expect(dayRequests).toBe(1);
 await held!.fulfill({status:200,contentType:'application/json',body:JSON.stringify({...day,from:day.from+86400,to:day.to+86400,recordings:day.recordings.map((record:{start:number})=>({...record,start:record.start+86400}))})});
 await expect(page.locator('#clock')).toHaveText('08:30:00');
 await expect(page.locator('.camera-tile').first()).not.toContainText('Keine Aufnahme zu diesem Zeitpunkt');
});
