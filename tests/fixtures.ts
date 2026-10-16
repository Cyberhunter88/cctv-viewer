import {test as base,expect} from '@playwright/test';
import {defaultView} from '../web/src/view-state';
export {expect};
export type {Page} from '@playwright/test';

// Browser cases share a disposable server and synthetic account. Reset its
// server preferences once per case; reloads and second devices remain real.
export const test=base.extend<{preferenceIsolation:void}>({
 preferenceIsolation:[async({page},use)=>{
  let initialized=false;
  await page.route('**/api/preferences',async route=>{
   if(initialized||route.request().method()!=='GET'){await route.continue();return}
   initialized=true;
   const session=await(await page.request.get('/api/session')).json();
   const cameras=await(await page.request.get('/api/cameras')).json();
   const ids=cameras.map((c:{id:string})=>c.id);
   const response=await page.request.patch('/api/preferences',{headers:{'X-CSRF-Token':session.csrf,Origin:new URL(page.url()).origin},data:{...defaultView(),cameras:ids,order:ids,hotspot:'',railCollapsed:null,theme:'system'}});
   expect(response.ok(),await response.text()).toBe(true);
   await route.continue();
  });
  await use();
 },{auto:true}],
});
