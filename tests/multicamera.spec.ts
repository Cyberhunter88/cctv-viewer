import {test,expect,type Page} from '@playwright/test';
test.use({extraHTTPHeaders:{'X-Forwarded-For':'192.0.2.20'}});
async function login(page:Page){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
}
async function six(page:Page,kind:'single'|'dual'|'mixed'){
 const session=await(await page.request.get('/api/session')).json();const headers={'X-CSRF-Token':session.csrf,Origin:new URL(page.url()).origin};const ids:string[]=[];
 try{
  for(let i=0;i<6;i++){
   const dual=kind==='dual'||kind==='mixed'&&i%2===0;
   const response=await page.request.post('/api/cameras',{headers,data:{name:i===5?'Testkamera mit einem sehr langen Beispielnamen':`Testkamera ${i+1}`,folder:`Six${dual?'Dual':'Single'}${i+1}`,format:'automatic',indexingMode:'full',timezone:'Europe/Berlin',offset:0,viewMode:dual?'dual':'single',supportsZoom:dual,wideMarker:'_wide',zoomMarker:'_zoom',zoomOffset:0}});
   expect(response.ok(),await response.text()).toBe(true);ids.push((await response.json()).id);
  }
  await page.evaluate(({key,ids})=>localStorage.setItem('cctv-view:'+key,JSON.stringify({cameras:ids,order:ids,layout:'auto'})),{key:session.preferenceKey,ids});
  await page.reload();await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText(`${kind==='dual'?14:kind==='mixed'?11:8} Aufnahmen`,{timeout:20000});await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
  return {ids,headers};
 }catch(error){for(const id of ids)await page.request.delete('/api/cameras/'+id,{headers});throw error}
}
async function cleanup(page:Page,setup:Awaited<ReturnType<typeof six>>){
 await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));
 await expect.poll(async()=> (await(await page.request.get('/api/scan')).json()).running,{timeout:20000}).toBe(false);
 for(const id of setup.ids)expect((await page.request.delete('/api/cameras/'+id,{headers:setup.headers})).ok()).toBe(true);
}
for(const kind of ['single','dual','mixed'] as const)test(`six ${kind} cameras remain readable across sizes and layouts`,async({page},info)=>{
 test.setTimeout(120000);const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));await login(page);const setup=await six(page,kind);
 try{
  for(const width of [390,768,980,1440,1920]){
   await page.setViewportSize({width,height:1000});
   for(const layout of ['auto','3x2','custom','hotspot']){
    await page.locator('#layout').selectOption(layout);
    await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    await expect.poll(()=>page.locator('.camera-tile').evaluateAll(tiles=>tiles.every(tile=>{const r=tile.getBoundingClientRect(),g=tile.parentElement!.getBoundingClientRect();return r.width>=250&&r.left>=g.left-1&&r.right<=g.right+1}))).toBe(true);
    const visited=new Set<string>();
    await page.locator('#page-prev').evaluate((button:HTMLButtonElement)=>{while(!button.disabled){button.click()}});
    do{for(const name of await page.locator('.camera-group .tile-title').allTextContents())visited.add(name.split(' · ')[0]);if(await page.locator('#page-next').isDisabled())break;await page.locator('#page-next').click()}while(true);
    expect(visited.size).toBe(6);
    if(width===390)expect(await page.locator('.camera-group').count()).toBeLessThanOrEqual(2);
    if(width===390&&kind!=='single'){
     for(const group of await page.locator('.camera-group.dual').all()){
      await expect(group.locator('.camera-tile')).toHaveCount(1);await group.locator('.zoom-toggle').click();
      const boxes=await group.locator('.camera-tile').evaluateAll(tiles=>tiles.map(t=>t.getBoundingClientRect().toJSON()));expect(boxes[1].top).toBeGreaterThanOrEqual(boxes[0].bottom);
      await group.locator('.zoom-toggle').click();
     }
    }
   }
   await page.locator('#layout').selectOption('auto');await page.screenshot({path:info.outputPath(`${kind}-${width}.png`),fullPage:true});
  }
  expect(errors).toEqual([]);
 }finally{await cleanup(page,setup)}
});
for(const kind of ['single','dual'] as const)test(`six ${kind} native videos play without transcode and preserve source aspect`,async({page})=>{
 await login(page);const setup=await six(page,kind);const transcodes:string[]=[];page.on('request',r=>{if(r.method()==='POST'&&r.url().endsWith('/api/streams'))transcodes.push(r.url())});
 try{
  await page.setViewportSize({width:1920,height:1080});await expect(page.locator('video')).toHaveCount(6);
  if(kind==='dual'){for(let i=0;i<6;i++)await page.locator('.zoom-toggle').nth(i).click();await expect(page.locator('video')).toHaveCount(12)}
  await expect(page.locator('#quality')).toHaveValue('native');await expect(page.locator('#aspect')).toHaveValue('auto');
  await page.locator('#play').click();await expect.poll(()=>page.locator('video').evaluateAll(v=>v.every(x=>x.currentTime>.5&&x.videoWidth>0)),{timeout:20000}).toBe(true);
  const values=await page.locator('video').evaluateAll(v=>v.map(x=>({time:x.currentTime,ratio:x.videoWidth/x.videoHeight,box:x.parentElement!.getBoundingClientRect().width/x.parentElement!.getBoundingClientRect().height,fit:getComputedStyle(x).objectFit})));
  expect(Math.max(...values.map(x=>x.time))-Math.min(...values.map(x=>x.time))).toBeLessThan(1);
  for(const v of values){expect(Math.abs(v.ratio-v.box)).toBeLessThan(.05);expect(v.fit).toBe('contain')}
  expect(transcodes).toEqual([]);await page.locator('#play').click();
  await page.locator('#layout').selectOption('1x1');await expect(page.locator('video')).toHaveCount(kind==='dual'?2:1);await page.locator('#page-next').click();await expect(page.locator('#camera-range')).toContainText('2–2');expect(transcodes).toEqual([]);
 }finally{await cleanup(page,setup)}
});
test('light, dark and system modes persist and redraw canvas and dialogs',async({page},info)=>{
 await page.emulateMedia({colorScheme:'light'});await page.goto('/');await expect(page.locator('html')).toHaveAttribute('data-theme','light');await expect(page.locator('#theme')).toHaveValue('system');
 await page.locator('#login-theme').selectOption('dark');await page.reload();await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
 await login(page);
 for(const theme of ['light','dark']){
  await page.locator('#preferences-button').click();await page.locator('#theme').selectOption(theme);await expect(page.locator('html')).toHaveAttribute('data-theme',theme);
  const contrasts=await page.evaluate(()=>{
   const parse=(value:string)=>value.match(/[\d.]+/g)!.map(Number);
   const luminance=(rgb:number[])=>rgb.slice(0,3).map(x=>{const s=x/255;return s<=.04045?s/12.92:((s+.055)/1.055)**2.4}).reduce((n,x,i)=>n+x*[.2126,.7152,.0722][i],0);
   return ['.theme-control select','.page-heading h1','.camera-selection strong','#search-button','.account-menu summary','.scan-details'].map(selector=>{
    const node=document.querySelector(selector)!;const color=parse(getComputedStyle(node).color);let parent:Element|null=node,background:number[]=[];
    while(parent){background=parse(getComputedStyle(parent).backgroundColor);if(background.length===3||background[3]===1)break;parent=parent.parentElement}
    const a=luminance(color),b=luminance(background);return {selector,ratio:(Math.max(a,b)+.05)/(Math.min(a,b)+.05)};
   });
  });
  for(const entry of contrasts)expect(entry.ratio,`${theme}: ${entry.selector}`).toBeGreaterThanOrEqual(4.5);

  await page.locator('#preferences-close').click();await page.locator('#search-button').click();await page.screenshot({path:info.outputPath(`${theme}-search.png`),fullPage:true});await page.locator('#search-close').click();
  await page.locator('#settings-button').click();await page.screenshot({path:info.outputPath(`${theme}-settings.png`),fullPage:true});await page.locator('#close-dialog').click();
  const background=await page.locator('#overview').evaluate((c:HTMLCanvasElement)=>Array.from(c.getContext('2d')!.getImageData(10,5,1,1).data));
  const expected=theme==='light'?[220,229,234]:[37,53,64];
  for(let i=0;i<3;i++)expect(Math.abs(background[i]-expected[i])).toBeLessThanOrEqual(2);
 }
 await page.locator('#preferences-button').click();await page.locator('#theme').selectOption('system');await page.emulateMedia({colorScheme:'dark'});await expect(page.locator('html')).toHaveAttribute('data-theme','dark');await page.emulateMedia({colorScheme:'light'});await expect(page.locator('html')).toHaveAttribute('data-theme','light');await page.reload();await expect(page.locator('#theme')).toHaveValue('system');
});
