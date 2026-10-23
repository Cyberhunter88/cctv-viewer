import {test as base,expect,type Page} from '@playwright/test';
import {defaultView} from '../web/src/view-state';
export {expect};
export type {Page} from '@playwright/test';

// Successful page saves finish asynchronously after the account API reply.
export async function savePreferences(page:Page){
 if(await page.locator('#preferences-save').isEnabled())await page.locator('#preferences-save').click();else await page.locator('#preferences-close').click();
 await expect(page.locator('#preferences-dialog')).toBeHidden();
 await expect(page).toHaveURL(/#recordings$/);
}

// Browser cases share a disposable server and synthetic account. Reset its
// server preferences once per case; reloads and second devices remain real.
export const test=base.extend<{preferenceIsolation:void}>({
 preferenceIsolation:[async({page,context},use,info)=>{
  // Keep the real login limiter enabled without sharing its IP bucket across cases.
  const hash=[...info.testId].reduce((n,c)=>(Math.imul(n,31)+c.charCodeAt(0))>>>0,0);
  await context.setExtraHTTPHeaders({'X-Forwarded-For':`2001:db8:10::${(hash>>>16).toString(16)}:${(hash&65535).toString(16)}`});
  let initialized=false;
  await page.route('**/api/preferences',async route=>{
   if(initialized||route.request().method()!=='GET'){await route.continue();return}
   initialized=true;
   const session=await(await page.request.get('/api/session')).json();
   const cameras=await(await page.request.get('/api/cameras')).json();
   const ids=cameras.map((c:{id:string})=>c.id);
   const response=await page.request.patch('/api/preferences',{headers:{'X-CSRF-Token':session.csrf,Origin:new URL(page.url()).origin},data:{...defaultView(),cameras:ids,order:ids,liveCameras:ids,liveOrder:ids,liveLayout:'auto',hotspot:'',railCollapsed:null,theme:'system'}});
   expect(response.ok(),await response.text()).toBe(true);
   await route.continue();
  });
  await use();
 },{auto:true}],
});

export async function openPage(page:Page,path:string){
 if(new URL(page.url()).hash==='#'+path)return;
 const toggle=page.locator('#mobile-nav-toggle');
 if(await toggle.isVisible()&&await toggle.getAttribute('aria-expanded')!=='true')await toggle.click();
 const link=page.locator('a[data-page="'+path+'"]');
 const group=link.locator('xpath=ancestor::details');
 if(await group.count()&&!(await link.isVisible())){
  if(await group.getAttribute('open')===null||await page.locator('#rail-toggle').getAttribute('aria-expanded')==='false'&&!(await toggle.isVisible()))await group.locator('summary').click();
 }
 await link.click();await expect(page).toHaveURL(new RegExp('#'+path+'$'));await expect(page.locator('.content-page[aria-busy=true]:visible')).toHaveCount(0);
}
export async function openNavigation(page:Page,id:string){
 const routes:Record<string,string>={'preferences-button':'settings/appearance','settings-button':'admin/cameras','search-button':'search'};
 await openPage(page,routes[id]);
}
export async function preferenceControl(page:Page,id:string){
 const appearance=['theme','pref-density'];
 await openPage(page,'settings/'+(appearance.includes(id)?'appearance':'playback'));
 return page.locator('#'+id);
}
