import {test,expect,type Page} from '@playwright/test';
test.use({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.31'}});
async function setup(page:Page,dual=false){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();
 const session=await(await page.request.get('/api/session')).json(),headers={'X-CSRF-Token':session.csrf,Origin:new URL(page.url()).origin},ids:string[]=[];
 try{
 for(const folder of dual?['AudioDual']:['Audio/Testkamera-A','Audio/Testkamera-B']){
  await expect.poll(async()=>(await(await page.request.get('/api/scan')).json()).running).toBe(false);
  const r=await page.request.post('/api/cameras',{headers,data:{name:folder,folder,format:dual?'automatic':'custom',pattern:'{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4',indexingMode:'full',timezone:'Europe/Berlin',offset:0,viewMode:dual?'dual':'single',supportsZoom:dual,wideMarker:'_wide',zoomMarker:'_zoom',zoomOffset:0}});expect(r.ok(),await r.text()).toBe(true);ids.push((await r.json()).id);
 }
 await page.evaluate(({key,ids})=>localStorage.setItem('cctv-view:'+key,JSON.stringify({cameras:ids,order:ids,layout:'2x1',quality:'native'})),{key:session.preferenceKey,ids});
 await page.reload();await page.locator('#date').fill('2026-10-14');
 await expect.poll(async()=>{const day=await(await page.request.get('/api/day?date=2026-10-14')).json();return day.recordings.filter((r:{cameraId:string})=>ids.includes(r.cameraId)).length},{timeout:20000}).toBe(dual?2:3);
 await page.reload();await page.locator('#date').fill('2026-10-14');
 if(dual){await expect(page.locator('video')).toHaveCount(1);await page.locator('.zoom-toggle').click()}
 await expect(page.locator('video')).toHaveCount(2);
 return async()=>{await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));for(const id of ids)expect((await page.request.delete('/api/cameras/'+id,{headers})).ok()).toBe(true)};
 }catch(error){for(const id of ids)await page.request.delete('/api/cameras/'+id,{headers});throw error}
}
async function audible(page:Page,index:number|undefined){
 await expect.poll(()=>page.locator('video').evaluateAll(v=>v.map(x=>x.muted))).toEqual(index===undefined?[true,true]:index===0?[false,true]:[true,false]);
 await expect(page.locator('.tile-audio[aria-pressed=true]')).toHaveCount(index===undefined?0:1);
}
for(const dual of [false,true])test(`audio selects one ${dual?'perspective':'camera'} and survives playback changes`,async({page})=>{
 test.setTimeout(120000);const cleanup=await setup(page,dual);
 try{
  await audible(page,undefined);
  for(const width of [1440,390]){
   await page.setViewportSize({width,height:1000});
   for(const theme of ['light','dark']){
    await page.locator('html').evaluate((el,theme)=>el.setAttribute('data-theme',theme),theme);
    const bounds=await page.locator('.camera-tile').first().evaluate(el=>{
     const video=el.querySelector('video')!.getBoundingClientRect(),button=el.querySelector('.tile-audio')!.getBoundingClientRect();
     return {pictureBottom:video.bottom,buttonTop:button.top};
    });
    expect(bounds.buttonTop).toBeGreaterThanOrEqual(bounds.pictureBottom);
   }
  }
  await page.setViewportSize({width:1440,height:1000});
  await page.locator('.tile-audio').first().focus();await expect(page.locator('.tile-audio').first()).toBeFocused();
  await page.locator('.tile-audio').first().click();await audible(page,0);
  await page.locator('#play').click();await expect.poll(()=>page.locator('video').first().evaluate(v=>v.currentTime),{timeout:20000}).toBeGreaterThan(.5);
  await page.locator('.tile-audio').last().click();await audible(page,1);
  await page.locator('#play').click();await audible(page,1);await page.locator('#play').click();
  for(const quality of ['balanced','fast','native']){
   await page.locator('#quality').selectOption(quality);await audible(page,1);
   await expect.poll(()=>page.locator('video').last().evaluate(v=>!v.paused&&v.readyState>=2),{timeout:25000}).toBe(true);
  }
  // A media backend can clear muted asynchronously during a rate change.
  // Keep the explicitly selected camera audible and restore all other mutes.
  await page.locator('video').first().evaluate(v=>{v.muted=false});await audible(page,1);
  await page.locator('#speed').selectOption('2');await audible(page,1);await page.locator('#speed').selectOption('1');await audible(page,1);
  if(!dual){
   await page.locator('.tile-audio').first().click();await audible(page,0);
   const previous=await page.locator('video').first().getAttribute('src');await page.locator('#skip-forward').click();await expect.poll(()=>page.locator('video').first().getAttribute('src')).not.toBe(previous);await audible(page,0);await expect.poll(()=>page.locator('video').first().evaluate(v=>!v.paused&&v.readyState>=2),{timeout:20000}).toBe(true);
  }
  await page.locator('.tile-audio[aria-pressed=true]').click();await audible(page,undefined);
  await page.locator('.tile-audio').first().click();await page.locator('#camera-filters .selected').first().click();
  await expect(page.locator('.tile-audio[aria-pressed=true]')).toHaveCount(0);
  await expect.poll(()=>page.locator('video').evaluateAll(v=>v.every(x=>x.muted))).toBe(true);
 }finally{await cleanup()}
});

test('blocked audio is muted with a retry hint and can be enabled again',async({page})=>{
 const cleanup=await setup(page);
 try{
  await page.evaluate(()=>{const original=HTMLMediaElement.prototype.play;let reject=true;HTMLMediaElement.prototype.play=function(){if(!this.muted&&reject){reject=false;return Promise.reject(new DOMException('blocked','NotAllowedError'))}return original.call(this)}});
  await page.locator('.tile-audio').first().click();await page.locator('#play').click();
  await expect(page.locator('#toast')).toContainText('Bitte Ton erneut einschalten');await audible(page,undefined);
  await page.locator('.tile-audio').first().click();await audible(page,0);
  await expect.poll(()=>page.locator('video').first().evaluate(v=>!v.paused&&v.currentTime>0),{timeout:20000}).toBe(true);
  await page.setViewportSize({width:390,height:844});await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 }finally{await cleanup()}
});


test('enabling audio on a recording without a track keeps video playing',async({page})=>{
 const cleanup=await setup(page);
 try{
  while(await page.locator('#camera-filters .selected').count())await page.locator('#camera-filters .selected').first().click();
  await page.locator('#camera-filters').getByRole('button',{name:'Testkamera-A',exact:true}).click();
  await expect(page.locator('video')).toHaveCount(1);await page.locator('.tile-audio').click();await page.locator('#play').click();
  for(const quality of ['native','balanced','fast']){
   await page.locator('#quality').selectOption(quality);
   await expect.poll(()=>page.locator('video').evaluate(v=>!v.paused&&v.currentTime>.2),{timeout:25000}).toBe(true);
   await expect(page.locator('.tile-message.error')).toHaveCount(0);
   await expect(page.locator('.tile-audio')).toHaveAttribute('aria-pressed','true');
  }
 }finally{await cleanup()}
});

test('compressed playback starts at the requested position when native HLS is advertised',async({page})=>{
 // Recent Chrome versions advertise native HLS. Exercise that capability even
 // when the local Chromium build normally chooses progressive MP4.
 await page.addInitScript(()=>{
  const original=HTMLMediaElement.prototype.canPlayType;
  HTMLMediaElement.prototype.canPlayType=function(type:string){return type==='application/vnd.apple.mpegurl'?'probably':original.call(this,type)};
 });
 const expanded=new Set<string>(),requests=new Map<string,number>();
 await page.route('**/api/streams/*/index.m3u8',async route=>{
  const response=await route.fetch(),body=await response.text(),url=route.request().url();
  if(!response.ok())return route.fulfill({response});
  const count=(requests.get(url)||0)+1;requests.set(url,count);
  const header=body.slice(0,body.indexOf('#EXTINF:'));
  const segments=[...body.matchAll(/#EXTINF:[^\n]+\n[^\n]+/g)].map(m=>m[0]);
  // Match a live EVENT manifest while FFmpeg is still producing segments:
  // first expose a short window, then expand it to the full recording.
  const visible=count<=2?segments.slice(0,2):segments;
  if(count>2&&visible.length>2)expanded.add(url);
  await route.fulfill({response,body:header+visible.join('\n')+'\n'});
 });
 const cleanup=await setup(page);
 try{
  await page.locator('.tile-audio').last().click();await page.locator('#play').click();
  await expect.poll(()=>page.locator('video').last().evaluate(v=>!v.paused&&v.currentTime>.2)).toBe(true);
  const streams:{transport:string;profile:string}[]=[];
  page.on('request',request=>{if(request.method()==='POST'&&new URL(request.url()).pathname==='/api/streams')streams.push(request.postDataJSON())});
  for(const quality of ['balanced','fast']){
   const previousExpanded=expanded.size;
   await page.locator('#quality').selectOption(quality);await audible(page,1);
   await expect.poll(()=>expanded.size,{timeout:15000}).toBeGreaterThanOrEqual(previousExpanded+2);
   await expect.poll(()=>page.locator('video').last().evaluate(v=>!v.paused&&v.readyState>=2&&v.currentTime>1.5),{timeout:25000}).toBe(true);
   await expect(page.locator('.tile-message.error')).toHaveCount(0);
   expect(streams.filter(s=>s.profile===quality).length).toBeGreaterThanOrEqual(2);
   expect(streams.every(s=>s.transport==='hls')).toBe(true);
   // The shared clock and both recordings must remain near the start, rather
   // than jumping to the live edge of a playlist that is still growing.
   const seconds=await page.locator('#clock').textContent();expect(Number(seconds!.split(':')[2])).toBeLessThan(20);
   await expect.poll(()=>page.locator('video').evaluateAll(videos=>videos.every(v=>v.currentTime<20))).toBe(true);
  }
 }finally{await cleanup()}
});
