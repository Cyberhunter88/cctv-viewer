import {test,expect,type Page} from './fixtures';

// Keyboard and focus behaviour required by WCAG 2.2 AA that axe cannot check automatically.
async function login(page:Page){
 await page.goto('/');await page.locator('#login-form input[name=username]').fill('browser');await page.locator('#login-form input[name=password]').fill('synthetic browser test password');await page.locator('#login-form button[type=submit]').click();await expect(page.locator('#app')).toBeVisible();
 await page.locator('#date').fill('2026-10-14');await expect(page.locator('#recording-count')).toContainText('2 Aufnahmen',{timeout:20000});
}

test('skip link moves focus past the navigation to the page heading',async({page})=>{
 await login(page);
 // The skip link is the first focusable element: Shift+Tab from the first navigation control reaches it.
 await page.locator('#rail-toggle').focus();await page.keyboard.press('Shift+Tab');await expect(page.locator('#skip-link')).toBeFocused();await expect(page.locator('#skip-link')).toBeInViewport();
 await page.keyboard.press('Enter');await expect(page.locator('#recordings h1')).toBeFocused();
});

test('accent buttons show an outer focus ring',async({page})=>{
 await login(page);
 await page.locator('#skip-back').focus();await page.keyboard.press('Tab');await expect(page.locator('#play')).toBeFocused();
 const ring=await page.locator('#play').evaluate(e=>{const s=getComputedStyle(e);return {style:s.outlineStyle,offset:parseFloat(s.outlineOffset),width:parseFloat(s.outlineWidth)}});
 expect(ring.style).toBe('solid');expect(ring.offset).toBeGreaterThan(0);expect(ring.width).toBeGreaterThanOrEqual(2);
});

test('timeline sliders expose the time and support Home and End',async({page})=>{
 await login(page);
 const timeline=page.locator('#timeline');
 await expect(timeline).toHaveAttribute('role','slider');
 await timeline.focus();await page.keyboard.press('End');
 await expect(page.locator('#clock')).toHaveText('23:59:59');await expect(timeline).toHaveAttribute('aria-valuetext','23:59:59 Uhr');
 await page.keyboard.press('Home');await expect(page.locator('#clock')).toHaveText('00:00:00');await expect(timeline).toHaveAttribute('aria-valuetext','00:00:00 Uhr');
 await page.keyboard.press('PageUp');await expect(page.locator('#clock')).toHaveText('00:01:00');
 await expect(page.locator('#overview')).toHaveAttribute('aria-valuetext','00:01:00 Uhr');
});

test('expanded tile without fullscreen locks the background and Escape restores focus',async({page})=>{
 await page.addInitScript(()=>{Object.defineProperty(Document.prototype,'fullscreenEnabled',{configurable:true,get:()=>false})});
 await login(page);
 const expand=page.locator('#video-grid .tile-expand').first();
 await expand.focus();await page.keyboard.press('Enter');
 await expect(page.locator('#video-grid .camera-tile.expanded')).toHaveCount(1);
 await expect(page.locator('.rail')).toHaveJSProperty('inert',true);
 await page.keyboard.press('Escape');
 await expect(page.locator('#video-grid .camera-tile.expanded')).toHaveCount(0);
 await expect(page.locator('.rail')).toHaveJSProperty('inert',false);
 await expect(page).toHaveURL(/#recordings$/);await expect(expand).toBeFocused();
});

test('camera order can be changed without dragging',async({page})=>{
 await login(page);
 const chips=page.locator('#camera-filters .filter');const first=await chips.first().getAttribute('id');
 await page.locator('#reorder-cameras').click();await expect(page.locator('#reorder-cameras')).toHaveAttribute('aria-pressed','true');
 await page.locator('#'+first+'-move-right').click();
 await expect(chips.nth(1)).toHaveAttribute('id',first!);
 await page.locator('#'+first+'-move-left').click();
 await expect(chips.first()).toHaveAttribute('id',first!);
});

test('recordings reflow at 320 CSS pixels without horizontal scrolling',async({page})=>{
 await page.setViewportSize({width:320,height:720});
 await login(page);
 await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
