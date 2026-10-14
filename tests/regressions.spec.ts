import {test,expect,type Page} from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';

async function login(page:Page){
 await page.goto('/?date=2026-10-14&cameras=Eingang,Garten');
 await page.locator('#login-form input[name=username]').fill('browser');
 await page.locator('#login-form input[name=password]').fill('synthetic browser test password');
 await page.locator('#login-form button').click();await expect(page.locator('#app')).toBeVisible();
 await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
}
async function addCamera(page:Page,name:string,folder:string,reolink=false){
 await page.locator('#settings-button').click();await page.locator('#new-camera').click();
 await page.locator('#camera-form input[name=name]').fill(name);await page.locator('#camera-form input[name=folder]').fill(folder);
 await page.locator('#camera-format').selectOption(reolink?'reolink':'custom');
 await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
 await page.locator('#camera-form button[type=submit]').click();await expect(page.locator('#camera-list')).toContainText(name);
 await page.locator('#close-dialog').click();await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
}
async function cleanup(page:Page){
 const response=await page.request.get('/api/session');if(!response.ok())return;
 const session=await response.json(),cameras=await (await page.request.get('/api/cameras')).json();
 await expect(page.locator('#rescan')).toBeEnabled({timeout:20000});
 for(const c of cameras.filter((c:{name:string})=>c.name.startsWith('Bugfix '))){
  const response=await page.request.delete('/api/cameras/'+c.id,{headers:{Origin:'http://127.0.0.1:18080','X-CSRF-Token':session.csrf}});expect(response.ok()).toBe(true);
 }
}
test.afterEach(async({page})=>{await cleanup(page)});

test('image pages reset on camera changes and clamp after recordings are removed',async({page})=>{
 await login(page);await addCamera(page,'Bugfix Paar','Reolink/Eingang',true);
 await expect(page.locator('#image-events .image-event-list button')).toHaveCount(2,{timeout:20000});
 await addCamera(page,'Bugfix Bilder','Reolink/Bilder',true);await expect(page.locator('#image-events .image-event-list button')).toHaveCount(100,{timeout:20000});
 await page.getByRole('button',{name:'Weitere Bilder',exact:true}).click();await expect(page.locator('#image-events .image-event-list button')).toHaveCount(7);
 await page.locator('#camera-filters button').filter({hasText:'Bugfix Bilder'}).click();await expect(page.locator('#image-events .image-event-list button')).toHaveCount(2);
 await page.locator('#camera-filters button').filter({hasText:'Bugfix Bilder'}).click();await expect(page.locator('#image-events .image-event-list button')).toHaveCount(100);
 await page.getByRole('button',{name:'Weitere Bilder',exact:true}).click();await expect(page.locator('#image-events .image-event-list button')).toHaveCount(7);
 // Delete only generated, disposable fixture pictures, then exercise a real
 // index refresh while the user is still on page two.
 const settings=await (await page.request.get('/api/admin/settings')).json();const recordings=String(settings.external.recordingsDir);
 expect(recordings).toMatch(/^\/tmp\/cctv-ui-[^/]+\/recordings$/);
 const folder=path.join(recordings,'Reolink/Bilder/2026/10/14');for(const file of fs.readdirSync(folder))fs.unlinkSync(path.join(folder,file));
 await page.locator('#rescan').click();await expect(page.locator('#image-events .image-event-list button')).toHaveCount(2,{timeout:20000});
 await expect(page.getByRole('button',{name:'Weitere Bilder',exact:true})).toHaveCount(0);
});

async function setRange(page:Page,camera:string,from:string,to:string){
 await page.locator('#export-button').click();await page.locator('#export-form select').selectOption({label:camera});
 await page.locator('#export-form input[name=from]').fill(from.replace(/:00$/,''));await page.locator('#export-form input[name=to]').fill(to.replace(/:00$/,''));
}
async function downloadClip(page:Page,name:string){
 await expect(page.getByRole('link',{name:'MP4 herunterladen'})).toBeVisible({timeout:25000});
 const promise=page.waitForEvent('download');await page.getByRole('link',{name:'MP4 herunterladen'}).click();const download=await promise;
 const file=test.info().outputPath(name);await download.saveAs(file);
 return {file,info:JSON.parse(execFileSync('ffprobe',['-v','error','-show_entries','format=duration:stream=codec_name,codec_type,width,height,sample_rate,channels','-of','json',file],{encoding:'utf8'}))};
}
test('cold midnight exports prepare both days; playback and exported clips include audio',async({page})=>{
 await login(page);await addCamera(page,'Bugfix Mitternacht','Mitternacht');
 let indexing=0;page.on('response',r=>{if(r.url().includes('/api/exports')&&r.status()===409)indexing++});
 await setRange(page,'Bugfix Mitternacht','2026-10-14T23:59:58','2026-10-15T00:00:02');await page.locator('#export-preview').click();
 await expect(page.locator('#toast')).toContainText('lückenlos',{timeout:20000});expect(indexing).toBeGreaterThan(0);await expect(page.locator('#export-error')).toBeEmpty();
 await page.locator('#export-form button[type=submit]').click();const midnight=await downloadClip(page,'midnight.mp4');expect(Number(midnight.info.format.duration)).toBeGreaterThan(3.9);expect(midnight.info.streams[0]).toMatchObject({codec_name:'h264',width:160,height:90});
 await page.getByRole('button',{name:'Entfernen',exact:true}).click();await expect(page.locator('#export-jobs')).toBeEmpty();await page.locator('#export-close').click();
 // Direct creation of a range on two entirely unopened days must prepare them
 // as well, without requiring a separate preview first.
 indexing=0;await setRange(page,'Bugfix Mitternacht','2026-10-17T23:59:58','2026-10-18T00:00:02');await page.locator('#export-form button[type=submit]').click();
 await expect(page.locator('#export-status')).toContainText('eingelesen');await expect(page.locator('#export-form input[name=from]')).toBeDisabled();
 await downloadClip(page,'unopened.mp4');expect(indexing).toBeGreaterThan(0);await expect(page.locator('#export-error')).toBeEmpty();await page.getByRole('button',{name:'Entfernen',exact:true}).click();await expect(page.locator('#export-jobs')).toBeEmpty();await page.locator('#export-close').click();
 await addCamera(page,'Bugfix Ton 1','Audio/Eingang');await addCamera(page,'Bugfix Ton 2','Audio/Garten');
 const cameras=await (await page.request.get('/api/cameras')).json();const ids=cameras.filter((c:{name:string})=>c.name.startsWith('Bugfix Ton')).map((c:{id:string})=>c.id);
 await page.goto('/?date=2026-10-14&time=1791959401&cameras='+ids.join(','));await expect(page.locator('video')).toHaveCount(2);await expect.poll(()=>page.locator('video').first().evaluate(v=>v.readyState)).toBeGreaterThanOrEqual(2);
 expect(await page.locator('video').evaluateAll(vs=>vs.every(v=>v.muted))).toBe(true);
 await page.getByRole('button',{name:'Bugfix Ton 1 Ton einschalten',exact:true}).click();await expect(page.getByRole('button',{name:'Bugfix Ton 1 Ton ausschalten',exact:true})).toHaveAttribute('aria-pressed','true');
 await page.getByRole('slider',{name:'Bugfix Ton 1 Lautstärke',exact:true}).press('ArrowLeft');expect(await page.locator('video').first().evaluate(v=>v.volume)).toBe(.95);
 await page.getByRole('button',{name:'Bugfix Ton 2 Ton einschalten',exact:true}).click();expect(await page.locator('video').evaluateAll(vs=>vs.map(v=>v.muted))).toEqual([true,false]);
 await page.locator('#play').click();await expect.poll(()=>page.locator('video').last().evaluate(v=>v.currentTime)).toBeGreaterThan(1.1);await page.locator('#play').click();
 await page.locator('#camera-filters button').filter({hasText:'Bugfix Ton 1'}).click();await page.locator('#quality').selectOption('compatible');await expect.poll(()=>page.locator('video').last().evaluate(v=>v.readyState),{timeout:20000}).toBeGreaterThanOrEqual(2);expect(await page.locator('video').last().evaluate(v=>v.muted)).toBe(false);
 await page.getByRole('button',{name:'Bugfix Ton 2 Ton ausschalten',exact:true}).click();expect(await page.locator('video').evaluateAll(vs=>vs.every(v=>v.muted))).toBe(true);await page.locator('#quality').selectOption('native');await page.locator('#camera-filters button').filter({hasText:'Bugfix Ton 1'}).click();
 await setRange(page,'Bugfix Ton 1','2026-10-14T08:30:01','2026-10-14T08:30:03');await page.locator('#export-form button[type=submit]').click();const audio=await downloadClip(page,'audio.mp4');expect(audio.info.streams.find((s:{codec_type:string})=>s.codec_type==='audio')).toMatchObject({codec_name:'aac',sample_rate:'48000',channels:2});
 const pcm=execFileSync('ffmpeg',['-nostdin','-v','error','-i',audio.file,'-map','0:a:0','-ac','1','-f','s16le','pipe:1']);expect(pcm.some((v:number)=>v!==0)).toBe(true);
 await page.getByRole('button',{name:'Entfernen',exact:true}).click();await expect(page.locator('#export-jobs')).toBeEmpty();await page.locator('#export-close').click();
 // Closing an in-progress preparation cancels its retry loop and re-enables
 // the next dialog; it must not create an export after the dialog was closed.
 let retries=0;await page.route('**/api/exports/preview',r=>{retries++;return r.fulfill({status:409,contentType:'application/json',body:JSON.stringify({indexing:true,error:'Test scan pending'})})});
 await setRange(page,'Bugfix Ton 1','2026-10-14T08:30:01','2026-10-14T08:30:03');await page.locator('#export-preview').click();await expect(page.locator('#export-status')).toContainText('eingelesen');await page.locator('#export-close').click();await page.waitForTimeout(1000);expect(retries).toBe(1);await page.unroute('**/api/exports/preview');
 await page.locator('#export-button').click();await expect(page.locator('#export-preview')).toBeEnabled();await page.locator('#export-close').click();
 await page.setViewportSize({width:390,height:844});await page.getByRole('button',{name:'Bugfix Ton 1 Ton einschalten',exact:true}).click();await expect(page.getByRole('slider',{name:'Bugfix Ton 1 Lautstärke',exact:true})).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:'test-results/audio-mobile.png',fullPage:true});
});
