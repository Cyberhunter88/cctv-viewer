import AxeBuilder from '@axe-core/playwright';
import {test,expect,openPage,type Page} from './fixtures';

async function login(page:Page){await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button[type=submit]').click();await expect(page.locator('#app')).toBeVisible()}

// Automatic WCAG A/AA checks on the main pages. Findings of critical or serious impact fail the test.
const pages=['recordings','settings/appearance','settings/sessions','admin/users','admin/audit','admin/operation'];
for(const theme of ['light','dark'] as const){
 test(`no serious accessibility violations in ${theme} theme`,async({page})=>{
  await page.emulateMedia({colorScheme:theme});
  await login(page);
  for(const path of pages){
   if(path!=='recordings')await openPage(page,path);
   await page.waitForLoadState('networkidle');
   const result=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa']).analyze();
   const serious=result.violations.filter(v=>v.impact==='critical'||v.impact==='serious');
   expect(serious.map(v=>`${path}: ${v.id} (${v.nodes.length})`),`${path} (${theme})`).toEqual([]);
  }
 });
}
