import {defineConfig} from '@playwright/test';
const testPort=process.env.CTV_TEST_PORT||'18080';
export default defineConfig({
 testDir:'./tests',timeout:60000,globalTimeout:8*60*1000,workers:1,retries:0,
 use:{baseURL:`http://127.0.0.1:${testPort}`,headless:true,browserName:process.env.CTV_TEST_ENGINE==='webkit'?'webkit':'chromium',channel:process.env.CTV_TEST_ENGINE==='webkit'||process.env.CTV_TEST_BROWSER?undefined:'chrome',launchOptions:process.env.CTV_TEST_ENGINE!=='webkit'&&process.env.CTV_TEST_BROWSER?{executablePath:process.env.CTV_TEST_BROWSER}:undefined,viewport:{width:1440,height:1000},trace:'retain-on-failure'},
 webServer:{command:`${process.env.CTV_TEST_PYTHON?`"${process.env.CTV_TEST_PYTHON}"`:'python3'} scripts/ui_server.py`,url:`http://127.0.0.1:${testPort}/healthz`,reuseExistingServer:false,timeout:30000},
 reporter:'list',
});
