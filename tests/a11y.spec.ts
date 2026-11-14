import AxeBuilder from '@axe-core/playwright';
import {test,expect,openPage,type Page} from './fixtures';

async function login(page:Page){await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button[type=submit]').click();await expect(page.locator('#app')).toBeVisible()}

// Automatic WCAG 2.2 A/AA checks on the main pages. Findings of critical or serious impact fail the test.
const tags=['wcag2a','wcag2aa','wcag21a','wcag21aa','wcag22aa'];
const pages=['recordings','live','search','settings/appearance','settings/playback','settings/account','settings/sessions','admin/cameras','admin/users','admin/audit','admin/oidc','admin/operation'];
for(const theme of ['light','dark'] as const){
 test(`no serious accessibility violations in ${theme} theme`,async({page})=>{
  await page.emulateMedia({colorScheme:theme});
  await page.goto('/');await expect(page.locator('#login')).toBeVisible();
  const loginResult=await new AxeBuilder({page}).withTags(tags).analyze();
  expect(loginResult.violations.filter(v=>v.impact==='critical'||v.impact==='serious').map(v=>`login: ${v.id} (${v.nodes.length})`),`login (${theme})`).toEqual([]);
  await login(page);
  for(const path of pages){
   if(path!=='recordings')await openPage(page,path);
   // Live streams keep connections open; the page is checked once it has settled or after five seconds.
   await page.waitForLoadState('networkidle',{timeout:5000}).catch(()=>{});
   const result=await new AxeBuilder({page}).withTags(tags).analyze();
   const serious=result.violations.filter(v=>v.impact==='critical'||v.impact==='serious');
   expect(serious.map(v=>`${path}: ${v.id} (${v.nodes.length})`),`${path} (${theme})`).toEqual([]);
  }
 });
}

// The redesign must hold on phones and in the compact density as well.
const phonePages=['recordings','live','search','settings/appearance','admin/cameras','admin/users'];
for(const theme of ['light','dark'] as const){
 test(`no serious accessibility violations on a phone in compact density (${theme})`,async({page})=>{
  await page.setViewportSize({width:390,height:844});await page.emulateMedia({colorScheme:theme});
  await page.goto('/');await expect(page.locator('#login')).toBeVisible();
  const loginResult=await new AxeBuilder({page}).withTags(tags).analyze();
  expect(loginResult.violations.filter(v=>v.impact==='critical'||v.impact==='serious').map(v=>`login: ${v.id} (${v.nodes.length})`),`phone login (${theme})`).toEqual([]);
  await login(page);
  await page.evaluate(()=>document.documentElement.dataset.density='compact');
  for(const path of phonePages){
   if(path!=='recordings')await openPage(page,path);
   await page.waitForLoadState('networkidle',{timeout:5000}).catch(()=>{});
   await expect(page.locator('html')).toHaveAttribute('data-density','compact');
   const result=await new AxeBuilder({page}).withTags(tags).analyze();
   const serious=result.violations.filter(v=>v.impact==='critical'||v.impact==='serious');
   expect(serious.map(v=>`${path}: ${v.id} (${v.nodes.length})`),`phone ${path} (${theme})`).toEqual([]);
  }
 });
}
