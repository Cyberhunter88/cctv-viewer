import {defineConfig} from '@playwright/test';
import {existsSync} from 'node:fs';
import {join} from 'node:path';
const testPort=process.env.CTV_TEST_PORT||'18080';
// Google Chrome is the default test browser. Without it (for example on CachyOS) the
// Chromium installed by "npx playwright install chromium" is used instead.
const chromeInstalled=[
 '/opt/google/chrome/chrome',
 '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
 join(process.env.PROGRAMFILES||'C:\\Program Files','Google','Chrome','Application','chrome.exe'),
 join(process.env['PROGRAMFILES(X86)']||'C:\\Program Files (x86)','Google','Chrome','Application','chrome.exe'),
 join(process.env.LOCALAPPDATA||'','Google','Chrome','Application','chrome.exe'),
].some(path=>existsSync(path));
const channel=process.env.CTV_TEST_ENGINE==='webkit'||process.env.CTV_TEST_BROWSER||!chromeInstalled?undefined:'chrome';
export default defineConfig({
 testDir:'./tests',timeout:60000,globalTimeout:12*60*1000,workers:1,retries:0,
 use:{baseURL:`http://127.0.0.1:${testPort}`,headless:true,browserName:process.env.CTV_TEST_ENGINE==='webkit'?'webkit':'chromium',channel,launchOptions:process.env.CTV_TEST_ENGINE!=='webkit'&&process.env.CTV_TEST_BROWSER?{executablePath:process.env.CTV_TEST_BROWSER}:undefined,viewport:{width:1440,height:1000},trace:'retain-on-failure'},
 webServer:{command:`${process.env.CTV_TEST_PYTHON?`"${process.env.CTV_TEST_PYTHON}"`:'python3'} scripts/ui_server.py`,url:`http://127.0.0.1:${testPort}/healthz`,reuseExistingServer:false,timeout:30000},
 reporter:'list',
});
